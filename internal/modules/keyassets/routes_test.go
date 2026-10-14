package keyassets

import (
	"net/http"
	"testing"

	httpx "panel/internal/platform/http"
)

func TestRegisterRoutesDoesNotPanic(t *testing.T) {
	mux := http.NewServeMux()
	handler := NewHandler(nil)
	auth := func(next http.Handler) http.Handler {
		return next
	}

	handler.RegisterRoutes(mux, httpx.Middleware(auth))
}

func TestKeyAssetDownloadRoute(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantKind   keyAssetDownloadKind
		wantFirst  string
		wantSecond string
	}{
		{
			name:       "asset file",
			path:       "asset-1/files/private-key",
			wantKind:   keyAssetFileDownload,
			wantFirst:  "asset-1",
			wantSecond: "private-key",
		},
		{
			name:      "export archive",
			path:      "exports/task-1/download",
			wantKind:  keyAssetExportDownload,
			wantFirst: "task-1",
		},
		{
			name:     "unrecognized path",
			path:     "exports/task-1/files",
			wantKind: keyAssetDownloadNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotKind, gotFirst, gotSecond := keyAssetDownloadRoute(test.path)
			if gotKind != test.wantKind || gotFirst != test.wantFirst || gotSecond != test.wantSecond {
				t.Fatalf(
					"keyAssetDownloadRoute(%q) = (%d, %q, %q), want (%d, %q, %q)",
					test.path,
					gotKind,
					gotFirst,
					gotSecond,
					test.wantKind,
					test.wantFirst,
					test.wantSecond,
				)
			}
		})
	}
}
