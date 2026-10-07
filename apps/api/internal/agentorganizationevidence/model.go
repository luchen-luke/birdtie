// Package agentorganizationevidence manages human references to native sources.
// Metadata is neither verified fact nor source-purpose/model permission.
package agentorganizationevidence

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationmemory"
)

const (
	Profile       agentevent.SourceType = "ORGANIZATION_PROFILE"
	Activity      agentevent.SourceType = "ORGANIZATION_ACTIVITY"
	AdminInput    agentevent.SourceType = "ORGANIZATION_ADMIN_INPUT"
	PublicContent agentevent.SourceType = "ORGANIZATION_PUBLIC_FAQ"
	Announcement  agentevent.SourceType = "ORGANIZATION_ANNOUNCEMENT"
	Declaration                         = "组织管理员人工关联，未经独立事实核验；不代表发布、到场、认知授权或模型分析许可。"
)

type Store interface {
	PutOrganizationMemoryEvidence(context.Context, agentorganizationmemory.Access, string, string, agentmemory.EvidenceReferenceInput) (agentmemory.Evidence, error)
	RemoveOrganizationMemoryEvidence(context.Context, agentorganizationmemory.Access, string, string, int64) (agentmemory.Evidence, error)
	ReadOrganizationMemoryProvenance(context.Context, agentorganizationmemory.Access, string) (Provenance, error)
}
type Provenance struct {
	SchemaVersion         string                 `json:"schemaVersion"`
	OrganizationID        string                 `json:"organizationId"`
	OrganizationAccountID string                 `json:"organizationAccountId"`
	AgentID               string                 `json:"agentId"`
	MemoryID              string                 `json:"memoryId"`
	MemoryVersion         int64                  `json:"memoryVersion"`
	Declaration           string                 `json:"declaration"`
	Explanation           string                 `json:"explanation"`
	Evidence              []agentmemory.Evidence `json:"evidence"`
}

func canonical(id string) bool { n, e := agentmemory.NormalizeMemoryID(id); return e == nil && n == id }
func sourceKind(k agentevent.SourceType) (agentevent.VersionKind, error) {
	switch k {
	case Activity, AdminInput, Announcement:
		return agentevent.RevisionVersion, nil
	case Profile, PublicContent:
		return agentevent.UpdatedAtDigestVersion, nil
	default:
		return "", agentmemory.ErrInvalid
	}
}
func NormalizeReference(in agentmemory.EvidenceReferenceInput) (agentmemory.EvidenceReferenceInput, error) {
	if in.ExpectedMemoryVersion < 1 || !canonical(in.SourceID) {
		return agentmemory.EvidenceReferenceInput{}, agentmemory.ErrInvalid
	}
	_, e := sourceKind(in.SourceType)
	if e != nil {
		return agentmemory.EvidenceReferenceInput{}, e
	}
	return in, e
}
func validTime(t time.Time) bool { return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 }
func ValidateEvidence(e agentmemory.Evidence) error {
	if e.SchemaVersion != agentmemory.EvidenceSchemaV1 || e.Version < 1 || e.MemoryVersion < 1 || !canonical(e.ID) || !canonical(e.MemoryID) || !canonical(e.AgentID) || !canonical(e.OwnerID) || e.OwnerType != actorref.Organization || !validTime(e.ObservedAt) || !validTime(e.CreatedAt) || e.CreatedAt.Before(e.ObservedAt) {
		return agentmemory.ErrInvalid
	}
	if e.Status == agentmemory.EvidenceRemoved {
		if e.Source != nil || e.SignalType != "" || e.Weight != 0 || e.EventTime != nil {
			return agentmemory.ErrInvalid
		}
		return nil
	}
	if e.Status != agentmemory.EvidenceCurrent || e.Source == nil || !canonical(e.Source.ID) || e.Source.Owner.Type != actorref.Organization || e.Source.Owner.ID != e.OwnerID || e.SignalType != agentmemory.SignalManualReference || e.Weight != 1 || e.EventTime == nil || !validTime(*e.EventTime) || e.EventTime.After(e.ObservedAt) {
		return agentmemory.ErrInvalid
	}
	kind, err := sourceKind(e.Source.Type)
	if err != nil || e.Source.Version.Kind != kind {
		return agentmemory.ErrInvalid
	}
	v := e.Source.Version
	if kind == agentevent.RevisionVersion {
		if v.Revision < 1 || v.Token != "" {
			return agentmemory.ErrInvalid
		}
	} else {
		b, err := hex.DecodeString(v.Token)
		if err != nil || len(b) != 32 || v.Token != strings.ToLower(v.Token) || v.Revision != 0 {
			return agentmemory.ErrInvalid
		}
	}
	if e.Source.Type == AdminInput && e.Source.ID == e.MemoryID {
		return agentmemory.ErrInvalid
	}
	return nil
}
func explain(counts map[agentevent.SourceType]int) string {
	parts := []string{}
	for _, pair := range []struct {
		k     agentevent.SourceType
		label string
	}{{Profile, "条组织资料"}, {Activity, "条组织活动"}, {AdminInput, "条管理员声明"}, {PublicContent, "条公开问答"}, {Announcement, "条组织公告"}} {
		if n := counts[pair.k]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d%s", n, pair.label))
		}
	}
	if len(parts) == 0 {
		return "组织管理员明确填写，尚未关联当前有效来源。"
	}
	return "组织管理员关联了" + strings.Join(parts, "、") + "；人工关联不代表已核验事实。"
}
func Build(org string, m agentmemory.Record, all []agentmemory.Evidence, now time.Time) (Provenance, error) {
	if !canonical(org) || agentmemory.ValidateOrganizationRecord(m) != nil || !validTime(now) {
		return Provenance{}, agentmemory.ErrInvalid
	}
	if m.SourceType != agentmemory.SourceExplicit {
		return Provenance{}, agentmemory.ErrUnavailable
	}
	if m.Status != agentmemory.StatusActive || m.ValidFrom.After(now) || !m.ValidUntil.After(now) {
		return Provenance{}, agentmemory.ErrNotFound
	}
	p := Provenance{SchemaVersion: agentmemory.ProvenanceSchemaV1, OrganizationID: org, OrganizationAccountID: m.OwnerID, AgentID: m.AgentID, MemoryID: m.ID, MemoryVersion: m.Version, Declaration: Declaration, Evidence: []agentmemory.Evidence{}}
	seen := map[string]bool{}
	sources := map[string]bool{}
	counts := map[agentevent.SourceType]int{}
	for _, e := range all {
		if ValidateEvidence(e) != nil || e.ObservedAt.After(now) || e.CreatedAt.After(now) || seen[e.ID] {
			return Provenance{}, agentmemory.ErrInvalid
		}
		seen[e.ID] = true
		if e.MemoryID != m.ID || e.AgentID != m.AgentID || e.OwnerID != m.OwnerID {
			return Provenance{}, agentmemory.ErrForbidden
		}
		if e.Status == agentmemory.EvidenceRemoved {
			continue
		}
		if e.MemoryVersion != m.Version {
			return Provenance{}, agentmemory.ErrConflict
		}
		key := string(e.Source.Type) + ":" + e.Source.ID
		if sources[key] {
			return Provenance{}, agentmemory.ErrConflict
		}
		sources[key] = true
		counts[e.Source.Type]++
		source := *e.Source
		stamp := *e.EventTime
		e.Source = &source
		e.EventTime = &stamp
		p.Evidence = append(p.Evidence, e)
	}
	sort.Slice(p.Evidence, func(i, j int) bool { return p.Evidence[i].ID < p.Evidence[j].ID })
	p.Explanation = explain(counts)
	if ValidateProvenance(p, org, m.ID) != nil {
		return Provenance{}, agentmemory.ErrInvalid
	}
	return p, nil
}
func ValidateProvenance(p Provenance, org, id string) error {
	if !canonical(org) || p.OrganizationID != org || p.MemoryID != id || !canonical(id) || !canonical(p.AgentID) || !canonical(p.OrganizationAccountID) || p.MemoryVersion < 1 || p.SchemaVersion != agentmemory.ProvenanceSchemaV1 || p.Declaration != Declaration || p.Evidence == nil || len(p.Evidence) > agentmemory.MaxProvenanceEvidence {
		return agentmemory.ErrInvalid
	}
	ids, sources := map[string]bool{}, map[string]bool{}
	counts := map[agentevent.SourceType]int{}
	for _, e := range p.Evidence {
		if ValidateEvidence(e) != nil || e.Status != agentmemory.EvidenceCurrent || e.MemoryID != id || e.MemoryVersion != p.MemoryVersion || e.AgentID != p.AgentID || e.OwnerID != p.OrganizationAccountID || ids[e.ID] {
			return agentmemory.ErrInvalid
		}
		key := string(e.Source.Type) + ":" + e.Source.ID
		if sources[key] {
			return agentmemory.ErrInvalid
		}
		ids[e.ID] = true
		sources[key] = true
		counts[e.Source.Type]++
	}
	if p.Explanation != explain(counts) {
		return agentmemory.ErrInvalid
	}
	return nil
}
func DecodeReference(raw []byte) (agentmemory.EvidenceReferenceInput, error) {
	normalized, e := agentmemory.NormalizeStructuredValue(raw)
	if e != nil {
		return agentmemory.EvidenceReferenceInput{}, e
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(normalized, &keys) != nil || len(keys) != 3 {
		return agentmemory.EvidenceReferenceInput{}, agentmemory.ErrInvalid
	}
	for _, k := range []string{"expectedMemoryVersion", "sourceType", "sourceId"} {
		if v, ok := keys[k]; !ok || string(v) == "null" {
			return agentmemory.EvidenceReferenceInput{}, agentmemory.ErrInvalid
		}
	}
	// The original type's JSON method intentionally accepts PERSON sources only.
	// A local wire alias preserves that boundary instead of widening it.
	type organizationReferenceWire agentmemory.EvidenceReferenceInput
	var in organizationReferenceWire
	if json.Unmarshal(normalized, &in) != nil {
		return agentmemory.EvidenceReferenceInput{}, agentmemory.ErrInvalid
	}
	return NormalizeReference(agentmemory.EvidenceReferenceInput(in))
}
