package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mm "github.com/birdtie/birdtie/apps/api/internal/momentmedia"
)

const mapDisplayHTTPMoment = "33333333-3333-3333-3333-333333333333"
const mapDisplayHTTPMedia = "44444444-4444-4444-4444-444444444444"
const mapDisplayHTTPPlace = "55555555-5555-5555-5555-555555555555"

type mapDisplayHTTPSpy struct {
	hcSpy
	content.MomentStore
	calls                          []string
	target, media, source, variant string
	cursor                         string
	body                           []byte
	authErr, readErr, proofErr     error
	agent, loseSession             bool
	editDescriptor                 func(*content.MapContentDisplayDescriptor)
}

func newMapDisplayHTTPSpy(t *testing.T) *mapDisplayHTTPSpy {
	t.Helper()
	var b bytes.Buffer
	if e := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 3)), nil); e != nil {
		t.Fatal(e)
	}
	return &mapDisplayHTTPSpy{body: b.Bytes()}
}

func (p *mapDisplayHTTPSpy) AuthenticateHumanSocial(context.Context, [32]byte) (identity.Actor, error) {
	kind := "person"
	if p.agent {
		kind = "agent"
	}
	return identity.Actor{ID: hcTestID, AccountType: kind}, p.authErr
}

func (p *mapDisplayHTTPSpy) record(method string) {
	p.calls = append(p.calls, method)
	if p.loseSession {
		p.final = identity.ErrUnauthorized
	}
}

func (p *mapDisplayHTTPSpy) currentProof() identity.HumanControlProof {
	return humanControlFunctionProof(func(context.Context) error {
		p.proofs++
		return p.proofErr
	})
}

func (p *mapDisplayHTTPSpy) displayMedia(mid, id, source string) content.MapContentMedia {
	displaySource := content.MomentControlHash([]any{source, "display1280-v1"})
	return content.MapContentMedia{Kind: "IMAGE", ID: id, Revision: 1, MIMEType: "image/jpeg", Width: 2, Height: 3,
		ByteCount: len(p.body), SHA256: mm.DigestBytes(p.body), SourceVersion: displaySource,
		ContentPath: "/v1/me/map-content/moments/" + mid + "/media/" + id + "/content?sourceVersion=" + displaySource + "&variant=display"}
}

func (p *mapDisplayHTTPSpy) moment(id string, lazy bool) content.MapContentMoment {
	now := time.Now().UTC()
	source := strings.Repeat("a", 64)
	media := p.displayMedia(id, mapDisplayHTTPMedia, source)
	if lazy {
		media.SourceVersion, media.SHA256, media.ByteCount = source, strings.Repeat("b", 64), 4096
		media.Width, media.Height = 2000, 3000
		media.ContentPath = "/v1/me/map-content/moments/" + id + "/media/" + media.ID + "/content?sourceVersion=" + source
		media.DisplayDescriptorPath = content.MapContentDisplayDescriptorPath(id, media.ID, source)
	}
	return content.MapContentMoment{SchemaVersion: content.MapContentMomentSchema, ViewerAccountID: hcTestID, ID: id,
		Revision: 1, Status: "draft", Visibility: "private", VisibilitySource: "SELF_PRIVATE", CanEdit: true,
		CreatedAt: now.Add(-time.Hour), Locations: []content.MapContentLocation{}, Media: []content.MapContentMedia{media},
		ObservedAt: now, ValidUntil: now.Add(30 * time.Second), SourceVersion: source, ResponseProof: p.currentProof()}
}

func (p *mapDisplayHTTPSpy) place(id string, lazy bool) content.MapContentPlacePage {
	m := p.moment(mapDisplayHTTPMoment, lazy)
	return content.MapContentPlacePage{SchemaVersion: content.MapContentPlaceSchema, ViewerAccountID: hcTestID, PlaceID: id,
		Items: []content.MapContentMoment{m}, ObservedAt: m.ObservedAt, ValidUntil: m.ValidUntil,
		SourceVersion: strings.Repeat("a", 64), ResponseProof: p.currentProof()}
}

func (p *mapDisplayHTTPSpy) ReadHumanMapContent(context.Context, [32]byte, identity.Actor, content.MapContentQuery) (content.MapContentPage, error) {
	panic("display requests must not load the viewport")
}

func (p *mapDisplayHTTPSpy) ReadHumanMapContentMoment(_ context.Context, _ [32]byte, _ identity.Actor, id string) (content.MapContentMoment, error) {
	p.record("moment-eager")
	p.target = id
	return p.moment(id, false), p.readErr
}

func (p *mapDisplayHTTPSpy) ReadHumanMapContentMomentLazy(_ context.Context, _ [32]byte, _ identity.Actor, id string) (content.MapContentMoment, error) {
	p.record("moment-lazy")
	p.target = id
	return p.moment(id, true), p.readErr
}

func (p *mapDisplayHTTPSpy) ReadHumanMapContentPlace(_ context.Context, _ [32]byte, _ identity.Actor, id, cursor string) (content.MapContentPlacePage, error) {
	p.record("place-eager")
	p.target, p.cursor = id, cursor
	return p.place(id, false), p.readErr
}

func (p *mapDisplayHTTPSpy) ReadHumanMapContentPlaceLazy(_ context.Context, _ [32]byte, _ identity.Actor, id, cursor string) (content.MapContentPlacePage, error) {
	p.record("place-lazy")
	p.target, p.cursor = id, cursor
	return p.place(id, true), p.readErr
}

func (p *mapDisplayHTTPSpy) ReadHumanMapContentDisplayDescriptor(_ context.Context, _ [32]byte, _ identity.Actor, mid, id, source string) (content.MapContentDisplayDescriptor, error) {
	p.record("descriptor")
	p.target, p.media, p.source = mid, id, source
	now := time.Now().UTC()
	v := content.MapContentDisplayDescriptor{SchemaVersion: content.MapContentDisplaySchema, ViewerAccountID: hcTestID,
		MomentID: mid, OriginalSourceVersion: source, ObservedAt: now, ValidUntil: now.Add(30 * time.Second),
		Media: p.displayMedia(mid, id, source), ResponseProof: p.currentProof()}
	if p.editDescriptor != nil {
		p.editDescriptor(&v)
	}
	return v, p.readErr
}

func (p *mapDisplayHTTPSpy) ReadHumanMapContentBinary(_ context.Context, _ [32]byte, _ identity.Actor, kind, mid, id, source, variant string) (content.MapContentBinary, error) {
	if kind != "moments" {
		panic("unexpected display target kind")
	}
	p.record("bytes")
	p.target, p.media, p.source, p.variant = mid, id, source, variant
	return content.MapContentBinary{Bytes: append([]byte(nil), p.body...), MIMEType: "image/jpeg", SHA256: mm.DigestBytes(p.body),
		ValidUntil: time.Now().UTC().Add(30 * time.Second), ResponseProof: p.currentProof()}, p.readErr
}

func mapDisplayHTTPHandler(p *mapDisplayHTTPSpy) http.Handler {
	s := &server{access: p, content: p}
	mux := http.NewServeMux()
	s.RegisterMapContentRoutes(mux)
	return mux
}

func mapDisplayHTTPCall(p *mapDisplayHTTPSpy, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	mapDisplayHTTPHandler(p).ServeHTTP(w, r)
	return w
}

func TestMapContentDisplayHTTPMediaModeSelection(t *testing.T) {
	for _, kind := range []string{"moment", "place"} {
		for _, lazy := range []bool{false, true} {
			name := kind + "-eager"
			if lazy {
				name = kind + "-lazy"
			}
			t.Run(name, func(t *testing.T) {
				p := newMapDisplayHTTPSpy(t)
				path, id := "/v1/me/map-content/moments/"+mapDisplayHTTPMoment, mapDisplayHTTPMoment
				if kind == "place" {
					path, id = "/v1/me/map-content/places/"+mapDisplayHTTPPlace+"/moments?cursor=next-page", mapDisplayHTTPPlace
				}
				if lazy {
					separator := "?"
					if kind == "place" {
						separator = "&"
					}
					path += separator + "mediaMode=lazy"
				}
				w := mapDisplayHTTPCall(p, hcRequest("GET", path, ""))
				if w.Code != 200 || len(p.calls) != 1 || p.calls[0] != name || p.target != id || p.proofs != 1 {
					t.Fatalf("wrong selected reader or missing proof: status=%d calls=%v proofs=%d body=%s", w.Code, p.calls, p.proofs, w.Body.String())
				}
				if kind == "place" && p.cursor != "next-page" {
					t.Fatal("place cursor lost")
				}
				if strings.Contains(w.Body.String(), `"displayDescriptorPath"`) != lazy || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
					t.Fatal("wrong media contract or cache policy", w.Body.String(), w.Header())
				}
			})
		}
	}
}

func TestMapContentDisplayHTTPClosedInputs(t *testing.T) {
	for _, base := range []string{"/v1/me/map-content/moments/" + mapDisplayHTTPMoment, "/v1/me/map-content/places/" + mapDisplayHTTPPlace + "/moments"} {
		for _, query := range []string{"?", "?mediaMode=", "?mediaMode=eager", "?mediaMode=LAZY", "?mediaMode=lazy&mediaMode=lazy", "?mediaMode=lazy&unexpected=1"} {
			t.Run(base+query, func(t *testing.T) {
				p := newMapDisplayHTTPSpy(t)
				w := mapDisplayHTTPCall(p, hcRequest("GET", base+query, ""))
				if w.Code != 400 || len(p.calls) != 0 || p.proofs != 0 {
					t.Fatal("invalid mode reached a reader", w.Code, p.calls)
				}
			})
		}
	}
	base := "/v1/me/map-content/moments/" + mapDisplayHTTPMoment + "/media/" + mapDisplayHTTPMedia + "/display-descriptor"
	source := strings.Repeat("a", 64)
	for _, tc := range []struct{ name, path, body, header, value string }{
		{"missing-source", base, "", "", ""},
		{"empty-source", base + "?sourceVersion=", "", "", ""},
		{"invalid-source", base + "?sourceVersion=not-a-digest", "", "", ""},
		{"duplicate-source", base + "?sourceVersion=" + source + "&sourceVersion=" + source, "", "", ""},
		{"unknown-query", base + "?sourceVersion=" + source + "&variant=display", "", "", ""},
		{"invalid-moment", strings.Replace(base, mapDisplayHTTPMoment, "not-a-uuid", 1) + "?sourceVersion=" + source, "", "", ""},
		{"invalid-media", strings.Replace(base, mapDisplayHTTPMedia, "not-a-uuid", 1) + "?sourceVersion=" + source, "", "", ""},
		{"range", base + "?sourceVersion=" + source, "", "Range", "bytes=0-1"},
		{"organization", base + "?sourceVersion=" + source, "", "X-Birdtie-Organization-Workspace", hcTestID},
		{"body", base + "?sourceVersion=" + source, "{}", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newMapDisplayHTTPSpy(t)
			r := hcRequest("GET", tc.path, tc.body)
			if tc.header != "" {
				r.Header.Set(tc.header, tc.value)
			}
			w := mapDisplayHTTPCall(p, r)
			if w.Code != 400 || len(p.calls) != 0 || p.proofs != 0 {
				t.Fatal("invalid descriptor request reached reader", w.Code, p.calls)
			}
		})
	}
}

func TestMapContentDisplayHTTPDescriptorBinding(t *testing.T) {
	source := strings.Repeat("a", 64)
	path := content.MapContentDisplayDescriptorPath(mapDisplayHTTPMoment, mapDisplayHTTPMedia, source)
	for _, tc := range []struct {
		name string
		edit func(*mapDisplayHTTPSpy, *content.MapContentDisplayDescriptor)
	}{
		{"viewer", func(_ *mapDisplayHTTPSpy, v *content.MapContentDisplayDescriptor) { v.ViewerAccountID = hcTestOp }},
		{"moment", func(p *mapDisplayHTTPSpy, v *content.MapContentDisplayDescriptor) {
			v.MomentID = hcTestOp
			v.Media = p.displayMedia(v.MomentID, v.Media.ID, v.OriginalSourceVersion)
		}},
		{"media", func(p *mapDisplayHTTPSpy, v *content.MapContentDisplayDescriptor) {
			v.Media = p.displayMedia(v.MomentID, hcTestOp, v.OriginalSourceVersion)
		}},
		{"source", func(p *mapDisplayHTTPSpy, v *content.MapContentDisplayDescriptor) {
			v.OriginalSourceVersion = strings.Repeat("b", 64)
			v.Media = p.displayMedia(v.MomentID, v.Media.ID, v.OriginalSourceVersion)
		}},
		{"proof", func(_ *mapDisplayHTTPSpy, v *content.MapContentDisplayDescriptor) { v.ResponseProof = nil }},
		{"expired", func(_ *mapDisplayHTTPSpy, v *content.MapContentDisplayDescriptor) {
			v.ValidUntil = v.ObservedAt.Add(-time.Second)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newMapDisplayHTTPSpy(t)
			p.editDescriptor = func(v *content.MapContentDisplayDescriptor) { tc.edit(p, v) }
			w := mapDisplayHTTPCall(p, hcRequest("GET", path, ""))
			if w.Code != 503 || len(p.calls) != 1 || p.proofs != 0 || strings.Contains(w.Body.String(), `"data"`) {
				t.Fatal("unbound descriptor disclosed", w.Code, p.proofs, w.Body.String())
			}
		})
	}
}

func TestMapContentDisplayHTTPCurrentAuthority(t *testing.T) {
	source := strings.Repeat("a", 64)
	for _, endpoint := range []string{"descriptor", "bytes"} {
		for _, tc := range []struct {
			name          string
			status, calls int
			proofs        int
			setup         func(*mapDisplayHTTPSpy, *http.Request)
		}{
			{"allowed", 200, 1, 1, func(*mapDisplayHTTPSpy, *http.Request) {}},
			{"no-session", 401, 0, 0, func(_ *mapDisplayHTTPSpy, r *http.Request) { r.Header.Del("Authorization") }},
			{"invalid-session", 401, 0, 0, func(p *mapDisplayHTTPSpy, _ *http.Request) { p.authErr = identity.ErrUnauthorized }},
			{"agent", 403, 0, 0, func(p *mapDisplayHTTPSpy, _ *http.Request) { p.agent = true }},
			{"blocked-at-read", 404, 1, 0, func(p *mapDisplayHTTPSpy, _ *http.Request) { p.readErr = content.ErrNotFound }},
			{"source-changed", 409, 1, 0, func(p *mapDisplayHTTPSpy, _ *http.Request) { p.readErr = identity.ErrControlChanged }},
			{"withdrawn-before-write", 409, 1, 1, func(p *mapDisplayHTTPSpy, _ *http.Request) { p.proofErr = identity.ErrControlChanged }},
			{"logout-before-write", 401, 1, 0, func(p *mapDisplayHTTPSpy, _ *http.Request) { p.loseSession = true }},
		} {
			t.Run(endpoint+"/"+tc.name, func(t *testing.T) {
				p := newMapDisplayHTTPSpy(t)
				path, expectedSource := content.MapContentDisplayDescriptorPath(mapDisplayHTTPMoment, mapDisplayHTTPMedia, source), source
				if endpoint == "bytes" {
					media := p.displayMedia(mapDisplayHTTPMoment, mapDisplayHTTPMedia, source)
					path, expectedSource = media.ContentPath, media.SourceVersion
				}
				r := hcRequest("GET", path, "")
				tc.setup(p, r)
				w := mapDisplayHTTPCall(p, r)
				if w.Code != tc.status || len(p.calls) != tc.calls || p.proofs != tc.proofs {
					t.Fatalf("current authority not enforced: status=%d calls=%v proofs=%d body=%s", w.Code, p.calls, p.proofs, w.Body.String())
				}
				if tc.calls == 1 && (p.target != mapDisplayHTTPMoment || p.media != mapDisplayHTTPMedia || p.source != expectedSource || p.calls[0] != endpoint) {
					t.Fatal("exact selector not forwarded")
				}
				if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
					t.Fatal("cacheable authorization-bound response")
				}
				if tc.status != 200 {
					if strings.Contains(w.Body.String(), `"data"`) || bytes.Equal(w.Body.Bytes(), p.body) {
						t.Fatal("denied request disclosed descriptor or image")
					}
					return
				}
				if endpoint == "bytes" {
					if p.variant != "display" || !bytes.Equal(w.Body.Bytes(), p.body) || w.Header().Get("Content-Type") != "image/jpeg" {
						t.Fatal("display bytes changed or wrong variant")
					}
				} else {
					var envelope struct {
						Data content.MapContentDisplayDescriptor `json:"data"`
					}
					if e := json.Unmarshal(w.Body.Bytes(), &envelope); e != nil || !envelope.Data.Valid(time.Now().UTC()) || envelope.Data.Media.SHA256 != mm.DigestBytes(p.body) {
						t.Fatal("invalid descriptor envelope", e)
					}
				}
			})
		}
	}
}
