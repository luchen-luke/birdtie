package agenttool

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
)

const OpenStreetMapPlacesEndpoint = "https://overpass-api.de/api/interpreter"
const OpenStreetMapPlacesMaxResults = 15
const OpenStreetMapPlacesMaxRadiusMeters = 6000
const OpenStreetMapPlacesMaxTimeout = 8 * time.Second
const OpenStreetMapPlacesMaxResponseBytes = 512 * 1024
const openStreetMapQueryMemoryBytes = 32 * 1024 * 1024
const openStreetMapAttribution = "© OpenStreetMap contributors (ODbL 1.0)"
const openStreetMapLicenseURL = "https://opendatacommons.org/licenses/odbl/1-0/"

var ErrOpenStreetMapPlacesRequest = errors.New("invalid OpenStreetMap place request")
var ErrOpenStreetMapPlacesResponse = errors.New("invalid OpenStreetMap place response")

// The native caller supplies a real catalog city center after checking current
// city-editor access. This transport grants no actor, city, egress or review
// rights, and accepts neither client-provided URLs nor arbitrary Overpass QL.
type OpenPlaceQuery struct {
	Latitude, Longitude float64
	RadiusMeters        int
	Category            string
}

type OpenStreetMapPlaces struct {
	client *http.Client
	now    func() time.Time
}

// Only the optional client's Transport and shorter timeout are used, as a
// trusted server/test seam. Cookies and redirect handlers are never inherited;
// the caller's client is never changed. Nil uses a dedicated HTTP/1 transport
// without proxies, redirects, replay, reuse or automatic decompression.
func NewOpenStreetMapPlaces(client *http.Client) (*OpenStreetMapPlaces, error) {
	var transport http.RoundTripper
	timeout := OpenStreetMapPlacesMaxTimeout
	if client != nil {
		transport = client.Transport
		if client.Timeout < 0 {
			return nil, ErrOpenStreetMapPlacesRequest
		}
		if client.Timeout > 0 && client.Timeout < timeout {
			timeout = client.Timeout
		}
	}
	if transport == nil {
		protocols := new(http.Protocols)
		protocols.SetHTTP1(true)
		transport = &http.Transport{
			Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
			Protocols: protocols, ForceAttemptHTTP2: false,
			TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}},
			TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{},
			DisableKeepAlives: true, DisableCompression: true, MaxConnsPerHost: 1,
			TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 5 * time.Second,
		}
	} else {
		v := reflect.ValueOf(transport)
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
			if v.IsNil() {
				return nil, ErrOpenStreetMapPlacesRequest
			}
		}
	}
	return &OpenStreetMapPlaces{now: time.Now, client: &http.Client{
		Transport: transport, Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

type openPlaceCategory struct{ key, value string }

// Literal pairs only: no regex, free-text tag, union or caller-supplied QL.
func openPlaceCategoryFor(category string) (openPlaceCategory, bool) {
	switch category {
	case "restaurant":
		return openPlaceCategory{"amenity", "restaurant"}, true
	case "cafe":
		return openPlaceCategory{"amenity", "cafe"}, true
	case "library":
		return openPlaceCategory{"amenity", "library"}, true
	case "museum":
		return openPlaceCategory{"tourism", "museum"}, true
	case "gallery":
		return openPlaceCategory{"tourism", "gallery"}, true
	case "park":
		return openPlaceCategory{"leisure", "park"}, true
	}
	return openPlaceCategory{}, false
}

func openPlaceCoordinatesValid(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsInf(lat, 0) && !math.IsNaN(lon) && !math.IsInf(lon, 0) && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}

// Search performs one bounded public-data request and returns unreviewed
// SubmitInput candidates. It never inserts Places or substitutes for the
// original human session, Submit audit or independent native review. A node's
// coordinate is a mapped point, not a verified building entrance. Opening hours
// remain verbatim community tags, without an inferred timezone or open-now claim.
func (a *OpenStreetMapPlaces) Search(ctx context.Context, query OpenPlaceQuery) ([]cityseed.SubmitInput, error) {
	if a == nil || a.client == nil || a.now == nil || ctx == nil {
		return nil, ErrOpenStreetMapPlacesRequest
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	category, ok := openPlaceCategoryFor(query.Category)
	if !ok || !openPlaceCoordinatesValid(query.Latitude, query.Longitude) || query.RadiusMeters < 1 || query.RadiusMeters > OpenStreetMapPlacesMaxRadiusMeters {
		return nil, ErrOpenStreetMapPlacesRequest
	}
	ql := fmt.Sprintf(`[out:json][timeout:8][maxsize:%d];node["%s"="%s"]["name"](around:%d,%s,%s);out body %d;`,
		openStreetMapQueryMemoryBytes, category.key, category.value, query.RadiusMeters,
		strconv.FormatFloat(query.Latitude, 'f', -1, 64), strconv.FormatFloat(query.Longitude, 'f', -1, 64), OpenStreetMapPlacesMaxResults)
	body := url.Values{"data": {ql}}.Encode()
	callctx, cancel := context.WithTimeout(ctx, a.client.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callctx, http.MethodPost, OpenStreetMapPlacesEndpoint, strings.NewReader(body))
	if err != nil {
		return nil, ErrOpenStreetMapPlacesRequest
	}
	req.GetBody = nil // A POST must not be replayed following a redirect or retry.
	req.Close = true
	req.Proto, req.ProtoMajor, req.ProtoMinor = "HTTP/1.1", 1, 1
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Birdtie/1.0 (OpenStreetMap place candidate acquisition)")
	resp, err := a.client.Do(req)
	if err != nil {
		if current := callctx.Err(); current != nil {
			return nil, current
		}
		return nil, ErrOpenStreetMapPlacesResponse // No raw URL, query or upstream message.
	}
	if resp == nil || resp.Body == nil {
		return nil, ErrOpenStreetMapPlacesResponse
	}
	defer resp.Body.Close()
	if err := callctx.Err(); err != nil {
		return nil, err
	}
	mediaType, _, mimeErr := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if resp.StatusCode != http.StatusOK || resp.ProtoMajor != 1 || mimeErr != nil || mediaType != "application/json" || resp.Uncompressed || resp.Header.Get("Content-Encoding") != "" || resp.ContentLength > OpenStreetMapPlacesMaxResponseBytes {
		return nil, ErrOpenStreetMapPlacesResponse
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, OpenStreetMapPlacesMaxResponseBytes+1))
	defer clear(raw)
	if err != nil || len(raw) == 0 || len(raw) > OpenStreetMapPlacesMaxResponseBytes {
		return nil, ErrOpenStreetMapPlacesResponse
	}
	if err := callctx.Err(); err != nil {
		return nil, err
	}
	observedAt := a.now().UTC().Truncate(time.Microsecond)
	return parseOpenStreetMapPlaces(raw, query, category, observedAt)
}

type openPlaceElement struct {
	Type string            `json:"type"`
	ID   int64             `json:"id"`
	Lat  *float64          `json:"lat"`
	Lon  *float64          `json:"lon"`
	Tags map[string]string `json:"tags"`
}

func parseOpenStreetMapPlaces(raw []byte, query OpenPlaceQuery, category openPlaceCategory, at time.Time) ([]cityseed.SubmitInput, error) {
	// Reuse the package's duplicate-key/depth JSON guard; do not create another
	// permissive parser. Unknown OSM metadata/tags are never copied to candidates.
	if !utf8.Valid(raw) || wsaStrictJSON(raw) != nil || at.IsZero() {
		return nil, ErrOpenStreetMapPlacesResponse
	}
	var envelope struct {
		Elements json.RawMessage `json:"elements"`
		Remark   string          `json:"remark"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Remark != "" || len(envelope.Elements) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Elements), []byte("null")) {
		return nil, ErrOpenStreetMapPlacesResponse
	}
	var elements []openPlaceElement
	if json.Unmarshal(envelope.Elements, &elements) != nil || elements == nil || len(elements) > OpenStreetMapPlacesMaxResults {
		return nil, ErrOpenStreetMapPlacesResponse
	}
	out := make([]cityseed.SubmitInput, 0, len(elements))
	seen := make(map[int64]bool)
	for _, e := range elements {
		// Way/relation centers are never reinterpreted as point/entrance facts.
		if e.Type != "node" {
			continue
		}
		if e.ID < 1 || seen[e.ID] {
			return nil, ErrOpenStreetMapPlacesResponse
		}
		seen[e.ID] = true
		if e.Lat == nil || e.Lon == nil || !openPlaceCoordinatesValid(*e.Lat, *e.Lon) || e.Tags[category.key] != category.value || !cityseed.SourceTextValid(e.Tags["name"], 150) {
			continue
		}
		if openPlaceDistanceMeters(query.Latitude, query.Longitude, *e.Lat, *e.Lon) > float64(query.RadiusMeters) {
			continue
		}
		candidate := openPlaceCandidate(e, query.Category, at)
		if _, _, err := cityseed.NormalizeSourcePermission(candidate, at); err != nil {
			return nil, ErrOpenStreetMapPlacesResponse
		}
		out = append(out, candidate)
	}
	return out, nil
}

func openPlaceDistanceMeters(lat1, lon1, lat2, lon2 float64) float64 {
	const toRadians = math.Pi / 180
	dLat, dLon := (lat2-lat1)*toRadians, (lon2-lon1)*toRadians
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*toRadians)*math.Cos(lat2*toRadians)*math.Sin(dLon/2)*math.Sin(dLon/2)
	h = math.Max(0, math.Min(1, h))
	return 6371000 * 2 * math.Atan2(math.Sqrt(h), math.Sqrt(1-h))
}

func openPlaceAddress(tags map[string]string) string {
	if full, ok := tags["addr:full"]; ok {
		if cityseed.SourceTextValid(full, 1200) && utf8.RuneCountInString(full) <= 240 {
			return full
		}
		return ""
	}
	var parts []string
	for _, key := range []string{"addr:housenumber", "addr:street", "addr:postcode", "addr:city"} {
		if value, ok := tags[key]; ok {
			if !cityseed.SourceTextValid(value, 1200) {
				return ""
			}
			parts = append(parts, value)
		}
	}
	address := strings.Join(parts, " ")
	if cityseed.SourceTextValid(address, 1200) && utf8.RuneCountInString(address) <= 240 {
		return address
	}
	return ""
}

func openPlaceCandidate(e openPlaceElement, category string, at time.Time) cityseed.SubmitInput {
	expiresAt := at.Add(24 * time.Hour) // Birdtie candidate freshness cap, not an ODbL expiry.
	lat, lon := *e.Lat, *e.Lon
	candidate := cityseed.SubmitInput{
		Name: e.Tags["name"], CategoryCode: category, AddressLabel: openPlaceAddress(e.Tags),
		Latitude: &lat, Longitude: &lon, LocationPrecision: "point",
		SourceLabel: "OpenStreetMap 社区地图数据", SourceURL: "https://www.openstreetmap.org/node/" + strconv.FormatInt(e.ID, 10),
		RightsNote:   "依据 ODbL 1.0 提取的社区地图字段候选；保留 OpenStreetMap 署名，须经 Birdtie 原有独立人审；坐标非已核验入口，营业时间未作营业状态推断。",
		ProviderCode: "openstreetmap", ProviderPlaceID: "node/" + strconv.FormatInt(e.ID, 10),
		Attribution: openStreetMapAttribution, ExpiresAt: expiresAt,
		Rights: &cityseed.SourcePermissionInput{
			State: cityseed.PermissionConfirmed, Acquisition: "OPEN_LICENSE", OriginProvider: "OPEN_DATA_DOWNLOAD",
			LicenseURL: openStreetMapLicenseURL, LicenseVersion: "ODbL-1.0", Attribution: openStreetMapAttribution,
			AcquiredAt: at, RetentionUntil: expiresAt,
		},
	}
	field := func(key string, value any, until time.Time) cityseed.PlaceFieldInput {
		raw, _ := json.Marshal(value) // Values below have only validated strings and finite floats.
		return cityseed.PlaceFieldInput{Key: key, Value: raw, ObservedAt: at, ValidFrom: at, ValidTo: until, RefreshDue: until, ConflictState: "NONE"}
	}
	candidate.Fields = append(candidate.Fields, field("name", candidate.Name, expiresAt))
	if candidate.AddressLabel != "" {
		candidate.Fields = append(candidate.Fields, field("address", candidate.AddressLabel, expiresAt))
	}
	candidate.Fields = append(candidate.Fields, field("coordinates", struct {
		CoordinateSystem string  `json:"coordinateSystem"`
		Latitude         float64 `json:"latitude"`
		Longitude        float64 `json:"longitude"`
	}{"wgs84", lat, lon}, expiresAt))
	if hours := e.Tags["opening_hours"]; cityseed.SourceTextValid(hours, 1200) {
		candidate.Fields = append(candidate.Fields, field("opening_hours", hours, at.Add(6*time.Hour)))
	}
	return candidate
}
