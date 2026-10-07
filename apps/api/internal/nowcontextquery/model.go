// Package nowcontextquery is the explicit human requested PUBLIC ONLINE rules
// query. It grants no model, private-context, inference, message or write tool.
package nowcontextquery

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid online query")
var ErrDenied = errors.New("online query denied")
var ErrConflict = errors.New("online query version changed")
var ErrNotFound = errors.New("online source unavailable")
var ErrUnavailable = errors.New("online query unavailable")

const Schema = "now-public-online-query-v1"
const Intent = "FIND_PUBLIC_ONLINE_INTENT"
const MaxResults = 20

type Access struct {
	Digest [32]byte
	Actor  identity.Actor
}

func (a Access) Valid() bool {
	return a.Digest != [32]byte{} && a.Actor.AccountType == "person" && businessconsole.ValidID(a.Actor.ID)
}
func (Access) MarshalJSON() ([]byte, error) { return nil, ErrDenied }

type Input struct {
	ContextID             string `json:"contextId"`
	Query                 string `json:"query"`
	TaskID                string `json:"taskId"`
	ExpectedTaskUpdatedAt string `json:"expectedTaskUpdatedAt"`
}

func Decode(raw []byte) (Input, error) {
	var in Input
	n, e := businessconsole.StrictObject(raw, "contextId", "query", "taskId", "expectedTaskUpdatedAt")
	if e != nil || json.Unmarshal(n, &in) != nil || in.Valid() != nil {
		return in, ErrInvalid
	}
	return in, nil
}
func (in Input) Valid() error {
	if !businessconsole.ValidID(in.ContextID) || !utf8.ValidString(in.Query) || strings.TrimSpace(in.Query) != in.Query || len([]rune(in.Query)) < 1 || len(in.Query) > 240 || strings.ContainsRune(in.Query, 0) {
		return ErrInvalid
	}
	if in.TaskID == "" {
		if in.ExpectedTaskUpdatedAt != "" {
			return ErrInvalid
		}
		return nil
	}
	t, e := time.Parse(time.RFC3339Nano, in.ExpectedTaskUpdatedAt)
	if !businessconsole.ValidID(in.TaskID) || e != nil || t.Year() < 1 || t.Year() > 9999 {
		return ErrInvalid
	}
	return nil
}

type Context struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Type  string `json:"type"`
}
type Item struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	Modality        string    `json:"modality"`
	ContextID       string    `json:"contextId"`
	SourceUpdatedAt time.Time `json:"sourceUpdatedAt"`
	ExpiresAt       time.Time `json:"expiresAt"`
}
type Response struct {
	Schema      string               `json:"schema"`
	Context     Context              `json:"context"`
	Query       string               `json:"query"`
	Answer      string               `json:"answer"`
	Task        *agentworkspace.Task `json:"task,omitempty"`
	Items       []Item               `json:"items"`
	ObservedAt  time.Time            `json:"observedAt"`
	ModelAccess string               `json:"modelAccess"`
	Promotion   bool                 `json:"promotion"`
	Truncated   bool                 `json:"truncated"`
}

// Receipt never crosses the HTTP boundary. A native process seals both response
// bytes and current-source frame; a client object cannot manufacture one.
type Receipt struct {
	Response Response
	Proof    string
	Seal     string
}
type ContextListReceipt struct {
	Contexts []Context
	Proof    string
	Seal     string
}

func (ContextListReceipt) MarshalJSON() ([]byte, error) { return nil, ErrDenied }
func (Receipt) MarshalJSON() ([]byte, error)            { return nil, ErrDenied }

type Store interface {
	ListOwnOnlineContexts(context.Context, Access) (ContextListReceipt, error)
	RevalidateOwnOnlineContexts(context.Context, Access, ContextListReceipt) error
	QueryOwnPublicOnline(context.Context, Access, Input) (Receipt, error)
	RestoreOwnPublicOnline(context.Context, Access, string) (Receipt, error)
	ReadOwnPublicOnlineIntent(context.Context, Access, string) (Receipt, error)
	RevalidateOwnPublicOnline(context.Context, Access, Receipt) error
}
