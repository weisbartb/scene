# Scene

Scene is a dependency management system that uses contexts to carry various components and configuration across an
application.

Scene compatible providers have a variety of lifecycle events that can be hooked
into `onCreate`, `onSpawn`, `onFactoryMount`, `onFactoryUmount`.

**Note**: `onCreate` and `onSpawn` can both hook into `onComplete`.

## Logging

Logging is handled by [zerolog](https://www.github.com/rs/zerolog) currently.
There is a plan to allow for pluggable logging in the future.
However, it is quite the time sink to add currently interfaces that provide leveled output with variables in an
efficient manner.

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

## Best practices

### Scenes should have a deadline

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
    - Because Scene uses a map, if you have a few values a chained context is going to be a better way to pass values. (
      You probably want to pass the raw values in a configuration struct)
- Your contexts are simple and consist of only a handful (under 12) dependencies.
    - Scene uses a map to store values rather than a linked list. Like switch statements, linked lists can be much
      faster than maps at lower value counts.
- Your application uses nested contexts (uncommon)
    - Some applications store specific state values in their context chains and push new wrapped contexts in methods for
      traceability. Because Scene is flat, this isn't a possible option.
    - Some applications will alter a specific value higher on the chain as an override from the base value.