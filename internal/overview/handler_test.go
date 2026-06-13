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
