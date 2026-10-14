package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const CollectionControlSchemaVersion = "human-personal-collection-control-v1"
const MaxCollectionControlRevision = MaxCollectionAppendRevision
const MaxCollectionControlRequestBytes = 128 * 1024

// Separate from the authoritative read entry: no availability, name, point or
// source lease can be submitted or echoed as an input projection.
type CollectionControlEntry struct {
	PlaceID            string `json:"placeId"`
	Note               string `json:"note"`
	LocationVisibility string `json:"locationVisibility"`
}
type CollectionControlBody struct {
	Kind    string                   `json:"kind"`
	CityID  string                   `json:"cityId"`
	Title   string                   `json:"title"`
	Summary string                   `json:"summary"`
	Entries []CollectionControlEntry `json:"entries"`
}
type CollectionControlInput struct {
	SchemaVersion    string                 `json:"schemaVersion"`
	OperationID      string                 `json:"operationId"`
	Action           string                 `json:"action"`
	Kind             string                 `json:"kind"`
	CollectionID     string                 `json:"collectionId"`
	ExpectedRevision int64                  `json:"expectedRevision"`
	Input            *CollectionControlBody `json:"input,omitempty"`
}
type CollectionControlSubmission struct {
	CollectionControlInput
	Approval string `json:"approval"`
}
type CollectionControlPreview struct {
	identity.HumanControlPreview
	SchemaVersion    string                 `json:"schemaVersion"`
	Kind             string                 `json:"kind"`
	CollectionID     string                 `json:"collectionId"`
	ExpectedRevision int64                  `json:"expectedRevision"`
	NormalizedInput  *CollectionControlBody `json:"normalizedInput,omitempty"`
}
type CollectionControlReceipt struct {
	SchemaVersion    string                     `json:"schemaVersion"`
	OperationID      string                     `json:"operationId"`
	Status           string                     `json:"status"`
	Action           string                     `json:"action,omitempty"`
	Kind             string                     `json:"kind,omitempty"`
	CollectionID     string                     `json:"collectionId,omitempty"`
	ExpectedRevision *int64                     `json:"expectedRevision,omitempty"`
	AppliedRevision  int64                      `json:"appliedRevision,omitempty"`
	PayloadDigest    string                     `json:"payloadDigest,omitempty"`
	CommittedAt      *time.Time                 `json:"committedAt,omitempty"`
	ResponseProof    identity.HumanControlProof `json:"-"`
}
type HumanCollectionControlStore interface {
	PreviewHumanCollectionControl(context.Context, [32]byte, identity.Actor, CollectionControlInput) (CollectionControlPreview, error)
	ApplyHumanCollectionControl(context.Context, [32]byte, identity.Actor, CollectionControlSubmission) (CollectionControlReceipt, error)
	ReadHumanCollectionControl(context.Context, [32]byte, identity.Actor, string) (CollectionControlReceipt, error)
	StopHumanCollectionControl(context.Context, [32]byte, identity.Actor, string) (CollectionControlReceipt, error)
	FinalizeHumanCollectionControlReceipt(context.Context, [32]byte, identity.Actor, CollectionControlReceipt) error
}

func (b CollectionControlBody) CollectionInput() CollectionInput {
	entries := make([]CollectionEntry, len(b.Entries))
	for i, e := range b.Entries {
		entries[i] = CollectionEntry{PlaceID: e.PlaceID, Note: e.Note, LocationVisibility: e.LocationVisibility}
	}
	return CollectionInput{Kind: b.Kind, CityID: b.CityID, Title: b.Title, Summary: b.Summary, Entries: entries}
}
func normalizeCollectionControlBody(b CollectionControlBody) (CollectionControlBody, error) {
	// City IDs may be stable slugs, not Context UUIDs. Text is inert material.
	for _, s := range []string{b.Kind, b.CityID, b.Title, b.Summary} {
		if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
			return CollectionControlBody{}, ErrInvalid
		}
	}
	if strings.ContainsAny(b.CityID, "\r\n\t") {
		return CollectionControlBody{}, ErrInvalid
	}
	for _, e := range b.Entries {
		for _, s := range []string{e.PlaceID, e.Note, e.LocationVisibility} {
			if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
				return CollectionControlBody{}, ErrInvalid
			}
		}
	}
	in, e := NormalizeCollectionInput(b.CollectionInput())
	if e != nil {
		return CollectionControlBody{}, e
	}
	out := CollectionControlBody{Kind: in.Kind, CityID: in.CityID, Title: in.Title, Summary: in.Summary, Entries: make([]CollectionControlEntry, len(in.Entries))}
	for i, e := range in.Entries {
		out.Entries[i] = CollectionControlEntry{PlaceID: e.PlaceID, Note: e.Note, LocationVisibility: e.LocationVisibility}
	}
	return out, nil
}
func NormalizeCollectionControl(in CollectionControlInput) (CollectionControlInput, error) {
	if in.SchemaVersion != CollectionControlSchemaVersion || !ValidCollectionKind(in.Kind) || (in.Action != "UPDATE" && in.Action != "WITHDRAW") {
		return CollectionControlInput{}, ErrInvalid
	}
	in.OperationID = strings.ToLower(strings.TrimSpace(in.OperationID))
	in.CollectionID = strings.ToLower(strings.TrimSpace(in.CollectionID))
	if !identity.HumanControlUUID(in.OperationID) || !identity.HumanControlUUID(in.CollectionID) || in.ExpectedRevision < 1 || in.ExpectedRevision > MaxCollectionControlRevision {
		return CollectionControlInput{}, ErrInvalid
	}
	if in.Action == "WITHDRAW" {
		if in.Input != nil {
			return CollectionControlInput{}, ErrInvalid
		}
		return in, nil
	}
	if in.ExpectedRevision >= MaxCollectionControlRevision || in.Input == nil {
		return CollectionControlInput{}, ErrInvalid
	}
	body, e := normalizeCollectionControlBody(*in.Input)
	if e != nil || body.Kind != in.Kind {
		return CollectionControlInput{}, ErrInvalid
	}
	in.Input = &body
	return in, nil
}
func NormalizeCollectionControlSubmission(in CollectionControlSubmission) (CollectionControlSubmission, error) {
	normalized, e := NormalizeCollectionControl(in.CollectionControlInput)
	if e != nil || !utf8.ValidString(in.Approval) || len(in.Approval) < 1 || len(in.Approval) > 8192 || strings.TrimSpace(in.Approval) != in.Approval || strings.ContainsAny(in.Approval, "\x00\r\n\t") {
		return CollectionControlSubmission{}, ErrInvalid
	}
	in.CollectionControlInput = normalized
	return in, nil
}

// Digest is SHA256 of this compact, entirely ASCII JSON tuple:
// [schema,action,kind,collectionID,expectedRevision,bodyOrNull]. UPDATE body is
// [kind,b64url(cityId),b64url(title),b64url(summary),[[placeID,b64url(note),precision],...]].
// Each base64url encodes the exact normalized UTF8 bytes without padding.
// OperationID is separately bound by approval/ledger, not by this payload hash.
func CollectionControlDigest(in CollectionControlInput) string {
	in, e := NormalizeCollectionControl(in)
	if e != nil {
		return ""
	}
	var body any
	if in.Input != nil {
		b := in.Input
		entries := make([]any, len(b.Entries))
		for i, e := range b.Entries {
			entries[i] = []any{e.PlaceID, base64.RawURLEncoding.EncodeToString([]byte(e.Note)), e.LocationVisibility}
		}
		body = []any{b.Kind, base64.RawURLEncoding.EncodeToString([]byte(b.CityID)), base64.RawURLEncoding.EncodeToString([]byte(b.Title)), base64.RawURLEncoding.EncodeToString([]byte(b.Summary)), entries}
	}
	raw, _ := json.Marshal([]any{CollectionControlSchemaVersion, in.Action, in.Kind, in.CollectionID, in.ExpectedRevision, body})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func collectionControlObject(raw []byte, required []string, optional ...string) (map[string]json.RawMessage, error) {
	if len(raw) < 1 || len(raw) > MaxCollectionControlRequestBytes || !utf8.Valid(raw) || !collectionControlEscapes(raw) {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return nil, ErrInvalid
	}
	allowed := map[string]bool{}
	for _, k := range append(append([]string{}, required...), optional...) {
		allowed[k] = true
	}
	seen := map[string]json.RawMessage{}
	for d.More() {
		token, e = d.Token()
		key, ok := token.(string)
		if e != nil || !ok || !allowed[key] {
			return nil, ErrInvalid
		}
		if _, ok = seen[key]; ok {
			return nil, ErrInvalid
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, ErrInvalid
		}
		seen[key] = value
	}
	token, e = d.Token()
	if e != nil || token != json.Delim('}') {
		return nil, ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ErrInvalid
	}
	for _, k := range required {
		if _, ok := seen[k]; !ok {
			return nil, ErrInvalid
		}
	}
	return seen, nil
}
func collectionControlEscapes(raw []byte) bool {
	// Unpaired UTF16 escapes must be rejected, not replaced by encoding/json.
	for i := 0; i < len(raw); i++ {
		if raw[i] != '"' {
			continue
		}
		i++
		for ; i < len(raw) && raw[i] != '"'; i++ {
			if raw[i] != '\\' {
				continue
			}
			i++
			if i >= len(raw) {
				return false
			}
			if raw[i] != 'u' {
				continue
			}
			if i+4 >= len(raw) {
				return false
			}
			n, e := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
			if e != nil {
				return false
			}
			i += 4
			if n >= 0xd800 && n <= 0xdbff {
				if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
					return false
				}
				lo, e := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
				if e != nil || lo < 0xdc00 || lo > 0xdfff {
					return false
				}
				i += 6
			} else if n >= 0xdc00 && n <= 0xdfff {
				return false
			}
		}
	}
	return true
}
func decodeCollectionControl(raw []byte, submission bool) (CollectionControlSubmission, error) {
	required := []string{"schemaVersion", "operationId", "action", "kind", "collectionId", "expectedRevision"}
	if submission {
		required = append(required, "approval")
	}
	fields, e := collectionControlObject(raw, required, "input")
	if e != nil {
		return CollectionControlSubmission{}, e
	}
	var in CollectionControlSubmission
	if json.Unmarshal(raw, &in) != nil {
		return in, ErrInvalid
	}
	if in.Action == "UPDATE" {
		body, ok := fields["input"]
		if !ok {
			return in, ErrInvalid
		}
		m, e := collectionControlObject(body, []string{"kind", "cityId", "title", "summary", "entries"})
		if e != nil {
			return in, e
		}
		var entries []json.RawMessage
		if json.Unmarshal(m["entries"], &entries) != nil || len(entries) < 1 || len(entries) > 50 {
			return in, ErrInvalid
		}
		for _, entry := range entries {
			if _, e = collectionControlObject(entry, []string{"placeId", "note", "locationVisibility"}); e != nil {
				return in, e
			}
		}
	} else if _, ok := fields["input"]; ok {
		return in, ErrInvalid
	}
	if submission {
		return NormalizeCollectionControlSubmission(in)
	}
	in.CollectionControlInput, e = NormalizeCollectionControl(in.CollectionControlInput)
	return in, e
}
func DecodeCollectionControlInput(raw []byte) (CollectionControlInput, error) {
	in, e := decodeCollectionControl(raw, false)
	return in.CollectionControlInput, e
}
func DecodeCollectionControlSubmission(raw []byte) (CollectionControlSubmission, error) {
	return decodeCollectionControl(raw, true)
}

func collectionControlHex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func ValidCollectionControlReceipt(v CollectionControlReceipt, op string) bool {
	if v.SchemaVersion != CollectionControlSchemaVersion || !identity.HumanControlUUID(op) || v.OperationID != op {
		return false
	}
	if v.Status == "STOPPED" {
		return v.Action == "" && v.Kind == "" && v.CollectionID == "" && v.ExpectedRevision == nil && v.AppliedRevision == 0 && v.PayloadDigest == "" && v.CommittedAt == nil
	}
	if v.Status != "SUCCEEDED" || !ValidCollectionKind(v.Kind) || !identity.HumanControlUUID(v.CollectionID) || v.ExpectedRevision == nil || *v.ExpectedRevision < 1 || *v.ExpectedRevision > MaxCollectionControlRevision || !collectionControlHex(v.PayloadDigest) || v.CommittedAt == nil || v.CommittedAt.IsZero() || v.CommittedAt.Year() < 1 || v.CommittedAt.Year() > 9999 {
		return false
	}
	if v.Action == "UPDATE" {
		return *v.ExpectedRevision < MaxCollectionControlRevision && v.AppliedRevision == *v.ExpectedRevision+1
	}
	if v.Action != "WITHDRAW" {
		return false
	}
	applied := *v.ExpectedRevision
	if applied < MaxCollectionControlRevision {
		applied++
	}
	return v.AppliedRevision == applied
}
func ValidateCollectionControlPreview(v CollectionControlPreview, in CollectionControlInput, now time.Time) error {
	in, e := NormalizeCollectionControl(in)
	if e != nil {
		return e
	}
	if v.SchemaVersion != CollectionControlSchemaVersion || v.OperationID != in.OperationID || v.Action != in.Action || v.Kind != in.Kind || v.CollectionID != in.CollectionID || v.ExpectedRevision != in.ExpectedRevision || v.PayloadDigest != CollectionControlDigest(in) || v.ObservedAt.IsZero() || v.ExpiresAt.IsZero() || !v.ExpiresAt.After(v.ObservedAt) || v.ExpiresAt.Sub(v.ObservedAt) > 30*time.Second || !v.ExpiresAt.After(now) || now.Before(v.ObservedAt) || len(v.Approval) < 1 || len(v.Approval) > 8192 || !utf8.ValidString(v.Approval) || strings.TrimSpace(v.Approval) != v.Approval || strings.ContainsAny(v.Approval, "\x00\r\n\t") {
		return ErrInvalid
	}
	actual, e := json.Marshal(v.NormalizedInput)
	expected, e2 := json.Marshal(in.Input)
	if e != nil || e2 != nil || !bytes.Equal(actual, expected) {
		return ErrInvalid
	}
	return nil
}
