package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/cityartwork"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

type cityArtworkHTTPSpy struct {
	foundation.PublicCatalog
	content            cityartwork.Content
	err, finalErr      error
	reads, validations int
}

func (p *cityArtworkHTTPSpy) ReadCityArtwork(context.Context, string, string) (cityartwork.Content, error) {
	p.reads++
	return p.content, p.err
}
func (p *cityArtworkHTTPSpy) ValidateCityArtwork(context.Context, string, string) error {
	p.validations++
	return p.finalErr
}
func cityArtworkHTTPFixture() (*server, *cityArtworkHTTPSpy) {
	body := []byte("synthetic validated derivative")
	sum := sha256.Sum256(body)
	v := strings.Repeat("a", 64)
	p := &cityArtworkHTTPSpy{content: cityartwork.Content{Descriptor: cityartwork.Descriptor{CityID: "synthetic-city", Version: v, ContentPath: cityartwork.ContentPath("synthetic-city", v), ContentType: "image/png", Width: 324, Height: 162, ByteCount: len(body), SHA256: hex.EncodeToString(sum[:])}, Bytes: body}}
	return &server{catalog: p}, p
}
func cityArtworkRequest(s *server, descriptor bool, query, rangeHeader string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/v1/cities/synthetic-city/artwork/"+strings.Repeat("a", 64)+"/content"+query, nil)
	r.SetPathValue("cityID", "synthetic-city")
	r.SetPathValue("version", strings.Repeat("a", 64))
	r.Header.Set("Range", rangeHeader)
	w := httptest.NewRecorder()
	s.readCityArtwork(w, r, descriptor)
	return w
}
func TestCityArtworkHTTPDescriptorAndContentAreVersionBound(t *testing.T) {
	s, p := cityArtworkHTTPFixture()
	w := cityArtworkRequest(s, true, "", "")
	var data struct {
		Data cityartwork.Descriptor `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &data) != nil || data.Data != p.content.Descriptor || p.validations != 1 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = cityArtworkRequest(s, false, "", "")
	if w.Code != 200 || w.Body.String() != string(p.content.Bytes) || w.Header().Get("Content-Type") != "image/png" || p.validations != 2 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestCityArtworkHTTPFailsClosedOnRevocationMismatchAndRawSelectors(t *testing.T) {
	for _, kind := range []string{"revoked", "version", "remote", "hash", "missing", "query", "range"} {
		t.Run(kind, func(t *testing.T) {
			s, p := cityArtworkHTTPFixture()
			query, rangeHeader := "", ""
			want := 503
			switch kind {
			case "revoked":
				p.finalErr = cityartwork.ErrNotFound
				want = 404
			case "version":
				p.content.Descriptor.Version = strings.Repeat("b", 64)
			case "remote":
				p.content.Descriptor.ContentPath = "https://legacy.example/private"
			case "hash":
				p.content.Bytes = []byte("different bytes")
			case "missing":
				p.err = cityartwork.ErrNotFound
				want = 404
			case "query":
				query = "?sourceMediaId=private"
				want = 400
			case "range":
				rangeHeader = "bytes=0-20"
				want = 400
			}
			w := cityArtworkRequest(s, false, query, rangeHeader)
			if w.Code != want || strings.Contains(w.Body.String(), "synthetic validated derivative") {
				t.Fatal(kind, w.Code, w.Body.String())
			}
			if (kind == "query" || kind == "range") && p.reads != 0 {
				t.Fatal("unsupported selectors reached storage")
			}
		})
	}
}
