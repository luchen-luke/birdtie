// Package contextgraph identifies geographic and non-geographic contexts.
// Context references carry no account ownership, membership or visibility.
package contextgraph

import (
	"errors"
	"regexp"
	"strings"
)

type Type string

const (
	City        Type = "CITY"
	Country     Type = "COUNTRY"
	Institution Type = "INSTITUTION"
	Community   Type = "COMMUNITY"
	Online      Type = "ONLINE"
)

var (
	ErrType = errors.New("invalid context type")
	ErrID   = errors.New("invalid context id")
	uuid    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

type Ref struct {
	Type Type   `json:"contextType"`
	ID   string `json:"contextId"`
}

func Parse(kind, id string) (Ref, error) {
	t := Type(strings.ToUpper(strings.TrimSpace(kind)))
	switch t {
	case City, Country, Institution, Community, Online:
	default:
		return Ref{}, ErrType
	}
	id = strings.TrimSpace(id)
	if !uuid.MatchString(id) {
		return Ref{}, ErrID
	}
	return Ref{Type: t, ID: strings.ToLower(id)}, nil
}

func (r Ref) Equal(other Ref) bool {
	left, err := Parse(string(r.Type), r.ID)
	if err != nil {
		return false
	}
	right, err := Parse(string(other.Type), other.ID)
	return err == nil && left == right
}
