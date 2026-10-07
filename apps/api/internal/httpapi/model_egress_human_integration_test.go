package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelEgressHumanHTTPNativeRegisteredLifecycleAndNoDispatch(t *testing.T) {
	f := modelEgressHTTPNative(t)
	h := privateProfileHTTPNew(f.store, f.store)
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, privateProfileHTTPRequest("GET", modelEgressHTTPBase+path, "", f.tokens[0], ""))
		return w
	}
	w := get("/options")
	if w.Code != 200 || !strings.Contains(w.Body.String(), f.task.ID) || !strings.Contains(w.Body.String(), f.price.Version) || strings.Contains(w.Body.String(), "PRIVATE_CONVERSATION_NOT_EGRESS") {
		t.Fatal("native options", w.Code, w.Body.String())
	}
	p := f.preview(t)
	path := "/previews/" + p.ID
	w = get(path)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var envelope struct {
		Data humanEgressReceiptWire `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &envelope); e != nil || envelope.Data.Review == nil || envelope.Data.Review.RequestDigest != p.RequestDigest || !envelope.Data.Approvable {
		t.Fatal("exact receipt", e, w.Body.String())
	}
	body := `{"previewId":"` + p.ID + `","requestDigest":"` + p.RequestDigest + `"}`
	w = httptest.NewRecorder()
	h.ServeHTTP(w, privateProfileHTTPRequest("POST", modelEgressHTTPBase+"/approvals", body, f.tokens[0], "application/json"))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = get(path)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"APPROVED"`) {
		t.Fatal("recover lost approval result", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, privateProfileHTTPRequest("DELETE", modelEgressHTTPBase+path, "", f.tokens[0], ""))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = get(path)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"REVOKED"`) || strings.Contains(w.Body.String(), `"review":`) {
		t.Fatal("revoked body or bad recovery", w.Code, w.Body.String())
	}
	w = get("/previews")
	if w.Code != 200 || strings.Contains(w.Body.String(), `"review":`) {
		t.Fatal("history exposed body", w.Code, w.Body.String())
	}
	var reservations int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM model_budget_reservations WHERE owner_id=$1`, f.accountIDs[0]).Scan(&reservations); e != nil || reservations != 0 {
		t.Fatal("human UI dispatched", e, reservations)
	}
}
