package sshaccess

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeHandlerService struct {
	sessionFilter SessionFilter
	attemptFilter AttemptFilter
	sessions      SessionList
	attempts      AttemptList
	err           error
}

func (f *fakeHandlerService) ListSessions(_ context.Context, _ string, filter SessionFilter) (SessionList, error) {
	f.sessionFilter = filter
	return f.sessions, f.err
}

func (f *fakeHandlerService) ListAttempts(_ context.Context, _ string, filter AttemptFilter) (AttemptList, error) {
	f.attemptFilter = filter
	return f.attempts, f.err
}

func TestHandlerSessionsParsesQuery(t *testing.T) {
	svc := &fakeHandlerService{sessions: SessionList{Items: []Session{}, Total: 3, Page: 2, PageSize: 20, Coverage: Coverage{State: "live"}}}
	h := NewHandler(svc)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/servers/srv-1/ssh-access/sessions?state=open&page=2&pageSize=20&user=alice&source=203.0.113.5&q=pts&from=2026-01-02T03:04:05Z",
		nil)
	req.SetPathValue("id", "srv-1")
	rec := httptest.NewRecorder()
	h.Sessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	filter := svc.sessionFilter
	if filter.State != "open" || filter.Page != 2 || filter.PageSize != 20 || filter.User != "alice" ||
		filter.Source != "203.0.113.5" || filter.Query != "pts" {
		t.Fatalf("filter = %+v", filter)
	}
	if !filter.From.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("from = %v", filter.From)
	}
	var envelope struct {
		Data struct {
			Total    int      `json:"total"`
			Coverage Coverage `json:"coverage"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Total != 3 || envelope.Data.Coverage.State != "live" {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestHandlerAttemptsRejectsInvalidQuery(t *testing.T) {
	svc := &fakeHandlerService{attempts: AttemptList{Items: []Attempt{}}}
	h := NewHandler(svc)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/servers/srv-1/ssh-access/attempts?from=yesterday", nil)
	req.SetPathValue("id", "srv-1")
	rec := httptest.NewRecorder()
	h.Attempts(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/servers/srv-1/ssh-access/attempts?page=abc", nil)
	req.SetPathValue("id", "srv-1")
	rec = httptest.NewRecorder()
	h.Attempts(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}
