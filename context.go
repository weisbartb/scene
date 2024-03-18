package scene

import (
	ogContext "context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weisbartb/stack"
)

// context represents the state of a given context with a link to its parent factory
type context struct {
	// For now, this is a linked context to allow other context injectors to play nice with it
	ogContext.Context
	// The factory pointer
	factory *Factory
	// When the request needs to complete by, stored as int64 for atomic.LoadInt64
	completeBy int64 // unix-nano
	// The ID of the context
	id string
	// A complete channel used for ctx interface requirements
	complete chan struct{}
	// Context value map (values are not thread-safe) that stores various metadata about the context
	contextValues map[any]any
	// is this context marked as completed?
	isComplete bool
	// The error that is stored when Complete is invoked
	err error
	// List of on complete functions
	onComplete []CompleteFunc
	// How long this has from the start to complete
	deadline time.Duration
	// When the context was started
	startedAt time.Time
	// What file/line started the context
	startedBy   string
	mu          *sync.RWMutex
	activeTimer *time.Timer
}

// refreshDeadline updates the context deadline when called
func (c *context) refreshDeadline() {
	var setupMonitor bool
	c.mu.Lock()
	if c.activeTimer != nil {
		c.activeTimer.Reset(time.Until(time.Unix(0, c.completeBy)))
	} else {
		c.activeTimer = time.NewTimer(time.Until(time.Unix(0, c.completeBy)))
		setupMonitor = true
	}
	c.mu.Unlock()
	if !setupMonitor {
		return
	}
	// Force the context to complete at a specific time, this will close the context and signal everything to stop working
	// The logging instance is NOT destroyed
	select {
	case <-c.activeTimer.C:
		c.CompleteWithError(stack.Trace(ErrTimeout, stack.ErrorKVP{
			Key:   "startedBy",
			Value: c.startedBy,
		}, stack.ErrorKVP{
			Key:   "startedAt",
			Value: c.startedAt,
		}, stack.ErrorKVP{
			Key:   "deadline (ms)",
			Value: c.deadline.Milliseconds(),
		}, stack.ErrorKVP{
			Key:   "factoryIdentifier",
			Value: c.factory.factoryIdentifier,
		}))
		return
	case <-c.complete:
		return
	}
}

func (c *context) Extend(runUntil time.Time) {
	c.mu.Lock()
	deadline := runUntil.Sub(time.Now())
	c.deadline = deadline
	c.completeBy = time.Now().Add(deadline).UnixNano()
	go c.refreshDeadline()
	c.mu.Unlock()
}

func (c *context) Attach(ctx ogContext.Context) {
	if c2, ok := ctx.(*context); ok {
		c.Context = c2.Context
		return
	}
	c.Context = ctx
}

func (c *context) Defer(fn CompleteFunc) {
	c.mu.Lock()
	c.onComplete = append(c.onComplete, fn)
	c.mu.Unlock()
}

// Store puts a new value inside the context, the value does not need to be thread-safe (but can be)
func (c *context) Store(key, value any) {
	c.mu.Lock()
	if c.isComplete {
		c.mu.Unlock()
		return
	}
	c.contextValues[key] = value
	c.mu.Unlock()
}

func (c *context) GetBaseCtx() ogContext.Context {
	return c.Context
}

// Spawn a new context that needs to complete by a given time.
// A zero-value time will produce an infinitely running child context.
func (c *context) Spawn(completeBy time.Time) (Context, error) {
	c.mu.Lock()
	isComplete := c.isComplete
	c.mu.Unlock()
	if isComplete {
		return nil, ErrShutdownInProgress
	}
	var ttl time.Duration
	if !completeBy.IsZero() {
		ttl = time.Until(completeBy)
	}
	newCtx := c.factory.newCtx(c.Context, ttl)
	defer func() {
		if r := recover(); r != nil {
			// Complete the context since this can cause issues with a factory being stuck
			newCtx.Complete()
		}
	}()
	for _, v := range c.factory.injectors {
		if v != nil {
			v.OnSpawnedContext(newCtx, c)
		}
	}
	return newCtx, nil
}

// Deadline returns a time when the request will be marked as timed out.
// If ok is set to false, it can be ignored
func (c *context) Deadline() (deadline time.Time, ok bool) {
	if c.deadline == 0 {
		ok = false
		return
	}
	return time.Unix(0, atomic.LoadInt64(&c.completeBy)), true
}

// Done returns a completion channel notifying a listener if the context was completed or not
func (c *context) Done() <-chan struct{} {
	return c.complete
}

// GetLastError Returns the last error for a given context.
func (c *context) GetLastError() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.err != nil {
		return c.err
	}
	if c.Context != nil {
		return c.Context.Err()
	}
	return nil
}

// Err is the context override for GetLastError
func (c *context) Err() error {
	return c.GetLastError()
}

// Value will get an item from the context if found, otherwise will navigate through any child context(s) if applicable.
func (c *context) Value(key any) any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.contextValues != nil {
		if val, found := c.contextValues[key]; found {
			return val
		}
	}
	if c.Context != nil {
		return c.Context.Value(key)
	}
	return nil
}

// Complete a context, this sets a special error as the go implementation of context requires closed context's to have
// an error when its complete
func (c *context) Complete() {
	c.CompleteWithError(ErrComplete)
}

// CompleteWithError finishes an open context with a specific error, if the error is nil it will finish with ErrComplete
func (c *context) CompleteWithError(err error) {
	// Ensure this doesn't "complete" twice
	c.mu.Lock()
	if c.isComplete {
		c.mu.Unlock()
		return
	}
	c.isComplete = true
	// The lock is not held longer than to set isComplete.
	// New values can no longer be pushed to onComplete once this flag is set.
	// onComplete methods can access stored variables which cause a read lock.
	c.mu.Unlock()
	if err != nil {
		c.err = err
	}
	atomic.StoreInt64(&c.completeBy, time.Now().UnixNano())
	atomic.AddInt32(&c.factory.openContexts, -1)
	c.factory.openContextWg.Done()
	if c.err == nil {
		c.err = ErrComplete
	}
	// Do this as a LIFO queue
	// This section needs to be unlocked to allow these methods to access context variables
	for i := len(c.onComplete) - 1; i >= 0; i-- {
		c.onComplete[i](c, err)
	}
	c.mu.Lock()
	close(c.complete)
	// Clear out all references in the context values.
	// It is possible for a cyclical reference to be placed in the context leading to a subtle memory leak.
	for k := range c.contextValues {
		delete(c.contextValues, k)
	}
	c.contextValues = nil
	c.mu.Unlock()
}
