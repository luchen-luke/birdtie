package agentworkspace

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

var ErrNotFound = errors.New("agent task not found")

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
	ID       string               `json:"id"`
	Name     string               `json:"name"`
	Summary  string               `json:"summary"`
	Location *foundation.Location `json:"location,omitempty"`
	Source   foundation.Source    `json:"source"`
}

type Results struct {
	CityID        string                `json:"cityId"`
	Query         string                `json:"query"`
	Mode          string                `json:"mode"`
	TaskID        string                `json:"taskId,omitempty"`
	PrincipalType string                `json:"principalType"`
	PrincipalID   string                `json:"principalId"`
	Workspace     string                `json:"workspace"`
	Role          string                `json:"role,omitempty"`
	Permissions   []string              `json:"permissions"`
	Activities    []foundation.Activity `json:"activities"`
	People        []Person              `json:"people"`
	Groups        []Group               `json:"groups"`
	Places        []foundation.Place    `json:"places"`
}

type Task struct {
	ID        string    `json:"id"`
	CityID    string    `json:"cityId"`
	Query     string    `json:"query"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Store interface {
	Search(context.Context, string, string, []string) (Results, error)
	SaveTask(context.Context, string, string, string) (Task, error)
	ListTasks(context.Context, string) ([]Task, error)
	GetTask(context.Context, string, string) (Task, error)
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
