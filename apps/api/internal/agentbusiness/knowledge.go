// Package agentbusiness provides ordinary human management knowledge rules.
// It never enables a Business Agent, publishes Console facts, or invokes models.
package agentbusiness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
)

var (
	ErrInvalid     = errors.New("invalid business knowledge request")
	ErrUnavailable = errors.New("business agent invocation unavailable")
	ErrChanged     = errors.New("business knowledge source changed")
)

const Mode = "HUMAN_VERIFIED_RULES"

// The shared role pack remains disabled. Human knowledge rules are a separate
// ordinary management capability, never an activation of this Agent policy.
func Policy() agentruntime.Policy { return agentruntime.ForType(actorref.Business) }

type Query struct {
	Query   string `json:"query"`
	PlaceID string `json:"placeId"`
}

func ValidateQuery(q Query) error {
	if !utf8.ValidString(q.Query) || q.Query != strings.TrimSpace(q.Query) || len([]rune(q.Query)) < 2 || len([]rune(q.Query)) > 240 || (q.PlaceID != "" && !businessconsole.ValidID(q.PlaceID)) {
		return ErrInvalid
	}
	return nil
}

// Both fields are explicit, including empty placeId. No client authority keys.
func DecodeQuery(raw []byte) (Query, error) {
	var q Query
	if len(raw) > 4096 {
		return q, ErrInvalid
	}
	n, e := businessconsole.StrictObject(raw, "query", "placeId")
	if e != nil || json.Unmarshal(n, &q) != nil || ValidateQuery(q) != nil {
		return Query{}, ErrInvalid
	}
	return q, nil
}

// Snapshot is native-only; valid shape is never proof of source authority.
// Facts carry only fields already explicitly approved by the real Console.
type Snapshot struct {
	Actor         actorref.ActorRef     `json:"actor"`
	Principal     actorref.PrincipalRef `json:"principal"`
	SourceVersion string                `json:"sourceVersion"`
	At            time.Time             `json:"at"`
	Profile       *Profile              `json:"profile"`
	Venues        []Venue               `json:"venues"`
}

func (Snapshot) MarshalJSON() ([]byte, error)  { return nil, ErrUnavailable }
func (s *Snapshot) UnmarshalJSON([]byte) error { *s = Snapshot{}; return ErrUnavailable }

type Profile struct {
	Version    int64                        `json:"version"`
	ValidUntil time.Time                    `json:"validUntil"`
	Facts      businessconsole.ProfileFacts `json:"facts"`
}
type Venue struct {
	PlaceID    string                     `json:"placeId"`
	Version    int64                      `json:"version"`
	ValidUntil time.Time                  `json:"validUntil"`
	Facts      businessconsole.VenueFacts `json:"facts"`
}
type Source struct {
	Type       string    `json:"type"`
	ID         string    `json:"id"`
	Version    int64     `json:"version"`
	ValidUntil time.Time `json:"validUntil"`
}
type Answer struct {
	BusinessID    string   `json:"businessId"`
	Mode          string   `json:"mode"`
	Status        string   `json:"status"`
	Answer        string   `json:"answer"`
	SourceVersion string   `json:"sourceVersion"`
	Sources       []Source `json:"sources"`
	AgentStatus   string   `json:"agentStatus"`
	ModelStatus   string   `json:"modelStatus"`
	Tools         []string `json:"tools"`
}

func Digest(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func ValidVersion(v string) bool {
	b, e := hex.DecodeString(v)
	return e == nil && len(b) == sha256.Size && hex.EncodeToString(b) == v
}
func finite(t time.Time) bool {
	return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 && t.Location() == time.UTC
}
func ValidateSnapshot(s Snapshot) error {
	if s.Actor.Type != actorref.Business || s.Principal.Type != actorref.Business || !businessconsole.ValidID(s.Actor.ID) || !businessconsole.ValidID(s.Principal.ID) || !ValidVersion(s.SourceVersion) || !finite(s.At) || s.Venues == nil || len(s.Venues) > 100 {
		return fmt.Errorf("%w: native binding or snapshot shape", ErrInvalid)
	}
	if s.Profile != nil && (s.Profile.Version < 1 || !finite(s.Profile.ValidUntil) || !s.Profile.ValidUntil.After(s.At) || businessconsole.ValidateProfile(s.Profile.Facts) != nil) {
		return fmt.Errorf("%w: native profile version, time or facts", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, v := range s.Venues {
		if !businessconsole.ValidID(v.PlaceID) || seen[v.PlaceID] || v.Version < 1 || !finite(v.ValidUntil) || !v.ValidUntil.After(s.At) || businessconsole.ValidateVenueFacts(v.Facts) != nil {
			return ErrInvalid
		}
		seen[v.PlaceID] = true
	}
	return nil
}

// Exact, bounded rules: prose/prompt instructions never become tools or facts.
func Render(q Query, s Snapshot) (Answer, error) {
	if ValidateQuery(q) != nil || ValidateSnapshot(s) != nil {
		return Answer{}, ErrInvalid
	}
	a := Answer{BusinessID: s.Actor.ID, Mode: Mode, Status: "unknown", Answer: "没有当前可引用的已核验资料。商家 Agent 和模型暂不可用。", SourceVersion: s.SourceVersion, Sources: []Source{}, AgentStatus: "unavailable", ModelStatus: "unavailable", Tools: []string{}}
	profile := false
	switch q.Query {
	case "商家介绍", "商家简介", "营业时间", "官方网站":
		profile = true
	case "场地适用场景", "场地预约链接":
		if q.PlaceID == "" {
			a.Answer = "请选择具体场地；不猜测你指的是哪一家。"
			return a, nil
		}
	default:
		return a, nil
	}
	if profile {
		if q.PlaceID != "" || s.Profile == nil {
			return a, nil
		}
		p := s.Profile
		switch q.Query {
		case "商家介绍", "商家简介":
			if p.Facts.Description == "" {
				return a, nil
			}
			a.Answer = p.Facts.Description
		case "官方网站":
			if len(p.Facts.OfficialLinks) == 0 {
				return a, nil
			}
			a.Answer = strings.Join(p.Facts.OfficialLinks, "\n")
		case "营业时间":
			if len(p.Facts.OpeningHours) == 0 || p.Facts.TimeZone == "" {
				return a, nil
			}
			days := []string{"", "周一", "周二", "周三", "周四", "周五", "周六", "周日"}
			lines := []string{"已核验的营业时间（" + p.Facts.TimeZone + "）"}
			for day := 1; day <= 7; day++ {
				for _, h := range p.Facts.OpeningHours {
					if h.Day != day {
						continue
					}
					line := days[day] + "：休息"
					if !h.Closed {
						line = fmt.Sprintf("%s：%s–%s", days[day], h.OpensAt, h.ClosesAt)
						if h.NextDay {
							line += "（次日）"
						}
					}
					lines = append(lines, line)
				}
			}
			lines = append(lines, "未列出的日期未知；这不是此刻营业或预约成功的保证。")
			a.Answer = strings.Join(lines, "\n")
		}
		a.Sources = append(a.Sources, Source{"business_profile", s.Actor.ID, p.Version, p.ValidUntil})
	} else {
		for _, v := range s.Venues {
			if v.PlaceID != q.PlaceID {
				continue
			}
			switch q.Query {
			case "场地适用场景":
				if len(v.Facts.Suitability) == 0 {
					return a, nil
				}
				a.Answer = "已核验的适用场景：" + strings.Join(v.Facts.Suitability, "、") + "。这不是场地容量或可用时段承诺。"
			case "场地预约链接":
				if v.Facts.BookingURL == "" {
					return a, nil
				}
				a.Answer = "预约信息链接：" + v.Facts.BookingURL + "。请查看场地当前安排；未自动预约。"
			}
			a.Sources = append(a.Sources, Source{"business_venue", v.PlaceID, v.Version, v.ValidUntil})
			break
		}
	}
	if len(a.Sources) > 0 {
		a.Status = "known"
	}
	return a, nil
}

type Store interface {
	ReadOwnBusinessKnowledge(context.Context, businessconsole.Access) (Snapshot, error)
	ValidateOwnBusinessKnowledge(context.Context, businessconsole.Access, string) error
}
type Service struct{ store Store }

func NewService(s Store) *Service { return &Service{s} }
func (s *Service) Answer(ctx context.Context, a businessconsole.Access, q Query) (Answer, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.store == nil {
		return Answer{}, ErrUnavailable
	}
	if ValidateQuery(q) != nil {
		return Answer{}, ErrInvalid
	}
	snap, e := s.store.ReadOwnBusinessKnowledge(ctx, a)
	if e != nil {
		return Answer{}, e
	}
	if ctx.Err() != nil {
		return Answer{}, ErrUnavailable
	}
	answer, e := Render(q, snap)
	if e != nil {
		return Answer{}, e
	}
	if e = s.store.ValidateOwnBusinessKnowledge(ctx, a, snap.SourceVersion); e != nil {
		return Answer{}, e
	}
	if ctx.Err() != nil {
		return Answer{}, ErrUnavailable
	}
	return answer, nil
}

// No current trusted Business Agent/purpose/provider route exists. There is no
// fallback to the human renderer, even if a client/fixture says enabled.
func (*Service) Invoke(context.Context, businessconsole.Access, Query) (Answer, error) {
	return Answer{}, ErrUnavailable
}
