package socialintent

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
)

var ErrInvalidConstraints = errors.New("invalid social intent constraints")
var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type Constraints struct {
	Category        string     `json:"category,omitempty"`
	MinParticipants int        `json:"minParticipants,omitempty"`
	MaxParticipants int        `json:"maxParticipants,omitempty"`
	PlaceID         string     `json:"placeId,omitempty"`
	AreaLabel       string     `json:"areaLabel,omitempty"`
	OnlinePlatform  string     `json:"onlinePlatform,omitempty"`
	StartsAt        *time.Time `json:"startsAt,omitempty"`
	EndsAt          *time.Time `json:"endsAt,omitempty"`
}

// ParseConstraints checks the modality without inferring a city or exact
// coordinate. The returned JSON is the canonical shape persisted by the API.
func ParseConstraints(raw json.RawMessage, modality string) (Constraints, json.RawMessage, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if len(raw) > 3500 || !bytes.HasPrefix(bytes.TrimSpace(raw), []byte{'{'}) {
		return Constraints{}, nil, ErrInvalidConstraints
	}
	var c Constraints
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return Constraints{}, nil, ErrInvalidConstraints
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Constraints{}, nil, ErrInvalidConstraints
	}
	c.Category = strings.TrimSpace(c.Category)
	c.AreaLabel = strings.TrimSpace(c.AreaLabel)
	c.OnlinePlatform = strings.TrimSpace(c.OnlinePlatform)
	if (c.StartsAt == nil) != (c.EndsAt == nil) || (c.StartsAt != nil &&
		(c.StartsAt.Year() < 2000 || c.EndsAt.Year() > 2200 || !c.EndsAt.After(*c.StartsAt) || c.EndsAt.Sub(*c.StartsAt) > 90*24*time.Hour)) {
		return Constraints{}, nil, ErrInvalidConstraints
	}
	if len([]rune(c.Category)) > 80 || len([]rune(c.AreaLabel)) > 160 ||
		len([]rune(c.OnlinePlatform)) > 80 ||
		(c.PlaceID != "" && !uuid.MatchString(c.PlaceID)) ||
		c.MinParticipants < 0 || c.MaxParticipants < 0 ||
		c.MinParticipants > 100 || c.MaxParticipants > 100 ||
		(c.MinParticipants > 0 && c.MaxParticipants > 0 && c.MaxParticipants < c.MinParticipants) {
		return Constraints{}, nil, ErrInvalidConstraints
	}
	physical := c.PlaceID != "" || c.AreaLabel != ""
	switch modality {
	case "IN_PERSON":
		if !physical || c.OnlinePlatform != "" {
			return Constraints{}, nil, ErrInvalidConstraints
		}
	case "ONLINE":
		if physical {
			return Constraints{}, nil, ErrInvalidConstraints
		}
	case "HYBRID":
		if !physical || c.OnlinePlatform == "" {
			return Constraints{}, nil, ErrInvalidConstraints
		}
	default:
		return Constraints{}, nil, ErrInvalidConstraints
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		return Constraints{}, nil, err
	}
	return c, encoded, nil
}
