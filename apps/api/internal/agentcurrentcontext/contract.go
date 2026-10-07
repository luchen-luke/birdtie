// Package agentcurrentcontext reads bounded, current human self-review context.
// It neither promotes declarations to Memory nor grants model/cognitive access.
package agentcurrentcontext

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
)

const SchemaVersion = "agent-current-context-v1"
const MaxLease = 5 * time.Minute
const MaxRequestDeadline = 15 * time.Minute
const MaxQueryBytes = 240

type Selection string

const (
	CurrentDeclaration Selection = "CURRENT_CITY_DECLARATION"
	CurrentTask        Selection = "CURRENT_TASK"
	SelectedCity       Selection = "SELECTED_CITY"
)

var (
	ErrInvalid     = errors.New("当前上下文请求无效")
	ErrDenied      = errors.New("当前上下文不可读取或已失效")
	ErrExpired     = errors.New("当前上下文已过期，请重新读取")
	ErrUnavailable = errors.New("当前上下文服务不可用")
	ErrServerOnly  = errors.New("当前上下文控制对象仅供服务端使用")
)

type Request struct {
	Access     agentprofile.PrivateAccess
	Agent      agentcognitive.AgentReference
	Selection  Selection
	CityID     string
	TaskID     string
	Query      string
	DeadlineAt time.Time // A request lease limit, never a stored source expiry.
}

func (Request) MarshalJSON() ([]byte, error)  { return nil, ErrServerOnly }
func (r *Request) UnmarshalJSON([]byte) error { *r = Request{}; return ErrServerOnly }

type requestSelectors struct {
	Agent      agentcognitive.AgentReference
	Selection  Selection
	CityID     string
	TaskID     string
	Query      string
	DeadlineAt time.Time
}

func selectors(r Request) requestSelectors {
	return requestSelectors{r.Agent, r.Selection, r.CityID, r.TaskID, r.Query, r.DeadlineAt.UTC()}
}
func fromSelectors(s requestSelectors, a agentprofile.PrivateAccess) Request {
	return Request{a, s.Agent, s.Selection, s.CityID, s.TaskID, s.Query, s.DeadlineAt}
}
func validTime(t time.Time) bool { return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 }
func validID(id string) bool {
	r, e := actorref.ParsePrincipal("PERSON", id)
	return e == nil && r.ID == id && id != "00000000-0000-0000-0000-000000000000"
}
func validCityID(id string) bool {
	return len(id) > 0 && len(id) <= 100 && strings.TrimSpace(id) == id && !strings.ContainsAny(id, "\x00\r\n")
}
func validateRequest(r Request, now time.Time) error {
	if !validTime(now) || agentprofile.ValidatePrivateAccess(r.Access) != nil || r.Agent.Principal != r.Access.WorkspacePrincipal ||
		r.Agent.Principal.Type != actorref.Person || r.Agent.Role != agentruntime.PersonalAgent || !validID(r.Agent.AgentID) {
		return ErrDenied
	}
	if !validTime(r.DeadlineAt) || !r.DeadlineAt.After(now) || r.DeadlineAt.After(now.Add(MaxRequestDeadline)) {
		return ErrExpired
	}
	if !utf8.ValidString(r.Query) || len(r.Query) > MaxQueryBytes || strings.ContainsAny(r.Query, "\x00\r\n") || r.Query != strings.TrimSpace(r.Query) {
		return ErrInvalid
	}
	switch r.Selection {
	case CurrentDeclaration:
		if r.TaskID != "" || r.CityID != "" {
			return ErrInvalid
		}
	case CurrentTask:
		if !validID(r.TaskID) || r.CityID != "" || r.Query != "" {
			return ErrInvalid
		}
	case SelectedCity:
		if !validCityID(r.CityID) || r.TaskID != "" || r.Query == "" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

type Source struct {
	Kind       string                   `json:"kind"`
	ID         string                   `json:"id"`
	Version    agentevent.SourceVersion `json:"version"`
	NativeTime time.Time                `json:"nativeTime"`
}
type CityView struct {
	ID              string           `json:"id"`
	Label           string           `json:"label"`
	TimeZone        string           `json:"timeZone"`
	Context         contextgraph.Ref `json:"context"`
	Origin          Selection        `json:"origin"`
	SourceFreshness string           `json:"sourceFreshness"`
}
type Snapshot struct {
	SchemaVersion          string                        `json:"schemaVersion"`
	Agent                  agentcognitive.AgentReference `json:"agent"`
	Purpose                string                        `json:"purpose"`
	Scope                  string                        `json:"scope"`
	City                   CityView                      `json:"city"`
	Query                  string                        `json:"query"`
	QueryOrigin            string                        `json:"queryOrigin"`
	TimePreference         string                        `json:"timePreference"`
	WindowStart            *time.Time                    `json:"windowStart,omitempty"`
	WindowEnd              *time.Time                    `json:"windowEnd,omitempty"`
	ObservedAt             time.Time                     `json:"observedAt"`
	ExpiresAt              time.Time                     `json:"expiresAt"`
	Sources                []Source                      `json:"sources"`
	MemoryPromotionAllowed bool                          `json:"memoryPromotionAllowed"`
	ModelAccess            string                        `json:"modelAccess"`
	selector               requestSelectors
	authority              string
	nativeDigest           string
	seal                   []byte
	generation             uint64
}

func (s *Snapshot) UnmarshalJSON([]byte) error { *s = Snapshot{}; return ErrServerOnly }
func cloneSnapshot(s Snapshot) Snapshot {
	s.Sources = append([]Source(nil), s.Sources...)
	s.seal = append([]byte(nil), s.seal...)
	if s.WindowStart != nil {
		v := *s.WindowStart
		s.WindowStart = &v
	}
	if s.WindowEnd != nil {
		v := *s.WindowEnd
		s.WindowEnd = &v
	}
	return s
}

// contextVersion describes the actual native row/timestamp only. It does not
// expand agentevent's source catalog, invent CAS, or grant source permission.
func contextVersion(kind string, vkind agentevent.VersionKind, nativeTime time.Time, canonical []byte) (agentevent.SourceVersion, error) {
	if !validTime(nativeTime) || len(canonical) == 0 || len(canonical) > 64*1024 || (kind != "CITY_DECLARATION" && kind != "CITY_CATALOG") ||
		(kind == "CITY_DECLARATION" && vkind != agentevent.CreatedAtDigestVersion) || (kind == "CITY_CATALOG" && vkind != agentevent.UpdatedAtDigestVersion) {
		return agentevent.SourceVersion{}, ErrInvalid
	}
	h := sha256.New()
	for _, part := range []string{"birdtie.current-context.native.v1", kind, string(vkind), nativeTime.UTC().Format(time.RFC3339Nano)} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	h.Write(canonical)
	return agentevent.SourceVersion{Kind: vkind, Token: hex.EncodeToString(h.Sum(nil))}, nil
}
func sourceDigest(sources []Source) string {
	raw, _ := json.Marshal(sources)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

// temporaryWindow reuses the original closed timePreference vocabulary. A
// native task's relative time is anchored to its actual updated_at local date.
// No long-term preference or attendance/location fact is inferred.
func temporaryWindow(now time.Time, zone, pref string, taskUpdated *time.Time) (*time.Time, *time.Time, error) {
	if zone == "Local" {
		return nil, nil, ErrDenied
	}
	loc, e := time.LoadLocation(zone)
	if e != nil || zone == "" || !validTime(now) {
		return nil, nil, ErrDenied
	}
	local := now.In(loc)
	anchor := local
	if taskUpdated != nil {
		if !validTime(*taskUpdated) || taskUpdated.After(now) {
			return nil, nil, ErrDenied
		}
		anchor = taskUpdated.In(loc)
	}
	midnight := time.Date(anchor.Year(), anchor.Month(), anchor.Day(), 0, 0, 0, 0, loc)
	var start, end time.Time
	switch pref {
	case "", "anytime":
		return nil, nil, nil
	case "today":
		start = midnight
		end = midnight.AddDate(0, 0, 1)
	case "tonight":
		start = time.Date(anchor.Year(), anchor.Month(), anchor.Day(), 18, 0, 0, 0, loc)
		end = midnight.AddDate(0, 0, 1)
	case "tomorrow":
		start = midnight.AddDate(0, 0, 1)
		end = midnight.AddDate(0, 0, 2)
	case "weekend":
		days := (int(time.Saturday) - int(midnight.Weekday()) + 7) % 7
		if midnight.Weekday() == time.Sunday {
			days = -1
		}
		start = midnight.AddDate(0, 0, days)
		end = start.AddDate(0, 0, 2)
	default:
		return nil, nil, ErrDenied
	}
	if !end.After(now) {
		return nil, nil, ErrExpired
	}
	a, b := start.UTC(), end.UTC()
	return &a, &b, nil
}
func requestTimePreference(query string) string {
	return agentworkspace.ParseMVPIntent(query, nil).TimePreference
}
