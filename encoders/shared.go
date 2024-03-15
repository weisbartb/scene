package encoders

import "net/http"

type ResponseGenerator interface {
	// New should return a new instance of the response wrapper
	New() ResponseWrapper
}

type ResponseWrapper interface {
	// AddError should set the error state for the response wrapper.
	AddError(err error, statusCode int)
	GetStatusCode() int
	// Wrap should wrap the core response in the response wrapper.
	Wrap(writer http.ResponseWriter, obj any) any
}
