package wirtuald

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/coder/coder/v2/wirtuald/httpapi"
	"github.com/coder/coder/v2/wirtualsdk"
)

// parsePagination extracts pagination query params from the http request.
// If an error is encountered, the error is written to w and ok is set to false.
func parsePagination(w http.ResponseWriter, r *http.Request) (p wirtualsdk.Pagination, ok bool) {
	ctx := r.Context()
	queryParams := r.URL.Query()
	parser := httpapi.NewQueryParamParser()
	params := wirtualsdk.Pagination{
		AfterID: parser.UUID(queryParams, uuid.Nil, "after_id"),
		// A limit of 0 should be interpreted by the SQL query as "null" or
		// "no limit". Do not make this value anything besides 0.
		Limit:  int(parser.PositiveInt32(queryParams, 0, "limit")),
		Offset: int(parser.PositiveInt32(queryParams, 0, "offset")),
	}
	if len(parser.Errors) > 0 {
		httpapi.Write(ctx, w, http.StatusBadRequest, wirtualsdk.Response{
			Message:     "Query parameters have invalid values.",
			Validations: parser.Errors,
		})
		return params, false
	}

	return params, true
}
