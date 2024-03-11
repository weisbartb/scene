package scene

import (
	"context"
	"time"
)

type CompleteFunc func(ctx Context, completeErr error)

// Context extends the go context with a few extra methods required to power all the functionality this looks
//
//	to leverage
type Context interface {
	context.Context
	Store(key, value any)
	// Attach takes an existing context and attaches it to this context
	Attach(with func(chained context.Context) (newCtx context.Context))
	Complete()
	Defer(CompleteFunc)
	Spawn(completeBy time.Time) (Context, error)
	CompleteWithError(err error)
	GetLastError() error
}

// FactoryDefaultValuer is an interface pattern for defining a default value that every context will spawn with/
// This is for setting safe defaults (like a logger)
type FactoryDefaultValuer interface {
	StoreDefault(key, value any)
	GetDefault(key any) any
	IContextProvider
}

// FactoryStore is an object that directly taps into the factory to get default values, it can not set things.
type FactoryStore interface {
	GetDefault(key any) any
	IContextProvider
}

type Injector interface {
	// OnFactoryMount is called when the factory runs the initial injector, this is generally only called once per factory
	// when the application mounts
	// While this method provides access to FactoryDefaultValuer.NewCtx() it should not use it, doing so can lead to a data race.
	// This is needed so that it can be stored for setup purposes for use inside of default invocations.
	OnFactoryMount(valuer FactoryDefaultValuer)
	// OnFactoryUnmount is called when the factory is shutdown, this invokes after all background tasks have shut down
	// or the timeout occurs for trying to shut them down. Because of this, it is wise to make the shutdown leak proof
	OnFactoryUnmount(valuer FactoryDefaultValuer) error
	// OnNewContext is invoked every time a new context is created or spawned. If your plugin needs to set up initial state
	// this is the lifecycle you want to bind to. You
	OnNewContext(ctx Context)
	OnSpawnedContext(ctx Context, parentContext Context)
}

type BaseInjector struct{}

func (b BaseInjector) OnFactoryMount(valuer FactoryDefaultValuer) {

}

func (b BaseInjector) OnFactoryUnmount(valuer FactoryDefaultValuer) error {
	return nil
}

func (b BaseInjector) OnNewContext(ctx Context) {

}

func (b BaseInjector) OnSpawnedContext(ctx Context, parentContext Context) {

}

type IContextProvider interface {
	NewCtx() (Context, error)
}
