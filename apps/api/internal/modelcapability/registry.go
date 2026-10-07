// Package modelcapability owns versioned capability eligibility, not source
// authority, billing or live provider activation. All runners are offline.
package modelcapability

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = "air.model_capability.v1"
const MaxValidationAge = 30 * 24 * time.Hour
const MaxRecords = 32

type Support string

const (
	Unknown     Support = "UNKNOWN"
	Supported   Support = "SUPPORTED"
	Unsupported Support = "UNSUPPORTED"
)

type EvidenceMode string

const (
	DocumentationChecked EvidenceMode = "DOCUMENTATION_CHECKED"
	OfflineContract      EvidenceMode = "OFFLINE_CONTRACT"
)

type Region string

const (
	US   Region = "US"
	EU   Region = "EU"
	UK   Region = "UK"
	APAC Region = "APAC"
)

var (
	ErrInvalid     = errors.New("模型能力配置无效")
	ErrUnsupported = errors.New("UNSUPPORTED_CAPABILITY：没有符合要求的模型能力")
	ErrDenied      = errors.New("当前数据不允许使用该模型路由")
	ErrExpired     = errors.New("模型能力记录或授权已过期")
	ErrUnavailable = errors.New("模型路由授权服务当前不可用")
	ErrServerOnly  = errors.New("模型能力控制对象仅供服务端使用")
)
var identifier = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,79}$`)

type Key struct {
	Provider     string
	Model        string
	Version      string
	WireContract string
}

// These six provider capabilities are separate from baseline text and local
// schema enforcement. A compatibility label cannot establish any of them.
type Capabilities struct {
	Vision    Support
	Tools     Support
	Schema    Support
	Streaming Support
	State     Support
	Storage   Support
}
type Record struct {
	Key                Key
	Text               Support
	Capabilities       Capabilities
	ValidatedAt        time.Time
	ExpiresAt          time.Time
	Regions            []Region
	Evidence           EvidenceMode
	References         []string
	QualityRank        int    // Offline ordinal, not a measured live quality probability.
	CostEstimateMicros *int64 // Synthetic comparison only; nil remains UNKNOWN.
}

func (Record) MarshalJSON() ([]byte, error)  { return nil, ErrServerOnly }
func (r *Record) UnmarshalJSON([]byte) error { *r = Record{}; return ErrServerOnly }
func validTime(t time.Time) bool             { return !t.IsZero() && t.UTC().Year() >= 1 && t.UTC().Year() <= 9999 }
func validSupport(s Support) bool            { return s == Unknown || s == Supported || s == Unsupported }
func validRegion(r Region) bool              { return r == US || r == EU || r == UK || r == APAC }
func validKey(k Key) bool {
	for _, id := range []string{k.Provider, k.Model, k.Version, k.WireContract} {
		if !identifier.MatchString(id) {
			return false
		}
	}
	for _, id := range []string{k.Provider, k.Model, k.Version, k.WireContract} {
		if id == "latest" || id == "default" || id == "auto" || strings.HasSuffix(id, "-latest") || strings.Contains(id, "compatible") {
			return false
		}
	}
	return true
}
func keyString(k Key) string {
	return k.Provider + "/" + k.Model + "@" + k.Version + "#" + k.WireContract
}
func cloneRecord(r Record) Record {
	r.Regions = append([]Region(nil), r.Regions...)
	r.References = append([]string(nil), r.References...)
	if r.CostEstimateMicros != nil {
		v := *r.CostEstimateMicros
		r.CostEstimateMicros = &v
	}
	return r
}
func validateRecord(r Record) error {
	if !validKey(r.Key) || !validSupport(r.Text) || !validTime(r.ValidatedAt) || !validTime(r.ExpiresAt) ||
		!r.ExpiresAt.After(r.ValidatedAt) || r.ExpiresAt.After(r.ValidatedAt.Add(MaxValidationAge)) ||
		(r.Evidence != DocumentationChecked && r.Evidence != OfflineContract) || len(r.Regions) == 0 || len(r.Regions) > 4 ||
		r.QualityRank < 0 || r.QualityRank > 1000 || (r.CostEstimateMicros != nil && (*r.CostEstimateMicros < 0 || *r.CostEstimateMicros > 1_000_000_000_000)) {
		return ErrInvalid
	}
	for _, s := range []Support{r.Capabilities.Vision, r.Capabilities.Tools, r.Capabilities.Schema, r.Capabilities.Streaming, r.Capabilities.State, r.Capabilities.Storage} {
		if !validSupport(s) {
			return ErrInvalid
		}
	}
	seen := map[Region]bool{}
	for _, region := range r.Regions {
		if !validRegion(region) || seen[region] {
			return ErrInvalid
		}
		seen[region] = true
	}
	if len(r.References) > 8 || (r.Evidence == DocumentationChecked && len(r.References) == 0) {
		return ErrInvalid
	}
	for _, ref := range r.References {
		u, err := url.Parse(ref)
		if err != nil || len(ref) > 512 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
			return ErrInvalid
		}
	}
	return nil
}

// Registry is immutable, bounded and initially empty unless explicit server
// records are supplied. No JSON path accepts claimed verified/live records.
type Registry struct {
	records []Record
	digest  string
}

func (Registry) MarshalJSON() ([]byte, error)  { return nil, ErrServerOnly }
func (r *Registry) UnmarshalJSON([]byte) error { *r = Registry{}; return ErrServerOnly }
func NewRegistry(records []Record) (*Registry, error) {
	if len(records) > MaxRecords {
		return nil, ErrInvalid
	}
	r := &Registry{}
	seen := map[Key]bool{}
	for _, record := range records {
		if validateRecord(record) != nil || seen[record.Key] {
			return nil, ErrInvalid
		}
		seen[record.Key] = true
		r.records = append(r.records, cloneRecord(record))
	}
	sort.Slice(r.records, func(i, j int) bool { return keyString(r.records[i].Key) < keyString(r.records[j].Key) })
	type wireRecord Record
	wire := make([]wireRecord, len(r.records))
	for i, record := range r.records {
		wire[i] = wireRecord(record)
	}
	data, err := json.Marshal(wire)
	if err != nil {
		return nil, ErrInvalid
	}
	h := sha256.Sum256(append([]byte("birdtie.capability.registry.v1\x00"), data...))
	r.digest = hex.EncodeToString(h[:])
	return r, nil
}
func (r *Registry) Digest() string {
	if r == nil {
		return ""
	}
	return r.digest
}
func (r *Registry) Records() []Record {
	if r == nil {
		return nil
	}
	out := make([]Record, len(r.records))
	for i, record := range r.records {
		out[i] = cloneRecord(record)
	}
	return out
}
