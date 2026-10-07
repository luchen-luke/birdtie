// Package onlinesocialopportunity is a human-requested, read-only projection of
// current online sources. It is not a processing grant or a new domain ledger.
package onlinesocialopportunity

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"strings"
	"time"
	"unicode/utf8"
)

const SchemaVersion = "online-social-opportunities-v1"

var ErrInvalid = errors.New("invalid online discovery")
var ErrDenied = errors.New("online discovery denied")
var ErrChanged = errors.New("online discovery changed")
var ErrUnavailable = errors.New("online discovery unavailable")

type Access struct {
	Actor  identity.Actor
	Digest [32]byte
}

func (a Access) Valid() bool {
	return a.Actor.AccountType == "person" && businessconsole.ValidID(a.Actor.ID) && a.Digest != ([32]byte{})
}
func (Access) MarshalJSON() ([]byte, error) { return nil, ErrInvalid }

type Intent struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updatedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type Ref struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
type Item struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Source        Ref       `json:"sourceRef"`
	Relation      string    `json:"relation"`
	SourceVersion string    `json:"sourceVersion"`
	ExpiresAt     time.Time `json:"expiresAt"`
	TieID         string    `json:"tieId"`
	CommunityID   string    `json:"communityId"`
}
type View struct {
	SchemaVersion string    `json:"schemaVersion"`
	OwnerID       string    `json:"ownerId"`
	IntentID      string    `json:"intentId"`
	ObservedAt    time.Time `json:"observedAt"`
	ValidUntil    time.Time `json:"validUntil"`
	Truncated     bool      `json:"truncated"`
	Intents       []Intent  `json:"intents"`
	Items         []Item    `json:"items"`
}
type Receipt struct {
	View        View
	Proof, Seal string
}

func (Receipt) MarshalJSON() ([]byte, error) { return nil, ErrInvalid }

type Store interface {
	ReadOnlineSocialOpportunityOptions(context.Context, Access) (Receipt, error)
	ReadOnlineSocialOpportunities(context.Context, Access, string) (Receipt, error)
	RevalidateOnlineSocialOpportunities(context.Context, Access, Receipt) error
}

func Text(s string, max int) bool {
	return utf8.ValidString(s) && s == strings.TrimSpace(s) && len([]rune(s)) > 0 && len([]rune(s)) <= max && !strings.ContainsRune(s, 0)
}
func Validate(v View) error {
	if v.SchemaVersion != SchemaVersion || !businessconsole.ValidID(v.OwnerID) || (v.IntentID != "" && !businessconsole.ValidID(v.IntentID)) || v.ObservedAt.IsZero() || v.ObservedAt.Year() > 9999 || !v.ValidUntil.After(v.ObservedAt) || v.ValidUntil.Sub(v.ObservedAt) > 2*time.Minute || v.Intents == nil || v.Items == nil || len(v.Intents) > 20 || len(v.Items) > 30 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, i := range v.Intents {
		if !businessconsole.ValidID(i.ID) || !Text(i.Title, 300) || i.UpdatedAt.IsZero() || i.UpdatedAt.After(v.ObservedAt) || !i.ExpiresAt.After(v.ObservedAt) || seen[i.ID] {
			return ErrInvalid
		}
		seen[i.ID] = true
	}
	if v.IntentID == "" && len(v.Items) > 0 {
		return ErrInvalid
	}
	if v.IntentID != "" && (len(v.Intents) != 1 || v.Intents[0].ID != v.IntentID) {
		return ErrInvalid
	}
	seen = map[string]bool{}
	for _, i := range v.Items {
		if (i.Source.Type != "SOCIAL_INTENT" && i.Source.Type != "ACTIVITY") || !businessconsole.ValidID(i.Source.ID) || i.ID != v.IntentID+":"+i.Source.Type+":"+i.Source.ID || !Text(i.Title, 300) || !Text(i.SourceVersion, 180) || !i.ExpiresAt.After(v.ObservedAt) || seen[i.ID] {
			return ErrInvalid
		}
		seen[i.ID] = true
		switch i.Relation {
		case "FRIEND":
			if !businessconsole.ValidID(i.TieID) || i.CommunityID != "" {
				return ErrInvalid
			}
		case "COMMUNITY":
			if !businessconsole.ValidID(i.CommunityID) || i.TieID != "" {
				return ErrInvalid
			}
		case "PUBLIC":
			if i.TieID != "" || i.CommunityID != "" {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	}
	return nil
}
