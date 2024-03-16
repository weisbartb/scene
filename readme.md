# Scene

Scene is a dependency management system that uses contexts to carry various components and configuration across an
application.

Scene compatible providers have a variety of lifecycle events that can be hooked
into `onCreate`, `onSpawn`, `onFactoryMount`, `onFactoryUmount`.

**Note**: `onCreate` and `onSpawn` can both hook into `onComplete`.

## Automated clean-up

Scene will automatically clean up any contexts that timeout or complete.
This is handed by using a `ctx.Defer()` inside your provider for the `onSpawn` method.
You can use this to close files, release database connections, close open sockets, and more.

## Logging

Logging is handled by [zerolog](https://www.github.com/rs/zerolog) currently.
There are no plans to add pluggable logging to the core factory.
Logging providers can be added for any logger, this logging has very little impact on functionality of your application.
If you do not wish to user a logging instance you can leave the variable blank during construction and log events
with be suppressed.

**Note:** Logging is only used during shutdown if an error occurs from a call to `onFactoryUnmount`.

## HTTP Support

Scene natively has support for HTTP middleware that supports basic JSON encoding.
Additional encoders can be added via plugins and can be dynamically chosen based on HTTP request headers.
**Note**: Your handler must call `scene.GetEncoder(ctx).Encode(yourDataObject)` at the end of the handler
or nothing will be encoded.
You can see an example of this inside [middleware_test.go](./middleware_test.go).

The middleware constructor supports the ability to set an encoder provider and a setup hook for each request.

### Example of an encoder provider

```go
package scene_samples

func (ctx scene.Context, request *http.Request) scene.ResponseEncoder {
	switch strings.ToLower(request.Header.Get("Content-Type")) {
	case "application/json":
		return encoders.NewJSONEncoder(request.Header, yourDataWrapper{})
	case "application/xml":
		return encoders.NewXMLEncoder(request.Header, yourDataWrapper{})
	default:
		return encoders.NewJSONEncoder(request.Header, yourDataWrapper{})
	}
}

```

This provider allows for JSON and XML encoders to be used based on the incoming content type.
The default JSON encoder has support for gzip if the client accepts it.

### Example of an on request hook

```go
package scene_samples

func (ctx scene.Context, request *http.Request, encoder scene.ResponseEncoder) {
	encoder.GetWriter().Header().Set("X-Some-Custom-header", "value")
}

```

Thus allowing you to setup any custom values coming in from headers for access later on.

## Example

```go
package scene-db
type CtxContextKey struct{}

type Provider struct {
	scene.BaseProvider
	logger          logger
	DB              *sqlx.DB
	closeOnShutdown bool
}

func NewProvider(cfg MySQLConfig, loggingInstance logger) (Provider, error) {
	db, err := sqlx.Connect("mysql", cfg.BuildDSN())
	if err != nil {
		return Provider{}, stack.Trace(err)
	}
	db.SetMaxOpenConns(50)
	db.SetConnMaxLifetime(time.Second * 60 * 15)
	db.SetMaxIdleConns(25)
	return Provider{
		DB:              db,
		closeOnShutdown: true,
	}, stack.Trace(err)
}

func (p Provider) OnFactoryUnmount(valuer icontext.FactoryDefaultValuer) error {
	if p.closeOnShutdown {
		wg := deadlinewg.WaitGroup(time.Second * 3)
		wg.Add(1)
		var closeErr error
		go func() {
			defer wg.Done()
			closeErr = p.DB.Close()
		}()
		wgErr := wg.Wait()
		if wgErr != nil {
			return closeErr
		}
		return ShutdownErr
	}
	return nil
}

func (p Provider) OnNewContext(ctx scene.Context) {
	// There should be a default logger that is
	val := ctx.Value(CtxContextKey{})
	if val == nil {
		instance := NewInstance(ctx, p.DB)
		ctx.Defer(func(ctx scene.Context, completeErr error) {
			if err := instance.Close(); err != nil {
				p.logger.Errorf("Could not close connection on context completion. If you are seeing this, there is a bug in your code. %v", err)
			}
			ctx.Store(CtxContextKey{}, nil)
		})
		// Add the database
		ctx.Store(CtxContextKey{}, instance)
	}
}

func GetManagedDatabaseInstance(ctx context.Context) *Instance {
	if val := ctx.Value(CtxContextKey{}); val != nil {
		return val.(*Instance)
	}
	return nil
}

```

In this example you can see a basic provider from the `scene-db/mysql` package.

When the factory is shutdown,
all connections are closed and will block the factory (for up to 3 seconds) till they close.

When a new context is spawned, it will allocate a new database instance for that context
and pin that instance for the rest of the session.

## Best practices

### Scene contexts should have a deadline

All scene factories should have a default deadline, even if it's long.
While `NoTTL` is a valid factory deadline to provide, it can lead to long-running tasks that can bottleneck resources.

### One context per thread

Do not share a context across multiple threads. While Scene is thread safe, some of the injectors you may be using are
not.

### Injector helper methods should return values or  interfaces

This allows you to return zero values. If you have a pointer return, create a zero-value return that can allow fluent
functions to still operate without panicking.

### Shutdowns should be graceful

Provide a long enough window in the factory shutdown to allow threads to shut down gracefully. Long-lived background
jobs can make use of `factory.Done()` and listen to when it's closed to trigger a graceful shutdown. In many cases you
will want external controls to shut down those long-lived contexts before calling `factory.Shutdown()` as it can lead to
better results.

## When to not use Scene

There are times when you may want to use Scene in a mixed mode or not at all. These are some of the use cases and why
they at the case.

- If you have performant code that only needs access to a few variables.
    - Initial setup of a scene can be more expensive than a chained context and substantially more than just passing in
      access to the providers directly.
- Your contexts are basic and consist of only a handful (under 12) dependencies.
    - Scene uses a map to store values rather than a linked list. Like switch statements, linked lists can be much
      faster than maps at lower value counts.
- Your application must use nested contexts
    - Some applications store specific state values in their context chains and push new wrapped contexts in methods for
      traceability. Because Scene is flat, this isn't a possible option.
    - Some applications will alter a specific value higher on the chain as an override from the base value.