package scene_test

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"github.com/weisbartb/scene"
	"github.com/weisbartb/scene/encoders"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

type testWrapper struct {
	Data     any `json:"data"`
	Metadata struct {
		Errors     []error `json:"errors"`
		RequestID  string  `json:"requestId"`
		StatusCode int     `json:"statusCode"`
	} `json:"metadata"`
}

func (t *testWrapper) AddError(err error, statusCode int) {
	t.Metadata.StatusCode = statusCode
	t.Metadata.Errors = append(t.Metadata.Errors, err)
}

func (t *testWrapper) GetStatusCode() int {
	if len(t.Metadata.Errors) == 0 {
		return http.StatusOK
	}
	if t.Metadata.StatusCode > 600 || t.Metadata.StatusCode < 100 {
		return http.StatusBadRequest
	}
	return t.Metadata.StatusCode
}
func (t *testWrapper) Wrap(writer http.ResponseWriter, obj any) any {
	if len(t.Metadata.Errors) == 0 {
		t.Metadata.StatusCode = 200
	}
	t.Metadata.RequestID = writer.Header().Get("X-Request-ID")
	t.Data = obj
	return t
}

func (t testWrapper) New() encoders.ResponseWrapper {
	return &testWrapper{}
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
	factory, _ := scene.NewSceneFactor(scene.Config{
		FactoryIdentifier: "Test",
		MaxTTL:            time.Millisecond * 50,
		LogOutput:         logger,
	})

	t.Run("no-compression", func(t *testing.T) {
		middleware, err := scene.NewHTTPMiddleware(factory, func(ctx scene.Context, request *http.Request) scene.ResponseEncoder {
			switch strings.ToLower(request.Header.Get("Content-Type")) {
			case "application/json":
				return encoders.NewJSONEncoder(request.Header, testWrapper{})
			default:
				return encoders.NewJSONEncoder(request.Header, testWrapper{})
			}
		}, func(ctx scene.Context, request *http.Request, encoder scene.ResponseEncoder) {
			encoder.GetWriter().Header().Set("X-Test-Injection", "test")
		})
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		var requestID string
		handler := testHandler{
			call: func(writer http.ResponseWriter, r *http.Request) {
				ctx := scene.GetScene(r.Context())
				require.NotNil(t, ctx)
				_ = scene.GetEncoder(ctx).Encode(nil)
				requestID = scene.GetRequestID(ctx)
			},
		}
		middleware.Next(handler)
		parsedURL, err := url.Parse("https://www.google.com/search")
		require.NoError(t, err)
		req := &http.Request{
			URL:    parsedURL,
			Method: http.MethodPost,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
			},
		}
		middleware.ServeHTTP(recorder, req)
		recorder.Flush()
		require.Equal(t, http.StatusOK, recorder.Code)
		require.Equal(t, "test", recorder.Header().Get("X-Test-Injection"))
		require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
		require.Equal(t, fmt.Sprintf(`{"data":null,"metadata":{"errors":null,"requestId":"%v","statusCode":200}}`, requestID), strings.TrimSpace(recorder.Body.String()))
	})
	t.Run("gzip", func(t *testing.T) {
		middleware, err := scene.NewHTTPMiddleware(factory, func(ctx scene.Context, request *http.Request) scene.ResponseEncoder {
			switch strings.ToLower(request.Header.Get("Content-Type")) {
			case "application/json":
				return encoders.NewJSONEncoder(request.Header, testWrapper{})
			default:
				return encoders.NewJSONEncoder(request.Header, testWrapper{})
			}
		}, func(ctx scene.Context, request *http.Request, encoder scene.ResponseEncoder) {
			encoder.GetWriter().Header().Set("X-Test-Injection", "test")
		})
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		var requestID string
		handler := testHandler{
			call: func(writer http.ResponseWriter, r *http.Request) {
				ctx := scene.GetScene(r.Context())
				require.NotNil(t, ctx)
				_ = scene.GetEncoder(ctx).Encode(nil)
				requestID = scene.GetRequestID(ctx)
			},
		}
		middleware.Next(handler)
		parsedURL, err := url.Parse("https://www.google.com/search")
		require.NoError(t, err)
		req := &http.Request{
			URL:    parsedURL,
			Method: http.MethodPost,
			Header: http.Header{
				"Content-Type":    []string{"application/json"},
				"Accept-Encoding": []string{"gzip"},
			},
		}
		middleware.ServeHTTP(recorder, req)
		require.Equal(t, http.StatusOK, recorder.Code)
		require.Equal(t, "test", recorder.Header().Get("X-Test-Injection"))
		require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
		r, err := gzip.NewReader(recorder.Body)
		require.NoError(t, err)
		data, err := io.ReadAll(r)
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf(`{"data":null,"metadata":{"errors":null,"requestId":"%v","statusCode":200}}`, requestID), strings.TrimSpace(string(data)))
	})

}
