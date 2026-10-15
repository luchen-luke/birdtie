package content

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/momentmedia"
)

const MomentThreadMaxEntries = 20

type MomentThreadItem struct {
	MomentID         string                        `json:"momentId,omitempty"`
	ExpectedRevision int64                         `json:"expectedRevision,omitempty"`
	Restore          *bool                         `json:"restore,omitempty"`
	Input            MomentInput                   `json:"input"`
	Media            []momentmedia.AttachmentInput `json:"media"`
}

type MomentThreadRemoval struct {
	MomentID         string `json:"momentId"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

type MomentThreadInput struct {
	OperationID     string                 `json:"operationId"`
	Action          string                 `json:"action"`
	ThreadID        string                 `json:"threadId"`
	LocalScope      string                 `json:"localScope"`
	Audience        string                 `json:"audience"`
	ExpectedVersion int64                  `json:"expectedVersion"`
	Items           []MomentThreadItem     `json:"items"`
	BaseMomentID    *string                `json:"baseMomentId,omitempty"`
	RemovedItems    *[]MomentThreadRemoval `json:"removedItems,omitempty"`
	SourceVersion   string                 `json:"sourceVersion,omitempty"`
	Approval        string                 `json:"approval,omitempty"`
}

type MomentThreadPreview struct {
	Locations [][]MomentLocationResolved `json:"locations"`
	identity.HumanControlPreview
	Input          MomentThreadInput        `json:"input"`
	SourceVersion  string                   `json:"sourceVersion"`
	Members        []MomentThreadMember     `json:"members"`
	Materials      [][]MomentThreadMaterial `json:"materials"`
	Contexts       []MomentThreadContext    `json:"contexts"`
	PreservedMedia *[]*momentmedia.Set      `json:"preservedMedia,omitempty"`
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
	SchemaVersion   string                `json:"schemaVersion"`
	OperationID     string                `json:"operationId"`
	Status          string                `json:"status"`
	Action          string                `json:"action"`
	ThreadID        string                `json:"threadId"`
	RootMomentID    string                `json:"rootMomentId"`
	Audience        string                `json:"audience"`
	ExpectedVersion int64                 `json:"expectedVersion"`
	AppliedVersion  int64                 `json:"appliedVersion"`
	PayloadDigest   string                `json:"payloadDigest"`
	Members         []MomentThreadMember  `json:"members"`
	RemovedMembers  *[]MomentThreadMember `json:"removedMembers,omitempty"`
	CommittedAt     *time.Time            `json:"committedAt"`
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
	if in.Action == "EDIT" {
		items, removed := []any{}, []any{}
		for _, item := range in.Items {
			var media any
			if item.Media != nil {
				m := []any{}
				for _, a := range item.Media {
					m = append(m, []any{a.MediaID, a.MediaVersion, a.Rights.Digest()})
				}
				media = m
			}
			items = append(items, []any{item.MomentID, item.ExpectedRevision, item.Restore != nil && *item.Restore, MomentControlInputHash(&item.Input), media})
		}
		if in.RemovedItems != nil {
			for _, r := range *in.RemovedItems {
				removed = append(removed, []any{r.MomentID, r.ExpectedRevision})
			}
		}
		base := ""
		if in.BaseMomentID != nil {
			base = *in.BaseMomentID
		}
		return MomentControlHash([]any{"moment-thread-edit-v1", in.OperationID, in.Action, in.ThreadID, base, in.LocalScope, in.Audience, in.ExpectedVersion, items, removed})
	}
	items := []any{}
	for _, item := range in.Items {
		media := []any{}
		for _, attachment := range item.Media {
			media = append(media, []any{attachment.MediaID, attachment.MediaVersion, attachment.Rights.Digest()})
		}
		items = append(items, []any{MomentControlInputHash(&item.Input), media})
	}
	if in.Action == "PUBLISH" {
		items = []any{}
		for _, item := range in.Items {
			media := []any{}
			for _, a := range item.Media {
				media = append(media, []any{a.MediaID, a.MediaVersion, a.Rights.Digest()})
			}
			items = append(items, []any{item.MomentID, item.ExpectedRevision, MomentControlInputHash(&item.Input), media})
		}
		return MomentControlHash([]any{"moment-thread-existing-v2", in.OperationID, in.Action, in.ThreadID, in.LocalScope, in.Audience, in.ExpectedVersion, items})
	}
	return MomentControlHash([]any{"moment-thread-v1", in.OperationID, in.Action, in.ThreadID, in.LocalScope, in.Audience, in.ExpectedVersion, items})
}

func NormalizeMomentThread(in MomentThreadInput, commit bool) (MomentThreadInput, error) {
	bad := func() (MomentThreadInput, error) { return MomentThreadInput{}, identity.ErrControlInvalid }
	if !identity.HumanControlUUID(in.OperationID) || in.Audience != "PUBLIC" || in.Items == nil || in.ExpectedVersion < 0 || in.ExpectedVersion >= MomentControlMaxRevision {
		return bad()
	}
	switch in.Action {
	case "EDIT":
		var e error
		in, e = normalizeMomentThreadEdit(in)
		if e != nil {
			return bad()
		}
	case "CREATE_PUBLISH", "PUBLISH":
		if !identity.HumanControlUUID(in.LocalScope) || len(in.Items) < 2 || len(in.Items) > MomentThreadMaxEntries || (in.Action == "CREATE_PUBLISH" && (in.ThreadID != "" || in.ExpectedVersion != 0)) || (in.Action == "PUBLISH" && (!identity.HumanControlUUID(in.ThreadID) || in.ExpectedVersion < 1)) {
			return bad()
		}
		seen := map[string]bool{}
		locationIDs := map[string]bool{}
		for i := range in.Items {
			item := &in.Items[i]
			if in.Action == "CREATE_PUBLISH" && (item.MomentID != "" || item.ExpectedRevision != 0) || in.Action == "PUBLISH" && (!identity.HumanControlUUID(item.MomentID) || item.ExpectedRevision < 1 || item.ExpectedRevision >= MomentControlMaxRevision) {
				return bad()
			}
			control := MomentControlInput{OperationID: in.OperationID, Action: "PUBLISH", MomentID: in.OperationID, ExpectedRevision: 1, Input: &item.Input}
			if len(item.Media) > 0 {
				control.Media, control.LocalScope = &item.Media, in.LocalScope
			}
			v, e := NormalizeMomentControl(control, false)
			if e != nil || item.Media == nil || len(item.Media) > momentmedia.MaxImages {
				return bad()
			}
			item.Input = *v.Input
			if item.Input.Locations != nil {
				for _, location := range *item.Input.Locations {
					if locationIDs[location.ID] {
						return bad()
					}
					locationIDs[location.ID] = true
				}
			}
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
	if in.Action != "EDIT" {
		if in.BaseMomentID != nil || in.RemovedItems != nil {
			return bad()
		}
		for _, item := range in.Items {
			if item.Restore != nil {
				return bad()
			}
		}
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
	if len(raw) > 1024*1024 || !utf8.Valid(raw) {
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
	if json.Unmarshal(raw, &in) != nil {
		return in, identity.ErrControlInvalid
	}
	required := []string{"operationId", "action", "threadId", "localScope", "audience", "expectedVersion", "items"}
	if in.Action == "EDIT" {
		required = append(required, "baseMomentId", "removedItems")
	}
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
		required := []string{"input", "media"}
		var optional []string
		if in.Action == "EDIT" {
			required = []string{"input", "momentId", "expectedRevision", "restore"}
			optional = []string{"media"}
		}
		if in.Action == "PUBLISH" {
			required = append(required, "momentId", "expectedRevision")
		}
		v, e := threadClosed(item, required, optional)
		if e != nil {
			return in, e
		}
		if v["media"] == nil {
			return in, identity.ErrControlInvalid
		}
		inputOptional := []string{"occurredAt", "locations"}
		if in.Action == "EDIT" {
			inputOptional = append(inputOptional, "activityId", "communityId", "organizationId")
		}
		im, ie := threadClosed(v["input"], []string{"cityId", "placeId", "title", "body", "timePrecision", "locationPrecision"}, inputOptional)
		e = ie
		if e != nil {
			return in, e
		}
		if loc, ok := im["locations"]; ok {
			if e = DecodeMomentLocations(loc); e != nil {
				return in, e
			}
		}
		var media []json.RawMessage
		if bytes.Equal(v["media"], []byte("null")) && in.Action != "EDIT" || json.Unmarshal(v["media"], &media) != nil {
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
	if in.Action == "EDIT" {
		var removed []json.RawMessage
		if json.Unmarshal(m["removedItems"], &removed) != nil {
			return in, identity.ErrControlInvalid
		}
		for _, r := range removed {
			if _, e := threadClosed(r, []string{"momentId", "expectedRevision"}, nil); e != nil {
				return in, e
			}
		}
	}
	return NormalizeMomentThread(in, commit)
}

func ValidMomentThreadReceipt(r MomentThreadReceipt, op string) bool {
	if r.SchemaVersion == "moment-thread-operation-v2" {
		return validMomentThreadEditReceipt(r, op)
	}
	if r.RemovedMembers != nil {
		return false
	}
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
	} else if (r.Action != "WITHDRAW" && r.Action != "PUBLISH") || r.ExpectedVersion < 1 {
		return false
	}
	seen := map[string]bool{}
	for i, member := range r.Members {
		if !identity.HumanControlUUID(member.MomentID) || seen[member.MomentID] || member.Position != i || member.AppliedRevision < 2 || member.AppliedRevision > MomentControlMaxRevision || member.Media == nil || len(member.Media) > momentmedia.MaxImages || member.MediaSetVersion < 0 || member.MediaSetVersion > momentmedia.MaxVersion || (r.Action == "CREATE_PUBLISH" || r.Action == "PUBLISH") && member.Status != "published" || r.Action == "WITHDRAW" && member.Status != "withdrawn" {
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

// Owned sidecar retains original child identity and private travel metadata.
// It never contains a cache of child text or grants public geography.
type OwnedThreadEntryContext struct {
	Position                 int    `json:"position"`
	MomentID                 string `json:"momentId"`
	InboundOrigin            string `json:"inboundOrigin"`
	InboundMode              string `json:"inboundMode"`
	DurationMinutes          *int64 `json:"durationMinutes"`
	EstimatedDurationMinutes *int64 `json:"estimatedDurationMinutes"`
}
type OwnedThreadLeg struct {
	ID              string     `json:"id"`
	FromMomentID    string     `json:"fromMomentId"`
	ToMomentID      string     `json:"toMomentId"`
	FromPosition    int        `json:"fromPosition"`
	ToPosition      int        `json:"toPosition"`
	Mode            string     `json:"mode"`
	FromPlaceID     string     `json:"fromPlaceId"`
	ToPlaceID       string     `json:"toPlaceId"`
	SourceCreatedAt *time.Time `json:"sourceCreatedAt"`
	SourceUpdatedAt *time.Time `json:"sourceUpdatedAt"`
}
type OwnedMomentThreadView struct {
	SchemaVersion  string                     `json:"schemaVersion"`
	ThreadID       string                     `json:"threadId"`
	RootMomentID   *string                    `json:"rootMomentId"`
	Audience       string                     `json:"audience"`
	Status         string                     `json:"status"`
	Version        int64                      `json:"version"`
	Members        []MomentThreadPosition     `json:"members"`
	RetiredMembers *[]MomentThreadPosition    `json:"retiredMembers,omitempty"`
	EntryContexts  []OwnedThreadEntryContext  `json:"entryContexts"`
	Legs           []OwnedThreadLeg           `json:"legs"`
	SourceVersion  string                     `json:"sourceVersion"`
	ObservedAt     time.Time                  `json:"observedAt"`
	ValidUntil     time.Time                  `json:"validUntil"`
	ResponseProof  identity.HumanControlProof `json:"-"`
}
type OwnedMomentThreadStore interface {
	ReadOwnedMomentThread(context.Context, [32]byte, identity.Actor, string, bool) (OwnedMomentThreadView, error)
}

// EDIT distinguishes an explicit empty selector from the old actions' omitted
// selectors without changing the encoding of any historical request.
func (i MomentThreadItem) MarshalJSON() ([]byte, error) {
	type original MomentThreadItem
	if i.Restore == nil {
		return json.Marshal(original(i))
	}
	return json.Marshal(struct {
		MomentID         string                        `json:"momentId"`
		ExpectedRevision int64                         `json:"expectedRevision"`
		Restore          bool                          `json:"restore"`
		Input            MomentInput                   `json:"input"`
		Media            []momentmedia.AttachmentInput `json:"media"`
	}{i.MomentID, i.ExpectedRevision, *i.Restore, i.Input, i.Media})
}

func normalizeMomentThreadEdit(in MomentThreadInput) (MomentThreadInput, error) {
	bad := func() (MomentThreadInput, error) { return MomentThreadInput{}, identity.ErrControlInvalid }
	if in.BaseMomentID == nil || in.RemovedItems == nil || *in.RemovedItems == nil || len(*in.RemovedItems) > MomentThreadMaxEntries || !identity.HumanControlUUID(in.LocalScope) || len(in.Items) < 1 || len(in.Items) > MomentThreadMaxEntries {
		return bad()
	}
	if in.ThreadID == "" {
		if in.ExpectedVersion != 0 || *in.BaseMomentID != "" && !identity.HumanControlUUID(*in.BaseMomentID) || len(*in.RemovedItems) != 0 {
			return bad()
		}
	} else if !identity.HumanControlUUID(in.ThreadID) || in.ExpectedVersion < 1 || *in.BaseMomentID != "" {
		return bad()
	}
	seen, locations, mediaIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for n := range in.Items {
		i := &in.Items[n]
		if i.Restore == nil {
			return bad()
		}
		if i.MomentID == "" {
			if i.ExpectedRevision != 0 || *i.Restore || i.Media == nil {
				return bad()
			}
		} else {
			if !identity.HumanControlUUID(i.MomentID) || i.ExpectedRevision < 1 || i.ExpectedRevision >= MomentControlMaxRevision || seen[i.MomentID] || *i.Restore && i.Media == nil {
				return bad()
			}
			if in.ThreadID == "" && i.MomentID != *in.BaseMomentID {
				return bad()
			}
			seen[i.MomentID] = true
		}
		// EDIT retains independently authorized original relationships without
		// imposing the legacy private writer's City requirement on geography.
		contextInput := i.Input
		for _, slot := range []**string{&contextInput.ActivityID, &contextInput.CommunityID, &contextInput.OrganizationID} {
			if *slot != nil {
				value := strings.ToLower(strings.TrimSpace(**slot))
				if value != "" && !identity.HumanControlUUID(value) {
					return bad()
				}
				*slot = &value
			}
		}
		publicInput := contextInput
		publicInput.ActivityID, publicInput.CommunityID, publicInput.OrganizationID = nil, nil, nil
		control := MomentControlInput{OperationID: in.OperationID, Action: "UPDATE_PUBLIC", MomentID: in.OperationID, ExpectedRevision: 1, Input: &publicInput}
		if i.Media != nil {
			control.Media = &i.Media
			if len(i.Media) > 0 {
				control.LocalScope = in.LocalScope
			}
		}
		v, e := NormalizeMomentControl(control, false)
		if e != nil {
			return bad()
		}
		i.Input = *v.Input
		i.Input.ActivityID, i.Input.CommunityID, i.Input.OrganizationID = contextInput.ActivityID, contextInput.CommunityID, contextInput.OrganizationID
		if i.Input.Locations != nil {
			for _, l := range *i.Input.Locations {
				if locations[l.ID] {
					return bad()
				}
				locations[l.ID] = true
			}
		}
		for _, a := range i.Media {
			if mediaIDs[a.MediaID] {
				return bad()
			}
			mediaIDs[a.MediaID] = true
		}
	}
	if *in.BaseMomentID != "" && !seen[*in.BaseMomentID] {
		return bad()
	}
	for _, r := range *in.RemovedItems {
		if !identity.HumanControlUUID(r.MomentID) || r.ExpectedRevision < 1 || r.ExpectedRevision >= MomentControlMaxRevision || seen[r.MomentID] {
			return bad()
		}
		seen[r.MomentID] = true
	}
	return in, nil
}

func validMomentThreadEditReceipt(r MomentThreadReceipt, op string) bool {
	if !identity.HumanControlUUID(op) || r.OperationID != op || r.Status != "SUCCEEDED" || (r.Action != "EDIT" && r.Action != "WITHDRAW") || !identity.HumanControlUUID(r.ThreadID) || !identity.HumanControlUUID(r.RootMomentID) || r.Audience != "PUBLIC" || r.ExpectedVersion < 0 || r.AppliedVersion != r.ExpectedVersion+1 || r.AppliedVersion > MomentControlMaxRevision || !MomentControlHex(r.PayloadDigest) || r.CommittedAt == nil || r.CommittedAt.IsZero() || r.Members == nil || len(r.Members) < 1 || len(r.Members) > MomentThreadMaxEntries || r.RemovedMembers == nil || *r.RemovedMembers == nil {
		return false
	}
	if len(*r.RemovedMembers) > MomentThreadMaxEntries || r.Action == "WITHDRAW" && (r.ExpectedVersion < 1 || len(*r.RemovedMembers) != 0) {
		return false
	}
	seen := map[string]bool{}
	check := func(m MomentThreadMember, status string) bool {
		if !identity.HumanControlUUID(m.MomentID) || seen[m.MomentID] || m.Position < 0 || m.AppliedRevision < 2 || m.AppliedRevision > MomentControlMaxRevision || m.Status != status || m.Media == nil || len(m.Media) > momentmedia.MaxImages || m.MediaSetVersion < 0 || m.MediaSetVersion > momentmedia.MaxVersion {
			return false
		}
		seen[m.MomentID] = true
		for _, a := range m.Media {
			if !momentmedia.ID(a.MediaID) || a.Version < 1 || a.Version > momentmedia.MaxVersion {
				return false
			}
		}
		return true
	}
	status := "published"
	if r.Action == "WITHDRAW" {
		status = "withdrawn"
	}
	for n, m := range r.Members {
		if m.Position != n || !check(m, status) {
			return false
		}
	}
	for _, m := range *r.RemovedMembers {
		if !check(m, "withdrawn") {
			return false
		}
	}
	return true
}
