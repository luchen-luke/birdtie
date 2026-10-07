// Package agentparticipationsignal emits current own RSVP metadata only.
// It grants no analysis, model export, candidate acceptance or Memory write.
package agentparticipationsignal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
)

const SchemaVersion = "agent.participation_signal.v1"

var (
	ErrInvalid       = errors.New("invalid participation signal")
	ErrDenied        = errors.New("participation signal source denied")
	ErrExpired       = errors.New("participation signal source expired")
	ErrUnavailable   = errors.New("participation signal processing unavailable")
	ErrAuthorityJSON = errors.New("participation signal is server-produced metadata")
)

type Kind string

const (
	RSVPGoing     Kind = "RSVP_GOING"
	RSVPPending   Kind = "RSVP_PENDING"
	RSVPCancelled Kind = "RSVP_CANCELLED"
)

type Request struct {
	ParticipationID    string
	LogicalOperationID string
}
type Signal struct {
	SchemaVersion          string                     `json:"schema_version"`
	SignalID               string                     `json:"signal_id"`
	Kind                   Kind                       `json:"kind"`
	Subject                actorref.PrincipalRef      `json:"subject"`
	AgentID                string                     `json:"agent_id"`
	LogicalOperationID     string                     `json:"logical_operation_id"`
	Source                 agentevent.SourceReference `json:"source"`
	SourceUpdatedAt        time.Time                  `json:"source_updated_at"`
	ObservedAt             time.Time                  `json:"observed_at"`
	ExpiresAt              time.Time                  `json:"expires_at"`
	ActivityID             string                     `json:"activity_id,omitempty"`
	ActivityRevision       int64                      `json:"activity_revision,omitempty"`
	ActivitySnapshotDigest string                     `json:"activity_snapshot_digest,omitempty"`
	MetadataCreatedAt      time.Time                  `json:"metadata_created_at"`
	ReceiptSemantics       string                     `json:"receipt_semantics"`
	Attendance             string                     `json:"attendance"`
	StableInterest         string                     `json:"stable_interest"`
	ProcessingStatus       string                     `json:"processing_status"`
}

// JSON cannot manufacture a server-produced source snapshot or grant.
func (s *Signal) UnmarshalJSON([]byte) error {
	if s != nil {
		*s = Signal{}
	}
	return ErrAuthorityJSON
}
func validID(id string) bool {
	r, e := actorref.Parse("PERSON", id)
	return e == nil && r.ID == id && id != "00000000-0000-0000-0000-000000000000"
}
func stateKind(status string) (Kind, error) {
	switch status {
	case "going":
		return RSVPGoing, nil
	case "pending":
		return RSVPPending, nil
	case "cancelled":
		return RSVPCancelled, nil
	default:
		return "", ErrUnavailable
	}
}
func stableID(s Signal) string {
	// Observation time is intentionally excluded: retry cannot invent a new
	// operation or lifetime. This is a signal address, not an AIR event catalog.
	raw, _ := json.Marshal(struct {
		Domain            string
		Owner             actorref.PrincipalRef
		Agent, Operation  string
		Source            agentevent.SourceReference
		Kind              Kind
		Activity          string
		ActivityRevision  int64
		ActivityDigest    string
		MetadataCreatedAt time.Time
	}{SchemaVersion, s.Subject, s.AgentID, s.LogicalOperationID, s.Source, s.Kind, s.ActivityID, s.ActivityRevision, s.ActivitySnapshotDigest, s.MetadataCreatedAt.UTC()})
	h := sha256.Sum256(raw)
	id := h[:16]
	id[6] = (id[6] & 0x0f) | 0x80
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
}
func Validate(s Signal, now time.Time) error {
	for _, at := range []time.Time{s.MetadataCreatedAt, s.SourceUpdatedAt, s.ObservedAt, s.ExpiresAt, now} {
		if at.IsZero() || at.Year() < 1 || at.Year() > 9999 {
			return ErrInvalid
		}
	}
	if s.SchemaVersion != SchemaVersion || s.Subject.Type != actorref.Person || !validID(s.Subject.ID) || !validID(s.AgentID) || !validID(s.LogicalOperationID) || !validID(s.Source.ID) || s.Source.Type != agentevent.ParticipationSource || s.Source.Owner != s.Subject || s.Source.Version.Kind != agentevent.UpdatedAtDigestVersion || s.Source.Version.Revision != 0 || len(s.Source.Version.Token) != 64 {
		return ErrInvalid
	}
	raw, e := hex.DecodeString(s.Source.Version.Token)
	if e != nil || hex.EncodeToString(raw) != s.Source.Version.Token {
		return ErrInvalid
	}
	if s.ReceiptSemantics != string(agentevent.CurrentRetainedState) || s.Attendance != "UNKNOWN" || s.StableInterest != "UNKNOWN" || s.ProcessingStatus != "UNAVAILABLE" {
		return ErrInvalid
	}
	switch s.Kind {
	case RSVPGoing, RSVPPending:
		if !validID(s.ActivityID) || s.ActivityRevision < 1 || len(s.ActivitySnapshotDigest) != 64 {
			return ErrInvalid
		}
		digest, err := hex.DecodeString(s.ActivitySnapshotDigest)
		if err != nil || hex.EncodeToString(digest) != s.ActivitySnapshotDigest {
			return ErrInvalid
		}
	case RSVPCancelled:
		if s.ActivityID != "" || s.ActivityRevision != 0 || s.ActivitySnapshotDigest != "" {
			return ErrInvalid
		}
	default:
		return ErrUnavailable
	}
	if s.MetadataCreatedAt.After(s.ObservedAt) || s.SourceUpdatedAt.After(s.ObservedAt) || s.ObservedAt.After(now) || !s.ExpiresAt.Equal(s.SourceUpdatedAt.Add(agentevent.MaxEventTTL)) || s.SignalID != stableID(s) {
		return ErrInvalid
	}
	if !now.Before(s.ExpiresAt) {
		return ErrExpired
	}
	return nil
}
