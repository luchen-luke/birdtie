package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/momentmedia"
	"strings"
	"testing"
	"time"
)

type editingThreadHTTPSpy struct {
	momentControlHTTPSpy
	original content.MomentThreadReceipt
}

func (p *editingThreadHTTPSpy) PreviewMomentThread(_ context.Context, _ [32]byte, _ identity.Actor, in content.MomentThreadInput) (content.MomentThreadPreview, error) {
	preserved := []*momentmedia.Set{nil}
	return content.MomentThreadPreview{HumanControlPreview: p.base(in.OperationID, in.Action, content.MomentThreadDigest(in)), Input: in, SourceVersion: strings.Repeat("a", 64), Members: []content.MomentThreadMember{}, Materials: [][]content.MomentThreadMaterial{{}}, Contexts: []content.MomentThreadContext{{}}, Locations: [][]content.MomentLocationResolved{{}}, PreservedMedia: &preserved}, nil
}
func (p *editingThreadHTTPSpy) CommitMomentThread(_ context.Context, _ [32]byte, _ identity.Actor, in content.MomentThreadInput) (content.MomentThreadReceipt, error) {
	p.writes++
	at := time.Now()
	removed := []content.MomentThreadMember{}
	p.original = content.MomentThreadReceipt{SchemaVersion: "moment-thread-operation-v2", OperationID: in.OperationID, Status: "SUCCEEDED", Action: in.Action, ThreadID: hcTestID, RootMomentID: hcTestID, Audience: "PUBLIC", ExpectedVersion: 0, AppliedVersion: 1, PayloadDigest: content.MomentThreadDigest(in), Members: []content.MomentThreadMember{{MomentID: hcTestID, Position: 0, Status: "published", AppliedRevision: 2, Media: []momentmedia.StageSelector{}}}, RemovedMembers: &removed, CommittedAt: &at}
	if p.loseAfterCommit {
		p.final = identity.ErrUnauthorized
	}
	return p.original, nil
}
func (p *editingThreadHTTPSpy) ReadMomentThreadOperation(_ context.Context, _ [32]byte, _ identity.Actor, _ string, _ bool) (content.MomentThreadReceipt, error) {
	p.reads++
	return p.original, nil
}
func (p *editingThreadHTTPSpy) ReadMomentThread(context.Context, content.PublicMomentAccess, string, bool) (content.MomentThreadView, error) {
	return content.MomentThreadView{}, content.ErrNotFound
}
func TestMomentThreadEditingHTTPExplicitSingleEchoUnknownRecovery(t *testing.T) {
	p := &editingThreadHTTPSpy{}
	h := New(nil, p, nil, p, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	empty, restore := "", false
	removed := []content.MomentThreadRemoval{}
	in := content.MomentThreadInput{OperationID: hcTestOp, Action: "EDIT", LocalScope: hcTestID, Audience: "PUBLIC", BaseMomentID: &empty, RemovedItems: &removed, Items: []content.MomentThreadItem{{Restore: &restore, Input: content.MomentInput{Body: "single", TimePrecision: "unknown", LocationPrecision: "none"}, Media: []momentmedia.AttachmentInput{}}}}
	raw, _ := json.Marshal(in)
	w := momentControlHTTPCall(h, "POST", "/v1/me/moment-threads/previews", string(raw))
	if w.Code != 200 || p.writes != 0 || p.proofs != 1 {
		t.Fatal("actual closed HTTP single preview", w.Code, w.Body.String())
	}
	in.SourceVersion = strings.Repeat("a", 64)
	in.Approval = "EXACT_INPUT"
	raw, _ = json.Marshal(in)
	p.loseAfterCommit = true
	path := "/v1/me/moment-threads/operations/" + hcTestOp
	w = momentControlHTTPCall(h, "PUT", path, string(raw))
	if w.Code != 401 || p.writes != 1 || strings.Contains(w.Body.String(), "SUCCEEDED") {
		t.Fatal("unknown final frame", w.Code)
	}
	p.final = nil
	p.lost = true
	w = momentControlHTTPCall(h, "GET", path, "")
	if w.Code != 200 || p.writes != 1 || !strings.Contains(w.Body.String(), "moment-thread-operation-v2") {
		t.Fatal("original fact GET", w.Code, w.Body.String())
	}
}
