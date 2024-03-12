package encoders

type ResponseWrapper interface {
	AddError(err error, statusCode int)
	Wrap(obj any) any
	New() ResponseWrapper
}
