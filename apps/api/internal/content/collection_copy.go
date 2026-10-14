package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const CollectionCopyMaxVersion int64 = 8999999999999999

type CollectionRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type CollectionCopyGrant struct {
	SourceRef             CollectionRef `json:"sourceRef"`
	PublicationRevision   int64         `json:"publicationRevision"`
	GrantVersion          int64         `json:"grantVersion"`
	EligiblePlaceIDs      []string      `json:"eligiblePlaceIds"`
	OrderedPlaceIDsSHA256 string        `json:"orderedPlaceIdsSha256"`
	ExpiresAt             time.Time     `json:"expiresAt"`
}
type CollectionCopyInput struct {
	OperationID           string   `json:"operationId"`
	PublicationRevision   int64    `json:"publicationRevision"`
	GrantVersion          int64    `json:"grantVersion"`
	OrderedPlaceIDs       []string `json:"orderedPlaceIds"`
	OrderedPlaceIDsSHA256 string   `json:"orderedPlaceIdsSha256"`
	Title                 string   `json:"title"`
	Approval              string   `json:"approval,omitempty"`
}
type CollectionCopyPreview struct {
	SourceRef             CollectionRef       `json:"sourceRef"`
	OperationID           string              `json:"operationId"`
	PublicationRevision   int64               `json:"publicationRevision"`
	GrantVersion          int64               `json:"grantVersion"`
	OrderedPlaceIDsSHA256 string              `json:"orderedPlaceIdsSha256"`
	EntryCount            int                 `json:"entryCount"`
	Title                 string              `json:"title"`
	InputSHA256           string              `json:"inputSha256"`
	Approval              string              `json:"approval"`
	ExpiresAt             time.Time           `json:"expiresAt"`
	ResponseProof         CollectionReadProof `json:"-"`
}
type CollectionCopyReceipt struct {
	OperationID           string         `json:"operationId"`
	Status                string         `json:"status"`
	ResultRevision        int64          `json:"resultRevision"`
	DestinationRef        *CollectionRef `json:"destinationRef"`
	SourceRef             *CollectionRef `json:"sourceRef,omitempty"`
	PublicationRevision   int64          `json:"publicationRevision,omitempty"`
	GrantVersion          int64          `json:"grantVersion,omitempty"`
	OrderedPlaceIDsSHA256 string         `json:"orderedPlaceIdsSha256,omitempty"`
}
type CollectionReadProof interface{ RevalidateCollection(context.Context) error }
type CollectionCopyStore interface {
	PreviewHumanCollectionCopy(context.Context, [32]byte, identity.Actor, CollectionRef, CollectionCopyInput) (CollectionCopyPreview, error)
	CopyHumanCollection(context.Context, [32]byte, identity.Actor, CollectionRef, CollectionCopyInput) (CollectionCopyReceipt, error)
	GetHumanCollectionCopyOperation(context.Context, [32]byte, identity.Actor, string) (CollectionCopyReceipt, error)
	StopHumanCollectionCopyOperation(context.Context, [32]byte, identity.Actor, string) (CollectionCopyReceipt, error)
}
type CollectionSessionFrame struct {
	ID         string `json:"-"`
	Generation int64  `json:"-"`
}
type CollectionCopySessionStore interface {
	CaptureCollectionSession(context.Context, [32]byte, identity.Actor) (CollectionSessionFrame, error)
	ValidateCollectionSession(context.Context, [32]byte, identity.Actor, CollectionSessionFrame) error
}
type collectionSessionKey struct{}
type collectionSessionValue struct {
	Actor   identity.Actor
	Digest  [32]byte
	Frame   CollectionSessionFrame
	Invalid bool
}

func WithCollectionSession(ctx context.Context, d [32]byte, a identity.Actor, f CollectionSessionFrame) context.Context {
	if old, ok := ctx.Value(collectionSessionKey{}).(collectionSessionValue); ok {
		if old.Actor == a && old.Digest == d && old.Frame == f && !old.Invalid {
			return ctx
		}
		old.Invalid = true
		return context.WithValue(ctx, collectionSessionKey{}, old)
	}
	return context.WithValue(ctx, collectionSessionKey{}, collectionSessionValue{Actor: a, Digest: d, Frame: f})
}
func OriginalCollectionSession(ctx context.Context, d [32]byte, a identity.Actor) (CollectionSessionFrame, bool, bool) {
	v, ok := ctx.Value(collectionSessionKey{}).(collectionSessionValue)
	return v.Frame, ok && !v.Invalid && v.Digest == d && v.Actor.ID == a.ID && v.Actor.AccountType == a.AccountType, ok
}
func CollectionSessionOwnerFrame(ctx context.Context, owner string) (CollectionSessionFrame, bool) {
	v, ok := ctx.Value(collectionSessionKey{}).(collectionSessionValue)
	return v.Frame, ok && !v.Invalid && v.Actor.ID == owner && v.Actor.AccountType == "person"
}
func ValidCollectionRef(r CollectionRef) bool {
	return ValidCollectionKind(r.Kind) && ValidCollectionID(r.ID) && r.ID == strings.ToLower(r.ID)
}
func CollectionCopyPlacesDigest(ids []string) string {
	b, _ := json.Marshal([]any{"collection-copy-places-v1", ids})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func CollectionCopyDigest(r CollectionRef, in CollectionCopyInput) string {
	title := sha256.Sum256([]byte(in.Title))
	b, _ := json.Marshal([]any{"collection-private-copy-v1", r.Kind, r.ID, in.OperationID, in.PublicationRevision, in.GrantVersion, in.OrderedPlaceIDsSHA256, hex.EncodeToString(title[:])})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func CollectionPublicationCopyDigest(kind, id, action string, revision int64, entries []CollectionPublicationEntry, allow bool) [32]byte {
	if !allow {
		return CollectionPublicationDigest(kind, id, action, revision, entries)
	}
	b, _ := json.Marshal([]any{"collection-publication-copy-v1", kind, id, action, revision, entries, true})
	return sha256.Sum256(b)
}
func NormalizeCollectionCopy(r CollectionRef, in CollectionCopyInput, commit bool) (CollectionCopyInput, error) {
	in.Title = strings.TrimSpace(in.Title)
	if !ValidCollectionRef(r) || !ValidCollectionID(in.OperationID) || in.OperationID != strings.ToLower(in.OperationID) || in.PublicationRevision < 2 || in.PublicationRevision > CollectionCopyMaxVersion || in.GrantVersion < 1 || in.GrantVersion > CollectionCopyMaxVersion || len(in.OrderedPlaceIDs) < 1 || len(in.OrderedPlaceIDs) > 50 || len(in.Title) < 1 || len(in.Title) > 160 || !utf8.ValidString(in.Title) || strings.ContainsRune(in.Title, 0) {
		return CollectionCopyInput{}, ErrInvalid
	}
	seen := map[string]bool{}
	in.OrderedPlaceIDs = append([]string(nil), in.OrderedPlaceIDs...)
	for _, id := range in.OrderedPlaceIDs {
		if !ValidCollectionID(id) || id != strings.ToLower(id) || seen[id] {
			return CollectionCopyInput{}, ErrInvalid
		}
		seen[id] = true
	}
	if in.OrderedPlaceIDsSHA256 != CollectionCopyPlacesDigest(in.OrderedPlaceIDs) || (!commit && in.Approval != "") || (commit && (len(in.Approval) < 1 || len(in.Approval) > 8192 || !utf8.ValidString(in.Approval))) {
		return CollectionCopyInput{}, ErrInvalid
	}
	return in, nil
}
func CollectionCopySubsequence(chosen, eligible []string) bool {
	i := 0
	for _, id := range eligible {
		if i < len(chosen) && chosen[i] == id {
			i++
		}
	}
	return len(chosen) > 0 && i == len(chosen)
}
func ValidateCollectionCopyGrant(g CollectionCopyGrant, v PublicCollection) error {
	if g.SourceRef != (CollectionRef{v.Kind, v.ID}) || g.PublicationRevision != v.Revision || g.GrantVersion < 1 || g.GrantVersion > CollectionCopyMaxVersion || len(g.EligiblePlaceIDs) < 1 || len(g.EligiblePlaceIDs) > 50 || g.OrderedPlaceIDsSHA256 != CollectionCopyPlacesDigest(g.EligiblePlaceIDs) || g.ExpiresAt.IsZero() || g.ExpiresAt.After(v.SourceExpiresAt) {
		return ErrInvalid
	}
	ids := []string{}
	for _, e := range v.Entries {
		if e.LocationVisibility == "place" && e.PlaceID != nil {
			ids = append(ids, *e.PlaceID)
		}
	}
	if len(ids) != len(g.EligiblePlaceIDs) {
		return ErrInvalid
	}
	for i := range ids {
		if ids[i] != g.EligiblePlaceIDs[i] {
			return ErrInvalid
		}
	}
	return nil
}
func decodeCollectionClosed(raw []byte, required, optional []string) error {
	if len(raw) < 2 || len(raw) > 16384 {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return ErrInvalid
	}
	allowed := map[string]bool{}
	for _, k := range append(append([]string{}, required...), optional...) {
		allowed[k] = true
	}
	seen := map[string]bool{}
	for d.More() {
		t, e = d.Token()
		k, ok := t.(string)
		if e != nil || !ok || !allowed[k] || seen[k] {
			return ErrInvalid
		}
		seen[k] = true
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return ErrInvalid
		}
	}
	if t, e = d.Token(); e != nil || t != json.Delim('}') {
		return ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return ErrInvalid
	}
	for _, k := range required {
		if !seen[k] {
			return ErrInvalid
		}
	}
	return nil
}
func DecodeCollectionCopy(raw []byte, r CollectionRef, commit bool) (CollectionCopyInput, error) {
	keys := []string{"operationId", "publicationRevision", "grantVersion", "orderedPlaceIds", "orderedPlaceIdsSha256", "title"}
	if commit {
		keys = append(keys, "approval")
	}
	if decodeCollectionClosed(raw, keys, nil) != nil {
		return CollectionCopyInput{}, ErrInvalid
	}
	var in CollectionCopyInput
	if json.Unmarshal(raw, &in) != nil {
		return in, ErrInvalid
	}
	return NormalizeCollectionCopy(r, in, commit)
}
func DecodeCollectionPublication(raw []byte) (CollectionPublicationInput, error) {
	if decodeCollectionClosed(raw, []string{"operationId", "revision", "entries"}, []string{"allowPrivatePlaceCopy"}) != nil {
		return CollectionPublicationInput{}, ErrInvalid
	}
	var in CollectionPublicationInput
	var selectors struct {
		Entries []json.RawMessage `json:"entries"`
	}
	if json.Unmarshal(raw, &selectors) != nil {
		return in, ErrInvalid
	}
	for _, entry := range selectors.Entries {
		if decodeCollectionClosed(entry, []string{"placeId", "locationVisibility"}, nil) != nil {
			return in, ErrInvalid
		}
	}
	if json.Unmarshal(raw, &in) != nil {
		return in, ErrInvalid
	}
	return NormalizeCollectionPublication(in)
}
