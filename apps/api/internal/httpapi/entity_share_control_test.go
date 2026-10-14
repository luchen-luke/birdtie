package httpapi

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type shareControlSpy struct {
	*hcSpy
	connection.Store
	connection.CurrentStore
	writes, gets, stops, textWrites int
	missing                         bool
}

func (p *shareControlSpy) PreviewEntityShareControl(_ context.Context, a connection.EntityShareAccess, target connection.EntityShareRecipient, in connection.EntityShareControlInput) (connection.EntityShareControlPreview, error) {
	now := time.Now()
	return connection.EntityShareControlPreview{OperationID: in.OperationID, Recipient: target, Entity: in.Entity, EntityTitle: "当前合成实体", RecipientAccountID: hcTestOp, SourceVersion: strings.Repeat("a", 64), PayloadDigest: connection.EntityShareControlDigest(target, in), ObservedAt: now, ExpiresAt: now.Add(30 * time.Second), Approval: "specific-original-input", ResponseProof: p.proof()}, nil
}
func (p *shareControlSpy) success(target connection.EntityShareRecipient, op string) connection.EntityShareControlReceipt {
	now := time.Now()
	ref := &connection.EntityShareRef{Type: "place", ID: hcTestID}
	return connection.EntityShareControlReceipt{OperationID: op, Recipient: target, Status: "SUCCEEDED", ConversationID: hcTestID, MessageID: hcTestOp, Entity: ref, PayloadDigest: connection.EntityShareControlDigest(target, connection.EntityShareControlInput{OperationID: op, Entity: *ref}), CommittedAt: &now}
}
func (p *shareControlSpy) CommitEntityShareControl(_ context.Context, _ connection.EntityShareAccess, target connection.EntityShareRecipient, in connection.EntityShareControlInput) (connection.EntityShareControlReceipt, error) {
	p.writes++
	return p.success(target, in.OperationID), nil
}
func (p *shareControlSpy) ReadEntityShareControlOperation(_ context.Context, _ connection.EntityShareAccess, target connection.EntityShareRecipient, op string) (connection.EntityShareControlReceipt, error) {
	p.gets++
	if p.missing {
		return connection.EntityShareControlReceipt{}, connection.ErrNotFound
	}
	return p.success(target, op), nil
}
func (p *shareControlSpy) StopEntityShareControlOperation(_ context.Context, _ connection.EntityShareAccess, target connection.EntityShareRecipient, op string) (connection.EntityShareControlReceipt, error) {
	p.stops++
	return connection.EntityShareControlReceipt{OperationID: op, Status: "STOPPED", Recipient: target}, nil
}
func (p *shareControlSpy) Authenticate(context.Context, [32]byte) (identity.Actor, error) {
	return identity.Actor{ID: hcTestID, AccountType: "person"}, nil
}
func (p *shareControlSpy) SendMessageCurrent(_ context.Context, a ea.Access, id, body, kind, entity string) (connection.Message, error) {
	p.textWrites++
	return connection.Message{ID: hcTestOp, ConversationID: id, SenderID: a.Actor.ID, SpeakerKind: "human", Body: body, CreatedAt: time.Now()}, nil
}
func (p *shareControlSpy) ValidateMessageCurrentResponse(context.Context, ea.Access) error {
	return p.final
}
func shareControlMux(p *shareControlSpy) *http.ServeMux {
	s := &server{access: p, connections: p}
	mux := http.NewServeMux()
	s.registerEntityShareControlRoutes(mux)
	mux.HandleFunc("POST /v1/me/conversations/{conversationID}/entity-shares", s.shareHumanEntity)
	mux.HandleFunc("GET /v1/me/conversations/{conversationID}/entity-shares/{operationID}", s.getHumanEntityShare)
	mux.HandleFunc("POST /v1/me/conversations/{conversationID}/messages", s.sendMessage)
	return mux
}
func TestEntityShareControlHTTPExactPreviewCommitAndUnknown(t *testing.T) {
	for _, kind := range []string{"CONVERSATION", "FRIEND_TIE"} {
		t.Run(kind, func(t *testing.T) {
			p := &shareControlSpy{hcSpy: &hcSpy{}}
			mux := shareControlMux(p)
			base := "/v1/me/entity-shares/" + kind + "/" + hcTestID
			body := `{"operationId":"` + hcTestOp + `","entity":{"type":"place","id":"` + hcTestID + `"}}`
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, hcRequest("POST", base+"/previews", body))
			if w.Code != 200 || p.writes != 0 || p.proofs != 1 || !strings.Contains(w.Body.String(), `"recipientAccountId":"`+hcTestOp+`"`) {
				t.Fatal("real mapping preview", w.Code, w.Body.String())
			}
			commit := strings.TrimSuffix(body, "}") + `,"sourceVersion":"` + strings.Repeat("a", 64) + `","approval":"specific"}`
			p.final = identity.ErrUnauthorized
			w = httptest.NewRecorder()
			mux.ServeHTTP(w, hcRequest("PUT", base+"/operations/"+hcTestOp, commit))
			if w.Code != 401 || p.writes != 1 || strings.Contains(w.Body.String(), "SUCCEEDED") {
				t.Fatal("posteffect final unauthorized still definitive", w.Code)
			}
			p.final = nil
			p.lost = true
			w = httptest.NewRecorder()
			mux.ServeHTTP(w, hcRequest("GET", base+"/operations/"+hcTestOp, ""))
			if w.Code != 200 || !strings.Contains(w.Body.String(), "messageId") || strings.Contains(w.Body.String(), "当前合成实体") || strings.Contains(w.Body.String(), `"body"`) {
				t.Fatal("history borrowed current snapshot", w.Code, w.Body.String())
			}
			p.missing = true
			w = httptest.NewRecorder()
			mux.ServeHTTP(w, hcRequest("GET", base+"/operations/"+hcTestOp, ""))
			if w.Code != 404 || p.writes != 1 {
				t.Fatal("404 became retry")
			}
			w = httptest.NewRecorder()
			mux.ServeHTTP(w, hcRequest("POST", base+"/operations/"+hcTestOp+"/stop", ""))
			if w.Code != 200 || p.stops != 1 || !strings.Contains(w.Body.String(), "STOPPED") {
				t.Fatal("stop", w.Code, w.Body.String())
			}
			for _, bad := range []string{strings.TrimSuffix(body, "}") + `,"title":"旧快照"}`, strings.Replace(body, `"type":"place"`, `"type":"place","type":"moment"`, 1), strings.Replace(body, `"place"`, `"organization_url"`, 1)} {
				w = httptest.NewRecorder()
				mux.ServeHTTP(w, hcRequest("POST", base+"/previews", bad))
				if w.Code != 400 || p.writes != 1 {
					t.Fatal("raw/snapshot input bypass", w.Code)
				}
			}
		})
	}
}
func TestEntityShareControlHTTPLegacyAndMessagesEntityCannotBypass(t *testing.T) {
	p := &shareControlSpy{hcSpy: &hcSpy{}}
	mux := shareControlMux(p)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, hcRequest("POST", "/v1/me/conversations/"+hcTestID+"/entity-shares", `{"operationId":"`+hcTestOp+`","entity":{"type":"place","id":"`+hcTestID+`"}}`))
	if w.Code != 409 || p.writes != 0 {
		t.Fatal("old074 POST still sends", w.Code)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, hcRequest("POST", "/v1/me/conversations/"+hcTestID+"/messages", `{"body":"分享卡片","entity":{"type":"place","id":"`+hcTestID+`"}}`))
	if w.Code != 409 || p.writes != 0 {
		t.Fatal("raw message entity still sends", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, hcRequest("POST", "/v1/me/conversations/"+hcTestID+"/messages", `{"body":"普通文字消息"}`))
	if w.Code != 201 || p.textWrites != 1 || p.writes != 0 {
		t.Fatal("ordinary text protocol changed", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, hcRequest("GET", "/v1/me/conversations/"+hcTestID+"/entity-shares/"+hcTestOp, ""))
	if w.Code != 200 || strings.Contains(w.Body.String(), "title") {
		t.Fatal("074 namespace tiny bridge", w.Code, w.Body.String())
	}
}
