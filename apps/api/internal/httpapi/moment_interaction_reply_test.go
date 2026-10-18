package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"
	"strings"
	"testing"
	"time"
)

type replyHTTPSpy struct {
	hcSpy
	content.MomentStore
	input content.MomentInteractionInput
}

func (p *replyHTTPSpy) Authenticate(context.Context, [32]byte) (identity.Actor, error) {
	return identity.Actor{ID: hcTestID, AccountType: "person"}, nil
}
func (p *replyHTTPSpy) PreviewMomentInteraction(_ context.Context, _ [32]byte, _ identity.Actor, in content.MomentInteractionInput) (content.MomentInteractionPreview, error) {
	p.input = in
	return content.MomentInteractionPreview{HumanControlPreview: p.base(in.OperationID, in.Action, content.MomentInteractionDigest(in)), Input: in, SourceVersion: strings.Repeat("a", 64)}, nil
}
func (p *replyHTTPSpy) CommitMomentInteraction(_ context.Context, _ [32]byte, _ identity.Actor, in content.MomentInteractionInput) (content.MomentInteractionReceipt, error) {
	p.input = in
	p.writes++
	now := time.Now()
	return content.MomentInteractionReceipt{OperationID: in.OperationID, Status: "SUCCEEDED", Action: in.Action, MomentID: in.MomentID, CommentID: hcTestID, AppliedVersion: 1, PayloadDigest: content.MomentInteractionDigest(in), CommittedAt: &now}, nil
}
func (p *replyHTTPSpy) ReadMomentInteractionOperation(context.Context, [32]byte, identity.Actor, string, bool) (content.MomentInteractionReceipt, error) {
	return content.MomentInteractionReceipt{}, content.ErrNotFound
}
func (p *replyHTTPSpy) ReadMomentInteractions(context.Context, content.PublicMomentAccess, string, string, string) (content.MomentInteractionPage, error) {
	return content.MomentInteractionPage{}, content.ErrUnavailable
}
func TestMomentReplyHTTPPreservesExactParentAndRejectsInvalidPair(t *testing.T) {
	p := &replyHTTPSpy{}
	h := New(nil, p, nil, p, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	in := content.MomentInteractionInput{OperationID: hcTestOp, Action: "COMMENT", MomentID: hcTestID, ExpectedMomentRevision: 2, Body: "explicit reply", ParentCommentID: hcTestID, ExpectedParentCommentVersion: 1}
	raw, _ := json.Marshal(in)
	w := momentControlHTTPCall(h, http.MethodPost, "/v1/me/moment-interactions/previews", string(raw))
	if w.Code != 200 || p.input.ParentCommentID != in.ParentCommentID || p.input.ExpectedParentCommentVersion != 1 || p.writes != 0 {
		t.Fatal("parent preview binding", w.Code, w.Body.String())
	}
	in.ExpectedParentCommentVersion = 0
	raw, _ = json.Marshal(in)
	w = momentControlHTTPCall(h, http.MethodPost, "/v1/me/moment-interactions/previews", string(raw))
	if w.Code != 400 || p.writes != 0 {
		t.Fatal("invalid parent pair reached writer", w.Code)
	}
}
