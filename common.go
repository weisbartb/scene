package scene

import (
	"context"
	"time"

	"github.com/pkg/errors"
)

type RequestIDKey struct{}
type BaseContextKey struct{}

func GetRequestID(ctx context.Context) string {
	id := ctx.Value(RequestIDKey{})
	if id == nil {
		return ""
	}
	return id.(string)
}

func GetBaseContext(ctx context.Context) Context {
	return ctx.Value(BaseContextKey{}).(Context)
}

const NoTTL = time.Duration(0)

var RunForever = time.Time{}
var ErrTimeout = errors.New("request timed out")
var ErrComplete = errors.New("request marked complete")
var ErrClosed = errors.New("request factory shutdown triggered, all contexts should be closed")
