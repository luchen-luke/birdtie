package agentmemory

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
)

const EvidenceSchemaV1 = "agent-memory-evidence-v1"
const ProvenanceSchemaV1 = "agent-memory-provenance-v1"
const MaxProvenanceEvidence = 100

type EvidenceStatus string
type EvidenceSignalType string

const (
	EvidenceCurrent       EvidenceStatus     = "CURRENT"
	EvidenceRemoved       EvidenceStatus     = "REMOVED"
	SignalManualReference EvidenceSignalType = "MANUAL_REFERENCE"
)

// Evidence records a human's explicit association with an owned native source.
// Its shape is not source verification, an analysis grant or inference evidence
// acceptance. The current Store resolves every source and permission again.
type Evidence struct {
	SchemaVersion string                      `json:"schemaVersion"`
	ID            string                      `json:"id"`
	MemoryID      string                      `json:"memoryId"`
	MemoryVersion int64                       `json:"memoryVersion"`
	AgentID       string                      `json:"agentId"`
	OwnerType     actorref.Type               `json:"ownerType"`
	OwnerID       string                      `json:"ownerId"`
	Version       int64                       `json:"version"`
	Source        *agentevent.SourceReference `json:"source,omitempty"`
	SignalType    EvidenceSignalType          `json:"signalType,omitempty"`
	Weight        float64                     `json:"weight,omitempty"`
	ObservedAt    time.Time                   `json:"observedAt"`
	// EventTime is the native snapshot's updated_at (Moment/RSVP) or
	// created_at (bookmark). It is not an occurrence, attendance or visit time.
	EventTime *time.Time     `json:"eventTime,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	Status    EvidenceStatus `json:"status"`
}

type EvidenceReferenceInput struct {
	ExpectedMemoryVersion int64                 `json:"expectedMemoryVersion"`
	SourceType            agentevent.SourceType `json:"sourceType"`
	SourceID              string                `json:"sourceId"`
}

type EvidenceDetachInput struct {
	ExpectedVersion int64 `json:"expectedVersion"`
}

// Provenance is an owner human-management explanation. It neither includes
// source text nor concludes that a bookmark/RSVP proves an objective preference.
type Provenance struct {
	SchemaVersion string     `json:"schemaVersion"`
	MemoryID      string     `json:"memoryId"`
	MemoryVersion int64      `json:"memoryVersion"`
	Declaration   string     `json:"declaration"`
	Explanation   string     `json:"explanation"`
	Evidence      []Evidence `json:"evidence"`
}

type EvidenceStore interface {
	PutOwnMemoryEvidence(context.Context, Access, string, string, EvidenceReferenceInput) (Evidence, error)
	RemoveOwnMemoryEvidence(context.Context, Access, string, string, int64) (Evidence, error)
	ReadOwnMemoryProvenance(context.Context, Access, string) (Provenance, error)
}

func NormalizeEvidenceID(id string) (string, error) { return NormalizeMemoryID(id) }

func evidenceIDCanonical(id string) bool {
	normalized, err := NormalizeEvidenceID(id)
	return err == nil && normalized == id
}

func evidenceSourceKind(source agentevent.SourceType) (agentevent.VersionKind, bool) {
	switch source {
	case agentevent.MomentSource:
		return agentevent.RevisionVersion, true
	case agentevent.ParticipationSource:
		return agentevent.UpdatedAtDigestVersion, true
	case agentevent.SavedPlaceSource:
		return agentevent.CreatedAtDigestVersion, true
	default:
		return "", false
	}
}

// NormalizeReference checks shape only. A source address or expected version
// cannot authorize a user, Agent, record, source read or model purpose.
func NormalizeReference(input EvidenceReferenceInput) (EvidenceReferenceInput, error) {
	if input.ExpectedMemoryVersion <= 0 {
		return EvidenceReferenceInput{}, ErrInvalid
	}
	if _, ok := evidenceSourceKind(input.SourceType); !ok {
		return EvidenceReferenceInput{}, ErrInvalid
	}
	id, err := NormalizeEvidenceID(input.SourceID)
	if err != nil {
		return EvidenceReferenceInput{}, ErrInvalid
	}
	input.SourceID = id
	return input, nil
}

func NormalizeEvidenceDetach(input EvidenceDetachInput) (EvidenceDetachInput, error) {
	if input.ExpectedVersion <= 0 {
		return EvidenceDetachInput{}, ErrInvalid
	}
	return input, nil
}

func validEvidenceSource(source agentevent.SourceReference) bool {
	expected, ok := evidenceSourceKind(source.Type)
	if !ok || !evidenceIDCanonical(source.ID) || source.Owner.Type != actorref.Person || !evidenceIDCanonical(source.Owner.ID) || source.Version.Kind != expected {
		return false
	}
	if expected == agentevent.RevisionVersion {
		return source.Version.Revision > 0 && source.Version.Token == ""
	}
	if source.Version.Revision != 0 || len(source.Version.Token) != 64 || source.Version.Token != strings.ToLower(source.Version.Token) {
		return false
	}
	decoded, err := hex.DecodeString(source.Version.Token)
	return err == nil && len(decoded) == 32
}

// ValidateEvidence validates metadata, not a live source or consent resolver.
// Removed links retain only their bounded control identity/revisions/times.
func ValidateEvidence(record Evidence) error {
	if record.SchemaVersion != EvidenceSchemaV1 || record.Version <= 0 || record.MemoryVersion <= 0 ||
		!evidenceIDCanonical(record.ID) || !evidenceIDCanonical(record.MemoryID) || !evidenceIDCanonical(record.AgentID) ||
		record.OwnerType != actorref.Person || !evidenceIDCanonical(record.OwnerID) || !validTime(record.CreatedAt) || !validTime(record.ObservedAt) ||
		record.CreatedAt.Before(record.ObservedAt) {
		return ErrInvalid
	}
	switch record.Status {
	case EvidenceCurrent:
		if record.Source == nil || !validEvidenceSource(*record.Source) || record.Source.Owner.ID != record.OwnerID ||
			record.SignalType != SignalManualReference || record.Weight != 1 || record.EventTime == nil ||
			!validTime(*record.EventTime) || record.EventTime.After(record.ObservedAt) {
			return ErrInvalid
		}
	case EvidenceRemoved:
		if record.Source != nil || record.SignalType != "" || record.Weight != 0 || record.EventTime != nil {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

// BuildOwnProvenance must receive only metadata revalidated by the current
// Store. Pure shape checks cannot prove that a source version/ACL remains live.
// INFERRED acceptance is unavailable; manual references never grant analysis.
func BuildOwnProvenance(memory Record, evidence []Evidence, now time.Time) (Provenance, error) {
	if ValidateRecord(memory) != nil || !validTime(now) {
		return Provenance{}, ErrInvalid
	}
	if memory.SourceType != SourceExplicit {
		return Provenance{}, ErrUnavailable
	}
	if memory.Status != StatusActive || memory.ValidFrom.After(now) || !memory.ValidUntil.After(now) {
		return Provenance{}, ErrNotFound
	}
	out := Provenance{SchemaVersion: ProvenanceSchemaV1, MemoryID: memory.ID, MemoryVersion: memory.Version,
		Declaration: "本人明确填写", Explanation: "本人明确填写", Evidence: []Evidence{}}
	seenIDs := map[string]bool{}
	seenSources := map[string]int{}
	counts := map[agentevent.SourceType]int{}
	for _, record := range evidence {
		if ValidateEvidence(record) != nil || record.ObservedAt.After(now) || record.CreatedAt.After(now) {
			return Provenance{}, ErrInvalid
		}
		if record.MemoryID != memory.ID || record.AgentID != memory.AgentID || record.OwnerType != memory.OwnerType || record.OwnerID != memory.OwnerID {
			return Provenance{}, ErrForbidden
		}
		if seenIDs[record.ID] {
			return Provenance{}, ErrInvalid
		}
		seenIDs[record.ID] = true
		if record.Status == EvidenceRemoved {
			continue
		}
		if record.MemoryVersion != memory.Version {
			return Provenance{}, ErrConflict
		}
		key := string(record.Source.Type) + ":" + record.Source.ID
		if previous, exists := seenSources[key]; exists {
			prior := out.Evidence[previous]
			if prior.Source.Version != record.Source.Version || !prior.EventTime.Equal(*record.EventTime) {
				return Provenance{}, ErrConflict
			}
			// One source cannot become several independent signals. Choose a
			// stable representative when equivalent links arrive out of order.
			if record.ID < prior.ID {
				out.Evidence[previous] = copyEvidenceMetadata(record)
			}
			continue
		}
		seenSources[key] = len(out.Evidence)
		counts[record.Source.Type]++
		out.Evidence = append(out.Evidence, copyEvidenceMetadata(record))
	}
	sort.Slice(out.Evidence, func(i, j int) bool {
		if out.Evidence[i].Source.Type != out.Evidence[j].Source.Type {
			return out.Evidence[i].Source.Type < out.Evidence[j].Source.Type
		}
		if out.Evidence[i].Source.ID != out.Evidence[j].Source.ID {
			return out.Evidence[i].Source.ID < out.Evidence[j].Source.ID
		}
		return out.Evidence[i].ID < out.Evidence[j].ID
	})
	out.Explanation = renderOwnProvenanceExplanation(counts)
	if ValidateProvenance(out) != nil {
		return Provenance{}, ErrInvalid
	}
	return out, nil
}

// ValidateProvenance checks response metadata and fixed human wording only.
// Same-owner/Agent shape is not proof of a current session, source, ACL,
// active Agent or model purpose. The Store still resolves all authority.
func ValidateProvenance(record Provenance) error {
	if record.SchemaVersion != ProvenanceSchemaV1 || !evidenceIDCanonical(record.MemoryID) || record.MemoryVersion <= 0 ||
		record.Declaration != "本人明确填写" || record.Evidence == nil || len(record.Evidence) > MaxProvenanceEvidence {
		return ErrInvalid
	}
	var ownerID, agentID string
	seenIDs, seenSources := map[string]bool{}, map[string]bool{}
	counts := map[agentevent.SourceType]int{}
	for _, e := range record.Evidence {
		if ValidateEvidence(e) != nil || e.Status != EvidenceCurrent || e.MemoryID != record.MemoryID || e.MemoryVersion != record.MemoryVersion ||
			(ownerID != "" && e.OwnerID != ownerID) || (agentID != "" && e.AgentID != agentID) || seenIDs[e.ID] {
			return ErrInvalid
		}
		key := string(e.Source.Type) + ":" + e.Source.ID
		if seenSources[key] {
			return ErrInvalid
		}
		ownerID, agentID = e.OwnerID, e.AgentID
		seenIDs[e.ID], seenSources[key] = true, true
		counts[e.Source.Type]++
	}
	if record.Explanation != renderOwnProvenanceExplanation(counts) {
		return ErrInvalid
	}
	return nil
}

func renderOwnProvenanceExplanation(counts map[agentevent.SourceType]int) string {
	parts := []string{}
	if count := counts[agentevent.MomentSource]; count > 0 {
		parts = append(parts, fmt.Sprintf("%d条私人记录", count))
	}
	if count := counts[agentevent.ParticipationSource]; count > 0 {
		parts = append(parts, fmt.Sprintf("%d次报名记录", count))
	}
	if count := counts[agentevent.SavedPlaceSource]; count > 0 {
		parts = append(parts, fmt.Sprintf("%d条收藏记录", count))
	}
	if len(parts) > 0 {
		return "本人关联了" + strings.Join(parts, "、")
	}
	return "本人明确填写"
}

func copyEvidenceMetadata(record Evidence) Evidence {
	source := *record.Source
	eventTime := *record.EventTime
	record.Source, record.EventTime = &source, &eventTime
	return record
}
