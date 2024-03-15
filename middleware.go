package scene

import (
	"errors"
	"net/http"
)

type CtxHTTPHeaderKey struct{}
type CtxHTTPEncoder struct{}

var ErrEncoderProviderRequired = errors.New("encoder provider must return an encoder")
var ErrOnRequestIsRequired = errors.New("onRequest is a required function, even if its empty")

// NewHTTPMiddleware creates a new middleware handler for a given factory.
//
//	encoderProvider should return a pointer to a new encoder instance for that request.
//		Note: Ctx may be nil in error cases.
//	onRequestHook allows you to hook the request before it starts to serve anything from the middleware.
//		Note: The hook can nil if it's not used.
func NewHTTPMiddleware(factory *Factory, encoderProvider EncoderProvider, onRequestHook RequestHook) (*HTTPMiddleware, error) {
	if encoderProvider == nil {
		return nil, ErrEncoderProviderRequired
	}
	if onRequestHook == nil {
		return nil, ErrOnRequestIsRequired
	}
	return &HTTPMiddleware{
		factory:         factory,
		onRequestHook:   onRequestHook,
		encoderProvider: encoderProvider,
	}, nil
}

// EncoderProvider allows for the correct encoder to be returned based on the provided http request.
// This is useful for any systems that may need variable responses (including filters like gzip)
type EncoderProvider func(ctx Context, request *http.Request) ResponseEncoder

// RequestHook allow modification of the encoder (including the ability to modify its writer)
type RequestHook func(ctx Context, request *http.Request, encoder ResponseEncoder)
type ResponseEncoder interface {
	GetWriter() http.ResponseWriter
	SetWriter(ctx Context, w http.ResponseWriter) // Pointer receiver
	AddError(err error, statusCode int)           // Pointer receiver
	Encode(obj any) error
}

type HTTPMiddleware struct {
	factory         *Factory
	encoderProvider EncoderProvider
	onRequestHook   RequestHook
	next            []http.Handler
}

type capturingWriter struct {
	http.ResponseWriter
	statusCode int
}

func (cw *capturingWriter) SetStatusCode(status int) {
	cw.statusCode = status
}

func (cw *capturingWriter) WriteHeader(statusCode int) {
	cw.ResponseWriter.WriteHeader(statusCode)
	cw.statusCode = statusCode
}

// ServeHTTP adds the mux handler for the go built-in http server to serve requests. It will invoke the next item
// in the given chain when provided. Contexts will complete if they were not already completed/closed.
// Any error in the chain will cause the chain to terminate.
// Errors are defined as anything that sets a status code on the response writer >= 400.
// HTTP redirects will also cause a termination of the chain.
func (c HTTPMiddleware) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	newCtx, err := c.factory.Wrap(request.Context())
	if err != nil {
		// handle what is generally a transient error from a server shutdown/restart
		writer.WriteHeader(503)
		writer.Header().Add("Retry-After", "10")
		encoder := c.encoderProvider(nil, request)
		encoder.AddError(errors.New("service temporarily unavailable"), 503)
		_ = encoder.Encode(encoder)
		return
	}
	*request = *request.WithContext(newCtx)
	newCtx.Store(CtxHTTPHeaderKey{}, request.Header)
	out := c.encoderProvider(newCtx, request)
	captureWriter := &capturingWriter{ResponseWriter: writer}
	captureWriter.Header().Add("X-Request-ID", newCtx.Value(RequestIDKey{}).(string))
	// Set the encoder to the correct output
	out.SetWriter(newCtx, captureWriter)
	newCtx.Store(CtxHTTPEncoder{}, out)
	if c.onRequestHook != nil {
		c.onRequestHook(newCtx, request, out)
	}
	for _, handler := range c.next {
		handler.ServeHTTP(captureWriter, request)
		if captureWriter.statusCode >= 300 {
			break
		}
	}
	newCtx.Complete()
}

// Next adds a new handler to run in sequence after this one fires.
//
//	Any 400+ status code to the writer will stop the chain.
func (c *HTTPMiddleware) Next(handler http.Handler) {
	c.next = append(c.next, handler)
}

type emptyWriter struct {
}

func (e emptyWriter) Header() http.Header {
	return map[string][]string{}
}

func (e emptyWriter) Write(bytes []byte) (int, error) {
	return 0, nil
}

func (e emptyWriter) WriteHeader(statusCode int) {}

type emptyEncoder struct {
}

func (e emptyEncoder) GetWriter() http.ResponseWriter {
	return emptyWriter{}
}

func (e emptyEncoder) SetWriter(ctx Context, w http.ResponseWriter) {
}

func (e emptyEncoder) AddError(err error, statusCode int) {}

func (e emptyEncoder) Encode(obj any) error {
	return nil
}

func GetEncoder(ctx Context) ResponseEncoder {
	val := ctx.Value(CtxHTTPEncoder{})
	if val == nil {
		return emptyEncoder{}
	}
	return val.(ResponseEncoder)
}
