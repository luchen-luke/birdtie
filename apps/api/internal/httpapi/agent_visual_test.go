package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	av "github.com/birdtie/birdtie/apps/api/internal/agentvisual"
	"net/http/httptest"
	"strings"
	"testing"
)

type visualHTTPSpy struct{ calls int }

func (p *visualHTTPSpy) Preview(context.Context, av.Access, av.PreviewInput) (av.Preview, error) {
	p.calls++
	return av.Preview{}, av.ErrUnavailable
}
func (p *visualHTTPSpy) Pixels(context.Context, av.Access, string) (av.Pixel, []byte, error) {
	p.calls++
	return av.Pixel{}, nil, av.ErrUnavailable
}
func (p *visualHTTPSpy) Analyze(context.Context, av.Access, string, av.ApproveInput) (av.Operation, error) {
	p.calls++
	return av.Operation{}, av.ErrUnavailable
}
func (p *visualHTTPSpy) Operation(context.Context, av.Access, string) (av.Operation, error) {
	p.calls++
	return av.Operation{}, av.ErrUnavailable
}
func visualHTTPInput(size int) av.PreviewInput {
	return av.PreviewInput{OperationID: avatarTestOp, ClientDraftID: avatarTestOwner, ClientDraftRevision: 1, SourceContextType: "CITY", SourceContextID: "city-slug", Purpose: av.Purpose, Image: av.ImageInput{MIME: "image/png", ContentBase64: base64.StdEncoding.EncodeToString(make([]byte, size)), InputSHA256: strings.Repeat("a", 64), PixelRisk: "UNKNOWN"}}
}
func TestAgentVisualHTTPWholeTenMiBBase64UsesDedicatedBodyLimit(t *testing.T) {
	in := visualHTTPInput(av.MaxInputBytes)
	raw, _ := json.Marshal(in)
	r := avatarRequest("POST", "/v1/me/agent-visual/previews", string(raw))
	b, e := visualBody(httptest.NewRecorder(), r, true)
	if e != nil || len(b) != len(raw) {
		t.Fatal("valid max pixel request clipped by default JSON gate", e)
	}
	parsed, e := av.DecodePreview(b)
	if e != nil || len(parsed.Image.ContentBase64) != len(in.Image.ContentBase64) {
		t.Fatal(e)
	}
	tooBig := append(raw, make([]byte, 64*1024)...)
	if _, e = visualBody(httptest.NewRecorder(), avatarRequest("POST", "/v1/me/agent-visual/previews", string(tooBig)), true); e == nil {
		t.Fatal("unbounded body accepted")
	}
}
func TestAgentVisualHTTPPersonalNoSelectorsAndUnavailablePreservesDraft(t *testing.T) {
	raw, _ := json.Marshal(visualHTTPInput(8))
	for _, kind := range []string{"valid-unavailable", "workspace", "query", "organization", "anonymous", "duplicate", "legacy-purpose"} {
		p := &visualHTTPSpy{}
		s := &server{access: &personAvatarSpy{actorType: "person"}, agentVisual: p}
		r := avatarRequest("POST", "/v1/me/agent-visual/previews", string(raw))
		want := 503
		switch kind {
		case "workspace":
			r.Header.Set("X-Birdtie-Organization-Workspace", avatarTestOwner)
			want = 403
		case "query":
			r.URL.RawQuery = "ownerId=borrow"
			want = 400
		case "organization":
			s.access = &personAvatarSpy{actorType: "organization"}
			want = 403
		case "anonymous":
			r.Header.Del("Authorization")
			want = 401
		case "duplicate":
			r = avatarRequest("POST", r.URL.Path, strings.Replace(string(raw), `"pixelRisk":"UNKNOWN"`, `"pixelRisk":"UNKNOWN","pixelRisk":"USER_MASKED"`, 1))
			want = 400
		case "legacy-purpose":
			r = avatarRequest("POST", r.URL.Path, strings.Replace(string(raw), av.Purpose, "HUMAN_TASK_MESSAGE_ATTACHMENT", 1))
			want = 400
		}
		w := httptest.NewRecorder()
		s.createAgentVisualPreview(w, r)
		if w.Code != want {
			t.Fatal(kind, w.Code, w.Body.String())
		}
		calls := 0
		if kind == "valid-unavailable" {
			calls = 1
		}
		if p.calls != calls {
			t.Fatal(kind, "unsafe gateway invocation", p.calls)
		}
	}
}
