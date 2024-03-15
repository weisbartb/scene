package encoders

import (
	"compress/gzip"
	"encoding/json"
	"github.com/weisbartb/scene"
	"io"
	"net/http"
	"strings"
)

type jsonEncoder struct {
	w            http.ResponseWriter
	baseResponse ResponseWrapper
	gzip         bool
}

func NewJSONEncoder(reqHeaders http.Header, generator ResponseGenerator) scene.ResponseEncoder {
	wrapper := generator.New()
	return &jsonEncoder{
		w:            nil,
		baseResponse: wrapper,
		gzip:         strings.Contains(reqHeaders.Get("Accept-Encoding"), "gzip"),
	}
}

func (j *jsonEncoder) GetWriter() http.ResponseWriter {
	return j.w
}

func (j *jsonEncoder) SetWriter(ctx scene.Context, w http.ResponseWriter) {
	j.w = w
}

func (j *jsonEncoder) AddError(err error, statusCode int) {
	j.baseResponse.AddError(err, statusCode)
}

func (j *jsonEncoder) Encode(obj any) error {
	var w io.Writer = j.w
	if j.gzip {
		j.w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(j.w)
		w = gz
		defer gz.Close()
	}
	j.w.Header().Set("Content-Type", "application/json")
	j.w.WriteHeader(j.baseResponse.GetStatusCode())
	return json.NewEncoder(w).Encode(j.baseResponse.Wrap(j.w, obj))
}
