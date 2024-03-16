package scene

import (
	ogContext "context"
	"time"

	"github.com/pkg/errors"
)

type RequestIDKey struct{}
type ContextRef struct{}

// GetRequestID will get the request id from any Scene compatible context
func GetRequestID(ctx ogContext.Context) string {
	id := ctx.Value(RequestIDKey{})
	if id == nil {
		return ""
	}
	return id.(string)
}

// GetBaseContext will get the underlying context attached to a Scene.
func GetBaseContext(ctx ogContext.Context) ogContext.Context {
	return ctx.Value(ContextRef{}).(Context).GetBaseCtx()
}

// GetScene will get a Scene from a given context. If no Scene is found, nil is returned.
func GetScene(ctx ogContext.Context) Context {
	if c, ok := ctx.(Context); ok {
		return c
	}
	val := ctx.Value(ContextRef{})
	if val == nil {
		return nil
	}
	return val.(Context)
}

// NoTTL prevents contexts from automatically timing out
const NoTTL = time.Duration(0)

var RunForever = time.Time{}
var ErrTimeout = errors.New("request timed out")
var ErrComplete = errors.New("request marked complete")

type CompleteFunc func(ctx Context, completeErr error)

// Context extends the go context with a few extra methods required to power all the functionality this looks
//
//	to leverage
type Context interface {
	ogContext.Context
	Store(key, value any)
	// Attach takes an existing context and attaches it to this Scene
	Attach(ctx ogContext.Context)
	// Complete finishes the Scene and initiates the garbage collection for resources
	Complete()
	// Defer allows for any additional clean up that may be needed on the context,
	//	these run before the context storage is emptied.
	// De-referencing values from a provider is forbidden during this.
	// Run your cleanup methods in the OnComplete hooks for the given provider.
	Defer(CompleteFunc)
	// Spawn creates a new child scene from this scene. They are only loosely coupled and a new timeout is required
	Spawn(completeBy time.Time) (Context, error)
	// CompleteWithError sets the error state prior ot calling Complete
	CompleteWithError(err error)
	// GetLastError will get the last error in the scene, this doesn't get unset or destroyed when a Scene completes.
	GetLastError() error
	// GetBaseCtx gets the underlying context.Context that may have been used to create the Scene.
	GetBaseCtx() ogContext.Context
	// Extend extends the duration of the context
	Extend(until time.Time)
}

// FactoryDefaultValuer allows for full access to the factory's default setup
type FactoryDefaultValuer interface {
	// StoreDefault stores a default value in all new Scenes created in this factory for a given value key
	StoreDefault(key, value any)
	FactoryStore
}

// FactoryStore allows for read-only access to a factory's default setup.
type FactoryStore interface {
	// GetDefault gets the default value for a key from a given factory.
	GetDefault(key any) any
	// NewScene creates a new Scene.
	NewCtx() (Context, error)
}

// Provider g
type Provider interface {
	// OnFactoryMount is called when the factory runs the initial injector, this is generally only called once per factory
	// when the application mounts
	// While this method provides access to FactoryDefaultValuer.NewCtx() it should not use it, doing so can lead to a data race.
	// This is needed so that it can be stored for setup purposes for use inside default invocations.
	OnFactoryMount(valuer FactoryDefaultValuer)
	// OnFactoryUnmount is called when the factory is shutdown.
	// This allows for resources to be properly closed after shutdowns are triggered.
	OnFactoryUnmount(valuer FactoryDefaultValuer) error
	// OnNewContext is invoked every time a new context is created or spawned.
	// This should be used to store a reference to the applicable object you want to provide.
	OnNewContext(ctx Context)
	// OnSpawnedContext triggers whenever a context spawns a new child context.
	// Most use cases for this involve copying some data from the parent context to the child.
	// By default, spawned contexts invoke the same things OnNewContext would invoke.
	// This runs after OnNewContext hooks are triggered.
	OnSpawnedContext(ctx Context, parentContext Context)
}

// BaseProvider is a skeleton that can be inherited to set up a new provider object.
// By default, this will make a dependency "injectable" into a factory;
// however, it will do nothing without overriding methods.
type BaseProvider struct{}

func (b BaseProvider) OnFactoryMount(valuer FactoryDefaultValuer) {

}

func (b BaseProvider) OnFactoryUnmount(valuer FactoryDefaultValuer) error {
	return nil
}

func (b BaseProvider) OnNewContext(ctx Context) {

}

func (b BaseProvider) OnSpawnedContext(ctx Context, parentContext Context) {

}
