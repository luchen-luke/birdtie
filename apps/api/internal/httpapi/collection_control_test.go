package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type collectionControlHTTPSpy struct {
	collectionHTTPSpy
	controlCalls, finalCalls int
	input                    content.CollectionControlInput
	proof                    identity.HumanControlProof
	afterControl             func()
	receipt                  content.CollectionControlReceipt
	failure, errorTail       error
}

func (s *collectionControlHTTPSpy) controlHit(d [32]byte, a identity.Actor) {
	s.controlCalls++
	s.digest = d
	s.actor = a
	if s.afterControl != nil {
		s.afterControl()
	}
}
func (s *collectionControlHTTPSpy) PreviewHumanCollectionControl(_ context.Context, d [32]byte, a identity.Actor, in content.CollectionControlInput) (content.CollectionControlPreview, error) {
	s.controlHit(d, a)
	s.input = in
	now := time.Now().UTC()
	return content.CollectionControlPreview{HumanControlPreview: identity.HumanControlPreview{OperationID: in.OperationID, Action: in.Action, PayloadDigest: content.CollectionControlDigest(in), ObservedAt: now, ExpiresAt: now.Add(20 * time.Second), Approval: "closed-synthetic-approval", ResponseProof: s.proof}, SchemaVersion: content.CollectionControlSchemaVersion, Kind: in.Kind, CollectionID: in.CollectionID, ExpectedRevision: in.ExpectedRevision, NormalizedInput: in.Input}, s.failure
}
func (s *collectionControlHTTPSpy) ApplyHumanCollectionControl(_ context.Context, d [32]byte, a identity.Actor, in content.CollectionControlSubmission) (content.CollectionControlReceipt, error) {
	s.controlHit(d, a)
	s.input = in.CollectionControlInput
	return s.receipt, s.failure
}
func (s *collectionControlHTTPSpy) ReadHumanCollectionControl(_ context.Context, d [32]byte, a identity.Actor, _ string) (content.CollectionControlReceipt, error) {
	s.controlHit(d, a)
	return s.receipt, s.failure
}
func (s *collectionControlHTTPSpy) StopHumanCollectionControl(_ context.Context, d [32]byte, a identity.Actor, _ string) (content.CollectionControlReceipt, error) {
	s.controlHit(d, a)
	return s.receipt, s.failure
}
func (s *collectionControlHTTPSpy) FinalizeHumanCollectionControlReceipt(context.Context, [32]byte, identity.Actor, content.CollectionControlReceipt) error {
	s.finalCalls++
	return s.errorTail
}

const collectionControlHTTPop = "14700000-0000-4000-8000-000000000001"
const collectionControlHTTPtarget = "14700000-0000-4000-8000-000000000002"

func collectionControlHTTPFixture(t *testing.T) (http.Handler, *collectionControlHTTPSpy, *humanMomentAccessSpy, string, content.CollectionControlInput) {
	t.Helper()
	token, _, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	a := &humanMomentAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}
	in := content.CollectionControlInput{SchemaVersion: content.CollectionControlSchemaVersion, OperationID: collectionControlHTTPop, Action: "UPDATE", Kind: "journey", CollectionID: collectionControlHTTPtarget, ExpectedRevision: 1, Input: &content.CollectionControlBody{Kind: "journey", CityID: "aberdeen-gb", Title: "本人具体修改稿", Summary: "", Entries: []content.CollectionControlEntry{{PlaceID: imageMomentID, Note: "私人说明", LocationVisibility: "hidden"}}}}
	proof := humanControlFunctionProof(func(context.Context) error { return nil })
	now := time.Now().UTC()
	rev := int64(1)
	spy := &collectionControlHTTPSpy{proof: proof, receipt: content.CollectionControlReceipt{SchemaVersion: content.CollectionControlSchemaVersion, OperationID: in.OperationID, Status: "SUCCEEDED", Action: in.Action, Kind: in.Kind, CollectionID: in.CollectionID, ExpectedRevision: &rev, AppliedRevision: 2, PayloadDigest: content.CollectionControlDigest(in), CommittedAt: &now, ResponseProof: proof}}
	return collectionTestHandler(a, spy), spy, a, token, in
}
func collectionControlHTTPCall(h http.Handler, token, method, path, body, org string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	if org != "" {
		r.Header.Set("X-Birdtie-Organization-Workspace", org)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestCollectionControlHTTPConcretePreviewOriginalReceiptAndRawGuards(t *testing.T) {
	h, s, a, token, in := collectionControlHTTPFixture(t)
	raw, _ := json.Marshal(in)
	w := collectionControlHTTPCall(h, token, "POST", "/v1/me/personal-collection-controls/preview", string(raw), "")
	if w.Code != 200 || s.actor != a.actor || s.digest != a.seen || s.input.CollectionID != collectionControlHTTPtarget || !strings.Contains(w.Body.String(), `"normalizedInput"`) {
		t.Fatal("concrete original-person preview", w.Code, w.Body.String())
	}
	raw, _ = json.Marshal(content.CollectionControlSubmission{CollectionControlInput: in, Approval: "closed-synthetic-approval"})
	w = collectionControlHTTPCall(h, token, "PUT", "/v1/me/personal-collection-controls/operations/"+in.OperationID, string(raw), "")
	if w.Code != 200 || s.finalCalls != 1 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "私人说明") || strings.Contains(w.Body.String(), "ResponseProof") {
		t.Fatal("immutable tiny receipt/private projection", w.Code, w.Body.String())
	}
	for _, method := range []string{"PUT", "DELETE"} {
		w = collectionControlHTTPCall(h, token, method, "/v1/me/personal-collections/journey/"+in.CollectionID, "{}", "")
		if w.Code != 409 || s.collectionHTTPSpy.calls != 0 {
			t.Fatal("old raw effect bypassed operation", w.Code)
		}
	}
	for _, endpoint := range []struct{ method, path string }{{"GET", "/v1/me/personal-collection-controls/operations/" + in.OperationID}, {"POST", "/v1/me/personal-collection-controls/operations/" + in.OperationID + "/stop"}} {
		w = collectionControlHTTPCall(h, token, endpoint.method, endpoint.path, "", "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"SUCCEEDED"`) {
			t.Fatal("original fact was erased by history/stop", w.Code)
		}
	}
}
func TestCollectionControlHTTPClosedSourceSelectorsBodiesAndNamespace(t *testing.T) {
	for _, which := range []string{"query", "duplicate", "owner", "projection", "different-op", "org", "stop-body"} {
		h, s, _, token, in := collectionControlHTTPFixture(t)
		raw, _ := json.Marshal(in)
		method := "POST"
		path := "/v1/me/personal-collection-controls/preview"
		body := string(raw)
		org := ""
		switch which {
		case "query":
			path += "?ownerId=foreign"
		case "duplicate":
			body = strings.Replace(body, `"action":"UPDATE"`, `"action":"UPDATE","action":"WITHDRAW"`, 1)
		case "owner":
			body = strings.TrimSuffix(body, "}") + `,"ownerAccountId":"foreign"}`
		case "projection":
			body = strings.Replace(body, `"note":"私人说明"`, `"note":"私人说明","available":true`, 1)
		case "different-op":
			method = "PUT"
			path = "/v1/me/personal-collection-controls/operations/14700000-0000-4000-8000-000000000099"
			raw, _ = json.Marshal(content.CollectionControlSubmission{CollectionControlInput: in, Approval: "synthetic"})
			body = string(raw)
		case "org":
			org = imageMomentID
		case "stop-body":
			path = "/v1/me/personal-collection-controls/operations/" + in.OperationID + "/stop"
			body = "{}"
		}
		w := collectionControlHTTPCall(h, token, method, path, body, org)
		if (w.Code != 400 && w.Code != 403) || s.controlCalls != 0 {
			t.Fatal("foreign/malformed authority reached native port", which, w.Code)
		}
	}
}
func TestCollectionControlHTTPTombstoneAndLateOrMalformedReceiptLeakNoFact(t *testing.T) {
	for _, which := range []string{"stop", "proof", "tail", "wrong-id", "missing-version", "no-proof"} {
		h, s, _, token, in := collectionControlHTTPFixture(t)
		want := 503
		switch which {
		case "stop":
			s.receipt = content.CollectionControlReceipt{SchemaVersion: content.CollectionControlSchemaVersion, OperationID: in.OperationID, Status: "STOPPED", ResponseProof: s.proof}
			want = 200
		case "proof":
			s.receipt.ResponseProof = humanControlFunctionProof(func(context.Context) error { return identity.ErrControlChanged })
			want = 409
		case "tail":
			s.errorTail = identity.ErrControlChanged
			want = 409
		case "wrong-id":
			s.receipt.OperationID = collectionControlHTTPtarget
		case "missing-version":
			s.receipt.ExpectedRevision = nil
		case "no-proof":
			s.receipt.ResponseProof = nil
		}
		w := collectionControlHTTPCall(h, token, "GET", "/v1/me/personal-collection-controls/operations/"+in.OperationID, "", "")
		if w.Code != want {
			t.Fatal("tail or malformed fact escaped", which, w.Code, w.Body.String())
		}
		if which == "stop" {
			var body struct {
				Data map[string]any `json:"data"`
			}
			if json.Unmarshal(w.Body.Bytes(), &body) != nil || len(body.Data) != 3 || body.Data["status"] != "STOPPED" {
				t.Fatal("STOP invented a target/digest/result")
			}
		} else if strings.Contains(w.Body.String(), collectionControlHTTPtarget) || strings.Contains(w.Body.String(), `"SUCCEEDED"`) {
			t.Fatal("retired original fact leaked")
		}
	}
}
