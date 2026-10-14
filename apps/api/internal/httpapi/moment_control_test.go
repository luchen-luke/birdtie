package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type momentControlHTTPSpy struct {
	hcSpy
	content.MomentStore
	input           content.MomentControlInput
	committed       content.MomentControlReceipt
	loseAfterCommit bool
}

func (p *momentControlHTTPSpy) Authenticate(context.Context, [32]byte) (identity.Actor, error) {
	return identity.Actor{ID: hcTestID, AccountType: "person"}, nil
}

func (p *momentControlHTTPSpy) PreviewMomentControl(_ context.Context, _ [32]byte, _ identity.Actor, in content.MomentControlInput) (content.MomentControlPreview, error) {
	p.input = in
	return content.MomentControlPreview{HumanControlPreview: p.base(in.OperationID, in.Action, content.MomentControlDigest(in)), MomentID: in.MomentID, ExpectedRevision: in.ExpectedRevision, Input: in.Input, InputSHA256: content.MomentControlInputHash(in.Input), SourceVersion: strings.Repeat("a", 64)}, nil
}
func (p *momentControlHTTPSpy) CommitMomentControl(_ context.Context, _ [32]byte, _ identity.Actor, in content.MomentControlInput) (content.MomentControlReceipt, error) {
	p.writes++
	expected := in.ExpectedRevision
	id := in.MomentID
	if id == "" {
		id = hcTestID
	}
	at := time.Now()
	r := content.MomentControlReceipt{OperationID: in.OperationID, Status: "SUCCEEDED", Action: in.Action, MomentID: id, ExpectedRevision: &expected, AppliedRevision: expected + 1, PayloadDigest: content.MomentControlDigest(in), CommittedAt: &at}
	p.committed = r
	if p.loseAfterCommit {
		p.final = identity.ErrUnauthorized
	}
	return r, nil
}
func (p *momentControlHTTPSpy) ReadMomentControlOperation(_ context.Context, _ [32]byte, _ identity.Actor, op string) (content.MomentControlReceipt, error) {
	p.reads++
	if p.missing {
		return content.MomentControlReceipt{}, content.ErrNotFound
	}
	return p.committed, nil
}
func (p *momentControlHTTPSpy) StopMomentControlOperation(context.Context, [32]byte, identity.Actor, string) (content.MomentControlReceipt, error) {
	p.stops++
	return content.MomentControlReceipt{OperationID: hcTestOp, Status: "STOPPED"}, nil
}
func momentControlHTTPHandler(p *momentControlHTTPSpy) http.Handler {
	return New(nil, p, nil, p, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
}
func momentControlHTTPCall(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := hcRequest(method, path, body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func momentControlHTTPInput() content.MomentControlInput {
	return content.MomentControlInput{OperationID: hcTestOp, Action: "CREATE", Input: &content.MomentInput{CityID: "aberdeen-gb", Title: "私有标题不得进入回执", Body: "私人正文", TimePrecision: "unknown", LocationPrecision: "city"}}
}
func TestMomentControlHTTPConcretePreviewUnknownAndOriginalHistory(t *testing.T) {
	p := &momentControlHTTPSpy{}
	h := momentControlHTTPHandler(p)
	in := momentControlHTTPInput()
	raw, _ := json.Marshal(in)
	w := momentControlHTTPCall(h, "POST", "/v1/me/moment-controls/previews", string(raw))
	if w.Code != 200 || p.writes != 0 || p.proofs != 1 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("preview effect or absent exact proof", w.Code, w.Body.String())
	}
	in.SourceVersion = strings.Repeat("a", 64)
	in.Approval = "EXACT_INPUT"
	raw, _ = json.Marshal(in)
	p.loseAfterCommit = true
	path := "/v1/me/moment-controls/operations/" + hcTestOp
	w = momentControlHTTPCall(h, "PUT", path, string(raw))
	if w.Code != 401 || p.writes != 1 || strings.Contains(w.Body.String(), "SUCCEEDED") {
		t.Fatal("commit final frame must leave UNKNOWN", w.Code)
	}
	p.final = nil
	p.lost = true
	w = momentControlHTTPCall(h, "GET", path, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "私有标题") || strings.Contains(w.Body.String(), "私人正文") || p.writes != 1 {
		t.Fatal("original fact replaced by current state", w.Code, w.Body.String())
	}
	p.missing = true
	w = momentControlHTTPCall(h, "GET", path, "")
	if w.Code != 404 || p.writes != 1 {
		t.Fatal("404 guessed zero or retried")
	}
	w = momentControlHTTPCall(h, "POST", path+"/stop", "")
	if w.Code != 200 || p.stops != 1 || strings.Contains(w.Body.String(), "momentId") {
		t.Fatal("bodyless zero-effect stop", w.Code, w.Body.String())
	}
	w = momentControlHTTPCall(h, "POST", path+"/stop", `{}`)
	if w.Code != 400 || p.stops != 1 {
		t.Fatal("stop accepts speculative input")
	}
}
func TestMomentControlHTTPRegisteredRawWritesAndClosedAuthority(t *testing.T) {
	p := &momentControlHTTPSpy{}
	h := momentControlHTTPHandler(p)
	for _, route := range []struct{ method, path, body string }{{"POST", "/v1/me/moments", historicalMomentBody("unknown", "", 0)}, {"PUT", "/v1/me/moments/" + hcTestID, historicalMomentBody("unknown", "", 1)}, {"DELETE", "/v1/me/moments/" + hcTestID + "?revision=1", ""}} {
		t.Run(route.method, func(t *testing.T) {
			w := momentControlHTTPCall(h, route.method, route.path, route.body)
			if w.Code != 409 || p.writes != 0 {
				t.Fatal("legacy route bypassed concrete approval", w.Code)
			}
		})
	}
	in := momentControlHTTPInput()
	raw, _ := json.Marshal(in)
	for _, body := range []string{strings.TrimSuffix(string(raw), "}") + `,"actorId":"other"}`, strings.TrimSuffix(string(raw), "}") + `,"approval":"client"}`, strings.Replace(string(raw), `"action":"CREATE"`, `"action":"PUBLISH"`, 1)} {
		w := momentControlHTTPCall(h, "POST", "/v1/me/moment-controls/previews", body)
		if w.Code != 400 || p.writes != 0 {
			t.Fatal("authority input reached writer", w.Code)
		}
	}
	req := hcRequest("POST", "/v1/me/moment-controls/previews", string(raw))
	req.Header.Set("X-Birdtie-Organization-Workspace", hcTestID)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("borrowed organization authority", w.Code)
	}
}
