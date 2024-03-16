package scene_test

import (
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/weisbartb/scene"
	"github.com/weisbartb/tsbuffer"
	"testing"
	"time"
)

func TestFactory_Defaults(t *testing.T) {
	buf := tsbuffer.New()
	logger := zerolog.New(buf)
	factory, _ := scene.NewSceneFactory(scene.Config{
		FactoryIdentifier: "Test",
		MaxTTL:            time.Millisecond * 50,
		LogOutput:         logger,
	}, scene.BaseProvider{})
	t.Cleanup(func() {
		factory.Shutdown(time.Second)
	})
	factory.StoreDefault("test", "val")
	require.Equal(t, "val", factory.GetDefault("test").(string))
	ctx, err := factory.NewCtx()
	require.NoError(t, err)
	require.Equal(t, "val", ctx.Value("test").(string))
	ctx.Complete()
	factory.StoreDefault("test", "val2")
	require.Equal(t, "val2", factory.GetDefault("test").(string))
	ctx, err = factory.NewCtx()
	require.NoError(t, err)
	require.Equal(t, "val2", ctx.Value("test").(string))
	ctx.Complete()
}
func TestFactory_Shutdown(t *testing.T) {
	buf := tsbuffer.New()
	logger := zerolog.New(buf)
	factory, _ := scene.NewSceneFactory(scene.Config{
		FactoryIdentifier: "Test",
		MaxTTL:            time.Millisecond * 50,
		LogOutput:         logger,
	}, scene.BaseProvider{})
	require.True(t, factory.Shutdown(time.Second))
	require.False(t, factory.Shutdown(time.Second))
	<-factory.Done()
}
func TestFactory_OpenContexts(t *testing.T) {
	buf := tsbuffer.New()
	logger := zerolog.New(buf)
	factory, _ := scene.NewSceneFactory(scene.Config{
		FactoryIdentifier: "Test",
		MaxTTL:            time.Millisecond * 50,
		LogOutput:         logger,
	}, scene.BaseProvider{})
	t.Cleanup(func() {
		factory.Shutdown(time.Second)
	})
	var ctxs []scene.Context
	for i := 0; i < 10; i++ {
		ctx, err := factory.NewCtx()
		require.NoError(t, err)
		require.Equal(t, i+1, factory.OpenContexts())
		ctxs = append(ctxs, ctx)
	}
	for k, v := range ctxs {
		v.Complete()
		require.Equal(t, 10-(k+1), factory.OpenContexts())
	}
}
