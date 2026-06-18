package overview

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCardsHandlersGetAndUpdate(t *testing.T) {
	svc, closeStore := newCardTestService(t)
	defer closeStore()
	handler := NewHandler(svc)

	getRecorder := httptest.NewRecorder()
	handler.GetCards(getRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/overview/cards", nil))
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("get status = %d", getRecorder.Code)
	}

	body := bytes.NewBufferString(`{"cards":[]}`)
	putRecorder := httptest.NewRecorder()
	handler.UpdateCards(putRecorder, httptest.NewRequest(http.MethodPut, "/api/v1/overview/cards", body))
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("put status = %d body=%s", putRecorder.Code, putRecorder.Body.String())
	}
	var response struct {
		Data CardConfigurationSet `json:"data"`
	}
	if err := json.NewDecoder(putRecorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Cards == nil || len(response.Data.Cards) != 0 {
		t.Fatalf("unexpected response: %#v", response.Data)
	}
}

func serveOverviewRoute(handler *Handler, method, target string, body *bytes.Buffer) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, func(next http.Handler) http.Handler { return next })
	rec := httptest.NewRecorder()
	if body == nil {
		mux.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
		return rec
	}
	mux.ServeHTTP(rec, httptest.NewRequest(method, target, body))
	return rec
}

func TestUpdateCardsHandlerRejectsInvalidCard(t *testing.T) {
	svc, closeStore := newCardTestService(t)
	defer closeStore()

	body := bytes.NewBufferString(`{"cards":[{"id":"x","kind":"cpu","width":9,"height":2,"range":"1h","networkDirection":"both","serverIds":[]}]}`)
	recorder := httptest.NewRecorder()
	NewHandler(svc).UpdateCards(recorder, httptest.NewRequest(http.MethodPut, "/api/v1/overview/cards", body))
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestGetCardDataHandler(t *testing.T) {
	svc, _, closeStore := newCardDataTestService(t)
	defer closeStore()
	if _, err := svc.UpdateCards(httptest.NewRequest(http.MethodGet, "/", nil).Context(), CardConfigurationSet{Cards: []CardConfiguration{{
		ID:               "card-cpu",
		Kind:             CardKindCPU,
		Width:            3,
		Height:           2,
		Range:            "1h",
		NetworkDirection: "both",
		ServerIDs:        []string{},
	}}}); err != nil {
		t.Fatalf("update cards: %v", err)
	}

	recorder := serveOverviewRoute(NewHandler(svc), http.MethodGet, "/api/v1/overview/cards/card-cpu/data", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Data CardData `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Card.ID != "card-cpu" || len(response.Data.MetricsByServer) == 0 {
		t.Fatalf("unexpected response: %#v", response.Data)
	}
}
