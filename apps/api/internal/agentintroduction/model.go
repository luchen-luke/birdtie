// Package agentintroduction exposes human-only suggestions from current public
// declarations. Neither its DTO nor a policy preference authorizes an effect.
package agentintroduction

import (
	"context"
	"encoding/hex"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"time"
)

var (
	ErrInvalid     = errors.New("invalid introduction selection")
	ErrDenied      = errors.New("introduction unavailable for this person")
	ErrChanged     = errors.New("introduction sources changed")
	ErrUnavailable = errors.New("introduction service unavailable")
)

const SchemaVersion = "human-introduction-suggestions-v1"

type Basis struct {
	Kind        string `json:"kind"`
	Explanation string `json:"explanation"`
}
type Candidate struct {
	SourceIntentID    string    `json:"sourceIntentId"`
	CandidateIntentID string    `json:"candidateIntentId"`
	AccountID         string    `json:"accountId"`
	DisplayName       string    `json:"displayName"`
	Basis             []Basis   `json:"basis"`
	SourceBinding     string    `json:"sourceBinding"` // Current read receipt, never permission.
	ExpiresAt         time.Time `json:"expiresAt"`
}
type Response struct {
	SchemaVersion          string            `json:"schemaVersion"`
	Mode                   string            `json:"mode"`
	SourceIntentID         string            `json:"sourceIntentId"`
	SourceStatus           map[string]string `json:"sourceStatus"`
	Candidates             []Candidate       `json:"candidates"`
	Truncated              bool              `json:"truncated"`
	ObservedAt             time.Time         `json:"observedAt"`
	ExpiresAt              time.Time         `json:"expiresAt"`
	Explanation            string            `json:"explanation"`
	ModelAccess            bool              `json:"modelAccess"`
	SendAllowed            bool              `json:"sendAllowed"`
	MemoryPromotionAllowed bool              `json:"memoryPromotionAllowed"`
}
type Store interface {
	ReadOwnIntroductionSuggestions(context.Context, agentprofile.PrivateAccess, string) (Response, error)
}

func NewResponse(source string, now, end time.Time) Response {
	return Response{SchemaVersion: SchemaVersion, Mode: "HUMAN_REVIEW_ONLY", SourceIntentID: source,
		SourceStatus: map[string]string{"SHARED_INTEREST": "PUBLIC_DECLARATIONS_ONLY", "SHARED_CITY": "PUBLIC_DECLARATIONS_ONLY", "SHARED_COMMUNITY": "PUBLIC_DECLARATIONS_ONLY", "SHARED_ACTIVITY": "UNAVAILABLE"},
		Candidates:   []Candidate{}, ObservedAt: now.UTC(), ExpiresAt: end.UTC(), Explanation: "仅供本人查看双方当前公开声明相同的依据；不代表现实关系、所在地、社群成员身份或共同到场，也不会发送引荐。"}
}

// PublicBasis preserves the existing explicit 052 compatibility rules. It
// cannot discover private preferences, rank people or invent common activity.
func PublicBasis(source, peer newpeople.Signal, community bool) ([]Basis, bool) {
	if _, ok := newpeople.Match(source, peer); !ok {
		return nil, false
	}
	out := []Basis{{Kind: "SHARED_INTEREST", Explanation: "双方当前公开找搭子意图填写了相同类别；这不是长期兴趣推断。"}}
	if source.CityID != "" && source.CityID == peer.CityID {
		out = append(out, Basis{Kind: "SHARED_CITY", Explanation: "双方当前公开意图选择了同一城市；这不代表所在地或距离。"})
	}
	if community {
		out = append(out, Basis{Kind: "SHARED_COMMUNITY", Explanation: "双方公开声明了同一社群情境；这不证明成员身份。"})
	}
	return out, true
}

func ValidateResponse(r Response, source string) error {
	if r.SchemaVersion != SchemaVersion || r.Mode != "HUMAN_REVIEW_ONLY" || r.SourceIntentID != source || r.ModelAccess || r.SendAllowed || r.MemoryPromotionAllowed || r.ObservedAt.IsZero() || !r.ObservedAt.Before(r.ExpiresAt) || len(r.Candidates) > 50 || r.Explanation == "" {
		return ErrUnavailable
	}
	want := NewResponse(source, r.ObservedAt, r.ExpiresAt).SourceStatus
	if len(r.SourceStatus) != len(want) {
		return ErrUnavailable
	}
	for k, v := range want {
		if r.SourceStatus[k] != v && !(k == "SHARED_ACTIVITY" && r.SourceStatus[k] == "PUBLIC_REGISTRATIONS_ONLY") {
			return ErrUnavailable
		}
	}
	seen := map[string]bool{}
	for _, c := range r.Candidates {
		for _, id := range []string{c.CandidateIntentID, c.AccountID} {
			p, e := actorref.ParsePrincipal("person", id)
			if e != nil || p.ID != id {
				return ErrUnavailable
			}
		}
		digest, e := hex.DecodeString(c.SourceBinding)
		if e != nil || len(digest) != 32 || c.SourceBinding != hex.EncodeToString(digest) || c.SourceIntentID != source || c.CandidateIntentID == source || c.DisplayName == "" || !r.ObservedAt.Before(c.ExpiresAt) || c.ExpiresAt.After(r.ExpiresAt) || seen[c.AccountID] || len(c.Basis) < 1 || len(c.Basis) > 4 {
			return ErrUnavailable
		}
		seen[c.AccountID] = true
		kinds := map[string]bool{}
		for _, basis := range c.Basis {
			if kinds[basis.Kind] || basis.Explanation == "" || (basis.Kind != "SHARED_INTEREST" && basis.Kind != "SHARED_CITY" && basis.Kind != "SHARED_COMMUNITY" && basis.Kind != "SHARED_ACTIVITY") || (basis.Kind == "SHARED_ACTIVITY" && r.SourceStatus["SHARED_ACTIVITY"] != "PUBLIC_REGISTRATIONS_ONLY") {
				return ErrUnavailable
			}
			kinds[basis.Kind] = true
		}
		if !kinds["SHARED_INTEREST"] {
			return ErrUnavailable
		}
	}
	return nil
}
