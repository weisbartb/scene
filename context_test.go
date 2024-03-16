package scene_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/weisbartb/scene"
	"github.com/weisbartb/tsbuffer"
)

type _testKey struct{}

var testKey = _testKey{}

type _testKey2 struct{}

var testKey2 = _testKey2{}

// TestNewCoreContextFactory ensures that factories are properly created
func TestNewCoreContextFactory(t *testing.T) {
	t.Parallel()
	buf := tsbuffer.New()
	logger := zerolog.New(buf)
	factory, _ := scene.NewSceneFactor(scene.Config{
		FactoryIdentifier: "Test factory",
		MaxTTL:            0,
		LogOutput:         logger,
	}, nil)
	t.Cleanup(func() {
		require.True(t, factory.Shutdown(time.Second))
	})
	ctx, _ := factory.NewCtx()
	ctx.Complete()
	// This will block if complete didn't fire
	<-ctx.Done()
	ctx2, _ := factory.NewCtx()
	defer ctx2.Complete()
	require.NotEqual(t, scene.GetRequestID(ctx), scene.GetRequestID(ctx2))
}

// TestCoreCtxDeadline ensures that deadlines are properly respected by not closing the context but blocking till complete
func TestCoreCtxDeadline(t *testing.T) {
	t.Parallel()
	t.Run("Timeout test", func(t *testing.T) {
		buf := tsbuffer.New()
		logger := zerolog.New(buf)
		factory, _ := scene.NewSceneFactor(scene.Config{
			FactoryIdentifier: "Test",
			MaxTTL:            time.Millisecond * 100,
			LogOutput:         logger,
		}, nil)
		t.Cleanup(func() {
			require.True(t, factory.Shutdown(time.Second))
		})
		now := time.Now()
		ctx, _ := factory.NewCtx()
		defer ctx.Complete()
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.True(t, deadline.After(now))
		// Travis has some issues with clock timing in their vms, this needs to be a bit longer than the TTL,
		// an extra few MS wasn't enough
		require.True(t, deadline.Before(now.Add(time.Millisecond*150)))
		// This will block if complete didn't fire
		<-ctx.Done()
		require.Equal(t, ctx.Err().Error(), scene.ErrTimeout.Error())
	})
	t.Run("Completion test", func(t *testing.T) {
		buf := tsbuffer.New()
		logger := zerolog.New(buf)
		factory, _ := scene.NewSceneFactor(scene.Config{
			MaxTTL:    time.Millisecond * 100,
			LogOutput: logger,
		}, nil)
		t.Cleanup(func() {
			require.True(t, factory.Shutdown(time.Second))
		})
		// Cover for completion and ensure timer doesn't have issues
		ctx, _ := factory.NewCtx()
		defer ctx.Complete()
		// Reset clock
		now := time.Now()
		time.Sleep(10 * time.Microsecond) // Sleep to prevent a data race in high-precision time
		ctx.Complete()
		<-ctx.Done()
		require.Equal(t, scene.ErrComplete, ctx.Err())
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.True(t, deadline.After(now))
		require.True(t, deadline.Before(now.Add(time.Millisecond*50)))
	})

	t.Run("Infinite deadline", func(t *testing.T) {
		buf := tsbuffer.New()
		logger := zerolog.New(buf)
		factory, _ := scene.NewSceneFactor(scene.Config{
			MaxTTL:    scene.NoTTL,
			LogOutput: logger,
		}, nil)
		t.Cleanup(func() {
			require.True(t, factory.Shutdown(time.Second))
		})
		// Cover for completion and ensure timer doesn't have issues
		ctx, _ := factory.NewCtx()
		defer ctx.Complete()
		ts, done := ctx.Deadline()
		require.False(t, done)
		require.True(t, ts.IsZero())
	})

}
func TestCustomContextFromContext(t *testing.T) {
	t.Parallel()
	buf := tsbuffer.New()
	logger := zerolog.New(buf)
	factory, _ := scene.NewSceneFactor(scene.Config{
		MaxTTL:    time.Millisecond * 100,
		LogOutput: logger,
	}, nil)
	t.Cleanup(func() {
		require.True(t, factory.Shutdown(time.Second))
	})
	ctx, _ := factory.NewCtx()
	defer ctx.Complete()
	wrappedContext := context.WithValue(ctx, testKey, "test")
	resolvedCtx := scene.GetScene(wrappedContext)
	require.Equal(t, ctx, resolvedCtx)
	// Handle a double wrapped context
	ctx2, _ := factory.NewCtx()
	defer ctx2.Complete()
	ctx2.Attach(resolvedCtx)
	require.Equal(t, scene.GetBaseContext(ctx2), scene.GetBaseContext(resolvedCtx))
	// Handle unwrapped query
	ctx3, _ := factory.NewCtx()
	defer ctx3.Complete()
	require.Nil(t, ctx3.Value("banana"))
}
func TestCustomContextFactory_Wrap(t *testing.T) {
	t.Parallel()
	buf := tsbuffer.New()
	logger := zerolog.New(buf)
	factory, _ := scene.NewSceneFactor(scene.Config{
		LogOutput: logger,
	}, nil)
	t.Cleanup(func() {
		require.True(t, factory.Shutdown(time.Second))
	})
	t.Run("Simple nesting", func(t *testing.T) {
		t.Parallel()
		initialCtx := context.WithValue(context.Background(), testKey, "bar")
		ctx, _ := factory.Wrap(initialCtx)
		defer ctx.Complete()
		require.Equal(t, "bar", ctx.Value(testKey))
		require.Equal(t, nil, ctx.Value(testKey2))
		require.Equal(t, scene.GetRequestID(ctx), scene.GetRequestID(ctx))
		require.Equal(t, "", scene.GetRequestID(initialCtx))
	})
	t.Run("Complex nesting", func(t *testing.T) {
		t.Parallel()
		initialCtx := context.WithValue(context.Background(), testKey, "bar")
		ctx, _ := factory.Wrap(initialCtx)
		defer ctx.Complete()
		complexCtx := context.WithValue(ctx, testKey2, "bar2")
		require.Equal(t, "bar", complexCtx.Value(testKey))
		require.Equal(t, "bar2", complexCtx.Value(testKey2))
		require.Equal(t, scene.GetRequestID(ctx), scene.GetRequestID(complexCtx))
	})
}

func TestCustomContext_Spawn(t *testing.T) {
	buf := tsbuffer.New()
	logger := zerolog.New(buf)
	factory, _ := scene.NewSceneFactor(scene.Config{
		MaxTTL:    time.Millisecond * 50,
		LogOutput: logger,
	})
	t.Cleanup(func() {
		require.True(t, factory.Shutdown(time.Second))
	})
	t.Run("Expiring child", func(t *testing.T) {

		ctx, _ := factory.NewCtx()
		defer ctx.Complete()
		child, _ := ctx.Spawn(time.Now().Add(time.Millisecond * 100))
		child2, _ := ctx.Spawn(time.Now().Add(time.Millisecond * 300))

		state := 0
		ctxDone := false

	statePoll:
		for {
			select {
			case <-ctx.Done():
				if !ctxDone {
					require.Equal(t, 0, state)
					state++
					ctxDone = true
				}
			case <-child.Done():
				require.Equal(t, 1, state)
				require.Equal(t, scene.ErrTimeout.Error(), child.Err().Error())
				break statePoll
			}
		}
		child2.Complete()
		<-child2.Done()
		require.Equal(t, child2.Err().Error(), scene.ErrComplete.Error())
		_, err := child.Spawn(scene.RunForever)
		require.ErrorIs(t, scene.ErrShutdownInProgress, err)
	})
	t.Run("Infinite child", func(t *testing.T) {

		ctx, _ := factory.NewCtx()
		defer ctx.Complete()
		child, _ := ctx.Spawn(scene.RunForever)
		timer2 := time.After(time.Millisecond * 200)
		state := 0
		ctxDone := false

	statePoll:
		for {
			select {
			case <-ctx.Done():
				if !ctxDone {
					require.Equal(t, 0, state)
					state++
					ctxDone = true
				}
			case <-timer2:
				require.Equal(t, 1, state)
				child.Complete()
				break statePoll
			case <-child.Done():
				t.Fatal("Should not complete while in this loop")
			}
		}
		require.Equal(t, scene.ErrComplete, child.Err())
	})

}

func TestStoreAndValue(t *testing.T) {
	k, v := "foo", "bar"
	buf := tsbuffer.New()
	logger := zerolog.New(buf)
	factory, _ := scene.NewSceneFactor(scene.Config{
		MaxTTL:    0,
		LogOutput: logger,
	}, nil)
	t.Cleanup(func() {
		require.True(t, factory.Shutdown(time.Second))
	})
	ctx, _ := factory.NewCtx()
	defer ctx.Complete()
	ctx.Store(k, v)
	require.NotNil(t, v, ctx.Value(k))
	require.Equal(t, v, ctx.Value(k).(string))
}

func TestStoreAndValue_WithHTTPHeader(t *testing.T) {
	k, v := "Foo", "bar"
	h := http.Header{k: []string{v}}
	buf := tsbuffer.New()
	logger := zerolog.New(buf)
	factory, _ := scene.NewSceneFactor(scene.Config{
		MaxTTL:    0,
		LogOutput: logger,
	}, nil)
	t.Cleanup(func() {
		require.True(t, factory.Shutdown(time.Second))
	})
	ctx, _ := factory.NewCtx()
	defer ctx.Complete()
	ctx.Store(scene.CtxHTTPHeaderKey{}, h)
	require.NotNil(t, v, ctx.Value(scene.CtxHTTPHeaderKey{}))
	require.Equal(t, v, ctx.Value(scene.CtxHTTPHeaderKey{}).(http.Header).Get(k))
}

func TestFactory_NewCtxDeadlockFix(t *testing.T) {
	buf := tsbuffer.New()
	logger := zerolog.New(buf)
	factory, _ := scene.NewSceneFactor(scene.Config{
		MaxTTL:    0,
		LogOutput: logger,
	}, nil)
	ctx, _ := factory.NewCtx()
	go func() {
		time.Sleep(time.Millisecond)
		_, err := factory.NewCtx()
		require.Error(t, err)
		ctx.Complete()
	}()
	require.True(t, factory.Shutdown(time.Second))
}

func TestContext_Extend(t *testing.T) {
	buf := tsbuffer.New()
	logger := zerolog.New(buf)
	factory, _ := scene.NewSceneFactor(scene.Config{
		FactoryIdentifier: "Test factory",
		MaxTTL:            time.Millisecond * 100,
		LogOutput:         logger,
	}, nil)
	t.Cleanup(func() {
		require.True(t, factory.Shutdown(time.Second))
	})
	ctx, err := factory.NewCtx()
	defer ctx.Complete()
	require.NoError(t, err)
	dl, ok := ctx.Deadline()
	require.True(t, ok)
	require.True(t, dl.After(time.Now()))
	ctx.Extend(time.Now().Add(time.Millisecond * 200))
	dl2, ok := ctx.Deadline()
	require.True(t, ok)
	require.True(t, dl2.After(dl))
	<-ctx.Done()
	require.GreaterOrEqual(t, time.Now().Sub(dl2), time.Duration(0))
	require.LessOrEqual(t, time.Now().Sub(dl2), time.Millisecond*400)
}

func TestContext_Attach(t *testing.T) {
	buf := tsbuffer.New()
	logger := zerolog.New(buf)
	factory, _ := scene.NewSceneFactor(scene.Config{
		FactoryIdentifier: "Test factory",
		MaxTTL:            time.Millisecond * 100,
		LogOutput:         logger,
	}, nil)
	t.Cleanup(func() {
		require.True(t, factory.Shutdown(time.Second))
	})
	ctx, err := factory.NewCtx()
	defer ctx.Complete()
	require.NoError(t, err)
	ctx.Attach(context.Background())
	require.Equal(t, context.Background(), ctx.GetBaseCtx())
}

func TestContext_Defer(t *testing.T) {
	buf := tsbuffer.New()
	logger := zerolog.New(buf)
	factory, _ := scene.NewSceneFactor(scene.Config{
		FactoryIdentifier: "Test factory",
		MaxTTL:            time.Millisecond * 100,
		LogOutput:         logger,
	}, nil)
	var c = make(chan struct{})
	t.Cleanup(func() {
		<-c
		require.True(t, factory.Shutdown(time.Second))
	})
	ctx, err := factory.NewCtx()
	defer ctx.Complete()
	require.NoError(t, err)
	ctx.Defer(func(ctx scene.Context, completeErr error) {
		close(c)
	})
}

func TestContext_Store(t *testing.T) {
	buf := tsbuffer.New()
	logger := zerolog.New(buf)
	factory, _ := scene.NewSceneFactor(scene.Config{
		FactoryIdentifier: "Test factory",
		MaxTTL:            time.Millisecond * 100,
		LogOutput:         logger,
	}, nil)

	ctx, err := factory.NewCtx()
	require.NoError(t, err)
	ctx.Store("test", "val")
	require.Equal(t, "val", ctx.Value("test").(string))
	ctx.Complete()
	ctx.Store("test", "val")
	require.Equal(t, nil, ctx.Value("test"))

}
