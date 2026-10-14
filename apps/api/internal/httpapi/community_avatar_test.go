package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ca "github.com/birdtie/birdtie/apps/api/internal/communityavatar"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	av "github.com/birdtie/birdtie/apps/api/internal/profileavatar"
)

const communityAvatarID = "01234567-89ab-4cde-8fab-0123456789ad"

type communityAvatarSpy struct {
	identity.AccessStore
	kind          string
	calls, finals int
	finalErr, err error
	state         ca.State
	metadata      ca.Metadata
	raw           []byte
	lastAccess    ca.Access
	lastInput     ca.Input
	malformed     bool
}

func (p *communityAvatarSpy) Authenticate(context.Context, [32]byte) (identity.Actor, error) {
	return identity.Actor{ID: avatarTestOwner, AccountType: p.kind}, nil
}
func (p *communityAvatarSpy) ValidateHumanSocialResponse(_ context.Context, d [32]byte, a identity.Actor) error {
	p.finals++
	if a.ID != avatarTestOwner || a.AccountType != "person" {
		return identity.ErrUnauthorized
	}
	return p.finalErr
}
func (p *communityAvatarSpy) ReadCommunityAvatar(_ context.Context, a ca.Access, id string) (ca.State, error) {
	p.calls++
	p.lastAccess = a
	return p.state, p.err
}
func (p *communityAvatarSpy) PreviewCommunityAvatar(_ context.Context, a ca.Access, id string, in ca.Input) (ca.Approval, error) {
	p.calls++
	p.lastAccess = a
	p.lastInput = in
	_, _, action, sum, _ := ca.Normalize(in)
	result := ca.Approval{CommunityID: id, OperationID: in.OperationID, ExpectedVersion: in.ExpectedVersion, Action: action, InputSHA256: sum, ExpiresAt: time.Now().Add(30 * time.Second), Token: "ca1.unit.approval.bound"}
	if p.malformed {
		result.CommunityID = avatarTestOwner
	}
	return result, p.err
}
func (p *communityAvatarSpy) ChangeCommunityAvatar(_ context.Context, a ca.Access, id string, in ca.Input) (ca.Result, error) {
	p.calls++
	p.lastAccess = a
	p.lastInput = in
	result := ca.Result{CommunityID: id, OperationID: in.OperationID, Action: map[bool]string{true: "DELETE", false: "SAVE"}[in.Delete], AppliedVersion: in.ExpectedVersion + 1, Current: ca.State{Version: in.ExpectedVersion + 1}}
	if p.malformed {
		result.OperationID = avatarTestOwner
	}
	return result, p.err
}
func (p *communityAvatarSpy) ReadCommunityAvatarOperation(_ context.Context, a ca.Access, id, op string) (ca.Result, error) {
	p.calls++
	p.lastAccess = a
	return ca.Result{CommunityID: id, OperationID: op, Action: "DELETE", AppliedVersion: 1, Current: ca.State{Version: 2}}, p.err
}
func (p *communityAvatarSpy) ReadCommunityAvatarContent(_ context.Context, a ca.Access, id string, v int64) (ca.Metadata, []byte, error) {
	p.calls++
	p.lastAccess = a
	return p.metadata, p.raw, p.err
}
func communityAvatarHandler(p *communityAvatarSpy) http.Handler {
	mux := http.NewServeMux()
	(&server{access: p}).registerCommunityAvatarRoutes(mux)
	return mux
}
func communityAvatarRequest(method, path, body string) *http.Request {
	return avatarRequest(method, "/v1/communities/"+communityAvatarID+path, body)
}
func communityAvatarBody(t *testing.T, commit, deleting bool) string {
	t.Helper()
	payload := map[string]any{"operationId": avatarTestOp, "expectedVersion": 0, "action": "DELETE"}
	if !deleting {
		canvas := image.NewRGBA(image.Rect(0, 0, 8, 8))
		canvas.Set(0, 0, color.RGBA{R: 255, A: 255})
		var buffer bytes.Buffer
		if err := png.Encode(&buffer, canvas); err != nil {
			t.Fatal(err)
		}
		payload["action"] = "SAVE"
		payload["contentType"] = "image/png"
		payload["contentBase64"] = base64.StdEncoding.EncodeToString(buffer.Bytes())
	}
	if commit {
		payload["approval"] = "ca1.unit.approval.bound"
	}
	raw, _ := json.Marshal(payload)
	return string(raw)
}
func TestCommunityAvatarHTTPStrictBodyAndNoActionFallback(t *testing.T) {
	valid := communityAvatarBody(t, true, true)
	for _, body := range []string{
		strings.Replace(valid, `"expectedVersion":0`, `"expectedVersion":null`, 1),
		strings.Replace(valid, `"expectedVersion":0`, `"expectedVersion":-1`, 1),
		strings.Replace(valid, `"expectedVersion":0`, `"expectedVersion":9007199254740991`, 1),
		strings.Replace(valid, `"DELETE"`, `"ARCHIVE"`, 1),
		strings.TrimSuffix(valid, "}") + `,"actorId":"` + avatarTestOwner + `"}`,
		strings.TrimSuffix(valid, "}") + `,"action":"DELETE"}`,
		strings.TrimSuffix(valid, "}") + `,"contentBase64":""}`,
		strings.Replace(valid, `"ca1.unit.approval.bound"`, `""`, 1),
		valid + "{}",
	} {
		p := &communityAvatarSpy{kind: "person"}
		w := httptest.NewRecorder()
		communityAvatarHandler(p).ServeHTTP(w, communityAvatarRequest("PUT", "/avatar", body))
		if w.Code != 400 || p.calls != 0 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("unsafe body accepted status=%d calls=%d", w.Code, p.calls)
		}
	}
	for _, r := range []*http.Request{
		communityAvatarRequest("POST", "/avatar/preview", valid),
		communityAvatarRequest("PUT", "/avatar", communityAvatarBody(t, false, true)),
		communityAvatarRequest("GET", "/avatar", "{}"),
		communityAvatarRequest("GET", "/avatar?actorId="+avatarTestOwner, ""),
	} {
		p := &communityAvatarSpy{kind: "person"}
		w := httptest.NewRecorder()
		communityAvatarHandler(p).ServeHTTP(w, r)
		if w.Code != 400 || p.calls != 0 {
			t.Fatalf("accepted wrong route contract %d calls=%d", w.Code, p.calls)
		}
	}
}
func TestCommunityAvatarHTTPCurrentPersonApprovalReceiptAndFinalRevocation(t *testing.T) {
	for _, action := range []string{"preview-save", "put-delete", "receipt", "state"} {
		p := &communityAvatarSpy{kind: "person"}
		w := httptest.NewRecorder()
		var r *http.Request
		switch action {
		case "preview-save":
			r = communityAvatarRequest("POST", "/avatar/preview", communityAvatarBody(t, false, false))
		case "put-delete":
			r = communityAvatarRequest("PUT", "/avatar", communityAvatarBody(t, true, true))
		case "receipt":
			r = communityAvatarRequest("GET", "/avatar/operations/"+avatarTestOp, "")
		case "state":
			r = communityAvatarRequest("GET", "/avatar", "")
		}
		communityAvatarHandler(p).ServeHTTP(w, r)
		if w.Code != 200 || p.calls != 1 || p.finals != 1 || p.lastAccess.AccountID != avatarTestOwner || p.lastAccess.AccountType != "person" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s status=%d calls=%d finals=%d", action, w.Code, p.calls, p.finals)
		}
	}
	for _, boundary := range []string{"anonymous", "organization", "workspace", "revoked", "malformed", "conflict"} {
		p := &communityAvatarSpy{kind: "person"}
		r := communityAvatarRequest("PUT", "/avatar", communityAvatarBody(t, true, true))
		want := 403
		calls := 0
		switch boundary {
		case "anonymous":
			r.Header.Del("Authorization")
			want = 401
		case "organization":
			p.kind = "organization"
		case "workspace":
			r.Header.Set("X-Birdtie-Organization-Workspace", communityAvatarID)
		case "revoked":
			p.finalErr = identity.ErrUnauthorized
			want = 401
			calls = 1
		case "malformed":
			p.malformed = true
			want = 503
			calls = 1
		case "conflict":
			p.err = ca.ErrChanged
			want = 409
			calls = 1
		}
		w := httptest.NewRecorder()
		communityAvatarHandler(p).ServeHTTP(w, r)
		if w.Code != want || p.calls != calls || strings.Contains(w.Body.String(), "operationId") {
			t.Fatalf("%s status=%d calls=%d leaked=%s", boundary, w.Code, p.calls, w.Body)
		}
	}
}
func TestCommunityAvatarHTTPContentByteHashCurrentCommunityAndSession(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	thumbnail, err := av.Prepare(encoded.Bytes(), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	raw := thumbnail.Bytes
	digest := sha256.Sum256(raw)
	m := ca.Metadata{CommunityID: communityAvatarID, Version: 1, ThumbnailPath: ca.Path(communityAvatarID, 1), Width: av.Size, Height: av.Size, ByteCount: len(raw), SHA256: hex.EncodeToString(digest[:])}
	for _, boundary := range []string{"valid", "hash", "community", "new-version", "revoked"} {
		p := &communityAvatarSpy{kind: "person", metadata: m, raw: raw, state: ca.State{Version: 1, Avatar: &m}}
		want := 503
		switch boundary {
		case "valid":
			want = 200
		case "hash":
			p.raw = []byte("different-test-byte-stream")
		case "community":
			p.metadata.CommunityID = avatarTestOwner
		case "new-version":
			p.state = ca.State{Version: 2}
			want = 404
		case "revoked":
			p.finalErr = identity.ErrUnauthorized
			want = 401
		}
		w := httptest.NewRecorder()
		communityAvatarHandler(p).ServeHTTP(w, communityAvatarRequest("GET", "/avatar/1/content", ""))
		if w.Code != want || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s status=%d body=%s", boundary, w.Code, w.Body)
		}
		if want == 200 {
			if !bytes.Equal(w.Body.Bytes(), raw) || w.Header().Get("Content-Type") != "image/jpeg" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("content contract changed")
			}
		} else if bytes.Equal(w.Body.Bytes(), raw) {
			t.Fatal("unauthorized byte release")
		}
	}
}
