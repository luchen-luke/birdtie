package content

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/momentmedia"
)

const MomentThreadMaxEntries = 20

type MomentThreadItem struct {
	Input MomentInput                   `json:"input"`
	Media []momentmedia.AttachmentInput `json:"media"`
}

type MomentThreadInput struct {
	OperationID     string             `json:"operationId"`
	Action          string             `json:"action"`
	ThreadID        string             `json:"threadId"`
	LocalScope      string             `json:"localScope"`
	Audience        string             `json:"audience"`
	ExpectedVersion int64              `json:"expectedVersion"`
	Items           []MomentThreadItem `json:"items"`
	SourceVersion   string             `json:"sourceVersion,omitempty"`
	Approval        string             `json:"approval,omitempty"`
}

type MomentThreadPreview struct {
	identity.HumanControlPreview
	Input         MomentThreadInput        `json:"input"`
	SourceVersion string                   `json:"sourceVersion"`
	Members       []MomentThreadMember     `json:"members"`
	Materials     [][]MomentThreadMaterial `json:"materials"`
	Contexts      []MomentThreadContext    `json:"contexts"`
}

type MomentThreadContext struct {
	CityID    string `json:"cityId"`
	CityName  string `json:"cityName"`
	PlaceID   string `json:"placeId"`
	PlaceName string `json:"placeName"`
}

type MomentThreadMaterial struct {
	Material        momentmedia.Stage `json:"material"`
	ThumbnailBase64 string            `json:"thumbnailBase64"`
}

type MomentThreadMember struct {
	MomentID        string                      `json:"momentId"`
	Status          string                      `json:"status"`
	Position        int                         `json:"position"`
	AppliedRevision int64                       `json:"appliedRevision"`
	MediaSetVersion int64                       `json:"mediaSetVersion"`
	Media           []momentmedia.StageSelector `json:"media"`
}

type MomentThreadReceipt struct {
	SchemaVersion   string               `json:"schemaVersion"`
	OperationID     string               `json:"operationId"`
	Status          string               `json:"status"`
	Action          string               `json:"action"`
	ThreadID        string               `json:"threadId"`
	RootMomentID    string               `json:"rootMomentId"`
	Audience        string               `json:"audience"`
	ExpectedVersion int64                `json:"expectedVersion"`
	AppliedVersion  int64                `json:"appliedVersion"`
	PayloadDigest   string               `json:"payloadDigest"`
	Members         []MomentThreadMember `json:"members"`
	CommittedAt     *time.Time           `json:"committedAt"`
}

type MomentThreadPosition struct {
	Position int     `json:"position"`
	MomentID *string `json:"momentId"`
	Revision int64   `json:"revision"`
	Status   string  `json:"status"`
}

// Thread reads expose current selectors, never a cache of withdrawn child text.
type MomentThreadView struct {
	SchemaVersion string                     `json:"schemaVersion"`
	ThreadID      string                     `json:"threadId"`
	RootMomentID  *string                    `json:"rootMomentId"`
	Audience      string                     `json:"audience"`
	Version       int64                      `json:"version"`
	Members       []MomentThreadPosition     `json:"members"`
	CanWithdraw   bool                       `json:"canWithdraw"`
	SourceVersion string                     `json:"sourceVersion"`
	ObservedAt    time.Time                  `json:"observedAt"`
	ValidUntil    time.Time                  `json:"validUntil"`
	ResponseProof identity.HumanControlProof `json:"-"`
}

type MomentThreadStore interface {
	PreviewMomentThread(context.Context, [32]byte, identity.Actor, MomentThreadInput) (MomentThreadPreview, error)
	CommitMomentThread(context.Context, [32]byte, identity.Actor, MomentThreadInput) (MomentThreadReceipt, error)
	ReadMomentThreadOperation(context.Context, [32]byte, identity.Actor, string, bool) (MomentThreadReceipt, error)
	ReadMomentThread(context.Context, PublicMomentAccess, string, bool) (MomentThreadView, error)
}

func MomentThreadDigest(in MomentThreadInput) string {
	items := []any{}
	for _, item := range in.Items {
		media := []any{}
		for _, attachment := range item.Media {
			media = append(media, []any{attachment.MediaID, attachment.MediaVersion, attachment.Rights.Digest()})
		}
		items = append(items, []any{MomentControlInputHash(&item.Input), media})
	}
	return MomentControlHash([]any{"moment-thread-v1", in.OperationID, in.Action, in.ThreadID, in.LocalScope, in.Audience, in.ExpectedVersion, items})
}

func NormalizeMomentThread(in MomentThreadInput, commit bool) (MomentThreadInput, error) {
	bad := func() (MomentThreadInput, error) { return MomentThreadInput{}, identity.ErrControlInvalid }
	if !identity.HumanControlUUID(in.OperationID) || in.Audience != "PUBLIC" || in.Items == nil || in.ExpectedVersion < 0 || in.ExpectedVersion >= MomentControlMaxRevision {
		return bad()
	}
	switch in.Action {
	case "CREATE_PUBLISH":
		if in.ThreadID != "" || in.ExpectedVersion != 0 || !identity.HumanControlUUID(in.LocalScope) || len(in.Items) < 2 || len(in.Items) > MomentThreadMaxEntries {
			return bad()
		}
		seen := map[string]bool{}
		for i := range in.Items {
			item := &in.Items[i]
			control := MomentControlInput{OperationID: in.OperationID, Action: "PUBLISH", MomentID: in.OperationID, ExpectedRevision: 1, Input: &item.Input}
			if len(item.Media) > 0 {
				control.Media, control.LocalScope = &item.Media, in.LocalScope
			}
			v, e := NormalizeMomentControl(control, false)
			if e != nil || item.Media == nil || len(item.Media) > momentmedia.MaxImages {
				return bad()
			}
			item.Input = *v.Input
			for _, a := range item.Media {
				// Current expiry belongs to the native source capture. Structural
				// decoding must still permit recovery of an original successful fact.
				if !momentmedia.ID(a.MediaID) || seen[a.MediaID] || a.MediaVersion < 1 || a.MediaVersion > momentmedia.MaxVersion || !a.Rights.Valid(time.Time{}) {
					return bad()
				}
				seen[a.MediaID] = true
			}
		}
	case "WITHDRAW":
		if !identity.HumanControlUUID(in.ThreadID) || in.LocalScope != "" || in.ExpectedVersion < 1 || len(in.Items) != 0 {
			return bad()
		}
	default:
		return bad()
	}
	if commit {
		if !MomentControlHex(in.SourceVersion) || len(in.Approval) < 1 || len(in.Approval) > 8192 || !utf8.ValidString(in.Approval) || bytes.IndexByte([]byte(in.Approval), 0) >= 0 {
			return bad()
		}
	} else if in.SourceVersion != "" || in.Approval != "" {
		return bad()
	}
	return in, nil
}

func threadClosed(raw []byte, required, optional []string) (map[string]json.RawMessage, error) {
	if len(raw) > 256*1024 || !utf8.Valid(raw) {
		return nil, identity.ErrControlInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return nil, identity.ErrControlInvalid
	}
	allowed := map[string]bool{}
	for _, key := range append(append([]string{}, required...), optional...) {
		allowed[key] = true
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		t, e = d.Token()
		key, ok := t.(string)
		if e != nil || !ok || !allowed[key] || out[key] != nil {
			return nil, identity.ErrControlInvalid
		}
		var v json.RawMessage
		if d.Decode(&v) != nil {
			return nil, identity.ErrControlInvalid
		}
		out[key] = v
	}
	if t, e = d.Token(); e != nil || t != json.Delim('}') {
		return nil, identity.ErrControlInvalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, identity.ErrControlInvalid
	}
	for _, key := range required {
		if out[key] == nil || bytes.Equal(out[key], []byte("null")) {
			return nil, identity.ErrControlInvalid
		}
	}
	return out, nil
}

func DecodeMomentThread(raw []byte, commit bool) (MomentThreadInput, error) {
	var in MomentThreadInput
	required := []string{"operationId", "action", "threadId", "localScope", "audience", "expectedVersion", "items"}
	if commit {
		required = append(required, "sourceVersion", "approval")
	}
	m, e := threadClosed(raw, required, nil)
	if e != nil || json.Unmarshal(raw, &in) != nil {
		return in, identity.ErrControlInvalid
	}
	var items []json.RawMessage
	if json.Unmarshal(m["items"], &items) != nil {
		return in, identity.ErrControlInvalid
	}
	for _, item := range items {
		v, e := threadClosed(item, []string{"input", "media"}, nil)
		if e != nil {
			return in, e
		}
		if _, e = threadClosed(v["input"], []string{"cityId", "placeId", "title", "body", "timePrecision", "locationPrecision"}, []string{"occurredAt"}); e != nil {
			return in, e
		}
		var media []json.RawMessage
		if bytes.Equal(v["media"], []byte("null")) || json.Unmarshal(v["media"], &media) != nil {
			return in, identity.ErrControlInvalid
		}
		for _, image := range media {
			a, e := threadClosed(image, []string{"mediaId", "mediaVersion", "rights"}, nil)
			if e != nil {
				return in, e
			}
			if _, e = threadClosed(a["rights"], []string{"basis", "attribution", "licenseReference", "retentionUntil", "canRetain", "canPublish"}, nil); e != nil {
				return in, e
			}
		}
	}
	return NormalizeMomentThread(in, commit)
}

func ValidMomentThreadReceipt(r MomentThreadReceipt, op string) bool {
	if r.SchemaVersion != "moment-thread-operation-v1" || !identity.HumanControlUUID(op) || r.OperationID != op || r.Members == nil {
		return false
	}
	if r.Status == "STOPPED" {
		return r.Action == "" && r.ThreadID == "" && r.RootMomentID == "" && r.Audience == "" && r.ExpectedVersion == 0 && r.AppliedVersion == 0 && r.PayloadDigest == "" && len(r.Members) == 0 && r.CommittedAt == nil
	}
	if r.Status != "SUCCEEDED" || !identity.HumanControlUUID(r.ThreadID) || !identity.HumanControlUUID(r.RootMomentID) || r.Audience != "PUBLIC" || !MomentControlHex(r.PayloadDigest) || r.CommittedAt == nil || r.CommittedAt.IsZero() || r.ExpectedVersion < 0 || r.AppliedVersion != r.ExpectedVersion+1 || len(r.Members) < 2 || len(r.Members) > MomentThreadMaxEntries {
		return false
	}
	if r.Action == "CREATE_PUBLISH" {
		if r.ExpectedVersion != 0 {
			return false
		}
	} else if r.Action != "WITHDRAW" || r.ExpectedVersion < 1 {
		return false
	}
	seen := map[string]bool{}
	for i, member := range r.Members {
		if !identity.HumanControlUUID(member.MomentID) || seen[member.MomentID] || member.Position != i || member.AppliedRevision < 2 || member.AppliedRevision > MomentControlMaxRevision || member.Media == nil || len(member.Media) > momentmedia.MaxImages || member.MediaSetVersion < 0 || member.MediaSetVersion > momentmedia.MaxVersion || r.Action == "CREATE_PUBLISH" && member.Status != "published" || r.Action == "WITHDRAW" && member.Status != "withdrawn" {
			return false
		}
		seen[member.MomentID] = true
		if r.Action == "CREATE_PUBLISH" && (member.AppliedRevision != 2 || len(member.Media) == 0 && member.MediaSetVersion != 0 || len(member.Media) > 0 && member.MediaSetVersion != 1) {
			return false
		}
		for _, media := range member.Media {
			if !momentmedia.ID(media.MediaID) || media.Version < 1 || media.Version > momentmedia.MaxVersion {
				return false
			}
		}
	}
	return r.Members[0].MomentID == r.RootMomentID
}
