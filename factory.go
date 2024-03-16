package scene

import (
	ogContext "context"
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

type Config struct {
	FactoryIdentifier string // Makes it easier to track down stuck contexts
	MaxTTL            time.Duration
	LogOutput         zerolog.Logger
	DebugMode         bool
}

type Factory struct {
	closed               atomic.Bool
	defaultsLock         *sync.RWMutex
	requestTTL           time.Duration
	injectors            []Provider
	defaultContextValues map[any]any
	defaultContextCt     int
	openContexts         int32
	openContextWg        *sync.WaitGroup
	factoryLogger        zerolog.Logger
	factoryIdentifier    string
	config               Config
	done                 chan struct{}
}

func (factory *Factory) StoreDefault(key, value any) {
	factory.defaultsLock.Lock()
	if _, found := factory.defaultContextValues[key]; !found {
		factory.defaultContextCt++
	}
	factory.defaultContextValues[key] = value
	factory.defaultsLock.Unlock()
}

// GetDefault pulls the default injector for new contexts for a given key.
func (factory *Factory) GetDefault(key any) any {
	factory.defaultsLock.RLock()
	defer factory.defaultsLock.RUnlock()
	return factory.defaultContextValues[key]
}

// NewSceneFactor creates a new context factory off a given configuration.
// Factories should be created with all injectors allocated at the time they are created.
// Dynamic addition of injectors is not supported
func NewSceneFactor(config Config, injectors ...Provider) (*Factory, error) {
	factory := &Factory{
		defaultsLock:         &sync.RWMutex{},
		requestTTL:           config.MaxTTL,
		factoryLogger:        config.LogOutput,
		factoryIdentifier:    config.FactoryIdentifier,
		defaultContextValues: make(map[any]any),
		openContextWg:        &sync.WaitGroup{},
		injectors:            injectors,
		done:                 make(chan struct{}),
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

// Shutdown ensures that background tasks are completed before the factory shut down, returns true if a clean shutdown
// occurred
// A deadline that is at least as long as the average request context is recommended
func (factory *Factory) Shutdown(deadline time.Duration) bool {
	// Set the shutdown bit
	if !factory.closed.CompareAndSwap(false, true) {
		return false
	}
	close(factory.done)
	c := make(chan struct{})
	go func() {
		factory.openContextWg.Wait()
		close(c)
	}()
	defer func() {
		for k := len(factory.injectors); k >= 0; k-- {
			func() {
				factory.defaultsLock.RLock()
				defer factory.defaultsLock.RUnlock()
				defer func() {
					// Handle any panics that are recoverable and bubbled up through.
					if r := recover(); r != nil {
						factory.factoryLogger.Error().Interface("err", r).Send()
					}
				}()
				if err := factory.injectors[k].OnFactoryUnmount(factory); err != nil {
					factory.factoryLogger.Error().Err(err).Send()
				}
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

func (factory *Factory) Done() <-chan struct{} {
	return factory.done
}

// Wrap a context with a core context
func (factory *Factory) Wrap(ctx ogContext.Context) (Context, error) {
	if factory.closed.Load() {
		return nil, ErrShutdownInProgress
	}
	newCtx := factory.newCtx(ctx, factory.requestTTL)
	return newCtx, nil
}

// OpenContexts gets the count of all the open contexts
func (factory *Factory) OpenContexts() int {
	return int(atomic.LoadInt32(&factory.openContexts))
}

// NewCtx creates a new context for the application
func (factory *Factory) NewCtx() (Context, error) {
	if factory.closed.Load() {
		return nil, ErrShutdownInProgress
	}
	return factory.newCtx(ogContext.Background(), factory.requestTTL), nil
}

func (factory *Factory) newCtx(baseCtx ogContext.Context, deadline time.Duration) Context {
	atomic.AddInt32(&factory.openContexts, 1)
	factory.openContextWg.Add(1)
	requestID := uuid.New().String()
	ctx := &context{
		Context:       baseCtx,
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
	// Get what created this context for debug purposes
	_, file, line, _ := runtime.Caller(2)
	ctx.startedBy = file + ":" + strconv.Itoa(line)
	ctx.deadline = deadline
	// Store the initial base context that was used to create this.
	// If no values are found in this context, it will resolve this context chain to try to find the value.
	ctx.contextValues[ContextRef{}] = ctx
	if deadline > 0 {
		ctx.completeBy = time.Now().Add(deadline).UnixNano()
		go ctx.refreshDeadline()
	}
	return ctx
}
