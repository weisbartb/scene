package scene

import (
	"context"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

var ErrShutdownInProgress = errors.New("factory shutdown in progress")

// Config is factory configuration to set values that persist through all spawned children
type Config struct {
	FactoryIdentifier string // Makes it easier to track down stuck contexts
	MaxTTL            time.Duration
	LogOutput         zerolog.Logger
	DebugMode         bool
}

// Factory holds the state of a context factory
type Factory struct {
	closed               int32
	defaultsLock         *sync.RWMutex
	requestTTL           time.Duration
	injectors            []Injector
	defaultContextValues map[any]any
	defaultContextCt     int
	openContexts         int32
	openContextWg        *sync.WaitGroup
	openBackgroundTasks  *sync.WaitGroup
	// factoryLogger is still a required due to the fact that shutdown crashes need to be logged, there is probably
	// some room for improvement later to remove this
	factoryLogger     zerolog.Logger
	factoryIdentifier string
	config            Config
}

func (factory *Factory) StoreDefault(key, value any) {
	factory.defaultsLock.Lock()
	if _, found := factory.defaultContextValues[key]; !found {
		factory.defaultContextCt++
	}
	factory.defaultContextValues[key] = value
	factory.defaultsLock.Unlock()
}

func (factory *Factory) GetDefault(key any) any {
	factory.defaultsLock.RLock()
	defer factory.defaultsLock.RUnlock()
	return factory.defaultContextValues[key]
}

// NewRequestFactory creates a new context factory off a given configuration
func NewRequestFactory(config Config, injectors ...Injector) (*Factory, error) {
	factory := &Factory{
		defaultsLock:         &sync.RWMutex{},
		requestTTL:           config.MaxTTL,
		factoryLogger:        config.LogOutput,
		factoryIdentifier:    config.FactoryIdentifier,
		defaultContextValues: make(map[any]any),
		openBackgroundTasks:  &sync.WaitGroup{},
		openContextWg:        &sync.WaitGroup{},
		injectors:            injectors,
		config:               config,
	}
	// Bind all mounts
	for _, v := range injectors {
		if v != nil {
			v.OnFactoryMount(factory)
		}
	}
	return factory, nil
}

// Shutdown ensures that background tasks are completed before the factory shuts down, returns true if a clean shutdown
// occurred
func (factory *Factory) Shutdown(deadline time.Duration) bool {
	atomic.StoreInt32(&factory.closed, 1)
	// This can technically still race when shutdown is called with a spawn at the same time.
	// This can be fixed but requires a lot of work, while this sleep is less than ideal, this will stop 99.99% of the issues
	time.Sleep(time.Millisecond * 10)
	c := make(chan struct{})
	go func() {
		factory.openContextWg.Wait()
		factory.openBackgroundTasks.Wait()
		close(c)
	}()
	defer func() {
		// Call the lifecycle for the unmount
		for k := len(factory.injectors); k >= 0; k-- {
			func() {
				factory.defaultsLock.RLock()
				defer factory.defaultsLock.RUnlock()
				// Unlike other methods we need to make sure these are crash proof as there could severe side-effects
				// from an improper shutdown.
				defer func() {
					if r := recover(); r != nil {
						factory.factoryLogger.Error().Interface("error", r)
					}
				}()
				factory.injectors[k].OnFactoryUnmount(factory)
			}()
		}
	}()
	select {
	case <-c:
		return true
	case <-time.After(deadline):
		return false
	}
}

// Wrap a context with a core context
func (factory *Factory) Wrap(ctx context.Context) (Context, error) {
	if atomic.LoadInt32(&factory.closed) == 1 {
		return nil, ErrShutdownInProgress
	}
	newCtx := factory.newCtx(factory.requestTTL)
	newCtx.Context = ctx
	return newCtx, nil
}

// OpenContexts gets the count of all of the open contexts
func (factory *Factory) OpenContexts() int {
	return int(atomic.LoadInt32(&factory.openContexts))
}

// NewCtx creates a new context for the application
func (factory *Factory) NewCtx() (Context, error) {
	if atomic.LoadInt32(&factory.closed) == 1 {
		return nil, ErrShutdownInProgress
	}
	return factory.newCtx(factory.requestTTL), nil
}

// DefaultTTL gets the default TTL
func (factory *Factory) DefaultTTL() time.Duration {
	return factory.requestTTL
}

func (factory *Factory) newCtx(deadline time.Duration) *Request {
	atomic.AddInt32(&factory.openContexts, 1)
	factory.openContextWg.Add(1)
	requestID := uuid.New().String()
	ctx := &Request{
		factory:       factory,
		complete:      make(chan struct{}),
		contextValues: make(map[any]any, factory.defaultContextCt+10), // Pre-size the context
		id:            requestID,
		mu:            &sync.RWMutex{},
	}
	ctx.contextValues[RequestIDKey{}] = ctx.id
	// Increase the open contexts (used to make sure we don't shut down with an active context)
	factory.defaultsLock.RLock()
	defer factory.defaultsLock.RUnlock()
	// Copy all defaults into the context
	for k, v := range factory.defaultContextValues {
		ctx.contextValues[k] = v
	}
	// Run hooks for every module
	for _, v := range factory.injectors {
		if v != nil {
			v.OnNewContext(ctx)
		}
	}
	ctx.startedAt = time.Now()
	// Calculate what created this context
	_, file, line, _ := runtime.Caller(2)
	ctx.startedBy = file + ":" + strconv.Itoa(line)
	ctx.deadline = deadline
	// This needs to store the context inside itself so it can resolve on a generic context call. This allows it to wrap
	// itself or be double resolved without danger
	ctx.contextValues[BaseContextKey{}] = ctx
	if factory.requestTTL == NoTTL {
		ctx.infinite = true
		return ctx
	}
	if deadline > 0 {
		ctx.completeBy = time.Now().Add(deadline).UnixNano()
		go ctx.startDeadline()
	}
	return ctx
}

func (factory *Factory) GetLogger() zerolog.Logger {
	return factory.factoryLogger
}
