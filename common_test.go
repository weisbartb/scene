package scene_test

import (
	"context"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/weisbartb/scene"
	"github.com/weisbartb/tsbuffer"
	"testing"
	"time"
)

func TestBaseProvider(t *testing.T) {
	// This is just to provide coverage for scene.BaseProvider which does nothing
	buf := tsbuffer.New()
	logger := zerolog.New(buf)
	factory, _ := scene.NewSceneFactor(scene.Config{
		FactoryIdentifier: "Test",
		MaxTTL:            time.Millisecond * 50,
		LogOutput:         logger,
	}, scene.BaseProvider{})
	t.Cleanup(func() {
		require.True(t, factory.Shutdown(time.Second))
	})
	ctx, err := factory.NewCtx()
	require.NoError(t, err)
	require.NotNil(t, ctx)
	ctx2, err := ctx.Spawn(time.Now().Add(time.Second))
	require.NoError(t, err)
	require.NotNil(t, ctx2)
	ctx.Complete()
	ctx2.Complete()
	factory.Shutdown(time.Second)
}

func TestGetEmptyScene(t *testing.T) {
	require.Nil(t, scene.GetScene(context.Background()))
}
