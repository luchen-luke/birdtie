package agentevent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

var (
	ErrInvalid       = errors.New("invalid AIR event")
	ErrDenied        = errors.New("AIR event source denied")
	ErrExpired       = errors.New("AIR event expired")
	ErrUnavailable   = errors.New("AIR event ingress unavailable")
	ErrAuthorityJSON = errors.New("AIR event authority is server-only")
)

type SourceVersion struct {
	Kind     VersionKind `json:"kind"`
	Revision int64       `json:"revision,omitempty"`
	Token    string      `json:"token,omitempty"`
}

type SourceReference struct {
	Type    SourceType            `json:"type"`
	ID      string                `json:"id"`
	Owner   actorref.PrincipalRef `json:"owner"`
	Version SourceVersion         `json:"version"`
}

type PayloadReference struct {
	Type SourceType `json:"type"`
	ID   string     `json:"id"`
}

// Envelope is intentionally metadata-only. It contains no source body, query,
// conversation, location, media URL, policy override, or invented consent epoch.
// Subject/Tenant are typed account principals; an Organization entity ID cannot
// replace its account principal. v1 registers only Personal sources.
type Envelope struct {
	SchemaVersion      string                `json:"schema_version"`
	EventID            string                `json:"event_id"`
	EventType          Type                  `json:"event_type"`
	Tenant             actorref.PrincipalRef `json:"tenant"`
	Subject            actorref.PrincipalRef `json:"subject"`
	Actor              actorref.ActorRef     `json:"actor"`
	AgentID            string                `json:"agent_id"`
	LogicalOperationID string                `json:"logical_operation_id"`
	OccurredAt         time.Time             `json:"occurred_at"`
	ReceivedAt         time.Time             `json:"received_at"`
	ExpiresAt          time.Time             `json:"expires_at"`
	Source             SourceReference       `json:"source"`
	Purpose            Purpose               `json:"purpose"`
	RootTraceID        string                `json:"root_trace_id"`
	CausationID        *string               `json:"causation_id"`
	PayloadRef         PayloadReference      `json:"payload_ref"`
	ProcessingStatus   ProcessingStatus      `json:"processing_status"`
}

// UnmarshalJSON also enforces the closed schema for standard JSON callers.
// ReceivedAt is a historical structural-validation point, not proof of current
// time or authority; a consumer must still Revalidate its actual session/source.
func (e *Envelope) UnmarshalJSON(data []byte) error {
	if e == nil {
		return ErrInvalid
	}
	*e = Envelope{}
	var header struct {
		ReceivedAt time.Time `json:"received_at"`
	}
	if len(data) > MaxEnvelopeBytes || json.Unmarshal(data, &header) != nil {
		return ErrInvalid
	}
	decoded, err := Decode(data, header.ReceivedAt)
	if err != nil {
		return err
	}
	*e = decoded
	return nil
}

func canonicalID(id string) (string, error) {
	if id != strings.TrimSpace(id) || id == "00000000-0000-0000-0000-000000000000" {
		return "", ErrInvalid
	}
	ref, err := actorref.ParsePrincipal("PERSON", id)
	if err != nil {
		return "", ErrInvalid
	}
	return ref.ID, nil
}

func validID(id string) bool {
	v, err := canonicalID(id)
	return err == nil && v == id
}

func validPerson(ref actorref.PrincipalRef) bool {
	return ref.Type == actorref.Person && validID(ref.ID)
}

func validVersion(version SourceVersion, expected VersionKind) bool {
	if version.Kind != expected {
		return false
	}
	switch expected {
	case RevisionVersion:
		return version.Revision > 0 && version.Token == ""
	case UpdatedAtDigestVersion, CreatedAtDigestVersion:
		if version.Revision != 0 || len(version.Token) != sha256.Size*2 || version.Token != strings.ToLower(version.Token) {
			return false
		}
		decoded, err := hex.DecodeString(version.Token)
		return err == nil && len(decoded) == sha256.Size
	default:
		return false
	}
}

// QueryVersion records an opaque fingerprint of the real stored source.
// canonicalContent must be a server-loaded canonical representation, never a
// client assertion. This is neither monotonic nor CAS, and grants no access.
func QueryVersion(updatedAt time.Time, canonicalContent []byte) (SourceVersion, error) {
	if updatedAt.IsZero() || len(canonicalContent) == 0 || len(canonicalContent) > 1024*1024 {
		return SourceVersion{}, ErrInvalid
	}
	h := sha256.New()
	_, _ = h.Write([]byte("birdtie.air.query-source.v1\x00"))
	_, _ = h.Write([]byte(updatedAt.UTC().Format(time.RFC3339Nano)))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(canonicalContent)
	return SourceVersion{Kind: UpdatedAtDigestVersion, Token: hex.EncodeToString(h.Sum(nil))}, nil
}

// SnapshotVersion fingerprints a server-loaded current native row with its real
// timestamp. CreatedAt is used only where no updated_at exists. This is not a
// monotonic counter, CAS, proof of a historical transition or an access grant.
func SnapshotVersion(source SourceType, kind VersionKind, sourceTime time.Time, canonicalContent []byte) (SourceVersion, error) {
	valid := (source == ParticipationSource || source == CommunityMembershipSource || source == ProfileSource) && kind == UpdatedAtDigestVersion || source == SavedPlaceSource && kind == CreatedAtDigestVersion
	if !valid || sourceTime.IsZero() || len(canonicalContent) == 0 || len(canonicalContent) > 1024*1024 {
		return SourceVersion{}, ErrInvalid
	}
	h := sha256.New()
	for _, part := range []string{"birdtie.age.retained-source.v1", string(source), string(kind), sourceTime.UTC().Format(time.RFC3339Nano)} {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	_, _ = h.Write(canonicalContent)
	return SourceVersion{Kind: kind, Token: hex.EncodeToString(h.Sum(nil))}, nil
}

// StableEventID uses the typed subject, exact Agent and real source version as
// well as the logical operation. It does not deduplicate deliberate identical
// queries across different operations, and is not an effect/inbox ledger.
func StableEventID(e Envelope) string {
	data, _ := json.Marshal(struct {
		Schema    string
		Type      Type
		Tenant    actorref.PrincipalRef
		Subject   actorref.PrincipalRef
		Agent     string
		Operation string
		Source    SourceReference
	}{e.SchemaVersion, e.EventType, e.Tenant, e.Subject, e.AgentID, e.LogicalOperationID, e.Source})
	digest := sha256.Sum256(data)
	// UUIDv8: application-defined deterministic identity, not a UUIDv5/SHA1 claim.
	digest[6] = digest[6]&0x0f | 0x80
	digest[8] = digest[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", digest[0:4], digest[4:6], digest[6:8], digest[8:10], digest[10:16])
}

// Validate checks the registered schema and its event lifetime. A successfully
// validated serialized event remains untrusted and cannot authorize processing.
func Validate(e Envelope, now time.Time) error {
	descriptor, known := Lookup(e.EventType)
	if known && descriptor.Support != CurrentNativeSource {
		return ErrUnavailable
	}
	if !known || e.SchemaVersion != SchemaVersion || e.Purpose != descriptor.Purpose ||
		e.ProcessingStatus != Unavailable || !validPerson(e.Tenant) || !validPerson(e.Subject) ||
		!e.Tenant.Equal(e.Subject) || e.Actor.Type != actorref.Person || !validID(e.Actor.ID) ||
		e.Actor.ID != e.Subject.ID || !validID(e.AgentID) || !validID(e.EventID) ||
		!validID(e.LogicalOperationID) || !validID(e.RootTraceID) ||
		(e.CausationID != nil && !validID(*e.CausationID)) ||
		e.Source.Type != descriptor.Source || !validID(e.Source.ID) || !e.Source.Owner.Equal(e.Subject) ||
		!validPerson(e.Source.Owner) || !validVersion(e.Source.Version, descriptor.VersionKind) ||
		(descriptor.VersionKind == RevisionVersion && e.Source.Version.Revision < descriptor.MinRevision) ||
		e.PayloadRef.Type != e.Source.Type || e.PayloadRef.ID != e.Source.ID ||
		e.EventID != StableEventID(e) || now.IsZero() || e.OccurredAt.IsZero() || e.ReceivedAt.IsZero() ||
		e.ExpiresAt.IsZero() || e.OccurredAt.After(e.ReceivedAt) || e.ReceivedAt.After(now) ||
		!e.ExpiresAt.Equal(e.OccurredAt.Add(MaxEventTTL)) {
		return ErrInvalid
	}
	if !e.ExpiresAt.After(now) {
		return ErrExpired
	}
	return nil
}

// rejectDuplicateKeys scans nested objects before ordinary strict decoding.
// DisallowUnknownFields alone otherwise permits last-value-wins authority IDs.
func rejectDuplicateKeys(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	keys := map[string]map[string]bool{}
	for path, names := range map[string][]string{
		"":        {"schema_version", "event_id", "event_type", "tenant", "subject", "actor", "agent_id", "logical_operation_id", "occurred_at", "received_at", "expires_at", "source", "purpose", "root_trace_id", "causation_id", "payload_ref", "processing_status"},
		".tenant": {"type", "id"}, ".subject": {"type", "id"}, ".actor": {"type", "id"},
		".source": {"type", "id", "owner", "version"}, ".source.owner": {"type", "id"},
		".source.version": {"kind", "revision", "token"}, ".payload_ref": {"type", "id"},
	} {
		keys[path] = map[string]bool{}
		for _, name := range names {
			keys[path][name] = true
		}
	}
	var read func(int, string) error
	read = func(depth int, path string) error {
		if depth > 12 {
			return ErrInvalid
		}
		token, err := d.Token()
		if err != nil {
			return ErrInvalid
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] || !keys[path][name] {
					return ErrInvalid
				}
				seen[name] = true
				if err := read(depth+1, path+"."+name); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := read(depth+1, path); err != nil {
					return err
				}
			}
		default:
			return ErrInvalid
		}
		end, err := d.Token()
		if err != nil || (delim == '{' && end != json.Delim('}')) || (delim == '[' && end != json.Delim(']')) {
			return ErrInvalid
		}
		return nil
	}
	if err := read(0, ""); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalid
	}
	return nil
}

// Decode rejects unknown/duplicate fields, oversized input and trailing values.
// Callers must still re-resolve the source with a current server-side session.
func Decode(data []byte, now time.Time) (Envelope, error) {
	var e Envelope
	if len(data) == 0 || len(data) > MaxEnvelopeBytes || rejectDuplicateKeys(data) != nil {
		return e, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	type wireEnvelope Envelope
	var wire wireEnvelope
	if err := d.Decode(&wire); err != nil {
		return Envelope{}, ErrInvalid
	}
	e = Envelope(wire)
	// A version is a tagged union, not an optional bag of zero-valued fields.
	// Reject even a null/zero extraneous field in the other variant.
	var shape struct {
		Source struct {
			Version map[string]json.RawMessage `json:"version"`
		} `json:"source"`
	}
	if err := json.Unmarshal(data, &shape); err != nil {
		return Envelope{}, ErrInvalid
	}
	versionFields := shape.Source.Version
	if len(versionFields) != 2 {
		return Envelope{}, ErrInvalid
	}
	if e.Source.Version.Kind == RevisionVersion {
		if _, ok := versionFields["revision"]; !ok {
			return Envelope{}, ErrInvalid
		}
	} else if e.Source.Version.Kind == UpdatedAtDigestVersion || e.Source.Version.Kind == CreatedAtDigestVersion {
		if _, ok := versionFields["token"]; !ok {
			return Envelope{}, ErrInvalid
		}
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return Envelope{}, ErrInvalid
	}
	if err := Validate(e, now); err != nil {
		return Envelope{}, err
	}
	return e, nil
}

func Encode(e Envelope, now time.Time) ([]byte, error) {
	if err := Validate(e, now); err != nil {
		return nil, err
	}
	data, err := json.Marshal(e)
	if err != nil || len(data) > MaxEnvelopeBytes {
		return nil, ErrInvalid
	}
	return data, nil
}
