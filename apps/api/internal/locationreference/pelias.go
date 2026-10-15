package locationreference

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Pelias struct {
	base   string
	client *http.Client
}

func NewPelias(base string) (*Pelias, error) {
	u, e := url.Parse(base)
	if e != nil || u.Scheme != "http" || u.Host != "127.0.0.1:14016" || u.Path != "" || u.RawQuery != "" || u.User != nil || u.Fragment != "" {
		return nil, ErrInvalid
	}
	return &Pelias{base: base, client: &http.Client{Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrUnavailable }}}, nil
}

type feature struct {
	Geometry struct {
		Type        string    `json:"type"`
		Coordinates []float64 `json:"coordinates"`
	} `json:"geometry"`
	Properties struct {
		GID      string `json:"gid"`
		Source   string `json:"source"`
		Layer    string `json:"layer"`
		SourceID string `json:"source_id"`
		Name     string `json:"name"`
		Label    string `json:"label"`
		Region   string `json:"region"`
		Country  string `json:"country_a"`
	} `json:"properties"`
}

func (p *Pelias) query(ctx context.Context, path string, q url.Values) ([]feature, error) {
	req, e := http.NewRequestWithContext(ctx, "GET", p.base+path+"?"+q.Encode(), nil)
	if e != nil {
		return nil, ErrUnavailable
	}
	res, e := p.client.Do(req)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, ErrUnavailable
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, 512*1024+1))
	if e != nil || len(b) > 512*1024 {
		return nil, ErrUnavailable
	}
	var v struct {
		Features []feature `json:"features"`
	}
	if json.Unmarshal(b, &v) != nil || v.Features == nil || len(v.Features) > 10 {
		return nil, ErrUnavailable
	}
	return v.Features, nil
}
func (p *Pelias) Search(ctx context.Context, q string, n int) ([]Candidate, error) {
	if ValidateQuery(q, n) != nil {
		return nil, ErrInvalid
	}
	fs, e := p.query(ctx, "/v1/autocomplete", url.Values{"text": {q}, "size": {strconv.Itoa(n)}, "sources": {"openstreetmap"}, "layers": {"venue,address"}})
	if e != nil {
		return nil, e
	}
	out := []Candidate{}
	seen := map[string]bool{}
	for _, f := range fs {
		id, kind, obj, ok := OSMIdentity(f.Properties.SourceID)
		if !ok || seen[id] || f.Properties.Source != "openstreetmap" || (f.Properties.Layer != "venue" && f.Properties.Layer != "address") || f.Properties.GID != "openstreetmap:"+f.Properties.Layer+":"+f.Properties.SourceID {
			continue
		}
		// An exact identity lookup confirms that the selectable suggestion is resolvable.
		gid := f.Properties.GID
		detail, e := p.query(ctx, "/v1/place", url.Values{"ids": {gid}})
		if e != nil || len(detail) != 1 {
			continue
		}
		f = detail[0]
		if f.Properties.GID != gid || (f.Properties.Layer != "venue" && f.Properties.Layer != "address") || f.Properties.Source != "openstreetmap" || f.Properties.SourceID != kind+"/"+obj || f.Geometry.Type != "Point" || len(f.Geometry.Coordinates) != 2 {
			continue
		}
		at := time.Now().UTC()
		v := Candidate{ReferenceID: id, ObjectKind: kind, ObjectID: obj, Name: strings.TrimSpace(f.Properties.Name), Address: strings.TrimSpace(f.Properties.Label), Region: strings.TrimSpace(f.Properties.Region), CountryCode: strings.TrimSpace(f.Properties.Country), SourceURL: "https://www.openstreetmap.org/" + kind + "/" + obj, Attribution: "© OpenStreetMap contributors · ODbL 1.0", CoordinateSystem: "wgs84", Latitude: f.Geometry.Coordinates[1], Longitude: f.Geometry.Coordinates[0], ObservedAt: at, ValidUntil: at.Add(24 * time.Hour)}
		if !v.Valid() {
			continue
		}
		v.SourceVersion = v.Version()
		seen[id] = true
		out = append(out, v)
	}
	return out, nil
}
