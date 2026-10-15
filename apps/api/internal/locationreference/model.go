// Package locationreference keeps provider identity separate from operating Cities.
package locationreference

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid location query")
var ErrUnavailable = errors.New("location source unavailable")
var osmID = regexp.MustCompile(`^(node|way|relation)/([1-9][0-9]{0,19})$`)

type Candidate struct {
	ReferenceID      string    `json:"referenceId"`
	Name             string    `json:"name"`
	Address          string    `json:"address"`
	Region           string    `json:"region"`
	CountryCode      string    `json:"countryCode"`
	SourceURL        string    `json:"sourceUrl"`
	Attribution      string    `json:"attribution"`
	CoordinateSystem string    `json:"coordinateSystem"`
	Latitude         float64   `json:"latitude"`
	Longitude        float64   `json:"longitude"`
	SourceVersion    string    `json:"sourceVersion"`
	ObservedAt       time.Time `json:"observedAt"`
	ValidUntil       time.Time `json:"validUntil"`
	ObjectKind       string    `json:"-"`
	ObjectID         string    `json:"-"`
}
type View struct {
	SchemaVersion string      `json:"schemaVersion"`
	Status        string      `json:"status"`
	Items         []Candidate `json:"items"`
	ObservedAt    time.Time   `json:"observedAt"`
	ValidUntil    time.Time   `json:"validUntil"`
}
type Store interface {
	SearchLocationReferences(context.Context, string, int) (View, error)
}

func ValidateQuery(q string, n int) error {
	if !utf8.ValidString(q) || q != strings.TrimSpace(q) || len(q) < 2 || len(q) > 160 || strings.ContainsAny(q, "\x00\r\n") || n < 1 || n > 10 {
		return ErrInvalid
	}
	return nil
}
func OSMIdentity(source string) (id, kind, object string, ok bool) {
	m := osmID.FindStringSubmatch(source)
	if m == nil {
		return
	}
	kind, object = m[1], m[2]
	h := sha256.Sum256([]byte("birdtie:location:openstreetmap:" + source))
	h[6] = (h[6] & 15) | 80
	h[8] = (h[8] & 63) | 128
	id = fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
	return id, kind, object, true
}
func (v Candidate) Valid() bool {
	id, kind, obj, ok := OSMIdentity(v.ObjectKind + "/" + v.ObjectID)
	if !ok || id != v.ReferenceID || kind != v.ObjectKind || obj != v.ObjectID || v.CoordinateSystem != "wgs84" || v.SourceURL != "https://www.openstreetmap.org/"+kind+"/"+obj || v.Attribution != "© OpenStreetMap contributors · ODbL 1.0" {
		return false
	}
	for _, s := range []string{v.Name, v.Address, v.Region, v.CountryCode} {
		if !utf8.ValidString(s) || len(s) > 640 || strings.ContainsRune(s, 0) || s != strings.TrimSpace(s) {
			return false
		}
	}
	return v.Name != "" && utf8.RuneCountInString(v.Name) <= 300 && !math.IsNaN(v.Latitude) && !math.IsNaN(v.Longitude) && !math.IsInf(v.Latitude, 0) && !math.IsInf(v.Longitude, 0) && math.Abs(v.Latitude) <= 90 && math.Abs(v.Longitude) <= 180 && v.ValidUntil.After(v.ObservedAt) && v.ValidUntil.Sub(v.ObservedAt) <= 24*time.Hour
}
func (v Candidate) Version() string {
	b, _ := json.Marshal([]any{v.ReferenceID, v.Name, v.Address, v.Region, v.CountryCode, v.SourceURL, v.Attribution, v.CoordinateSystem, v.Latitude, v.Longitude})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
