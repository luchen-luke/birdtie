package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
)

// All responses in this file are synthetic offline fixtures. No provider or
// database calls, imported real POIs, model runs or review grants occur here.
type openPlaceRoundTrip func(*http.Request) (*http.Response, error)

func (f openPlaceRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

var openPlaceTestTime = time.Date(2026, 10, 9, 3, 4, 5, 123456000, time.UTC)
var openPlaceTestQuery = OpenPlaceQuery{Latitude: 57.1497, Longitude: -2.0943, RadiusMeters: 6000, Category: "museum"}

func openPlaceTestResponse(raw string) *http.Response {
	return &http.Response{StatusCode: 200, ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{"Content-Type": {"application/json; charset=utf-8"}},
		Body:   io.NopCloser(strings.NewReader(raw)), ContentLength: int64(len(raw))}
}

func openPlaceTestAdapter(t *testing.T, transport openPlaceRoundTrip) *OpenStreetMapPlaces {
	t.Helper()
	a, err := NewOpenStreetMapPlaces(&http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	a.now = func() time.Time { return openPlaceTestTime }
	return a
}

func openPlaceTestNode(tags map[string]string) map[string]any {
	if tags == nil {
		tags = map[string]string{"name": "SYNTHETIC unit museum", "tourism": "museum"}
	}
	return map[string]any{"type": "node", "id": int64(999900001), "lat": 57.1497, "lon": -2.0943, "tags": tags}
}

func openPlaceTestJSON(t *testing.T, elements ...any) string {
	t.Helper()
	if elements == nil {
		elements = []any{}
	}
	b, err := json.Marshal(map[string]any{"version": 0.6, "generator": "Synthetic offline Overpass fixture", "elements": elements})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestOpenStreetMapConstructorBoundsAndIsolation(t *testing.T) {
	a, err := NewOpenStreetMapPlaces(nil)
	if err != nil || a.client.Timeout != 8*time.Second || a.client.Jar != nil {
		t.Fatalf("unsafe constructor: %v", err)
	}
	tr, ok := a.client.Transport.(*http.Transport)
	if !ok || tr.Proxy != nil || !tr.DisableCompression || !tr.DisableKeepAlives || tr.MaxConnsPerHost != 1 || tr.ForceAttemptHTTP2 || !tr.Protocols.HTTP1() || tr.Protocols.HTTP2() || tr.Protocols.UnencryptedHTTP2() || tr.TLSClientConfig.InsecureSkipVerify || tr.TLSClientConfig.MinVersion < 0x0303 {
		t.Fatal("default transport must be dedicated, TLS-verified HTTP/1 without proxy/reuse/decompression")
	}
	if a.client.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("redirects allowed")
	}
	jar, _ := cookiejar.New(nil)
	redirects := 0
	caller := &http.Client{Timeout: time.Second, Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { redirects++; return nil }}
	b, err := NewOpenStreetMapPlaces(caller)
	if err != nil || b.client == caller || b.client.Timeout != time.Second || b.client.Jar != nil || b.client.CheckRedirect(nil, nil) != http.ErrUseLastResponse || redirects != 0 || caller.Jar != jar || caller.Timeout != time.Second {
		t.Fatal("caller client was changed or unsafe fields inherited")
	}
	for _, timeout := range []time.Duration{0, time.Minute} {
		bounded, err := NewOpenStreetMapPlaces(&http.Client{Timeout: timeout})
		if err != nil || bounded.client.Timeout != OpenStreetMapPlacesMaxTimeout {
			t.Fatal("unbounded supplied timeout")
		}
	}
	if _, err := NewOpenStreetMapPlaces(&http.Client{Timeout: -time.Second}); !errors.Is(err, ErrOpenStreetMapPlacesRequest) {
		t.Fatal("negative timeout accepted")
	}
	var nilTransport openPlaceRoundTrip
	if _, err := NewOpenStreetMapPlaces(&http.Client{Transport: nilTransport}); !errors.Is(err, ErrOpenStreetMapPlacesRequest) {
		t.Fatal("typed nil transport accepted")
	}
}

func TestOpenStreetMapFixedRequestAndWhitelist(t *testing.T) {
	for category, pair := range map[string]string{"museum": `"tourism"="museum"`, "gallery": `"tourism"="gallery"`, "restaurant": `"amenity"="restaurant"`, "cafe": `"amenity"="cafe"`, "library": `"amenity"="library"`, "park": `"leisure"="park"`} {
		t.Run(category, func(t *testing.T) {
			calls := 0
			a := openPlaceTestAdapter(t, func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.String() != OpenStreetMapPlacesEndpoint || req.Method != "POST" || req.GetBody != nil || !req.Close || req.ProtoMajor != 1 || req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" || req.Header.Get("Accept-Encoding") != "" || req.Header.Get("Accept") != "application/json" {
					t.Fatal("request endpoint, method, protocol or replay/auth isolation changed")
				}
				deadline, ok := req.Context().Deadline()
				if !ok || time.Until(deadline) > 8*time.Second || time.Until(deadline) <= 0 {
					t.Fatal("request has no bounded deadline")
				}
				raw, _ := io.ReadAll(req.Body)
				form, err := url.ParseQuery(string(raw))
				want := `[out:json][timeout:8][maxsize:33554432];node[` + pair + `]["name"](around:6000,57.1497,-2.0943);out body 15;`
				if err != nil || len(form) != 1 || len(form["data"]) != 1 || form.Get("data") != want || len(raw) > 1024 || !strings.HasPrefix(req.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
					t.Fatalf("unexpected QL form: %q", form.Get("data"))
				}
				return openPlaceTestResponse(`{"elements":[]}`), nil
			})
			q := openPlaceTestQuery
			q.Category = category
			results, err := a.Search(context.Background(), q)
			if err != nil || results == nil || len(results) != 0 || calls != 1 {
				t.Fatalf("empty valid response: results %v, error %v, calls %d", results, err, calls)
			}
		})
	}
}

func TestOpenStreetMapInvalidQueryMakesNoRequest(t *testing.T) {
	cases := map[string]OpenPlaceQuery{}
	add := func(name string, change func(*OpenPlaceQuery)) { q := openPlaceTestQuery; change(&q); cases[name] = q }
	add("category injection", func(q *OpenPlaceQuery) { q.Category = `museum"];out meta;` })
	add("category URL", func(q *OpenPlaceQuery) { q.Category = "https://example.invalid" })
	add("category whitespace", func(q *OpenPlaceQuery) { q.Category = " museum" })
	add("category case", func(q *OpenPlaceQuery) { q.Category = "Museum" })
	add("missing category", func(q *OpenPlaceQuery) { q.Category = "" })
	add("zero radius", func(q *OpenPlaceQuery) { q.RadiusMeters = 0 })
	add("negative radius", func(q *OpenPlaceQuery) { q.RadiusMeters = -1 })
	add("excess radius", func(q *OpenPlaceQuery) { q.RadiusMeters = 6001 })
	add("latitude range", func(q *OpenPlaceQuery) { q.Latitude = 90.1 })
	add("longitude range", func(q *OpenPlaceQuery) { q.Longitude = -180.1 })
	add("latitude NaN", func(q *OpenPlaceQuery) { q.Latitude = math.NaN() })
	add("longitude infinity", func(q *OpenPlaceQuery) { q.Longitude = math.Inf(1) })
	for name, q := range cases {
		t.Run(name, func(t *testing.T) {
			calls := 0
			a := openPlaceTestAdapter(t, func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("must not call") })
			if result, err := a.Search(context.Background(), q); !errors.Is(err, ErrOpenStreetMapPlacesRequest) || result != nil || calls != 0 {
				t.Fatalf("invalid query called provider: %v, %d", err, calls)
			}
		})
	}
}

func TestOpenStreetMapCandidateReusesNativeRightsAndFieldContracts(t *testing.T) {
	tags := map[string]string{"name": "SYNTHETIC 多语言博物馆 Musée", "tourism": "museum", "addr:housenumber": "12", "addr:street": "SYNTHETIC 展览路", "addr:postcode": "SYN 1", "addr:city": "SYNTHETIC 城市", "opening_hours": `Mo-Fr 10:00-18:00; "需预约"`, "description": "DO_NOT_COPY recommendation", "review": "DO_NOT_COPY review", "image": "https://example.invalid/DO_NOT_COPY-media", "contact:phone": "DO_NOT_COPY phone", "cuisine": "DO_NOT_COPY cuisine"}
	node := openPlaceTestNode(tags)
	node["user"] = "DO_NOT_COPY user"
	node["uid"] = 111
	node["timestamp"] = "2000-01-01T00:00:00Z"
	a := openPlaceTestAdapter(t, func(*http.Request) (*http.Response, error) {
		return openPlaceTestResponse(openPlaceTestJSON(t, node)), nil
	})
	results, err := a.Search(context.Background(), openPlaceTestQuery)
	if err != nil || len(results) != 1 {
		t.Fatalf("candidate: %v, %v", results, err)
	}
	c := results[0]
	if c.Name != tags["name"] || c.CategoryCode != "museum" || c.Summary != "" || c.AddressLabel != "12 SYNTHETIC 展览路 SYN 1 SYNTHETIC 城市" || c.Latitude == nil || *c.Latitude != 57.1497 || c.Longitude == nil || *c.Longitude != -2.0943 || c.LocationPrecision != "point" || c.ProviderCode != "openstreetmap" || c.ProviderPlaceID != "node/999900001" || c.SourceURL != "https://www.openstreetmap.org/node/999900001" || strings.Contains(c.SourceLabel, "官方") {
		t.Fatal("canonical candidate fields were invented or lost")
	}
	rights, fields, err := cityseed.NormalizeSourcePermission(c, openPlaceTestTime)
	if err != nil || rights.Acquisition != "OPEN_LICENSE" || rights.OriginProvider != "OPEN_DATA_DOWNLOAD" || rights.LicenseURL != openStreetMapLicenseURL || rights.Attribution != openStreetMapAttribution || !c.ExpiresAt.Equal(openPlaceTestTime.Add(24*time.Hour)) || !rights.AcquiredAt.Equal(openPlaceTestTime) || len(fields) != 4 {
		t.Fatalf("native permission or fields rejected: %v", err)
	}
	if cityseed.PermissionUsable(cityseed.SourcePermission{Version: 1, SourcePermissionInput: rights, DeclaredBy: "synthetic-editor"}, openPlaceTestTime) {
		t.Fatal("license declaration incorrectly substitutes for independent human review")
	}
	for _, f := range fields {
		if !f.ObservedAt.Equal(openPlaceTestTime) || !f.ValidFrom.Equal(openPlaceTestTime) || f.ConflictState != "NONE" || f.ValidTo.After(c.ExpiresAt) || !f.RefreshDue.Equal(f.ValidTo) {
			t.Fatal("field observation/deadline is not bounded by original candidate")
		}
		if f.Key == "opening_hours" {
			var hours string
			if json.Unmarshal(f.Value, &hours) != nil || hours != tags["opening_hours"] || !f.ValidTo.Equal(openPlaceTestTime.Add(6*time.Hour)) {
				t.Fatal("opening-hours string interpreted or deadline lost")
			}
		}
	}
	serialized, _ := json.Marshal(c)
	if strings.Contains(string(serialized), "DO_NOT_COPY") || strings.Contains(string(serialized), `"user"`) || strings.Contains(string(serialized), `"status"`) || strings.Contains(string(serialized), `"reviewedBy"`) {
		t.Fatal("unapproved metadata, media or review state retained")
	}
}

func TestOpenStreetMapNodesOnlyNoInventedMissingFields(t *testing.T) {
	for _, name := range []string{"way center", "relation center", "missing latitude", "missing longitude", "missing name", "empty name", "name control", "name too long", "outside radius", "wrong category", "invalid latitude"} {
		t.Run(name, func(t *testing.T) {
			node := openPlaceTestNode(nil)
			tags := node["tags"].(map[string]string)
			switch name {
			case "way center", "relation center":
				node["type"] = strings.Split(name, " ")[0]
				delete(node, "lat")
				delete(node, "lon")
				node["center"] = map[string]float64{"lat": 57.1497, "lon": -2.0943}
			case "missing latitude":
				delete(node, "lat")
			case "missing longitude":
				delete(node, "lon")
			case "missing name":
				delete(tags, "name")
			case "empty name":
				tags["name"] = "  "
			case "name control":
				tags["name"] = "SYNTHETIC\u200bname"
			case "name too long":
				tags["name"] = strings.Repeat("名", 51)
			case "outside radius":
				node["lat"] = 0
			case "wrong category":
				tags["tourism"] = "hotel"
			case "invalid latitude":
				node["lat"] = 91
			}
			a := openPlaceTestAdapter(t, func(*http.Request) (*http.Response, error) {
				return openPlaceTestResponse(openPlaceTestJSON(t, node)), nil
			})
			results, err := a.Search(context.Background(), openPlaceTestQuery)
			if err != nil || len(results) != 0 {
				t.Fatalf("ineligible element became a point candidate: %v, %v", results, err)
			}
		})
	}
	for _, tags := range []map[string]string{
		{"name": "SYNTHETIC unit museum", "tourism": "museum"},
		{"name": "SYNTHETIC unit museum", "tourism": "museum", "addr:full": "bad\naddress", "addr:street": "must not replace rejected full address", "opening_hours": "bad\nhours"},
	} {
		a := openPlaceTestAdapter(t, func(*http.Request) (*http.Response, error) {
			return openPlaceTestResponse(openPlaceTestJSON(t, openPlaceTestNode(tags))), nil
		})
		results, err := a.Search(context.Background(), openPlaceTestQuery)
		if err != nil || len(results) != 1 || results[0].AddressLabel != "" || len(results[0].Fields) != 2 {
			t.Fatal("missing/invalid optional fields replaced with city or invented hours")
		}
	}
	if got := openPlaceAddress(map[string]string{"addr:full": "SYNTHETIC 原地址", "addr:city": "ignored"}); got != "SYNTHETIC 原地址" {
		t.Fatal("full address not preserved verbatim")
	}
	if got := openPlaceAddress(map[string]string{"addr:street": "SYNTHETIC road", "addr:city": "bad\u200bvalue"}); got != "" {
		t.Fatal("invalid address component silently dropped")
	}
}

func TestOpenStreetMapMalformedOrPartialResponseRejected(t *testing.T) {
	tooMany := make([]any, 16)
	for i := range tooMany {
		node := openPlaceTestNode(nil)
		node["id"] = int64(i + 1)
		tooMany[i] = node
	}
	cases := map[string]string{
		"missing elements": `{}`, "null elements": `{"elements":null}`, "not array": `{"elements":{}}`,
		"null envelope": `null`, "invalid JSON": `{"elements":[}`, "trailing JSON": `{"elements":[]} {}`,
		"duplicate elements": `{"elements":[],"elements":[]}`, "duplicate discarded tag": `{"elements":[{"type":"node","id":1,"tags":{"image":"a","image":"b"}}]}`,
		"provider partial failure": `{"elements":[],"remark":"DO_NOT_COPY runtime error"}`, "invalid remark type": `{"elements":[],"remark":17}`,
		"fractional id": `{"elements":[{"type":"node","id":1.5}]}`, "string id": `{"elements":[{"type":"node","id":"1"}]}`,
		"negative id": `{"elements":[{"type":"node","id":-1}]}`, "missing id": `{"elements":[{"type":"node"}]}`,
		"duplicate node":          openPlaceTestJSON(t, openPlaceTestNode(nil), openPlaceTestNode(nil)),
		"excess results":          openPlaceTestJSON(t, tooMany...),
		"deep discarded metadata": `{"elements":[],"ignored":` + strings.Repeat(`[`, 18) + `0` + strings.Repeat(`]`, 18) + `}`,
		"invalid UTF8":            "{\"elements\":[],\"ignored\":\"" + string([]byte{0xff}) + "\"}",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			a := openPlaceTestAdapter(t, func(*http.Request) (*http.Response, error) { return openPlaceTestResponse(raw), nil })
			result, err := a.Search(context.Background(), openPlaceTestQuery)
			if !errors.Is(err, ErrOpenStreetMapPlacesResponse) || result != nil || strings.Contains(err.Error(), "DO_NOT_COPY") {
				t.Fatalf("unsafe response accepted: %v, %v", result, err)
			}
		})
	}
}

type openPlaceCountRead struct {
	bytesRead int
	closed    bool
}

func (r *openPlaceCountRead) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	r.bytesRead += len(p)
	return len(p), nil
}
func (r *openPlaceCountRead) Close() error { r.closed = true; return nil }

func TestOpenStreetMapHTTPResponseBounds(t *testing.T) {
	for _, name := range []string{"redirect", "rate limit", "server failure", "html", "http2", "gzip", "auto decompressed", "declared large body", "stream large body", "read failure"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			reader := &openPlaceCountRead{}
			a := openPlaceTestAdapter(t, func(*http.Request) (*http.Response, error) {
				calls++
				resp := openPlaceTestResponse(`{"elements":[]}`)
				switch name {
				case "redirect":
					resp.StatusCode = 302
					resp.Header.Set("Location", "https://example.invalid/private")
				case "rate limit":
					resp.StatusCode = 429
				case "server failure":
					resp.StatusCode = 503
				case "html":
					resp.Header.Set("Content-Type", "text/html")
				case "http2":
					resp.ProtoMajor = 2
				case "gzip":
					resp.Header.Set("Content-Encoding", "gzip")
				case "auto decompressed":
					resp.Uncompressed = true
				case "declared large body":
					resp.ContentLength = OpenStreetMapPlacesMaxResponseBytes + 1
					resp.Body = reader
				case "stream large body":
					resp.ContentLength = -1
					resp.Body = reader
				case "read failure":
					resp.Body = io.NopCloser(openPlaceFailRead{})
				}
				return resp, nil
			})
			result, err := a.Search(context.Background(), openPlaceTestQuery)
			if !errors.Is(err, ErrOpenStreetMapPlacesResponse) || result != nil || calls != 1 {
				t.Fatalf("HTTP bounds/retry failure: %v, %v, %d", result, err, calls)
			}
			if name == "declared large body" && (reader.bytesRead != 0 || !reader.closed) {
				t.Fatal("declared oversize body was read or leaked")
			}
			if name == "stream large body" && (reader.bytesRead != OpenStreetMapPlacesMaxResponseBytes+1 || !reader.closed) {
				t.Fatalf("stream read unbounded: %d", reader.bytesRead)
			}
		})
	}
}

type openPlaceFailRead struct{}

func (openPlaceFailRead) Read([]byte) (int, error) {
	return 0, errors.New("DO_NOT_COPY upstream read failure")
}

type openPlaceCancelRead struct{ cancel context.CancelFunc }

func (r openPlaceCancelRead) Read(p []byte) (int, error) {
	r.cancel()
	return copy(p, `{"elements":[]}`), io.EOF
}

func TestOpenStreetMapCancellationAndTransportFailure(t *testing.T) {
	calls := 0
	a := openPlaceTestAdapter(t, func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("DO_NOT_COPY transport URL and body")
	})
	if result, err := a.Search(context.Background(), openPlaceTestQuery); !errors.Is(err, ErrOpenStreetMapPlacesResponse) || result != nil || strings.Contains(err.Error(), "DO_NOT_COPY") || calls != 1 {
		t.Fatal("raw error leaked or retried")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Search(ctx, openPlaceTestQuery); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("cancelled request was dispatched")
	}
	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()
	if _, err := a.Search(deadlineCtx, openPlaceTestQuery); !errors.Is(err, context.DeadlineExceeded) || calls != 1 {
		t.Fatal("expired request was dispatched")
	}
	if _, err := a.Search(nil, openPlaceTestQuery); !errors.Is(err, ErrOpenStreetMapPlacesRequest) {
		t.Fatal("nil context accepted")
	}
	var nilAdapter *OpenStreetMapPlaces
	if _, err := nilAdapter.Search(context.Background(), openPlaceTestQuery); !errors.Is(err, ErrOpenStreetMapPlacesRequest) {
		t.Fatal("nil adapter accepted")
	}
	active, activeCancel := context.WithCancel(context.Background())
	defer activeCancel()
	b := openPlaceTestAdapter(t, func(*http.Request) (*http.Response, error) {
		resp := openPlaceTestResponse(`{"elements":[]}`)
		resp.Body = io.NopCloser(openPlaceCancelRead{activeCancel})
		return resp, nil
	})
	if result, err := b.Search(active, openPlaceTestQuery); !errors.Is(err, context.Canceled) || result != nil {
		t.Fatal("late cancellation created fresh candidates")
	}
	earlyCtx, earlyCancel := context.WithTimeout(context.Background(), time.Second)
	defer earlyCancel()
	c := openPlaceTestAdapter(t, func(req *http.Request) (*http.Response, error) {
		want, _ := earlyCtx.Deadline()
		got, _ := req.Context().Deadline()
		if !got.Equal(want) {
			t.Fatal("outer deadline extended")
		}
		return openPlaceTestResponse(`{"elements":[]}`), nil
	})
	if _, err := c.Search(earlyCtx, openPlaceTestQuery); err != nil {
		t.Fatal(err)
	}
}

func TestOpenStreetMapGeometryUsesReturnedNodeAcrossAntimeridian(t *testing.T) {
	q := OpenPlaceQuery{Latitude: 0, Longitude: 179.99, RadiusMeters: 6000, Category: "museum"}
	node := openPlaceTestNode(nil)
	node["lat"] = 0.0
	node["lon"] = -179.99
	a := openPlaceTestAdapter(t, func(*http.Request) (*http.Response, error) {
		return openPlaceTestResponse(openPlaceTestJSON(t, node)), nil
	})
	results, err := a.Search(context.Background(), q)
	if err != nil || len(results) != 1 || *results[0].Longitude != -179.99 || *results[0].Latitude != 0 {
		t.Fatalf("actual node coordinates lost at meridian: %v", err)
	}
	keys := []string{}
	for _, f := range results[0].Fields {
		keys = append(keys, f.Key)
	}
	if !reflect.DeepEqual(keys, []string{"name", "coordinates"}) {
		t.Fatal("absent address or hours fabricated")
	}
}
