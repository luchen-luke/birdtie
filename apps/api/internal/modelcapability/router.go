package modelcapability

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

// Requirements adds metadata-only future capability needs. It never carries
// image bytes/URLs. Current 007 Complete rejects every extended modality.
type Requirements struct {
	Vision    bool
	Streaming bool
	State     bool
	Storage   bool
}
type Destination struct {
	Key          Key
	Region       Region
	AllowState   bool
	AllowStorage bool
}
type OfflineGrant struct {
	Agent                  agentcognitive.AgentReference
	RequestDigest          string
	RegistryDigest         string
	SourceVersion          agentevent.SourceVersion
	CurrentSourceVersion   agentevent.SourceVersion
	Purpose                string
	ConsentRevision        uint64
	CurrentConsentRevision uint64
	PolicyRevision         uint64
	CurrentPolicyRevision  uint64
	Revoked                bool
	Deleted                bool
	State                  string
	CheckedAt              time.Time
	ExpiresAt              time.Time
	Destinations           []Destination
}

func (OfflineGrant) MarshalJSON() ([]byte, error)  { return nil, ErrServerOnly }
func (g *OfflineGrant) UnmarshalJSON([]byte) error { *g = OfflineGrant{}; return ErrServerOnly }

type Plan struct {
	Key                 Key
	Region              Region
	LocalSchemaRequired bool
	CostStatus          string
	Mode                modelgateway.ExecutionMode
}

// RequestDigest covers exact normalized content/selectors/requirements. It
// neither proves source permission nor grants approval or a billable budget.
func RequestDigest(r modelgateway.Request, needs Requirements) string {
	r.DeadlineAt = r.DeadlineAt.UTC()
	data, _ := json.Marshal(struct {
		Request modelgateway.Request
		Needs   Requirements
	}{r, needs})
	h := sha256.Sum256(append([]byte("birdtie.capability.offline-request.v1\x00"), data...))
	return hex.EncodeToString(h[:])
}
func validVersion(v agentevent.SourceVersion) bool {
	if v.Kind != agentevent.UpdatedAtDigestVersion || v.Revision != 0 || len(v.Token) != 64 {
		return false
	}
	raw, err := hex.DecodeString(v.Token)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == v.Token
}
func validateGrant(registry *Registry, r modelgateway.Request, needs Requirements, g OfflineGrant, now time.Time) error {
	if registry == nil || registry.digest == "" || !validTime(now) || modelgateway.ValidateRequest(r, now) != nil {
		return ErrInvalid
	}
	if g.State == "UNAVAILABLE" {
		return ErrUnavailable
	}
	if g.State != "ALLOWED" || g.Agent != r.Agent || g.RequestDigest != RequestDigest(r, needs) || g.RegistryDigest != registry.digest ||
		g.Purpose != "MODEL_CONTEXT_EGRESS" || g.Revoked || g.Deleted || g.ConsentRevision == 0 || g.ConsentRevision != g.CurrentConsentRevision ||
		g.PolicyRevision == 0 || g.PolicyRevision != g.CurrentPolicyRevision || !validVersion(g.SourceVersion) || g.SourceVersion != g.CurrentSourceVersion ||
		len(g.Destinations) == 0 || len(g.Destinations) > MaxRecords {
		return ErrDenied
	}
	if !validTime(g.CheckedAt) || !validTime(g.ExpiresAt) || !g.CheckedAt.Equal(now) || !g.ExpiresAt.After(now) || g.ExpiresAt.After(r.DeadlineAt) {
		return ErrExpired
	}
	seen := map[Key]bool{}
	for _, destination := range g.Destinations {
		if !validKey(destination.Key) || !validRegion(destination.Region) || seen[destination.Key] {
			return ErrDenied
		}
		seen[destination.Key] = true
	}
	return nil
}
func eligible(record Record, r modelgateway.Request, needs Requirements, d Destination, now time.Time) bool {
	if record.Key != d.Key || record.Evidence != OfflineContract || record.ValidatedAt.After(now) || !record.ExpiresAt.After(now) ||
		record.Text != Supported {
		return false
	}
	region := false
	for _, known := range record.Regions {
		if d.Region == known {
			region = true
		}
	}
	if !region || (needs.State && !d.AllowState) || (needs.Storage && !d.AllowStorage) {
		return false
	}
	c := record.Capabilities
	if (needs.Vision && c.Vision != Supported) || (needs.Streaming && c.Streaming != Supported) ||
		(needs.State && c.State != Supported) || (needs.Storage && c.Storage != Supported) ||
		(r.OutputMode == modelgateway.ToolProposals && c.Tools != Supported) {
		return false
	}
	// Known unsupported native strict schema is allowed only because 007's
	// local closed parser is mandatory. UNKNOWN is not supported by assumption.
	return r.OutputMode == modelgateway.Text || c.Schema == Supported || c.Schema == Unsupported
}

// SelectOffline filters current synthetic data permission and exact capability
// before comparing ordinal quality, known synthetic cost, and stable key.
// This metadata plan cannot be supplied to the production Service as a grant.
func SelectOffline(registry *Registry, r modelgateway.Request, needs Requirements, g OfflineGrant, now time.Time) (Plan, error) {
	if err := validateGrant(registry, r, needs, g, now); err != nil {
		return Plan{}, err
	}
	type candidate struct {
		record      Record
		destination Destination
	}
	var candidates []candidate
	for _, record := range registry.records {
		for _, destination := range g.Destinations {
			if eligible(record, r, needs, destination, now) {
				candidates = append(candidates, candidate{record, destination})
			}
		}
	}
	if len(candidates) == 0 {
		return Plan{}, ErrUnsupported
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i].record, candidates[j].record
		if a.QualityRank != b.QualityRank {
			return a.QualityRank > b.QualityRank
		}
		if (a.CostEstimateMicros != nil) != (b.CostEstimateMicros != nil) {
			return a.CostEstimateMicros != nil
		}
		if a.CostEstimateMicros != nil && *a.CostEstimateMicros != *b.CostEstimateMicros {
			return *a.CostEstimateMicros < *b.CostEstimateMicros
		}
		return keyString(a.Key) < keyString(b.Key)
	})
	chosen := candidates[0]
	costStatus := "UNKNOWN"
	if chosen.record.CostEstimateMicros != nil {
		costStatus = "SYNTHETIC_ESTIMATE"
	}
	return Plan{chosen.record.Key, chosen.destination.Region, r.OutputMode != modelgateway.Text, costStatus, modelgateway.OfflineContract}, nil
}
