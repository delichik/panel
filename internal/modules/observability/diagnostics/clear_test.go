package diagnostics

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClearRuntimeDataRequiresExactConfirmation(t *testing.T) {
	called := false
	service := NewService()
	service.SetClearRuntimeDataHook(func(context.Context) (ClearRuntimeDataResult, error) {
		called = true
		return ClearRuntimeDataResult{Cleared: true}, nil
	})
	handler := NewHandler(service)
	for _, body := range []string{`{}`, `{"confirmation":"clear"}`, `{"confirmation":"CLEAR RUNTIME DATA","extra":true}`} {
		recorder := httptest.NewRecorder()
		handler.ClearRuntimeData(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/debug/clear-runtime-data", bytes.NewBufferString(body)))
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Fatalf("body %s status=%d", body, recorder.Code)
		}
	}
	if called {
		t.Fatal("clear hook called without exact confirmation")
	}

	recorder := httptest.NewRecorder()
	handler.ClearRuntimeData(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/debug/clear-runtime-data", bytes.NewBufferString(`{"confirmation":"CLEAR RUNTIME DATA"}`)))
	if recorder.Code != http.StatusOK || !called {
		t.Fatalf("confirmed clear status=%d called=%v body=%s", recorder.Code, called, recorder.Body.String())
	}
}
