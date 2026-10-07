// Package agentmembershipsignal assembles current native membership evidence
// into a Personal Context for human self-review. It grants no model access.
package agentmembershipsignal

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
)

const SchemaVersion = "agent-membership-context-v1"
const MaxLease = 5 * time.Minute
const MaxRequestDeadline = 15 * time.Minute

type Kind string

const (
	Community    Kind = "COMMUNITY_MEMBERSHIP"
	Organization Kind = "ORGANIZATION_MEMBERSHIP"
)

var (
	ErrInvalid     = errors.New("成员上下文请求无效")
	ErrDenied      = errors.New("成员上下文不可读取或已失效")
	ErrExpired     = errors.New("成员上下文已过期，请重新读取")
	ErrUnavailable = errors.New("成员上下文服务不可用")
	ErrServerOnly  = errors.New("成员上下文控制对象仅供服务端使用")
)

type Request struct {
	Access       agentprofile.PrivateAccess
	Agent        agentcognitive.AgentReference
	Kind         Kind
	MembershipID string
	DeadlineAt   time.Time // Request lease, not a stored membership expiry.
}

func (Request) MarshalJSON() ([]byte, error)  { return nil, ErrServerOnly }
func (r *Request) UnmarshalJSON([]byte) error { *r = Request{}; return ErrServerOnly }

type requestSelectors struct {
	Agent        agentcognitive.AgentReference
	Kind         Kind
	MembershipID string
	DeadlineAt   time.Time
}

func selectors(r Request) requestSelectors {
	return requestSelectors{r.Agent, r.Kind, r.MembershipID, r.DeadlineAt.UTC()}
}
func fromSelectors(s requestSelectors, a agentprofile.PrivateAccess) Request {
	return Request{a, s.Agent, s.Kind, s.MembershipID, s.DeadlineAt}
}
func validTime(t time.Time) bool { return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 }
func validID(id string) bool {
	r, e := actorref.ParsePrincipal("PERSON", id)
	return e == nil && r.ID == id && id != "00000000-0000-0000-0000-000000000000"
}
func validRole(kind Kind, role string) bool {
	return role == "member" || role == "admin" || role == "owner" || (kind == Organization && role == "moderator")
}
func validateRequest(r Request, now time.Time) error {
	if !validTime(now) || agentprofile.ValidatePrivateAccess(r.Access) != nil || r.Agent.Principal != r.Access.WorkspacePrincipal || r.Agent.Principal.Type != actorref.Person || r.Agent.Role != agentruntime.PersonalAgent || !validID(r.Agent.AgentID) {
		return ErrDenied
	}
	if !validTime(r.DeadlineAt) || !r.DeadlineAt.After(now) || r.DeadlineAt.After(now.Add(MaxRequestDeadline)) {
		return ErrExpired
	}
	if (r.Kind != Community && r.Kind != Organization) || !validID(r.MembershipID) {
		return ErrInvalid
	}
	return nil
}

// Evidence is the current retained member relation, never an interest, identity,
// Friend, attendance, historical join action or persisted Memory Evidence.
type Evidence struct {
	Kind               Kind                     `json:"kind"`
	MembershipID       string                   `json:"membershipId"`
	ResourceID         string                   `json:"resourceId"`
	PrincipalAccountID string                   `json:"principalAccountId,omitempty"`
	Label              string                   `json:"label"`
	Role               string                   `json:"role"`
	Status             string                   `json:"status"`
	SourceVersion      agentevent.SourceVersion `json:"sourceVersion"`
	NativeTime         time.Time                `json:"nativeTime"`
}
type Snapshot struct {
	SchemaVersion          string                        `json:"schemaVersion"`
	Agent                  agentcognitive.AgentReference `json:"agent"`
	Purpose                string                        `json:"purpose"`
	Scope                  string                        `json:"scope"`
	Evidence               []Evidence                    `json:"evidence"`
	ObservedAt             time.Time                     `json:"observedAt"`
	ExpiresAt              time.Time                     `json:"expiresAt"`
	InterestInferred       bool                          `json:"interestInferred"`
	IdentityVerified       bool                          `json:"identityVerified"`
	FriendEstablished      bool                          `json:"friendEstablished"`
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
	s.Evidence = append([]Evidence(nil), s.Evidence...)
	s.seal = append([]byte(nil), s.seal...)
	return s
}

// snapshotTimeAt receives only this service's actual native database clock.
// Host/VM/caller clocks cannot determine whether a DB-issued observation is
// future or expired. Strict future/expiry checks remain, with no tolerance.
func snapshotTimeAt(s Snapshot, checkedAt time.Time) error {
	if !validTime(checkedAt) || !validTime(s.ObservedAt) || !validTime(s.ExpiresAt) ||
		s.ObservedAt.After(checkedAt) || !s.ExpiresAt.After(checkedAt) || !s.ExpiresAt.After(s.ObservedAt) || s.ExpiresAt.Sub(s.ObservedAt) > MaxLease {
		return ErrExpired
	}
	return nil
}
func evidenceDigest(e []Evidence) string {
	b, _ := json.Marshal(e)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// The Organization source is local to this package, not a forged event catalog
// entry. Both typed versions bind the actual row, native time and xmin tokens.
func membershipVersion(kind Kind, t time.Time, b []byte) (agentevent.SourceVersion, error) {
	if (kind != Community && kind != Organization) || !validTime(t) || len(b) == 0 || len(b) > 64*1024 {
		return agentevent.SourceVersion{}, ErrInvalid
	}
	if kind == Community {
		return agentevent.SnapshotVersion(agentevent.CommunityMembershipSource, agentevent.UpdatedAtDigestVersion, t, b)
	}
	h := sha256.New()
	for _, s := range []string{"birdtie.membership-context.native.v1", string(kind), t.UTC().Format(time.RFC3339Nano)} {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	h.Write(b)
	return agentevent.SourceVersion{Kind: agentevent.UpdatedAtDigestVersion, Token: hex.EncodeToString(h.Sum(nil))}, nil
}

// assembleContext is actually called by the native service; fixture calls alone
// only prove shape. It does not save a Memory or grant analysis permission.
func assembleContext(r Request, s nativeState) (Snapshot, error) {
	if e := validateRequest(r, s.now); e != nil {
		return Snapshot{}, e
	}
	fact := s.fact
	if fact.Kind != r.Kind || fact.MembershipID != r.MembershipID || !validID(fact.ResourceID) || !validRole(fact.Kind, fact.Role) || fact.Status != "active" || !validTime(fact.NativeTime) || fact.NativeTime.After(s.now) || !utf8.ValidString(fact.Label) || strings.TrimSpace(fact.Label) == "" || len(fact.Label) > 640 || strings.ContainsAny(fact.Label, "\x00\r\n") {
		return Snapshot{}, ErrDenied
	}
	if fact.Kind == Community && fact.PrincipalAccountID != "" || fact.Kind == Organization && (!validID(fact.PrincipalAccountID) || fact.ResourceID == fact.PrincipalAccountID) {
		return Snapshot{}, ErrDenied
	}
	expires := s.now.Add(MaxLease)
	for _, limit := range []time.Time{r.DeadlineAt, s.sessionLimit} {
		if limit.Before(expires) {
			expires = limit
		}
	}
	if s.sourceExpires != nil && s.sourceExpires.Before(expires) {
		expires = *s.sourceExpires
	}
	if !expires.After(s.now) {
		return Snapshot{}, ErrExpired
	}
	facts := []Evidence{fact}
	return Snapshot{SchemaVersion: SchemaVersion, Agent: r.Agent, Purpose: "HUMAN_SELF_REVIEW", Scope: "PERSONAL_MEMBERSHIP_CONTEXT", Evidence: facts, ObservedAt: s.now.UTC(), ExpiresAt: expires.UTC(), ModelAccess: "UNAVAILABLE", selector: selectors(r), authority: s.authority, nativeDigest: evidenceDigest(facts)}, nil
}
