package activeintent

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"regexp"
	"sort"
	"strings"
	"time"
)

const Schema = "active-social-intents-v1"
const PreviewTTL = 90 * time.Second

var ErrInvalid = errors.New("invalid active intent")
var ErrDenied = errors.New("active intent denied")
var ErrNotFound = errors.New("active intent not found")
var ErrConflict = errors.New("active intent version conflict")
var ErrUnavailable = errors.New("active intent unavailable")
var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var version = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Access struct {
	Actor         identity.Actor
	SessionDigest [32]byte
}
type Item struct {
	Intent          socialintent.Record `json:"intent"`
	Version         string              `json:"version"`
	SourceAvailable bool                `json:"sourceAvailable"`
	LocationLabel   string              `json:"locationLabel"`
	AudienceLabel   string              `json:"audienceLabel"`
}
type Envelope struct {
	SchemaVersion string                `json:"schemaVersion"`
	Owner         actorref.PrincipalRef `json:"owner"`
	AgentID       string                `json:"agentId"`
	ObservedAt    time.Time             `json:"observedAt"`
	ModelAccess   bool                  `json:"modelAccess"`
	SendAllowed   bool                  `json:"sendAllowed"`
}
type List struct {
	Envelope
	Items     []Item `json:"items"`
	Limit     int    `json:"limit"`
	Truncated bool   `json:"truncated"`
}
type Option struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type Options struct {
	Envelope
	Cities      []Option `json:"cities"`
	Places      []Option `json:"places"`
	Communities []Option `json:"communities"`
	Invitees    []Option `json:"invitees"`
	Limit       int      `json:"limit"`
	Truncated   bool     `json:"truncated"`
}
type Detail struct {
	Envelope
	Item Item `json:"item"`
}
type Input struct {
	Operation       string                   `json:"operation"`
	ExpectedVersion string                   `json:"expectedVersion"`
	Edit            *socialintent.DraftInput `json:"edit,omitempty"`
}
type Preview struct {
	Envelope
	PreviewID   string    `json:"previewId"`
	Operation   string    `json:"operation"`
	Before      Item      `json:"before"`
	After       Item      `json:"after"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Explanation string    `json:"explanation"`
}
type Receipt struct {
	Envelope
	Item        Item   `json:"item"`
	Operation   string `json:"operation"`
	Committed   bool   `json:"committed"`
	Explanation string `json:"explanation"`
}
type Gateway interface {
	ListOwn(context.Context, Access) (List, error)
	ReadOwn(context.Context, Access, string) (Detail, error)
	OptionsOwn(context.Context, Access) (Options, error)
	PreviewOwn(context.Context, Access, string, Input) (Preview, error)
	ApproveOwn(context.Context, Access, string, string) (Receipt, error)
}

func ValidAccess(a Access) bool {
	return a.Actor.AccountType == "person" && uuid.MatchString(a.Actor.ID) && a.SessionDigest != ([32]byte{})
}
func UUID(id string) bool { return uuid.MatchString(id) }
func ValidateInput(in Input) error {
	if !version.MatchString(in.ExpectedVersion) {
		return ErrInvalid
	}
	switch in.Operation {
	case "EDIT":
		if in.Edit == nil {
			return ErrInvalid
		}
	case "CANCEL", "ACTIVATE":
		if in.Edit != nil {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	if in.Edit != nil {
		_, e := NormalizeDraft(*in.Edit)
		return e
	}
	return nil
}
func NormalizeDraft(in socialintent.DraftInput) (socialintent.DraftInput, error) {
	in.Title = strings.TrimSpace(in.Title)
	if len([]rune(in.Title)) < 1 || len([]rune(in.Title)) > 160 || in.ExpiresAt.Year() < 2000 || in.ExpiresAt.Year() > 2200 || (in.ContextID != "" && !UUID(in.ContextID)) {
		return in, ErrInvalid
	}
	switch in.Type {
	case "FIND_ACTIVITY", "FIND_COMPANION", "ORGANIZE", "ASK_HELP", "OTHER":
	default:
		return in, ErrInvalid
	}
	switch in.Audience {
	case "PRIVATE", "FRIENDS", "PUBLIC":
		if in.CityID != "" || in.CommunityID != "" || len(in.InviteeIDs) != 0 {
			return in, ErrInvalid
		}
	case "LOCAL":
		if in.CityID == "" || len(in.CityID) > 80 || in.CommunityID != "" || len(in.InviteeIDs) != 0 {
			return in, ErrInvalid
		}
	case "COMMUNITY":
		if !UUID(in.CommunityID) || in.CityID != "" || len(in.InviteeIDs) != 0 {
			return in, ErrInvalid
		}
	case "INVITE_ONLY":
		if len(in.InviteeIDs) < 1 || len(in.InviteeIDs) > 20 || in.CityID != "" || in.CommunityID != "" {
			return in, ErrInvalid
		}
	default:
		return in, ErrInvalid
	}
	seen := map[string]bool{}
	in.InviteeIDs = append([]string(nil), in.InviteeIDs...)
	for _, id := range in.InviteeIDs {
		if !UUID(id) || seen[id] {
			return in, ErrInvalid
		}
		seen[id] = true
	}
	sort.Strings(in.InviteeIDs)
	_, raw, e := socialintent.ParseConstraints(in.Constraints, in.Modality)
	if e != nil {
		return in, ErrInvalid
	}
	in.Constraints = raw
	return in, nil
}
func DraftOf(r socialintent.Record) socialintent.DraftInput {
	cid := ""
	if r.ContextID != nil {
		cid = *r.ContextID
	}
	return socialintent.DraftInput{Type: r.Type, Title: r.Title, Constraints: r.Constraints, Audience: r.Audience, Modality: r.Modality, ContextID: cid, CityID: r.CityID, CommunityID: r.CommunityID, InviteeIDs: append([]string(nil), r.InviteeIDs...), ExpiresAt: r.ExpiresAt}
}
func ValidateEnvelope(v Envelope, owner string) error {
	if v.SchemaVersion != Schema || v.Owner.Type != actorref.Person || v.Owner.ID != owner || !UUID(v.AgentID) || v.ObservedAt.IsZero() || v.ModelAccess || v.SendAllowed {
		return ErrUnavailable
	}
	return nil
}
func ValidateItem(i Item, owner string) error {
	if !UUID(i.Intent.ID) || i.Intent.CreatorID != owner || !version.MatchString(i.Version) || i.LocationLabel == "" || i.AudienceLabel == "" || i.Intent.CreatedAt.IsZero() || i.Intent.UpdatedAt.Before(i.Intent.CreatedAt) {
		return ErrUnavailable
	}
	if _, e := NormalizeDraft(DraftOf(i.Intent)); e != nil {
		return ErrUnavailable
	}
	switch i.Intent.Status {
	case "DRAFT", "ACTIVE", "MATCHED", "CONVERTED", "EXPIRED", "CANCELLED":
	default:
		return ErrUnavailable
	}
	return nil
}
func JSONEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
