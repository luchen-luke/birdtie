package follow

import (
	"context"
	"errors"
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
	ID        string    `json:"id"`
	Target    Target    `json:"target"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"createdAt"`
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
