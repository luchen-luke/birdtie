// Package threadrouteprovider obtains geometry only for server-authorized original
// thread legs. It never persists recommendations or invents missing road shapes.
package threadrouteprovider

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
)

type cacheEntry struct {
	geometry content.ThreadRouteGeometry
	until    time.Time
}
type Provider struct {
	amapKey, mapboxToken string
	client               *http.Client
	slots                chan struct{}
	mu                   sync.Mutex
	cache                map[[32]byte]cacheEntry
}

func New(amapKey, mapboxToken string) *Provider {
	return &Provider{amapKey: strings.TrimSpace(amapKey), mapboxToken: strings.TrimSpace(mapboxToken),
		client: &http.Client{Timeout: 6 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("route redirect refused") }},
		slots:  make(chan struct{}, 4), cache: make(map[[32]byte]cacheEntry)}
}
func unavailable(status, mode, provider string) content.ThreadRouteGeometry {
	return content.ThreadRouteGeometry{Status: status, Mode: mode, Provider: provider, CoordinateSystem: "wgs84", Segments: [][]content.ThreadRoutePoint{}}
}
func normalizeCity(v string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(v)), "市")
}
func region(country string) string {
	c := strings.ToUpper(strings.TrimSpace(country))
	if c == "CN" || c == "CHN" {
		return "amap"
	}
	if c == "HKG" || c == "MAC" || c == "TWN" {
		return "mapbox"
	}
	// Explicit catalog ISO country facts, never the coordinate-transform envelope.
	const iso = " AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ CA CC CD CF CG CH CI CK CL CM CO CR CU CV CW CX CY CZ DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI FJ FK FM FO FR GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU ID IE IL IM IN IO IQ IR IS IT JE JM JO JP KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ VA VC VE VG VI VN VU WF WS YE YT ZA ZM ZW "
	if len(c) == 2 && strings.Contains(iso, " "+c+" ") {
		return "mapbox"
	}
	return ""
}
func (p *Provider) Plan(ctx context.Context, r content.ThreadRouteRequest) (content.ThreadRouteGeometry, error) {
	if err := ctx.Err(); err != nil {
		return content.ThreadRouteGeometry{}, err
	}
	if !r.From.Valid() || !r.To.Valid() {
		return unavailable("NOT_AVAILABLE", r.Mode, ""), nil
	}
	if r.Mode == "train" || r.Mode == "flight" {
		return content.ThreadRouteGeometry{Status: "AVAILABLE", Mode: r.Mode, Provider: "saved-transfer", CoordinateSystem: "wgs84", GeometryKind: "TRANSFER", Segments: [][]content.ThreadRoutePoint{{r.From, r.To}}}, nil
	}
	provider := region(r.From.CountryCode)
	if provider == "" || region(r.To.CountryCode) != provider {
		return unavailable("BLOCKED", r.Mode, ""), nil
	}
	if p == nil || provider == "amap" && p.amapKey == "" || provider == "mapbox" && p.mapboxToken == "" {
		return unavailable("BLOCKED", r.Mode, provider), nil
	}
	if r.Mode != "recommended" && r.Mode != "walking" && r.Mode != "cycling" && r.Mode != "driving" && r.Mode != "transit" {
		return unavailable("NOT_AVAILABLE", r.Mode, provider), nil
	}
	// Only ephemeral route geometry is cached. Every use has a fresh domain ACL
	// proof, and this cache cannot prolong the response's original read lease.
	keyRaw, _ := json.Marshal(struct {
		Mode, Provider, FromCity, ToCity string
		A, B                             [2]float64
		Bucket                           int64
	}{r.Mode, provider, r.From.City, r.To.City, [2]float64{r.From.Latitude, r.From.Longitude}, [2]float64{r.To.Latitude, r.To.Longitude}, r.DepartureAt.Unix() / 300})
	key := sha256.Sum256(keyRaw)
	now := time.Now()
	p.mu.Lock()
	prior, ok := p.cache[key]
	p.mu.Unlock()
	if ok && prior.until.After(now) {
		return clone(prior.geometry), nil
	}
	select {
	case p.slots <- struct{}{}:
		defer func() { <-p.slots }()
	case <-ctx.Done():
		return content.ThreadRouteGeometry{}, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	modes := []string{r.Mode}
	if r.Mode == "recommended" {
		modes = []string{"walking", "cycling"}
		a, b := normalizeCity(r.From.City), normalizeCity(r.To.City)
		if a != "" && b != "" && a != b {
			modes = append(modes, "transit")
		} else {
			modes = append(modes, "driving", "transit")
		}
	}
	result := unavailable("NOT_AVAILABLE", r.Mode, provider)
	for _, mode := range modes {
		var v content.ThreadRouteGeometry
		if provider == "amap" {
			v = p.amap(ctx, r, mode)
		} else {
			v = p.mapbox(ctx, r, mode)
		}
		if ctx.Err() != nil {
			return content.ThreadRouteGeometry{}, ctx.Err()
		}
		if v.Status == "AVAILABLE" && (r.Mode != "recommended" || mode != "walking" && mode != "cycling" || v.DurationSeconds <= 900) {
			result = v
			break
		}
		if v.Status == "BLOCKED" {
			result = unavailable("BLOCKED", r.Mode, provider)
		}
	}
	ttl := 5 * time.Minute
	if result.Status != "AVAILABLE" {
		ttl = 30 * time.Second
	}
	p.mu.Lock()
	for k, v := range p.cache {
		if !v.until.After(now) {
			delete(p.cache, k)
		}
	}
	if len(p.cache) >= 128 {
		for k := range p.cache {
			delete(p.cache, k)
			break
		}
	}
	p.cache[key] = cacheEntry{clone(result), now.Add(ttl)}
	p.mu.Unlock()
	return result, nil
}
func clone(g content.ThreadRouteGeometry) content.ThreadRouteGeometry {
	out := g
	out.Segments = make([][]content.ThreadRoutePoint, len(g.Segments))
	for i, s := range g.Segments {
		out.Segments[i] = append([]content.ThreadRoutePoint{}, s...)
	}
	return out
}
func (p *Provider) get(ctx context.Context, endpoint string, params url.Values, out any) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return false
	}
	res, err := p.client.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		return false
	}
	return json.Unmarshal(raw, out) == nil
}
func pointText(p content.ThreadRoutePoint) string {
	return strconv.FormatFloat(p.Longitude, 'f', 6, 64) + "," + strconv.FormatFloat(p.Latitude, 'f', 6, 64)
}
func number(s string) int {
	f, e := strconv.ParseFloat(s, 64)
	if e != nil || math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 || f > 4e7 {
		return 0
	}
	return int(math.Round(f))
}

type amapCost struct {
	Duration string `json:"duration"`
}
type amapStep struct {
	Polyline string `json:"polyline"`
}
type amapPath struct {
	Distance string     `json:"distance"`
	Duration string     `json:"duration"`
	Cost     amapCost   `json:"cost"`
	Steps    []amapStep `json:"steps"`
}

func (p *Provider) amap(ctx context.Context, r content.ThreadRouteRequest, mode string) content.ThreadRouteGeometry {
	fail := unavailable("NOT_AVAILABLE", mode, "amap")
	if mode == "transit" {
		return unavailable("BLOCKED", mode, "amap")
	} // Current Civu transit supplies schedule metrics, not complete geometry. Never connect gaps.
	a, b := toAMap(r.From), toAMap(r.To)
	params := url.Values{"key": {p.amapKey}, "origin": {pointText(a)}, "destination": {pointText(b)}, "show_fields": {"polyline,cost"}}
	if mode == "driving" {
		params.Set("strategy", "32")
		params.Set("alternative_route", "0")
	}
	var data struct {
		Status   string `json:"status"`
		InfoCode string `json:"infocode"`
		Route    struct {
			Paths []amapPath `json:"paths"`
		} `json:"route"`
	}
	endpointMode := mode
	if mode == "cycling" {
		endpointMode = "bicycling"
	}
	if !p.get(ctx, "https://restapi.amap.com/v5/direction/"+endpointMode, params, &data) {
		return fail
	}
	if data.Status != "1" {
		switch data.InfoCode {
		case "10001", "10003", "10004", "10005", "10007", "10009", "10012", "10013", "10014", "10044":
			return unavailable("BLOCKED", mode, "amap")
		}
		return fail
	}
	if len(data.Route.Paths) == 0 {
		return fail
	}
	path := data.Route.Paths[0]
	duration := path.Cost.Duration
	if mode == "cycling" {
		duration = path.Duration
	}
	meters, seconds := number(path.Distance), number(duration)
	if meters <= 0 || seconds <= 0 || seconds > 7*86400 || len(path.Steps) == 0 || len(path.Steps) > 4000 {
		return fail
	}
	segments := [][]content.ThreadRoutePoint{}
	total := 0
	for _, step := range path.Steps {
		pts, ok := parseAMapPolyline(step.Polyline)
		if !ok {
			return fail
		}
		total += len(pts)
		if total > 24000 {
			return fail
		}
		for i, v := range pts {
			var valid bool
			pts[i], valid = fromAMap(v)
			if !valid {
				return fail
			}
		}
		// Adjacent steps join only at an actual shared provider coordinate. A gap
		// retains independent pieces, never a fabricated diagonal connector.
		if len(segments) > 0 && samePoint(segments[len(segments)-1][len(segments[len(segments)-1])-1], pts[0]) && len(segments[len(segments)-1])+len(pts)-1 <= 12000 {
			segments[len(segments)-1] = append(segments[len(segments)-1], pts[1:]...)
		} else {
			segments = append(segments, pts)
		}
		if len(segments) > 32 {
			return fail
		}
	}
	return content.ThreadRouteGeometry{Status: "AVAILABLE", Mode: mode, Provider: "amap", CoordinateSystem: "wgs84", GeometryKind: "ROAD", Segments: segments, DistanceMeters: meters, DurationSeconds: seconds}
}
func samePoint(a, b content.ThreadRoutePoint) bool {
	return math.Abs(a.Latitude-b.Latitude) < 1e-8 && math.Abs(a.Longitude-b.Longitude) < 1e-8
}
func parseAMapPolyline(raw string) ([]content.ThreadRoutePoint, bool) {
	pieces := strings.Split(raw, ";")
	if len(pieces) < 2 || len(pieces) > 12000 {
		return nil, false
	}
	pts := []content.ThreadRoutePoint{}
	for _, s := range pieces {
		v := strings.Split(s, ",")
		if len(v) != 2 {
			return nil, false
		}
		lon, e := strconv.ParseFloat(v[0], 64)
		lat, f := strconv.ParseFloat(v[1], 64)
		p := content.ThreadRoutePoint{Latitude: lat, Longitude: lon}
		if e != nil || f != nil || !p.Valid() {
			return nil, false
		}
		if len(pts) == 0 || !samePoint(pts[len(pts)-1], p) {
			pts = append(pts, p)
		}
	}
	return pts, len(pts) >= 2
}
func (p *Provider) mapbox(ctx context.Context, r content.ThreadRouteRequest, mode string) content.ThreadRouteGeometry {
	fail := unavailable("NOT_AVAILABLE", mode, "mapbox")
	if mode == "transit" {
		return unavailable("BLOCKED", mode, "mapbox")
	}
	params := url.Values{"access_token": {p.mapboxToken}, "geometries": {"geojson"}, "overview": {"full"}, "steps": {"false"}, "alternatives": {"false"}}
	var data struct {
		Code   string `json:"code"`
		Routes []struct {
			Distance, Duration float64
			Geometry           struct {
				Type        string
				Coordinates [][]float64
			}
		}
	}
	if !p.get(ctx, "https://api.mapbox.com/directions/v5/mapbox/"+mode+"/"+pointText(r.From)+";"+pointText(r.To), params, &data) || data.Code != "Ok" || len(data.Routes) == 0 {
		return fail
	}
	route := data.Routes[0]
	if route.Geometry.Type != "LineString" || len(route.Geometry.Coordinates) < 2 || len(route.Geometry.Coordinates) > 12000 || math.IsNaN(route.Distance) || math.IsInf(route.Distance, 0) || route.Distance <= 0 || route.Distance > 4e7 || math.IsNaN(route.Duration) || math.IsInf(route.Duration, 0) || route.Duration <= 0 || route.Duration > 7*86400 {
		return fail
	}
	pts := []content.ThreadRoutePoint{}
	for _, c := range route.Geometry.Coordinates {
		if len(c) != 2 {
			return fail
		}
		pt := content.ThreadRoutePoint{Longitude: c[0], Latitude: c[1]}
		if !pt.Valid() {
			return fail
		}
		if len(pts) == 0 || !samePoint(pts[len(pts)-1], pt) {
			pts = append(pts, pt)
		}
	}
	if len(pts) < 2 {
		return fail
	}
	return content.ThreadRouteGeometry{Status: "AVAILABLE", Mode: mode, Provider: "mapbox", CoordinateSystem: "wgs84", GeometryKind: "ROAD", Segments: [][]content.ThreadRoutePoint{pts}, DistanceMeters: int(math.Max(1, math.Round(route.Distance))), DurationSeconds: int(math.Max(1, math.Round(route.Duration)))}
}
