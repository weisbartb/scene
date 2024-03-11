package scene_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/weisbartb/scene"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brianvoe/gofakeit"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/ugorji/go/codec"
)

type testEncoder struct {
	w          scene.StatusWriter
	errors     []error
	statusCode int
}

func (t *testEncoder) GetWriter() scene.StatusWriter {
	return t.w
}
func (t *testEncoder) SetWriter(ctx scene.Context, w scene.StatusWriter) {
	t.w = w
}

func (t *testEncoder) AddError(err error, statusCode int) {
	t.errors = append(t.errors, err)
	t.statusCode = statusCode
}

func (t *testEncoder) Encode(obj any) error {
	t.w.WriteHeader(t.statusCode)
	return json.NewEncoder(t.w).Encode(obj)
}

type testHandler struct {
	call func(writer http.ResponseWriter, request *http.Request)
}

func (t testHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	t.call(writer, request)
}

var ct atomic.Int64

var unmountedCt atomic.Int64

type ctxKey struct{}

type testInjector struct {
	v  string
	id int64
}

func (t testInjector) OnFactoryMount(valuer scene.FactoryDefaultValuer) {
	valuer.StoreDefault(ctxKey{}, testInjector{v: "test"})
}

func (t testInjector) OnFactoryUnmount(valuer scene.FactoryDefaultValuer) error {
	unmountedCt.Add(1)
	return nil
}

func (t testInjector) OnNewContext(ctx scene.Context) {
	val := ctx.Value(ctxKey{}).(testInjector)
	val.id = ct.Add(1)
	ctx.Store(ctx, val)
}

func (t testInjector) OnSpawnedContext(ctx scene.Context, parentContext scene.Context) {
	val := parentContext.Value(ctxKey{}).(testInjector)
	val.id = ct.Add(1)
	ctx.Store(ctx, val)
}

// TestRequest_MiddlewareAndEncode Covers off on the HTTP middleware have a context injected and the encoding method working
func TestRequest_MiddlewareAndEncode(t *testing.T) {
	buf := bytes.Buffer{}
	logger := zerolog.New(&buf)
	factory, _ := scene.NewRequestFactory(scene.Config{
		FactoryIdentifier: "Test",
		MaxTTL:            time.Millisecond * 50,
		LogOutput:         logger,
	})
	middleware, err := scene.NewHTTPMiddleware(factory, func(ctx scene.Context, request *http.Request) scene.ResponseEncoder {
		return &testEncoder{}
	}, func(ctx scene.Context, request *http.Request, encoder scene.ResponseEncoder) {
		encoder.GetWriter().Header().Set("X-Test-Injection", "test")
	})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	var requestID string
	handler := testHandler{
		call: func(writer http.ResponseWriter, r *http.Request) {
			_, ok := r.Context().(*scene.Request)
			require.True(t, ok)
		},
	}
	middleware.Next(handler)
	parsedURL, err := url.Parse("https://www.google.com/search")

	require.NoError(t, err)
	middleware.ServeHTTP(recorder, &http.Request{
		URL:    parsedURL,
		Method: http.MethodPost,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
	})
	require.Equal(t, "test", recorder.Header().Get("X-Test-Injection"))
	require.Equal(t, fmt.Sprintf(`{"data":null,"metadata":{"errors":[],"requestId":"%v","statusCode":0}}`, requestID), recorder.Body.String())

}

func TestBindRequestToContext(t *testing.T) {
	buf := bytes.Buffer{}
	logger := zerolog.New(&buf)
	t.Run("All middleware bindings", func(t *testing.T) {
		factory, _ := scene.NewRequestFactory(scene.Config{
			MaxTTL:    time.Millisecond * 50,
			LogOutput: logger,
		}, testInjector{})
		t.Cleanup(func() {
			factory.Shutdown(time.Second * 3)
		})
		func() {
			ctx, _ := factory.NewCtx()
			defer ctx.Complete()
			payload := gofakeit.Address()
			payloadData := bytes.Buffer{}
			encoder := codec.NewEncoder(&payloadData, &codec.JsonHandle{})
			require.NoError(t, encoder.Encode(payload))
			req, err := http.NewRequest(http.MethodPost, gofakeit.URL(), bytes.NewReader(payloadData.Bytes()))
			require.NoError(t, err)
			require.Equal(t, ctx, req.Context())
		}()
		// Trigger shutdown
		factory.Shutdown(time.Second)
	})

}
