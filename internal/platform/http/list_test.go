package httpx

import (
	"net/http/httptest"
	"testing"
)

func TestParseListPage(t *testing.T) {
	tests := []struct {
		name         string
		url          string
		allowed      []string
		wantPage     int
		wantPageSize int
		wantErr      bool
	}{
		{name: "defaults", url: "/items", wantPage: 1, wantPageSize: DefaultPageSize},
		{name: "values", url: "/items?page=2&pageSize=25&q=test", allowed: []string{"q"}, wantPage: 2, wantPageSize: 25},
		{name: "unknown", url: "/items?extra=true", wantErr: true},
		{name: "legacy limit", url: "/items?limit=10", wantErr: true},
		{name: "invalid page", url: "/items?page=0", wantErr: true},
		{name: "oversized", url: "/items?pageSize=201", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", tt.url, nil)
			page, pageSize, err := ParseListPage(request, tt.allowed...)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if page != tt.wantPage || pageSize != tt.wantPageSize {
				t.Fatalf("got page=%d pageSize=%d", page, pageSize)
			}
		})
	}
}
