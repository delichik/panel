package facilityapps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFacilityAssetDownloadHandlersUseCommittedSourceAndSessionBlob(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	asset, err := svc.UploadStaticAsset(ctx, StaticAssetUploadInput{Name: "site", Kind: StaticSourceUploadedFile, FileName: "index.html", Content: []byte("committed")})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, func(next http.Handler) http.Handler { return next })

	committed := httptest.NewRecorder()
	mux.ServeHTTP(committed, httptest.NewRequest(http.MethodGet, "/api/v1/facility-apps/reverse-proxy/static-assets/"+asset.ID+"/content", nil))
	if committed.Code != http.StatusOK || committed.Body.String() != "committed" {
		t.Fatalf("committed status=%d body=%q", committed.Code, committed.Body.String())
	}

	session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{})
	if err != nil {
		t.Fatal(err)
	}
	source := httptest.NewRecorder()
	mux.ServeHTTP(source, httptest.NewRequest(http.MethodGet, "/api/v1/facility-apps/reverse-proxy/edit-sessions/"+session.ID+"/assets/"+asset.ID+"/content", nil))
	if source.Code != http.StatusOK || source.Body.String() != "committed" {
		t.Fatalf("source status=%d body=%q", source.Code, source.Body.String())
	}

	session, err = svc.PutFacilityEditAsset(ctx, session.ID, asset.ID, "replace", FacilityEditAssetInput{Revision: session.Revision, ClientOperationID: "replace", Name: "site", Kind: StaticSourceUploadedFile, FileName: "index.html", Content: []byte("replacement")})
	if err != nil {
		t.Fatal(err)
	}
	replacement := httptest.NewRecorder()
	mux.ServeHTTP(replacement, httptest.NewRequest(http.MethodGet, "/api/v1/facility-apps/reverse-proxy/edit-sessions/"+session.ID+"/assets/"+asset.ID+"/content", nil))
	if replacement.Code != http.StatusOK || replacement.Body.String() != "replacement" {
		t.Fatalf("replacement status=%d body=%q", replacement.Code, replacement.Body.String())
	}
}
