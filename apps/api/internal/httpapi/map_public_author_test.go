package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mp "github.com/birdtie/birdtie/apps/api/internal/mapprojection"
)

const mapPublicAuthorHTTPID = "27000000-0000-4000-8000-000000000011"
const mapPublicAuthorHTTPBounds = "west=-2.2&south=57.1&east=-2&north=57.3"

type mapPublicAuthorHTTPSpy struct {
	foundation.PublicCatalog
	view          mp.View
	access        mp.Access
	query         mp.Query
	reads, finals int
	err, finalErr error
	wrongQuery    bool
}

func (s *mapPublicAuthorHTTPSpy) ReadMapLayers(_ context.Context, a mp.Access, q mp.Query) (mp.Receipt, error) {
	s.reads++
	s.access, s.query = a, q
	if s.wrongQuery {
		q.CityID = "other-city"
	}
	return mp.Receipt{View: s.view, Query: q, Proof: "native-source-only", Seal: "native-seal-only"}, s.err
}
func (s *mapPublicAuthorHTTPSpy) RevalidateMapLayers(_ context.Context, a mp.Access, r mp.Receipt) error {
	s.finals++
	if a != s.access || r.Query != s.query {
		return mp.ErrChanged
	}
	return s.finalErr
}
func mapPublicAuthorHTTPView(t *testing.T, legacy bool) mp.View {
	t.Helper()
	now := time.Now().UTC().Add(-time.Millisecond)
	fields := map[string]any{"schemaVersion": "typed-map-layers-v2", "cityId": "aberdeen-gb", "scope": "PUBLIC_AUTHOR", "authorId": mapPublicAuthorHTTPID, "observedAt": now, "validUntil": now.Add(time.Minute), "truncated": false, "items": []any{}}
	if legacy {
		fields["schemaVersion"] = "typed-map-layers-v1"
		fields["scope"] = "PUBLIC"
		delete(fields, "authorId")
	}
	raw, _ := json.Marshal(fields)
	var v mp.View
	if e := json.Unmarshal(raw, &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func TestMapPublicAuthorHTTPStrictOptIn(t *testing.T) {
	valid := mapPublicAuthorHTTPBounds + "&scope=public-author&authorId=" + mapPublicAuthorHTTPID
	for _, tc := range []struct {
		name, query    string
		private, valid bool
	}{
		{"v1", mapPublicAuthorHTTPBounds, false, true}, {"author", valid, false, true},
		{"private_original", mapPublicAuthorHTTPBounds, true, true}, {"private_author", valid, true, false},
		{"no_scope", mapPublicAuthorHTTPBounds + "&authorId=" + mapPublicAuthorHTTPID, false, false},
		{"no_author", mapPublicAuthorHTTPBounds + "&scope=public-author", false, false},
		{"bad_id", strings.Replace(valid, mapPublicAuthorHTTPID, "bad", 1), false, false},
		{"wrong_case", strings.Replace(valid, "public-author", "PUBLIC_AUTHOR", 1), false, false},
		{"empty_author", strings.Replace(valid, mapPublicAuthorHTTPID, "", 1), false, false},
		{"duplicate_scope", valid + "&scope=public-author", false, false},
		{"duplicate_author", valid + "&authorId=" + mapPublicAuthorHTTPID, false, false},
		{"owner", valid + "&ownerId=" + mapPublicAuthorHTTPID, false, false},
		{"unknown", valid + "&include=private", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/v1/cities/aberdeen-gb/map-layers?"+tc.query, nil)
			r.SetPathValue("cityID", "aberdeen-gb")
			q, e := mapProjectionQuery(r, tc.private)
			if (e == nil) != tc.valid {
				t.Fatal("closed selector", q, e)
			}
		})
	}
}
func TestMapPublicAuthorHTTPCurrentFinalFence(t *testing.T) {
	for _, mode := range []string{"legacy", "anonymous_empty", "signed_empty", "moment", "truncated", "bad_token", "organization", "workspace", "wrong_query", "wrong_author", "wrong_schema", "other_kind", "retired_author", "retired_empty_source", "revoked_session", "read_unavailable", "missing_store", "get_body"} {
		t.Run(mode, func(t *testing.T) {
			s, auth, _, token := privateProfileHTTPFixture(t)
			p := &mapPublicAuthorHTTPSpy{view: mapPublicAuthorHTTPView(t, mode == "legacy")}
			s.catalog = p
			path := "/v1/cities/aberdeen-gb/map-layers?" + mapPublicAuthorHTTPBounds + "&scope=public-author&authorId=" + mapPublicAuthorHTTPID
			want, body := 200, ""
			switch mode {
			case "legacy":
				path = "/v1/cities/aberdeen-gb/map-layers?" + mapPublicAuthorHTTPBounds
			case "anonymous_empty":
				token = ""
			case "moment", "truncated":
				p.view.Items = []mp.Item{{Kind: "MOMENT", ID: mapPublicAuthorHTTPID, Title: "实际公开动态测试替身", Entity: mp.Ref{Type: "MOMENT", ID: mapPublicAuthorHTTPID}, Detail: mp.Ref{Type: "MOMENT", ID: mapPublicAuthorHTTPID}, Anchor: mp.Anchor{PlaceID: privateProfileHTTPForeign, Label: "已许可地点测试替身", CoordinateSystem: "wgs84", Precision: "point", Latitude: 57.2, Longitude: -2.1}, SourceVersion: "3"}}
				p.view.Truncated = mode == "truncated"
			case "bad_token":
				token = "invalid"
				want = 401
			case "organization":
				auth.actor.AccountType = "organization"
				want = 401
			case "workspace":
				want = 400
			case "wrong_query":
				p.wrongQuery = true
				want = 503
			case "wrong_author":
				raw, _ := json.Marshal(p.view)
				var fields map[string]any
				json.Unmarshal(raw, &fields)
				fields["authorId"] = privateProfileHTTPForeign
				raw, _ = json.Marshal(fields)
				json.Unmarshal(raw, &p.view)
				want = 503
			case "wrong_schema":
				p.view.SchemaVersion = "typed-map-layers-v1"
				want = 503
			case "other_kind":
				p.view.Items = []mp.Item{{Kind: "PLACE"}}
				want = 503
			case "retired_author":
				p.finalErr = mp.ErrNotFound
				want = 404
			case "retired_empty_source":
				p.finalErr = mp.ErrChanged
				want = 409
			case "revoked_session":
				p.finalErr = identity.ErrUnauthorized
				want = 401
			case "read_unavailable":
				p.err = mp.ErrUnavailable
				want = 503
			case "missing_store":
				s.catalog = struct{ foundation.PublicCatalog }{}
				want = 503
			case "get_body":
				body = `{"authorId":"ignored"}`
				want = 400
			}
			r := privateProfileHTTPRequest("GET", path, body, token, "")
			r.SetPathValue("cityID", "aberdeen-gb")
			if mode == "workspace" {
				r.Header.Set("X-Birdtie-Organization-Workspace", privateProfileHTTPForeign)
			}
			w := httptest.NewRecorder()
			s.getPublicMapLayers(w, r)
			if w.Code != want || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(mode, w.Code, w.Body.String())
			}
			if want != 200 {
				if strings.Contains(w.Body.String(), `"data"`) || strings.Contains(w.Body.String(), mapPublicAuthorHTTPID) {
					t.Fatal("failed author read leaked target/payload")
				}
				if (mode == "bad_token" || mode == "organization" || mode == "workspace" || mode == "get_body") && p.reads != 0 {
					t.Fatal("invalid request reached source store", mode)
				}
				return
			}
			var out struct{ Data map[string]json.RawMessage }
			if json.Unmarshal(w.Body.Bytes(), &out) != nil {
				t.Fatal("invalid envelope")
			}
			for _, key := range []string{"schemaVersion", "cityId", "scope", "observedAt", "validUntil", "truncated", "items"} {
				if out.Data[key] == nil {
					t.Fatal("required map key missing", key)
				}
			}
			count := 8
			if mode == "legacy" {
				count = 7
				if out.Data["authorId"] != nil {
					t.Fatal("author selector leaked into v1")
				}
			} else if string(out.Data["authorId"]) != `"`+mapPublicAuthorHTTPID+`"` {
				t.Fatal("author identity not present/matching", string(out.Data["authorId"]))
			}
			if mode == "truncated" && string(out.Data["truncated"]) != "true" {
				t.Fatal("bounded actual source truncation lost")
			}
			if len(out.Data) != count || p.reads != 1 || p.finals != 1 {
				t.Fatal("wire/final source contract", mode, w.Body.String(), p.reads, p.finals)
			}
			if mode == "anonymous_empty" && !p.access.Anonymous() {
				t.Fatal("anonymous inherited identity")
			}
			if mode == "signed_empty" && (p.access.Actor.ID != privateProfileHTTPOwner || p.access.Digest != auth.digest) {
				t.Fatal("actual session binding lost")
			}
			if strings.Contains(w.Body.String(), "native-source-only") || strings.Contains(w.Body.String(), "native-seal-only") {
				t.Fatal("internal proof on wire")
			}
		})
	}
}
func TestMapPublicAuthorHTTPUnavailableTargetsUniform(t *testing.T) {
	var expected string
	for _, mode := range []string{"nonexistent", "private", "hidden_name", "inactive_person", "non_person", "blocked_reader", "blocked_author"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _, token := privateProfileHTTPFixture(t)
			p := &mapPublicAuthorHTTPSpy{err: mp.ErrNotFound}
			s.catalog = p
			r := privateProfileHTTPRequest("GET", "/v1/cities/aberdeen-gb/map-layers?"+mapPublicAuthorHTTPBounds+"&scope=public-author&authorId="+mapPublicAuthorHTTPID, "", token, "")
			r.SetPathValue("cityID", "aberdeen-gb")
			w := httptest.NewRecorder()
			s.getPublicMapLayers(w, r)
			if w.Code != 404 || p.reads != 1 || p.finals != 0 {
				t.Fatal("unavailable target", mode, w.Code, w.Body.String())
			}
			if expected == "" {
				expected = w.Body.String()
			}
			if w.Body.String() != expected || strings.Contains(expected, mapPublicAuthorHTTPID) || strings.Contains(expected, mode) {
				t.Fatal("target state side channel", w.Body.String())
			}
			if !errors.Is(p.err, mp.ErrNotFound) {
				t.Fatal("unexpected mock")
			}
		})
	}
}
