// Package actorref defines typed references to actors. A reference identifies a
// subject; it never proves that a caller may act for that subject.
package actorref

import (
	"errors"
	"regexp"
	"strings"
)

type Type string

const (
	Person       Type = "PERSON"
	Organization Type = "ORGANIZATION"
	Business     Type = "BUSINESS"
	Community    Type = "COMMUNITY"
)

var (
	ErrType = errors.New("invalid actor type")
	ErrID   = errors.New("invalid actor id")
	uuid    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

type ActorRef struct {
	Type Type   `json:"type"`
	ID   string `json:"id"`
}

// PrincipalRef identifies the account principal backing an Agent. For an
// organization its ID is organizations.account_id, whereas ActorRef uses
// organizations.id. Keeping the Go types distinct prevents accidental ID
// substitution across these two namespaces.
type PrincipalRef struct {
	Type Type   `json:"type"`
	ID   string `json:"id"`
}

func ParseType(raw string) (Type, error) {
	t := Type(strings.ToUpper(strings.TrimSpace(raw)))
	switch t {
	case Person, Organization, Business, Community:
		return t, nil
	default:
		return "", ErrType
	}
}

func Parse(kind, id string) (ActorRef, error) {
	t, err := ParseType(kind)
	if err != nil {
		return ActorRef{}, err
	}
	id = strings.TrimSpace(id)
	if !uuid.MatchString(id) {
		return ActorRef{}, ErrID
	}
	return ActorRef{Type: t, ID: strings.ToLower(id)}, nil
}

func ParsePrincipal(kind, accountID string) (PrincipalRef, error) {
	ref, err := Parse(kind, accountID)
	if err != nil {
		return PrincipalRef{}, err
	}
	return PrincipalRef{Type: ref.Type, ID: ref.ID}, nil
}

func (ref ActorRef) Valid() bool {
	_, err := Parse(string(ref.Type), ref.ID)
	return err == nil
}

func (ref ActorRef) Equal(other ActorRef) bool {
	return ref.Valid() && other.Valid() && ref.Type == other.Type && strings.EqualFold(ref.ID, other.ID)
}

func (ref PrincipalRef) Equal(other PrincipalRef) bool {
	left, leftErr := ParsePrincipal(string(ref.Type), ref.ID)
	right, rightErr := ParsePrincipal(string(other.Type), other.ID)
	return leftErr == nil && rightErr == nil && left.Type == right.Type && left.ID == right.ID
}
