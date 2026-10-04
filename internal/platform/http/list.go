package httpx

import (
	"net/http"
	"strconv"

	panelerr "panel/internal/platform/errors"
)

const (
	DefaultPageSize = 50
	MaxPageSize     = 200
	// MaxPage caps page so (page-1)*pageSize cannot overflow or force
	// unbounded scans of the underlying table.
	MaxPage = 10000
)

type ListPage[T any] struct {
	Items    []T `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
}

func ParseListPage(r *http.Request, allowed ...string) (page, pageSize int, err error) {
	known := map[string]struct{}{"page": {}, "pageSize": {}}
	for _, key := range allowed {
		known[key] = struct{}{}
	}
	for key := range r.URL.Query() {
		if _, ok := known[key]; !ok {
			return 0, 0, panelerr.BadRequest("query_parameter_unknown", "Unknown query parameter: "+key)
		}
	}
	page, pageSize = 1, DefaultPageSize
	if raw := r.URL.Query().Get("page"); raw != "" {
		page, err = strconv.Atoi(raw)
		if err != nil || page < 1 || page > MaxPage {
			return 0, 0, panelerr.BadRequest("page_invalid", "page must be between 1 and 10000")
		}
	}
	if raw := r.URL.Query().Get("pageSize"); raw != "" {
		pageSize, err = strconv.Atoi(raw)
		if err != nil || pageSize < 1 || pageSize > MaxPageSize {
			return 0, 0, panelerr.BadRequest("page_size_invalid", "pageSize must be between 1 and 200")
		}
	}
	return page, pageSize, nil
}
