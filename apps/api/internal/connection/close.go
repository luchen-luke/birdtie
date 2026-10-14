package connection

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const CloseSchema = "close-connections-v1"
const CloseLimit = 100
const CloseMaxBody = 4096

var (
	ErrCloseInvalid     = errors.New("invalid close connection")
	ErrCloseChanged     = errors.New("close connection source changed")
	ErrCloseUnavailable = errors.New("close connection unavailable")
	closeUUID           = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	closeVersion        = regexp.MustCompile(`^[0-9a-f]{64}$`)
	closeStamp          = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,6})?Z$`)
)

func ValidCloseID(id string) bool {
	return closeUUID.MatchString(id) && id != "00000000-0000-0000-0000-000000000000"
}
func ValidCloseAccess(a ea.Access) bool {
	return !a.Public && a.Valid() && a.Actor.AccountType == "person" && ValidCloseID(a.Actor.ID)
}

type CloseEntry struct {
	PersonID       string `json:"personId"`
	TieID          string `json:"tieId"`
	RequestID      string `json:"requestId"`
	IsClose        bool   `json:"isClose"`
	NativeRevision int64  `json:"nativeRevision"`
	SourceVersion  string `json:"sourceVersion"`
	DisplayName    string `json:"displayName"`
}
type CloseView struct {
	SchemaVersion string       `json:"schemaVersion"`
	OwnerID       string       `json:"ownerId"`
	ObservedAt    time.Time    `json:"observedAt"`
	ValidUntil    time.Time    `json:"validUntil"`
	Connections   []CloseEntry `json:"connections"`
	Limit         int          `json:"limit"`
	Truncated     bool         `json:"truncated"`
	ModelAccess   bool         `json:"modelAccess"`
	ResponseProof any          `json:"-"`
}
type CloseResult struct {
	SchemaVersion string     `json:"schemaVersion"`
	OwnerID       string     `json:"ownerId"`
	ObservedAt    time.Time  `json:"observedAt"`
	ValidUntil    time.Time  `json:"validUntil"`
	Connection    CloseEntry `json:"connection"`
	ModelAccess   bool       `json:"modelAccess"`
	ResponseProof any        `json:"-"`
}

// This receipt is internal. A successful DELETE has an empty 204 response.
type CloseDeleteReceipt struct {
	PersonID      string `json:"-"`
	ResponseProof any    `json:"-"`
}
type ClosePut struct {
	TieID         string    `json:"tieId"`
	RequestID     string    `json:"requestId"`
	SourceVersion string    `json:"sourceVersion"`
	ValidUntil    time.Time `json:"validUntil"`
	Confirmed     bool      `json:"confirmed"`
}
type CloseDelete struct {
	TieID            string `json:"tieId"`
	RequestID        string `json:"requestId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Confirmed        bool   `json:"confirmed"`
}
type CloseCurrentStore interface {
	ListOwnCloseConnectionsCurrent(context.Context, ea.Access) (CloseView, error)
	PutOwnCloseConnectionCurrent(context.Context, ea.Access, string, ClosePut) (CloseResult, error)
	DeleteOwnCloseConnectionCurrent(context.Context, ea.Access, string, CloseDelete) (CloseDeleteReceipt, error)
}
type CloseResponseValidator interface {
	ValidateCloseConnectionsResponse(context.Context, ea.Access, CloseView) error
	ValidateCloseConnectionResponse(context.Context, ea.Access, string, CloseResult) error
	ValidateCloseDeleteResponse(context.Context, ea.Access, CloseDeleteReceipt) error
}

func ValidCloseEntry(v CloseEntry, owner string) bool {
	return ValidCloseID(v.PersonID) && v.PersonID != owner && ValidCloseID(v.TieID) && ValidCloseID(v.RequestID) && v.NativeRevision >= 0 && (!v.IsClose || v.NativeRevision > 0) && closeVersion.MatchString(v.SourceVersion) && utf8.ValidString(v.DisplayName) && strings.TrimSpace(v.DisplayName) != "" && utf8.RuneCountInString(v.DisplayName) <= 80
}
func validCloseHeader(schema, owner string, observed, until time.Time, model bool) bool {
	return schema == CloseSchema && ValidCloseID(owner) && !model && !observed.IsZero() && observed.Year() >= 1 && observed.Year() <= 9999 && until.After(observed) && !until.After(observed.Add(30*time.Second)) && observed.Nanosecond()%1000 == 0 && until.Nanosecond()%1000 == 0 && observed.Location() == time.UTC && until.Location() == time.UTC
}
func ValidCloseView(v CloseView, owner string) bool {
	if v.OwnerID != owner || !validCloseHeader(v.SchemaVersion, v.OwnerID, v.ObservedAt, v.ValidUntil, v.ModelAccess) || v.Limit != CloseLimit || v.Connections == nil || len(v.Connections) > CloseLimit || v.Truncated && len(v.Connections) != CloseLimit {
		return false
	}
	seen := map[string]bool{}
	for _, e := range v.Connections {
		if !ValidCloseEntry(e, owner) || seen[e.PersonID] {
			return false
		}
		seen[e.PersonID] = true
	}
	return true
}
func ValidCloseResult(v CloseResult, owner, peer string) bool {
	return v.OwnerID == owner && v.Connection.PersonID == peer && validCloseHeader(v.SchemaVersion, v.OwnerID, v.ObservedAt, v.ValidUntil, v.ModelAccess) && ValidCloseEntry(v.Connection, owner) && v.Connection.IsClose
}
func ValidClosePut(v ClosePut) bool {
	return ValidCloseID(v.TieID) && ValidCloseID(v.RequestID) && closeVersion.MatchString(v.SourceVersion) && v.Confirmed && !v.ValidUntil.IsZero() && v.ValidUntil.Year() >= 1 && v.ValidUntil.Year() <= 9999 && v.ValidUntil.Location() == time.UTC && v.ValidUntil.Nanosecond()%1000 == 0
}
func ValidCloseDelete(v CloseDelete) bool {
	return ValidCloseID(v.TieID) && ValidCloseID(v.RequestID) && v.ExpectedRevision > 0 && v.Confirmed
}
func decodeCloseObject(raw []byte, fields []string) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > CloseMaxBody || !utf8.Valid(raw) {
		return nil, ErrCloseInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return nil, ErrCloseInvalid
	}
	allowed := map[string]bool{}
	for _, f := range fields {
		allowed[f] = true
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		k, e := d.Token()
		key, ok := k.(string)
		if e != nil || !ok || !allowed[key] || out[key] != nil {
			return nil, ErrCloseInvalid
		}
		var v json.RawMessage
		if d.Decode(&v) != nil {
			return nil, ErrCloseInvalid
		}
		out[key] = v
	}
	if token, e = d.Token(); e != nil || token != json.Delim('}') {
		return nil, ErrCloseInvalid
	}
	if _, e = d.Token(); e != io.EOF || len(out) != len(fields) {
		return nil, ErrCloseInvalid
	}
	return out, nil
}
func DecodeClosePut(raw []byte) (ClosePut, error) {
	var v ClosePut
	m, e := decodeCloseObject(raw, []string{"tieId", "requestId", "sourceVersion", "validUntil", "confirmed"})
	if e != nil {
		return v, e
	}
	var stamp string
	if json.Unmarshal(m["tieId"], &v.TieID) != nil || json.Unmarshal(m["requestId"], &v.RequestID) != nil || json.Unmarshal(m["sourceVersion"], &v.SourceVersion) != nil || json.Unmarshal(m["confirmed"], &v.Confirmed) != nil || json.Unmarshal(m["validUntil"], &stamp) != nil || !closeStamp.MatchString(stamp) {
		return ClosePut{}, ErrCloseInvalid
	}
	v.ValidUntil, e = time.Parse(time.RFC3339Nano, stamp)
	if e != nil || !ValidClosePut(v) {
		return ClosePut{}, ErrCloseInvalid
	}
	return v, nil
}
func DecodeCloseDelete(raw []byte) (CloseDelete, error) {
	var v CloseDelete
	m, e := decodeCloseObject(raw, []string{"tieId", "requestId", "expectedRevision", "confirmed"})
	if e != nil {
		return v, e
	}
	if json.Unmarshal(m["tieId"], &v.TieID) != nil || json.Unmarshal(m["requestId"], &v.RequestID) != nil || json.Unmarshal(m["expectedRevision"], &v.ExpectedRevision) != nil || json.Unmarshal(m["confirmed"], &v.Confirmed) != nil || !ValidCloseDelete(v) {
		return CloseDelete{}, ErrCloseInvalid
	}
	return v, nil
}
