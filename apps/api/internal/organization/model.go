package organization

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

var ErrForbidden = errors.New("organization workspace forbidden")
var ErrNotFound = errors.New("public organization not found")
var ErrConflict = errors.New("organization membership conflict")
var ErrInvalidTarget = errors.New("organization membership target invalid")

type PublicProfile struct {
	ID                 string                `json:"id"`
	OrganizationType   string                `json:"organizationType"`
	Name               string                `json:"name"`
	Description        string                `json:"description"`
	OfficialLinks      []string              `json:"officialLinks"`
	VerificationStatus string                `json:"verificationStatus"`
	AgentAvailable     bool                  `json:"agentAvailable"`
	UpcomingActivities []foundation.Activity `json:"upcomingActivities"`
}

type Organization struct {
	ID               string   `json:"id"`
	AccountID        string   `json:"accountId"`
	OrganizationType string   `json:"organizationType"`
	Name             string   `json:"name"`
	Role             string   `json:"role"`
	Description      string   `json:"description,omitempty"`
	OfficialLinks    []string `json:"officialLinks,omitempty"`
}

type CreateInput struct {
	OrganizationType string `json:"organizationType"`
	Name             string `json:"name"`
}

type ProfileInput struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	OfficialLinks []string `json:"officialLinks"`
}

type Membership struct {
	ID               string `json:"id"`
	OrganizationID   string `json:"organizationId"`
	UserAccountID    string `json:"userAccountId"`
	DisplayName      string `json:"displayName"`
	Role             string `json:"role"`
	Status           string `json:"status"`
	OrganizationName string `json:"organizationName,omitempty"`
}

type MembershipStore interface {
	ListMembers(context.Context, string, string) ([]Membership, error)
	ListInvitations(context.Context, string) ([]Membership, error)
	InviteMember(context.Context, string, string, string, string) (Membership, error)
	AcceptInvitation(context.Context, string, string) (Membership, error)
	ChangeMemberRole(context.Context, string, string, string, string) (Membership, error)
	RevokeMember(context.Context, string, string, string) error
}

// PublicMapPin contains only an organization-owned, independently reviewed point.
type PublicMapPin struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Latitude         float64 `json:"latitude"`
	Longitude        float64 `json:"longitude"`
	CoordinateSystem string  `json:"coordinateSystem"`
	Precision        string  `json:"precision"`
}

type MapLocation struct {
	OrganizationID string  `json:"organizationId"`
	CityID         string  `json:"cityId"`
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	Visibility     string  `json:"visibility"`
	ReviewStatus   string  `json:"reviewStatus"`
	Revision       int64   `json:"revision"`
}

type MapLocationStore interface {
	ListPublicMapPins(context.Context, string) ([]PublicMapPin, error)
	GetMapLocation(context.Context, string, string) (MapLocation, error)
	SubmitMapLocation(context.Context, string, string, string, float64, float64) (MapLocation, error)
	HideMapLocation(context.Context, string, string) error
	ReviewMapLocation(context.Context, string, string, string, string) error
}

type Store interface {
	GetPublicProfile(context.Context, string, string) (PublicProfile, error)
	CreateOrganization(context.Context, string, CreateInput) (Organization, error)
	ListOrganizations(context.Context, string) ([]Organization, error)
	ResolveWorkspace(context.Context, string, string) (string, string, error)
	UpdateProfile(context.Context, string, string, ProfileInput) (Organization, error)
}
