// Package agentseed describes ordinary human-owned onboarding settings.
// A declared UserIntent is private and grants no inference, model or action access.
package agentseed

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"strings"
	"time"
)

var (
	ErrInvalid     = errors.New("invalid seed")
	ErrForbidden   = errors.New("seed forbidden")
	ErrConflict    = errors.New("seed source conflict")
	ErrUnavailable = errors.New("seed unavailable")
)

const Schema = "personal-agent-seed-v1"
const MaxBody = 16 * 1024

type City struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Snapshot string `json:"sourceSnapshot"`
}
type UserIntent struct {
	Version        int64      `json:"version"`
	BasicIntent    string     `json:"basicIntent"`
	Progress       string     `json:"progress"`
	InterestChoice string     `json:"interestChoice"`
	CreatedAt      *time.Time `json:"createdAt"`
	UpdatedAt      *time.Time `json:"updatedAt"`
}
type Record struct {
	SchemaVersion         string     `json:"schemaVersion"`
	OwnerID               string     `json:"ownerId"`
	AgentID               string     `json:"agentId"`
	Snapshot              string     `json:"snapshot"`
	DisplayName           string     `json:"displayName"`
	ProfileVisibility     string     `json:"profileVisibility"`
	CurrentCity           *City      `json:"currentCity"`
	Cities                []City     `json:"cities"`
	LanguagePreferences   []string   `json:"languagePreferences"`
	Interests             []string   `json:"interests"`
	PrivateProfileVersion int64      `json:"privateProfileVersion"`
	Intent                UserIntent `json:"userIntent"`
	NeedsPrompt           bool       `json:"needsPrompt"`
}
type Input struct {
	ExpectedSnapshot    string   `json:"expectedSnapshot"`
	Action              string   `json:"action"`
	DisplayName         string   `json:"displayName"`
	CurrentCityID       string   `json:"currentCityId"`
	CurrentCitySnapshot string   `json:"currentCitySnapshot"`
	LanguagePreferences []string `json:"languagePreferences"`
	BasicIntent         string   `json:"basicIntent"`
	InterestChoice      string   `json:"interestChoice"`
	Interests           []string `json:"interests"`
}
type HumanStore interface {
	ReadOwnAgentSeed(context.Context, [32]byte, identity.Actor) (Record, error)
	SaveOwnAgentSeed(context.Context, [32]byte, identity.Actor, Input) (Record, error)
}

func BasicIntentValid(v string) bool {
	switch v {
	case "FIND_PEOPLE", "FIND_ACTIVITIES", "EXPLORE_CITY", "SIMILAR_INTERESTS", "JOIN_COMMUNITIES", "DISCOVER_PLACES", "JUST_EXPLORE":
		return true
	}
	return false
}
func SnapshotValid(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func Normalize(input Input) (Input, error) {
	if !SnapshotValid(input.ExpectedSnapshot) {
		return Input{}, ErrInvalid
	}
	if input.Action == "DEFER" {
		if input.DisplayName != "" || input.CurrentCityID != "" || input.CurrentCitySnapshot != "" || len(input.LanguagePreferences) != 0 || input.BasicIntent != "" || input.InterestChoice != "" || len(input.Interests) != 0 {
			return Input{}, ErrInvalid
		}
		return input, nil
	}
	if input.Action != "SAVE" || !BasicIntentValid(input.BasicIntent) || !SnapshotValid(input.CurrentCitySnapshot) || input.CurrentCityID == "" || len(input.CurrentCityID) > 160 || strings.TrimSpace(input.CurrentCityID) != input.CurrentCityID {
		return Input{}, ErrInvalid
	}
	profile, e := identity.NormalizeHumanProfileInput(identity.ProfileInput{DisplayName: input.DisplayName, Visibility: "private"})
	if e != nil {
		return Input{}, ErrInvalid
	}
	input.DisplayName = profile.DisplayName
	if input.InterestChoice != "SET" && input.InterestChoice != "SKIP" {
		return Input{}, ErrInvalid
	}
	if input.InterestChoice == "SKIP" && len(input.Interests) != 0 {
		return Input{}, ErrInvalid
	}
	fields, e := agentprofile.NormalizePrivateFields(agentprofile.PrivateFields{LanguagePreferences: input.LanguagePreferences, PersonalPreferences: input.Interests})
	if e != nil || len(fields.LanguagePreferences) == 0 {
		return Input{}, ErrInvalid
	}
	input.LanguagePreferences = fields.LanguagePreferences
	input.Interests = fields.PersonalPreferences
	return input, nil
}
