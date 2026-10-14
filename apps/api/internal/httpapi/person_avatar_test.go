package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	av "github.com/birdtie/birdtie/apps/api/internal/profileavatar"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const avatarTestOwner = "01234567-89ab-4cde-8fab-0123456789ab"
const avatarTestOp = "01234567-89ab-4cde-8fab-0123456789ac"

type personAvatarSpy struct {
	identity.AccessStore
	calls     int
	actorType string
	err       error
}

func (p *personAvatarSpy) Authenticate(context.Context, [32]byte) (identity.Actor, error) {
	return identity.Actor{ID: avatarTestOwner, AccountType: p.actorType}, nil
}
func (p *personAvatarSpy) ReadOwnAvatar(context.Context, av.Access) (av.State, error) {
	p.calls++
	return av.State{}, p.err
}
func (p *personAvatarSpy) ChangeOwnAvatar(_ context.Context, a av.Access, in av.Input) (av.Result, error) {
	p.calls++
	return av.Result{OperationID: in.OperationID, Action: map[bool]string{true: "DELETE", false: "SAVE"}[in.Delete], AppliedVersion: in.ExpectedVersion + 1, Current: av.State{Version: in.ExpectedVersion + 1}}, p.err
}
func (p *personAvatarSpy) ReadOwnAvatarOperation(context.Context, av.Access, string) (av.Result, error) {
	p.calls++
	return av.Result{}, p.err
}
func (p *personAvatarSpy) ReadAvatarContent(context.Context, av.Access, string, int64) (av.Metadata, []byte, error) {
	p.calls++
	return av.Metadata{}, nil, p.err
}
func avatarRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer bts1_"+base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	r.Header.Set("Content-Type", "application/json")
	return r
}
func TestPersonAvatarWireRejectsSelectorsDuplicatesAndVersions(t *testing.T) {
	valid := `{"operationId":"` + avatarTestOp + `","expectedVersion":0}`
	for _, body := range []string{strings.TrimSuffix(valid, "}") + `,"ownerId":"` + avatarTestOwner + `"}`, strings.TrimSuffix(valid, "}") + `,"expectedVersion":0}`, strings.Replace(valid, ":0}", ":-1}", 1), strings.Replace(valid, ":0}", ":0.5}", 1), strings.Replace(valid, ":0}", ":null}", 1), valid + "{}"} {
		r := avatarRequest("DELETE", "/v1/me/avatar", body)
		if _, e := decodePersonAvatar(httptest.NewRecorder(), r, true); e != av.ErrInvalid {
			t.Fatalf("accepted unsafe wire %s: %v", body, e)
		}
	}
	if in, e := decodePersonAvatar(httptest.NewRecorder(), avatarRequest("DELETE", "/v1/me/avatar", valid), true); e != nil || !in.Delete || in.OperationID != avatarTestOp {
		t.Fatal(in, e)
	}
}
func TestPersonAvatarOwnerGuardsAndConflict(t *testing.T) {
	body := `{"operationId":"` + avatarTestOp + `","expectedVersion":0}`
	for _, kind := range []string{"workspace", "query", "organization", "anonymous", "conflict"} {
		p := &personAvatarSpy{actorType: "person"}
		s := &server{access: p}
		r := avatarRequest("DELETE", "/v1/me/avatar", body)
		want := 403
		switch kind {
		case "workspace":
			r.Header.Set("X-Birdtie-Organization-Workspace", avatarTestOwner)
		case "query":
			r.URL.RawQuery = "accountId=" + avatarTestOwner
			want = 400
		case "organization":
			p.actorType = "organization"
		case "anonymous":
			r.Header.Del("Authorization")
			want = 401
		case "conflict":
			p.err = av.ErrConflict
			want = 409
		}
		w := httptest.NewRecorder()
		s.deleteOwnPersonAvatar(w, r)
		if w.Code != want || w.Header().Get("Cache-Control") != "no-store" || kind != "conflict" && p.calls != 0 {
			t.Fatalf("%s status=%d calls=%d body=%s", kind, w.Code, p.calls, w.Body)
		}
	}
}
func TestPersonAvatarSaveCarriesVersionAndOperation(t *testing.T) {
	p := &personAvatarSpy{actorType: "person"}
	s := &server{access: p}
	w := httptest.NewRecorder()
	body := `{"operationId":"` + avatarTestOp + `","expectedVersion":3,"contentType":"image/png","contentBase64":"aGVsbG8="}`
	s.putOwnPersonAvatar(w, avatarRequest("PUT", "/v1/me/avatar", body))
	var data struct {
		Data av.Result `json:"data"`
	}
	e := json.Unmarshal(w.Body.Bytes(), &data)
	if w.Code != 200 || e != nil || data.Data.AppliedVersion != 4 || data.Data.OperationID != avatarTestOp || p.calls != 1 {
		t.Fatal(w.Code, w.Body, e)
	}
}
