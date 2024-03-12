package encoders

import (
	"encoding/json"
	"github.com/weisbartb/scene"
)

type jsonEncoder[baseResponse ResponseWrapper] struct {
	w            scene.StatusWriter
	baseResponse ResponseWrapper
}

func NewJSONEncoder[baseResponse ResponseWrapper](wrapper baseResponse) scene.ResponseEncoder {
	return &jsonEncoder[baseResponse]{
		w:            nil,
		baseResponse: wrapper,
	}
}

func (j *jsonEncoder[baseResponse]) GetWriter() scene.StatusWriter {
	return j.w
}

func (j *jsonEncoder[baseResponse]) SetWriter(ctx scene.Context, w scene.StatusWriter) {
	j.w = w
}

func (j *jsonEncoder[baseResponse]) AddError(err error, statusCode int) {
	j.baseResponse.AddError(err, statusCode)
}

func (j *jsonEncoder[baseResponse]) Encode(obj any) error {
	return json.NewEncoder(j.w).Encode(j.baseResponse.Wrap(obj))
}
