package community

import (
	"context"
	"errors"
	"time"
)

var ErrValidation = errors.New("community validation error")

type SocialInput struct {
	Name       string `json:"name"`
	Summary    string `json:"description"`
	AvatarURL  string `json:"avatarUrl"`
	CityID     string `json:"cityId"`
	Visibility string `json:"visibility"`
	JoinPolicy string `json:"joinPolicy"`
}

type SocialRecord struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	AvatarURL   *string   `json:"avatarUrl,omitempty"`
	CityID      *string   `json:"cityId,omitempty"`
	Visibility  string    `json:"visibility"`
	JoinPolicy  string    `json:"joinPolicy"`
	Status      string    `json:"status"`
	CreatedBy   string    `json:"createdByUserId"`
	MemberCount int       `json:"memberCount"`
	MyRole      *string   `json:"myRole,omitempty"`
	MyStatus    *string   `json:"myStatus,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Membership struct {
	ID            string    `json:"id"`
	CommunityID   string    `json:"communityId"`
	UserAccountID string    `json:"userAccountId"`
	DisplayName   string    `json:"displayName,omitempty"`
	Role          string    `json:"role"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type SocialStore interface {
	CreateSocialCommunity(context.Context, string, SocialInput) (SocialRecord, error)
	ListSocialCommunities(context.Context, string, string, bool) ([]SocialRecord, error)
	GetSocialCommunity(context.Context, string, string) (SocialRecord, error)
	UpdateSocialCommunity(context.Context, string, string, SocialInput) (SocialRecord, error)
	ArchiveSocialCommunity(context.Context, string, string) error
	JoinSocialCommunity(context.Context, string, string) (Membership, error)
	LeaveSocialCommunity(context.Context, string, string) error
	ListSocialMembers(context.Context, string, string, bool) ([]Membership, error)
	DecideSocialRequest(context.Context, string, string, string, bool) (Membership, error)
	InviteSocialMember(context.Context, string, string, string) (Membership, error)
	ChangeSocialMemberRole(context.Context, string, string, string, string) (Membership, error)
	RemoveSocialMember(context.Context, string, string, string) error
	TransferSocialOwner(context.Context, string, string, string) error
}
