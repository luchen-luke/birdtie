package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

const MomentControlMaxRevision = identity.HumanControlMaxVersion

type MomentControlInput struct {
	OperationID      string       `json:"operationId"`
	Action           string       `json:"action"`
	MomentID         string       `json:"momentId,omitempty"`
	ExpectedRevision int64        `json:"expectedRevision"`
	Input            *MomentInput `json:"input,omitempty"`
	SourceVersion    string       `json:"sourceVersion,omitempty"`
	Approval         string       `json:"approval,omitempty"`
}
type MomentControlPreview struct {
	identity.HumanControlPreview
	MomentID                  string       `json:"momentId,omitempty"`
	ExpectedRevision          int64        `json:"expectedRevision"`
	Input                     *MomentInput `json:"input,omitempty"`
	InputSHA256               string       `json:"inputSha256"`
	SourceVersion             string       `json:"sourceVersion"`
	PreservedPlaceUnavailable bool         `json:"preservedPlaceUnavailable"`
}
type MomentControlReceipt struct {
	OperationID      string     `json:"operationId"`
	Status           string     `json:"status"`
	AppliedRevision  int64      `json:"appliedRevision"`
	Action           string     `json:"action,omitempty"`
	MomentID         string     `json:"momentId,omitempty"`
	ExpectedRevision *int64     `json:"expectedRevision,omitempty"`
	PayloadDigest    string     `json:"payloadDigest,omitempty"`
	CommittedAt      *time.Time `json:"committedAt,omitempty"`
}
type MomentControlStore interface {
	PreviewMomentControl(context.Context, [32]byte, identity.Actor, MomentControlInput) (MomentControlPreview, error)
	CommitMomentControl(context.Context, [32]byte, identity.Actor, MomentControlInput) (MomentControlReceipt, error)
	ReadMomentControlOperation(context.Context, [32]byte, identity.Actor, string) (MomentControlReceipt, error)
	StopMomentControlOperation(context.Context, [32]byte, identity.Actor, string) (MomentControlReceipt, error)
}

func MomentControlHash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func MomentControlInputHash(in *MomentInput) string {
	if in == nil {
		return ""
	}
	b64 := func(v string) string { return base64.StdEncoding.EncodeToString([]byte(v)) }
	at := ""
	if in.OccurredAt != nil {
		at = in.OccurredAt.UTC().Format(time.RFC3339Nano)
	}
	return MomentControlHash([]any{"moment-input-v1", b64(in.CityID), in.PlaceID, b64(in.Title), b64(in.Body), at, in.TimePrecision, in.LocationPrecision, in.ActivityID, in.CommunityID, in.OrganizationID})
}
func MomentControlDigest(in MomentControlInput) string {
	return MomentControlHash([]any{"human-moment-control-v1", in.OperationID, in.Action, in.MomentID, in.ExpectedRevision, MomentControlInputHash(in.Input)})
}
func MomentControlHex(v string) bool {
	if len(v) != 64 || v != strings.ToLower(v) {
		return false
	}
	_, e := hex.DecodeString(v)
	return e == nil
}
func NormalizeMomentControl(in MomentControlInput, commit bool) (MomentControlInput, error) {
	bad := func() (MomentControlInput, error) { return MomentControlInput{}, identity.ErrControlInvalid }
	if !identity.HumanControlUUID(in.OperationID) || in.ExpectedRevision < 0 || in.ExpectedRevision > MomentControlMaxRevision {
		return bad()
	}
	switch in.Action {
	case "CREATE":
		if in.MomentID != "" || in.ExpectedRevision != 0 || in.Input == nil {
			return bad()
		}
	case "UPDATE", "UPDATE_PUBLIC", "PUBLISH":
		if !identity.HumanControlUUID(in.MomentID) || in.ExpectedRevision < 1 || in.ExpectedRevision >= MomentControlMaxRevision || in.Input == nil {
			return bad()
		}
	case "WITHDRAW":
		if !identity.HumanControlUUID(in.MomentID) || in.ExpectedRevision < 1 || in.Input != nil {
			return bad()
		}
	default:
		return bad()
	}
	if in.Input != nil {
		v := *in.Input
		v.PlaceID = strings.ToLower(strings.TrimSpace(v.PlaceID))
		for _, slot := range []**string{&v.ActivityID, &v.CommunityID, &v.OrganizationID} {
			if *slot != nil {
				x := strings.ToLower(strings.TrimSpace(**slot))
				*slot = &x
				if x != "" && !identity.HumanControlUUID(x) {
					return bad()
				}
			}
		}
		var e error
		v, e = NormalizeMomentInput(v)
		if e != nil {
			return bad()
		}
		for _, s := range []string{v.CityID, v.Title, v.Body} {
			if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
				return bad()
			}
		}
		if v.PlaceID != "" && !identity.HumanControlUUID(v.PlaceID) {
			return bad()
		}
		in.Input = &v
		if (in.Action == "UPDATE_PUBLIC" || in.Action == "PUBLISH") && (v.PlaceID == "" || v.LocationPrecision != "place" || v.ActivityID != nil || v.CommunityID != nil || v.OrganizationID != nil) {
			return bad()
		}
	}
	if commit {
		if !MomentControlHex(in.SourceVersion) || !utf8.ValidString(in.Approval) || len(in.Approval) < 1 || len(in.Approval) > 8192 || strings.ContainsRune(in.Approval, 0) {
			return bad()
		}
	} else if in.SourceVersion != "" || in.Approval != "" {
		return bad()
	}
	return in, nil
}
func momentControlClosed(raw []byte, required, optional []string, nullable string) (map[string]json.RawMessage, error) {
	bad := func() (map[string]json.RawMessage, error) { return nil, identity.ErrControlInvalid }
	if len(raw) < 2 || len(raw) > 32768 || !utf8.Valid(raw) {
		return bad()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return bad()
	}
	allowed := map[string]bool{}
	for _, k := range append(append([]string{}, required...), optional...) {
		allowed[k] = true
	}
	seen := map[string]json.RawMessage{}
	for d.More() {
		t, e = d.Token()
		k, ok := t.(string)
		if e != nil || !ok || !allowed[k] {
			return bad()
		}
		if _, ok = seen[k]; ok {
			return bad()
		}
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) && k != nullable {
			return bad()
		}
		seen[k] = v
	}
	if _, e = d.Token(); e != nil {
		return bad()
	}
	if _, e = d.Token(); e != io.EOF {
		return bad()
	}
	for _, k := range required {
		if _, ok := seen[k]; !ok {
			return bad()
		}
	}
	return seen, nil
}
func DecodeMomentControl(raw []byte, commit bool) (MomentControlInput, error) {
	required := []string{"operationId", "action", "expectedRevision"}
	optional := []string{"momentId", "input"}
	if commit {
		required = append(required, "sourceVersion", "approval")
	}
	m, e := momentControlClosed(raw, required, optional, "")
	if e != nil {
		return MomentControlInput{}, e
	}
	var in MomentControlInput
	if json.Unmarshal(raw, &in) != nil {
		return in, identity.ErrControlInvalid
	}
	if in.Action == "CREATE" {
		if _, ok := m["momentId"]; ok {
			return in, identity.ErrControlInvalid
		}
	} else {
		if _, ok := m["momentId"]; !ok {
			return in, identity.ErrControlInvalid
		}
	}
	if v, ok := m["input"]; ok {
		if in.Action == "WITHDRAW" {
			return in, identity.ErrControlInvalid
		}
		if _, e = momentControlClosed(v, []string{"cityId", "placeId", "title", "body", "timePrecision", "locationPrecision"}, []string{"occurredAt", "activityId", "communityId", "organizationId"}, "occurredAt"); e != nil {
			return in, e
		}
	}
	return NormalizeMomentControl(in, commit)
}
func ValidMomentControlReceipt(v MomentControlReceipt, op string) bool {
	if !identity.HumanControlUUID(op) || v.OperationID != op {
		return false
	}
	if v.Status == "STOPPED" {
		return v.AppliedRevision == 0 && v.Action == "" && v.MomentID == "" && v.ExpectedRevision == nil && v.PayloadDigest == "" && v.CommittedAt == nil
	}
	if v.Status != "SUCCEEDED" || !identity.HumanControlUUID(v.MomentID) || v.ExpectedRevision == nil || !MomentControlHex(v.PayloadDigest) || v.CommittedAt == nil || v.CommittedAt.IsZero() {
		return false
	}
	expected := *v.ExpectedRevision
	if expected < 0 || expected > MomentControlMaxRevision {
		return false
	}
	want := expected + 1
	switch v.Action {
	case "CREATE":
		if expected != 0 {
			return false
		}
	case "UPDATE", "UPDATE_PUBLIC", "PUBLISH":
		if expected < 1 || expected >= MomentControlMaxRevision {
			return false
		}
	case "WITHDRAW":
		if expected < 1 {
			return false
		}
		if want > MomentControlMaxRevision {
			want = MomentControlMaxRevision
		}
	default:
		return false
	}
	return v.AppliedRevision == want
}
