package follow

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/communityavatar"
	"github.com/birdtie/birdtie/apps/api/internal/profileavatar"
	"strings"
	"time"
)

var (
	ErrInvalid     = errors.New("invalid Follow target")
	ErrUnavailable = errors.New("Follow target unavailable")
	ErrNotFound    = errors.New("Follow not found")
)

const (
	Person       = "PERSON"
	Organization = "ORGANIZATION"
	Community    = "COMMUNITY"
	Business     = "BUSINESS"
)

type Target struct {
	Type string `json:"targetType"`
	ID   string `json:"targetId"`
}

type Record struct {
	ID              string                    `json:"id"`
	Target          Target                    `json:"target"`
	Label           string                    `json:"label"`
	Avatar          *profileavatar.Metadata   `json:"avatar,omitempty"`
	CommunityAvatar *communityavatar.Metadata `json:"communityAvatar,omitempty"`
	SourceProof     any                       `json:"-"`
	CreatedAt       time.Time                 `json:"createdAt"`
}

func (r Record) ValidMedia() bool {
	if r.Avatar != nil && (r.Target.Type != Person || !r.Avatar.Valid() || r.Avatar.AccountID != r.Target.ID) {
		return false
	}
	return r.CommunityAvatar == nil || r.Target.Type == Community && r.Avatar == nil && r.CommunityAvatar.Valid() && r.CommunityAvatar.CommunityID == r.Target.ID
}

type Store interface {
	ListOwnFollows(context.Context, string) ([]Record, error)
	Follow(context.Context, string, Target) (Record, error)
	Unfollow(context.Context, string, Target) error
}

func Normalize(target Target) (Target, error) {
	target.Type = strings.ToUpper(strings.TrimSpace(target.Type))
	target.ID = strings.ToLower(strings.TrimSpace(target.ID))
	switch target.Type {
	case Person, Organization, Community, Business:
	default:
		return Target{}, ErrInvalid
	}
	return target, nil
}
