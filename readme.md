# Scene
Scene is a dependency management system that uses contexts to carry various components and configuration across an application.

Scene compatible values have lifecycle methods that allow them to hook into `onCreate`, `onSpawn`, `onFactoryMount`, `onFactoryUmount`. (`onCreate` and `onSpawn` can both hook into `onComplete`)

## Why Scene?
### Scene automates setup
Scene configures every context automatically based on the injector methods. This simplifies setup for creating new contexts.

### Scene is flat
Scene uses a map to store context values in. For larger applications with a bunch of dependencies, this can be a large performance boost to accessing context values.

### Scene has lifecycle values
Scene allows you to configure automatic lifecycles for contexts. This allows database connections to be configured and tore down by the injector and not having to manage it externally.

### Scene handles HTTP natively
Scene has middleware for handling HTTP connections automatically along with an encoding framework that 

## Concepts
Scene has three main parts, a context, a factory, and an injector.

### Context
This functions as a normal context as well as a Scene Context. This carries all values throughout your application without passing an extensive list of options or a dependency struct around. Simply pass the context and pull the values from it.

## Interface
New dependency injectors can use `BaseInjector` to handle all lifecycle methods and then override the methods they wish to implement. There are examples in the `plugins.md` file on how to create various injectors.

## Best practices
### One context per thread
Do not share a context across multiple threads. While Scene is thread safe, some of the injectors you may be using are not.
### Injector helper methods should return values or  interfaces
This allows you to return zero values. If you have a pointer return, create a zero-value return that can allow fluent functions to still operate without panicking.
### Shutdowns should be graceful
Provide a long enough window in the factory shutdown to allow threads to shut down gracefully. Long-lived background jobs can make use of `factory.Done()` and listen to when it's closed to trigger a graceful shutdown. In many cases you will want external controls to shut down those long-lived contexts before calling `factory.Shutdown()` as it can lead to better results.
## When to not use Scene
There are times when you may want to use Scene in a mixed mode or not at all. These are some of the use cases and why they at the case.

- If you have performant code that only needs access to a few variables.
  - Because Scene uses a map, if you have a few values a chained context is going to be a better way to pass values. (You probably want to pass the raw values in a configuration struct)
- Your contexts are simple and consist of only a handful (under 12) dependencies.
  - Scene uses a map to store values rather than a linked list. Like switch statements, linked lists can be much faster than maps at lower value counts.
- Your application uses nested contexts (uncommon)
  - Some applications store specific state values in their context chains and push new wrapped contexts in methods for traceability. Because Scene is flat, this isn't a possible option. 
  - Some applications will alter a specific value higher on the chain as an override from the base value.