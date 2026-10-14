package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"io"
	"strings"
	"unicode/utf8"
)

const MaxCollectionAppendRevision int64 = 9000000000000000

type CollectionAppendEntry struct {
	PlaceID            string `json:"placeId"`
	Note               string `json:"note"`
	LocationVisibility string `json:"locationVisibility"`
}
type CollectionAppendInput struct {
	CityID           string                  `json:"cityId"`
	ExpectedRevision int64                   `json:"expectedRevision"`
	OperationID      string                  `json:"operationId"`
	Entries          []CollectionAppendEntry `json:"entries"`
}
type CollectionAppendReceipt struct {
	OperationID     string `json:"operationId"`
	Kind            string `json:"kind"`
	CollectionID    string `json:"collectionId"`
	AppliedRevision int64  `json:"appliedRevision"`
	Status          string `json:"status"`
}
type HumanCollectionAppendStore interface {
	AppendHumanCollectionPlaces(context.Context, [32]byte, identity.Actor, string, string, CollectionAppendInput) (CollectionAppendReceipt, error)
	GetHumanCollectionAppendOperation(context.Context, [32]byte, identity.Actor, string, string, string) (CollectionAppendReceipt, error)
	StopHumanCollectionAppendOperation(context.Context, [32]byte, identity.Actor, string, string, string, CollectionAppendStopInput) (CollectionAppendReceipt, error)
}
type CollectionAppendStopInput struct {
	CityID           string `json:"cityId"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

func NormalizeCollectionAppendStop(in CollectionAppendStopInput) (CollectionAppendStopInput, error) {
	in.CityID = strings.TrimSpace(in.CityID)
	if !utf8.ValidString(in.CityID) || len(in.CityID) < 1 || len(in.CityID) > 80 || strings.ContainsAny(in.CityID, "\x00\r\n\t") || in.ExpectedRevision < 1 || in.ExpectedRevision >= MaxCollectionAppendRevision {
		return CollectionAppendStopInput{}, ErrInvalid
	}
	return in, nil
}
func DecodeCollectionAppendStop(raw []byte) (CollectionAppendStopInput, error) {
	if !utf8.Valid(raw) || len(raw) < 1 || len(raw) > 1024 || collectionAppendObject(raw, "cityId", "expectedRevision") != nil {
		return CollectionAppendStopInput{}, ErrInvalid
	}
	var in CollectionAppendStopInput
	if json.Unmarshal(raw, &in) != nil {
		return in, ErrInvalid
	}
	return NormalizeCollectionAppendStop(in)
}
func NormalizeCollectionAppend(in CollectionAppendInput) (CollectionAppendInput, error) {
	in.CityID = strings.TrimSpace(in.CityID)
	if !ValidCollectionID(in.OperationID) || !utf8.ValidString(in.CityID) || len(in.CityID) < 1 || len(in.CityID) > 80 || strings.ContainsAny(in.CityID, "\x00\r\n\t") || in.ExpectedRevision < 1 || in.ExpectedRevision >= MaxCollectionAppendRevision || len(in.Entries) < 1 || len(in.Entries) > 50 {
		return CollectionAppendInput{}, ErrInvalid
	}
	in.Entries = append([]CollectionAppendEntry(nil), in.Entries...)
	seen := map[string]bool{}
	for i, e := range in.Entries {
		e.PlaceID = strings.ToLower(strings.TrimSpace(e.PlaceID))
		if !ValidCollectionID(e.PlaceID) || seen[e.PlaceID] || e.Note != "" || e.LocationVisibility != "hidden" {
			return CollectionAppendInput{}, ErrInvalid
		}
		seen[e.PlaceID] = true
		in.Entries[i] = e
	}
	return in, nil
}
func CollectionAppendDigest(kind, id string, in CollectionAppendInput) [32]byte {
	raw, _ := json.Marshal([]any{"collection-append-places-v1", kind, id, in})
	return sha256.Sum256(raw)
}
func (r CollectionAppendReceipt) Valid(kind, id, op string) bool {
	return ValidCollectionKind(kind) && r.Kind == kind && ValidCollectionID(id) && r.CollectionID == id && ValidCollectionID(op) && r.OperationID == op && ((r.AppliedRevision > 1 && r.AppliedRevision <= MaxCollectionAppendRevision && r.Status == "SUCCEEDED") || (r.AppliedRevision == 0 && r.Status == "STOPPED"))
}

func collectionAppendObject(raw []byte, keys ...string) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return ErrInvalid
	}
	allowed := map[string]bool{}
	for _, k := range keys {
		allowed[k] = true
	}
	seen := map[string]bool{}
	for d.More() {
		token, e = d.Token()
		k, ok := token.(string)
		if e != nil || !ok || !allowed[k] || seen[k] {
			return ErrInvalid
		}
		seen[k] = true
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return ErrInvalid
		}
	}
	if token, e = d.Token(); e != nil || token != json.Delim('}') || len(seen) != len(keys) {
		return ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return ErrInvalid
	}
	return nil
}
func DecodeCollectionAppend(raw []byte) (CollectionAppendInput, error) {
	if !utf8.Valid(raw) || len(raw) < 1 || len(raw) > 16384 || collectionAppendObject(raw, "cityId", "expectedRevision", "operationId", "entries") != nil {
		return CollectionAppendInput{}, ErrInvalid
	}
	var fields struct {
		Entries []json.RawMessage `json:"entries"`
	}
	if json.Unmarshal(raw, &fields) != nil {
		return CollectionAppendInput{}, ErrInvalid
	}
	for _, v := range fields.Entries {
		if collectionAppendObject(v, "placeId", "note", "locationVisibility") != nil {
			return CollectionAppendInput{}, ErrInvalid
		}
	}
	var in CollectionAppendInput
	if json.Unmarshal(raw, &in) != nil {
		return CollectionAppendInput{}, ErrInvalid
	}
	return NormalizeCollectionAppend(in)
}
