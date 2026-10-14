package organization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrControlInvalid = errors.New("invalid organization control")
var ErrControlChanged = errors.New("organization control changed")
var ErrControlUnavailable = errors.New("organization control unavailable")

const MaxControlVersion int64 = 9007199254740991

var controlUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func NormalizeMemberDisplayName(value string) string {
	p, e := identity.NormalizeHumanProfileInput(identity.ProfileInput{DisplayName: value, Visibility: "public"})
	if e != nil {
		return ""
	}
	return p.DisplayName
}

func ControlID(s string) bool { return controlUUID.MatchString(s) }

type HumanAccess struct {
	AccountID, AccountType string
	SessionDigest          [32]byte
}
type ControlInput struct {
	Action                    string  `json:"action"`
	OperationID               string  `json:"operationId"`
	ExpectedAuthorityVersion  int64   `json:"expectedAuthorityVersion"`
	UserAccountID             string  `json:"userAccountId,omitempty"`
	MembershipID              string  `json:"membershipId,omitempty"`
	Role                      string  `json:"role,omitempty"`
	ExpectedMemberVersion     int64   `json:"expectedMemberVersion,omitempty"`
	CityID                    string  `json:"cityId,omitempty"`
	Latitude                  float64 `json:"latitude,omitempty"`
	Longitude                 float64 `json:"longitude,omitempty"`
	ProposalID                string  `json:"proposalId,omitempty"`
	ExpectedProposalVersion   int64   `json:"expectedProposalVersion,omitempty"`
	ExpectedCanonicalRevision int64   `json:"expectedCanonicalRevision,omitempty"`
}

func NormalizeControl(in ControlInput) (ControlInput, error) {
	if !ControlID(in.OperationID) || in.ExpectedAuthorityVersion < 1 || in.ExpectedAuthorityVersion > MaxControlVersion {
		return in, ErrControlInvalid
	}
	for _, n := range []int64{in.ExpectedMemberVersion, in.ExpectedProposalVersion, in.ExpectedCanonicalRevision} {
		if n < 0 || n > MaxControlVersion {
			return in, ErrControlInvalid
		}
	}
	member := in.Action == "INVITE" || in.Action == "ACCEPT" || in.Action == "ROLE" || in.Action == "REVOKE"
	if member {
		if in.CityID != "" || in.Latitude != 0 || in.Longitude != 0 || in.ProposalID != "" || in.ExpectedProposalVersion != 0 || in.ExpectedCanonicalRevision != 0 {
			return in, ErrControlInvalid
		}
		if in.Action == "INVITE" {
			if !ControlID(in.UserAccountID) || in.MembershipID != "" || !inviteRole(in.Role) {
				return in, ErrControlInvalid
			}
		} else {
			if !ControlID(in.MembershipID) || in.ExpectedMemberVersion < 1 || in.UserAccountID != "" {
				return in, ErrControlInvalid
			}
			if in.Action == "ROLE" {
				if !memberRole(in.Role) {
					return in, ErrControlInvalid
				}
			} else if in.Role != "" {
				return in, ErrControlInvalid
			}
		}
	} else {
		if in.UserAccountID != "" || in.MembershipID != "" || in.Role != "" || in.ExpectedMemberVersion != 0 {
			return in, ErrControlInvalid
		}
		switch in.Action {
		case "PROPOSE":
			if !utf8.ValidString(in.CityID) || len(in.CityID) < 1 || len(in.CityID) > 160 || strings.TrimSpace(in.CityID) != in.CityID || strings.ContainsAny(in.CityID, "\x00\r\n") || math.IsNaN(in.Latitude) || math.IsInf(in.Latitude, 0) || math.IsNaN(in.Longitude) || math.IsInf(in.Longitude, 0) || in.Latitude < -90 || in.Latitude > 90 || in.Longitude < -180 || in.Longitude > 180 || in.ProposalID != "" || in.ExpectedProposalVersion != 0 {
				return in, ErrControlInvalid
			}
		case "APPROVE_POINT", "REJECT_POINT", "WITHDRAW_POINT":
			if !ControlID(in.ProposalID) || in.ExpectedProposalVersion < 1 || in.CityID != "" || in.Latitude != 0 || in.Longitude != 0 || in.Action == "WITHDRAW_POINT" && in.ExpectedCanonicalRevision != 0 {
				return in, ErrControlInvalid
			}
		case "HIDE_POINT":
			if in.ExpectedCanonicalRevision < 1 || in.ProposalID != "" || in.ExpectedProposalVersion != 0 || in.CityID != "" || in.Latitude != 0 || in.Longitude != 0 {
				return in, ErrControlInvalid
			}
		default:
			return in, ErrControlInvalid
		}
	}
	if in.Latitude == 0 {
		in.Latitude = 0
	}
	if in.Longitude == 0 {
		in.Longitude = 0
	}
	return in, nil
}
func inviteRole(s string) bool { return s == "member" || s == "moderator" || s == "admin" }
func memberRole(s string) bool { return inviteRole(s) || s == "owner" }

// Full tuple rather than an omitted-field JSON object. IEEE754 coordinates
// keep exact approved input without language-specific decimal formatting.
func ControlDigest(org string, in ControlInput) string {
	b, _ := json.Marshal([]any{"ORG_HUMAN_139", org, in.Action, in.OperationID, in.ExpectedAuthorityVersion, in.UserAccountID, in.MembershipID, in.Role, in.ExpectedMemberVersion, in.CityID, fmt.Sprintf("%016x", math.Float64bits(in.Latitude)), fmt.Sprintf("%016x", math.Float64bits(in.Longitude)), in.ProposalID, in.ExpectedProposalVersion, in.ExpectedCanonicalRevision})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type ControlPreview struct {
	OrganizationID    string    `json:"organizationId"`
	OperationID       string    `json:"operationId"`
	Action            string    `json:"action"`
	Approval          string    `json:"approval"`
	ExpiresAt         time.Time `json:"expiresAt"`
	InputSHA256       string    `json:"inputSha256"`
	AuthorityVersion  int64     `json:"authorityVersion"`
	MemberVersion     int64     `json:"memberVersion"`
	CanonicalRevision int64     `json:"canonicalRevision"`
	ProposalVersion   int64     `json:"proposalVersion"`
	Proof             string    `json:"-"`
}
type ControlReceipt struct {
	OrganizationID    string `json:"organizationId"`
	OperationID       string `json:"operationId"`
	Action            string `json:"action"`
	Status            string `json:"status"`
	AuthorityVersion  int64  `json:"authorityVersion"`
	MemberVersion     int64  `json:"memberVersion"`
	CanonicalRevision int64  `json:"canonicalRevision"`
	ProposalVersion   int64  `json:"proposalVersion"`
	MembershipID      string `json:"membershipId,omitempty"`
	ProposalID        string `json:"proposalId,omitempty"`
	Proof             string `json:"-"`
}
type MembersControl struct {
	OrganizationID   string       `json:"organizationId"`
	AuthorityVersion int64        `json:"authorityVersion"`
	Role             string       `json:"role"`
	Members          []Membership `json:"members"`
	ObservedAt       time.Time    `json:"observedAt"`
	ExpiresAt        time.Time    `json:"expiresAt"`
	Proof            string       `json:"-"`
}
type HumanControlStore interface {
	ReadInvitationsControl(context.Context, HumanAccess) (InvitationsControl, error)
	ReadMembersControl(context.Context, HumanAccess, string) (MembersControl, error)
	ReadMapControl(context.Context, HumanAccess, string) (MapControl, error)
	PreviewOrganizationControl(context.Context, HumanAccess, string, ControlInput) (ControlPreview, error)
	ApplyOrganizationControl(context.Context, HumanAccess, string, ControlInput, string) (ControlReceipt, error)
	ReadOrganizationControlOperation(context.Context, HumanAccess, string, string) (ControlReceipt, error)
	StopOrganizationControlOperation(context.Context, HumanAccess, string, string, int64) (ControlReceipt, error)
	ValidateOrganizationControlResponse(context.Context, HumanAccess, string, string, time.Time) error
	CaptureOrganizationControlSession(context.Context, HumanAccess) (string, error)
	ValidateOrganizationControlSession(context.Context, HumanAccess, string) error
}
type InvitationsControl struct {
	Items        []Membership
	Proofs       map[string]string
	ExpiresAt    time.Time
	SessionProof string
}
