package agentworkspace

import (
	"context"
	"errors"
	"fmt"
	so "github.com/birdtie/birdtie/apps/api/internal/sponsoredopportunity"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/relationshipcontext"
)

var ErrNotFound = errors.New("agent task not found")

const (
	TaskActive    = "ACTIVE"
	TaskCompleted = "COMPLETED"
	TaskFailed    = "FAILED"
)

type Person struct {
	AccountID     string   `json:"accountId"`
	DisplayName   string   `json:"displayName"`
	Topic         string   `json:"topic"`
	AreaLabel     string   `json:"areaLabel"`
	PublicMapZone string   `json:"publicMapZone,omitempty"`
	MapLatitude   *float64 `json:"mapLatitude,omitempty"`
	MapLongitude  *float64 `json:"mapLongitude,omitempty"`
}

type Group struct {
	// Set only by the native Community adapter, not inferred from a label.
	EntityType string               `json:"entityType,omitempty"`
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Summary    string               `json:"summary"`
	Location   *foundation.Location `json:"location,omitempty"`
	Source     foundation.Source    `json:"source"`
}

type Organization struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	VerificationStatus string `json:"verificationStatus"`
}

type EntityRef = arp.Ref

type ResultSet struct {
	Schema              string                   `json:"schema"`
	ID                  string                   `json:"id"`
	TaskID              string                   `json:"taskId,omitempty"`
	Query               string                   `json:"query"`
	CityID              string                   `json:"cityId"`
	Entities            []EntityRef              `json:"entities"`
	Items               []arp.Item               `json:"items"`
	Filters             map[string]string        `json:"filters"`
	GeneratedAt         time.Time                `json:"generatedAt"`
	Status              string                   `json:"status"`
	PublicFieldEvidence *arp.PublicFieldEvidence `json:"publicFieldEvidence,omitempty"`
	Sources             []AnswerSource           `json:"sources"`
	AnswerBinding       *SourcedAnswerBinding    `json:"answerBinding,omitempty"`
}

type Action struct {
	Type       string `json:"type"`
	Label      string `json:"label"`
	TargetType string `json:"targetType,omitempty"`
	TargetID   string `json:"targetId,omitempty"`
	// Created only after the HTTP resolver reads an authorized organization menu.
	// It is process-local evidence, never a JSON assertion or mutation permission.
	organizationMenuAuthority *organizationMenuAuthority
}

type MapEffects struct {
	Camera       string   `json:"camera"`
	PinEntityIDs []string `json:"pinEntityIds"`
}

type Results struct {
	// Trusted native projection adapter output; never accepted from request JSON.
	PublicCommercialRefs   []arp.Ref                    `json:"-"`
	NativeProjection       bool                         `json:"-"`
	ProjectionItems        []arp.Item                   `json:"-"`
	PublicFieldEvidence    *arp.PublicFieldEvidence     `json:"-"`
	CommercialTrustVersion string                       `json:"commercialTrustVersion,omitempty"`
	SponsoredStatus        string                       `json:"sponsoredStatus,omitempty"`
	SponsoredOpportunities []so.Public                  `json:"sponsoredOpportunities"`
	CityID                 string                       `json:"cityId"`
	ContextType            string                       `json:"contextType,omitempty"`
	ContextID              string                       `json:"contextId,omitempty"`
	Query                  string                       `json:"query"`
	Mode                   string                       `json:"mode"`
	Note                   string                       `json:"note"`
	Message                string                       `json:"message,omitempty"`
	TaskID                 string                       `json:"taskId,omitempty"`
	Task                   *Task                        `json:"task,omitempty"`
	PrincipalType          string                       `json:"principalType"`
	PrincipalID            string                       `json:"principalId"`
	Workspace              string                       `json:"workspace"`
	Role                   string                       `json:"role,omitempty"`
	Permissions            []string                     `json:"permissions"`
	Activities             []foundation.Activity        `json:"activities"`
	People                 []Person                     `json:"people"`
	Groups                 []Group                      `json:"groups"`
	Organizations          []Organization               `json:"organizations"`
	Places                 []foundation.Place           `json:"places"`
	FollowUps              []string                     `json:"followUps"`
	RequestID              string                       `json:"requestId"`
	ConversationID         string                       `json:"conversationId,omitempty"`
	ResultSet              ResultSet                    `json:"resultSet"`
	Actions                []Action                     `json:"actions"`
	MapEffects             MapEffects                   `json:"mapEffects"`
	RelationshipContext    *relationshipcontext.Context `json:"relationshipContext,omitempty"`
}

type MapBounds struct {
	West  float64 `json:"west"`
	South float64 `json:"south"`
	East  float64 `json:"east"`
	North float64 `json:"north"`
}

func (b MapBounds) Valid() bool {
	return b.West >= -180 && b.East <= 180 && b.South >= -90 && b.North <= 90 &&
		b.West < b.East && b.South < b.North
}

func (b MapBounds) Filters() map[string]string {
	return map[string]string{
		"mapWest":  strconv.FormatFloat(b.West, 'f', -1, 64),
		"mapSouth": strconv.FormatFloat(b.South, 'f', -1, 64),
		"mapEast":  strconv.FormatFloat(b.East, 'f', -1, 64),
		"mapNorth": strconv.FormatFloat(b.North, 'f', -1, 64),
	}
}

func BoundsFromFilters(filters map[string]string) (*MapBounds, error) {
	if filters["mapWest"] == "" && filters["mapSouth"] == "" && filters["mapEast"] == "" && filters["mapNorth"] == "" {
		return nil, nil
	}
	values := []string{filters["mapWest"], filters["mapSouth"], filters["mapEast"], filters["mapNorth"]}
	parsed := make([]float64, len(values))
	for i, value := range values {
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid map bounds")
		}
		parsed[i] = v
	}
	bounds := &MapBounds{West: parsed[0], South: parsed[1], East: parsed[2], North: parsed[3]}
	if !bounds.Valid() {
		return nil, fmt.Errorf("invalid map bounds")
	}
	return bounds, nil
}

type Task struct {
	ID            string            `json:"id"`
	PrincipalType string            `json:"principalType"`
	PrincipalID   string            `json:"principalId"`
	ActingUserID  string            `json:"actingUserId"`
	CityID        string            `json:"cityContext"`
	ContextType   string            `json:"contextType,omitempty"`
	ContextID     string            `json:"contextId,omitempty"`
	Query         string            `json:"query"`
	Intent        string            `json:"intent"`
	Status        string            `json:"status"`
	Filters       map[string]string `json:"filters"`
	Conversation  []Message         `json:"conversation"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

func (t Task) PrincipalRef() (actorref.PrincipalRef, error) {
	return actorref.ParsePrincipal(t.PrincipalType, t.PrincipalID)
}

func (t Task) ContextRef() (contextgraph.Ref, error) {
	return contextgraph.Parse(t.ContextType, t.ContextID)
}

type Message struct {
	Role                 string         `json:"role"`
	Text                 string         `json:"text"`
	Sources              []AnswerSource `json:"sources,omitempty"`
	SourceRunID          string         `json:"sourceRunId,omitempty"`
	SourceEvidenceDigest string         `json:"sourceEvidenceDigest,omitempty"`
}

type Store interface {
	HasActiveAgent(context.Context, string, string) (bool, error)
	Search(context.Context, string, string, []string) (Results, error)
	SaveTask(context.Context, Task) (Task, error)
	UpdateTask(context.Context, Task) (Task, error)
	ListTasks(context.Context, string) ([]Task, error)
	GetTask(context.Context, string, string) (Task, error)
	SearchActivities(context.Context, string, string, string, string, bool, *MapBounds) ([]foundation.Activity, error)
	SearchOrganizations(context.Context, string, string) ([]Organization, error)
}

var ignored = map[string]bool{
	"find": true, "someone": true, "somebody": true, "people": true,
	"person": true, "with": true, "this": true, "that": true, "want": true,
	"would": true, "like": true, "play": true, "meet": true, "near": true,
	"around": true, "weekend": true, "tonight": true, "today": true,
	"tomorrow": true, "the": true, "and": true, "for": true, "you": true,
	"me": true, "my": true, "to": true, "in": true, "at": true, "a": true,
	"an": true, "on": true, "do": true, "is": true,
}

// Terms is a transparent lexical filter, not semantic matching or an LLM.
func Terms(query string) []string {
	words := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
	out := make([]string, 0, 6)
	seen := make(map[string]bool)
	for _, word := range words {
		if len([]rune(word)) < 2 || ignored[word] || seen[word] {
			continue
		}
		out = append(out, word)
		seen[word] = true
		if len(out) == 6 {
			break
		}
	}
	return out
}
