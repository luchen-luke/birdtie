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

const copyHTTPOperationID = "22222222-2222-4222-8222-222222222222"
const copyHTTPDestinationID = "33333333-3333-4333-8333-333333333333"

type collectionCopyHTTPSpy struct {
	humanMomentLegacySpy
	calls        int
	generation   int64
	retire       bool
	proofError   error
	historyError error
	input        content.CollectionCopyInput
	action       string
}

func (g *collectionCopyHTTPSpy) CaptureCollectionSession(context.Context, [32]byte, identity.Actor) (content.CollectionSessionFrame, error) {
	if g.generation == 0 {
		g.generation = 1
	}
	return content.CollectionSessionFrame{ID: imageMomentID, Generation: g.generation}, nil
}
func (g *collectionCopyHTTPSpy) ValidateCollectionSession(_ context.Context, _ [32]byte, _ identity.Actor, f content.CollectionSessionFrame) error {
	if f.Generation != g.generation {
		return content.ErrConflict
	}
	return nil
}

type collectionCopyHTTPProof struct{ g *collectionCopyHTTPSpy }

func (p collectionCopyHTTPProof) RevalidateCollection(context.Context) error { return p.g.proofError }
func (g *collectionCopyHTTPSpy) hit(ctx context.Context, d [32]byte, a identity.Actor, action string) {
	g.calls++
	g.action = action
	if _, valid, present := content.OriginalCollectionSession(ctx, d, a); !valid || !present {
		panic("missing first actual Session")
	}
	if g.retire {
		g.generation++
	}
}
func (g *collectionCopyHTTPSpy) PreviewHumanCollectionCopy(ctx context.Context, d [32]byte, a identity.Actor, r content.CollectionRef, in content.CollectionCopyInput) (content.CollectionCopyPreview, error) {
	g.hit(ctx, d, a, "PREVIEW")
	g.input = in
	return content.CollectionCopyPreview{SourceRef: r, OperationID: in.OperationID, PublicationRevision: in.PublicationRevision, GrantVersion: in.GrantVersion, OrderedPlaceIDsSHA256: in.OrderedPlaceIDsSHA256, EntryCount: len(in.OrderedPlaceIDs), Title: in.Title, InputSHA256: content.CollectionCopyDigest(r, in), Approval: "opaque", ExpiresAt: time.Now().Add(30 * time.Second), ResponseProof: collectionCopyHTTPProof{g}}, nil
}
func (g *collectionCopyHTTPSpy) CopyHumanCollection(ctx context.Context, d [32]byte, a identity.Actor, r content.CollectionRef, in content.CollectionCopyInput) (content.CollectionCopyReceipt, error) {
	g.hit(ctx, d, a, "COPY")
	g.input = in
	dest := content.CollectionRef{Kind: r.Kind, ID: copyHTTPDestinationID}
	return content.CollectionCopyReceipt{OperationID: in.OperationID, Status: "SUCCEEDED", ResultRevision: 1, DestinationRef: &dest, SourceRef: &r, PublicationRevision: in.PublicationRevision, GrantVersion: in.GrantVersion, OrderedPlaceIDsSHA256: in.OrderedPlaceIDsSHA256}, nil
}
func (g *collectionCopyHTTPSpy) GetHumanCollectionCopyOperation(ctx context.Context, d [32]byte, a identity.Actor, op string) (content.CollectionCopyReceipt, error) {
	g.hit(ctx, d, a, "GET")
	return content.CollectionCopyReceipt{OperationID: op, Status: "STOPPED"}, g.historyError
}
func (g *collectionCopyHTTPSpy) StopHumanCollectionCopyOperation(ctx context.Context, d [32]byte, a identity.Actor, op string) (content.CollectionCopyReceipt, error) {
	g.hit(ctx, d, a, "STOP")
	return content.CollectionCopyReceipt{OperationID: op, Status: "STOPPED"}, nil
}
func copyHTTPRequest(t *testing.T, g *collectionCopyHTTPSpy, method, path, body, org string) *httptest.ResponseRecorder {
	t.Helper()
	token, _, _ := identity.NewToken()
	access := &humanMomentAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}
	s := &server{access: access, content: g}
	mux := http.NewServeMux()
	s.registerCollectionCopyRoutes(mux)
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	if org != "" {
		r.Header.Set("X-Birdtie-Organization-Workspace", org)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}
func copyHTTPBody(commit bool) string {
	ids := []string{imageMomentID}
	in := content.CollectionCopyInput{OperationID: copyHTTPOperationID, PublicationRevision: 2, GrantVersion: 1, OrderedPlaceIDs: ids, OrderedPlaceIDsSHA256: content.CollectionCopyPlacesDigest(ids), Title: "本人新集合"}
	if commit {
		in.Approval = "opaque"
	}
	b, _ := json.Marshal(in)
	return string(b)
}
func TestCollectionCopyHTTPClosedPreviewAndSeparateRecovery(t *testing.T) {
	base := "/v1/public-collections/theme_map/" + imageMomentID + "/copy"
	for _, c := range []struct {
		name, method, path, body, org string
		want                          int
	}{{"preview", "POST", base + "/preview", copyHTTPBody(false), "", 200}, {"commit", "PUT", base, copyHTTPBody(true), "", 200}, {"originalget", "GET", "/v1/me/personal-collections/copy-operations/" + copyHTTPOperationID + "/receipt", "", "", 200}, {"bodylessstop", "POST", "/v1/me/personal-collections/copy-operations/" + copyHTTPOperationID + "/stop", "", "", 200}, {"duplicate", "POST", base + "/preview", strings.TrimSuffix(copyHTTPBody(false), "}") + `,"title":"changed"}`, "", 400}, {"privatefields", "PUT", base, strings.TrimSuffix(copyHTTPBody(true), "}") + `,"note":"copied author"}`, "", 400}, {"missingapproval", "PUT", base, copyHTTPBody(false), "", 400}, {"orgdenied", "POST", base + "/preview", copyHTTPBody(false), imageMomentID, 403}, {"querydenied", "POST", base + "/preview?q=x", copyHTTPBody(false), "", 400}, {"stopbody", "POST", "/v1/me/personal-collections/copy-operations/" + copyHTTPOperationID + "/stop", `{}`, "", 400}} {
		t.Run(c.name, func(t *testing.T) {
			g := &collectionCopyHTTPSpy{}
			w := copyHTTPRequest(t, g, c.method, c.path, c.body, c.org)
			if w.Code != c.want {
				t.Fatal(w.Code, w.Body.String())
			}
			if c.want == 200 && g.calls != 1 {
				t.Fatal("extra write/recovery call", g.calls)
			}
			if c.want != 200 && g.calls != 0 {
				t.Fatal("invalid request changed domain")
			}
		})
	}
}
func TestCollectionCopyHTTPFinalOriginalFrameAndSourceProof(t *testing.T) {
	path := "/v1/public-collections/theme_map/" + imageMomentID + "/copy/preview"
	for _, c := range []struct {
		name   string
		retire bool
		err    error
	}{{"SessionABA", true, nil}, {"sourceRetired", false, content.ErrConflict}} {
		t.Run(c.name, func(t *testing.T) {
			g := &collectionCopyHTTPSpy{retire: c.retire, proofError: c.err}
			w := copyHTTPRequest(t, g, "POST", path, copyHTTPBody(false), "")
			if w.Code != 409 || strings.Contains(w.Body.String(), "approval") || strings.Contains(w.Body.String(), "本人新集合") {
				t.Fatal("late approval leaked", w.Code, w.Body.String())
			}
		})
	}
	g := &collectionCopyHTTPSpy{historyError: content.ErrNotFound}
	w := copyHTTPRequest(t, g, "GET", "/v1/me/personal-collections/copy-operations/"+copyHTTPOperationID+"/receipt", "", "")
	if w.Code != 404 || g.action != "GET" || g.calls != 1 {
		t.Fatal("404 inferred or rewrote", w.Code, g.action)
	}
}
