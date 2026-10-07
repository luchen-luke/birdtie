// Package agentorganization renders bounded public Organization knowledge using
// the shared role pack. Shape checks here never verify identity or source ACL;
// the native public-domain reader resolves those facts for each request.
package agentorganization

import (
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
)

var ErrInvalid = errors.New("invalid organization knowledge projection")
var ErrWire = errors.New("organization knowledge is native-only")

const MaxFAQs = 100
const MaxActivities = 20

// Projection carries only public facts needed by the existing rule renderer.
// There are no member/private memories, coordinates, model, grant or tools.
type Projection struct {
	Actor           actorref.ActorRef
	Principal       actorref.PrincipalRef
	AgentID         string
	MetadataVersion int64
	Profile         organization.PublicProfile
	FAQs            []organization.FAQ
}

func (Projection) MarshalJSON() ([]byte, error)  { return nil, ErrWire }
func (p *Projection) UnmarshalJSON([]byte) error { *p = Projection{}; return ErrWire }

func validID(id string) bool {
	ref, err := actorref.ParsePrincipal("PERSON", id)
	return err == nil && ref.ID == id && id != "00000000-0000-0000-0000-000000000000"
}

func ValidQuery(query string) bool {
	return utf8.ValidString(query) && strings.TrimSpace(query) == query && len([]rune(query)) >= 2 && len([]rune(query)) <= 240
}

func unknown(id string) organization.AgentAnswer {
	return organization.AgentAnswer{OrganizationID: id, Status: "unknown", Answer: "组织 Agent 暂不可用。", Sources: []organization.AnswerSource{}, Mode: "verified_rules"}
}

func Answer(query string, p Projection) (organization.AgentAnswer, error) {
	pack := agentruntime.ForType(p.Principal.Type)
	if !ValidQuery(query) || p.Actor.Type != actorref.Organization || p.Principal.Type != actorref.Organization || !pack.Allows(agentruntime.OrganizationContextRead) || !validID(p.Actor.ID) || !validID(p.Principal.ID) || p.Profile.ID != p.Actor.ID || len(p.FAQs) > MaxFAQs || len(p.Profile.UpcomingActivities) > MaxActivities {
		return organization.AgentAnswer{}, ErrInvalid
	}
	if !p.Profile.AgentAvailable || !validID(p.AgentID) || p.MetadataVersion <= 0 {
		return unknown(p.Actor.ID), nil
	}
	if p.Profile.VerificationStatus != "verified" {
		return organization.BuildAnswer(query, p.Profile, nil), nil
	}
	seen := map[string]bool{}
	for _, faq := range p.FAQs {
		if !validID(faq.ID) || faq.OrganizationID != p.Actor.ID || !faq.Published || seen["faq:"+faq.ID] || len([]rune(faq.Question)) > 300 || len([]rune(faq.Answer)) > 3000 {
			return organization.AgentAnswer{}, ErrInvalid
		}
		seen["faq:"+faq.ID] = true
	}
	for _, a := range p.Profile.UpcomingActivities {
		if !validID(a.ID) || a.Organizer.Type != "ORGANIZATION" || a.Organizer.ID != p.Actor.ID || a.OrganizationID == nil || *a.OrganizationID != p.Actor.ID || a.Visibility != "public" || a.Status != "upcoming" || a.Location != nil || a.Description != "" || seen["activity:"+a.ID] {
			return organization.AgentAnswer{}, ErrInvalid
		}
		seen["activity:"+a.ID] = true
	}
	answer := organization.BuildAnswer(query, p.Profile, p.FAQs)
	if !ValidAnswer(answer, p.Actor.ID) {
		return organization.AgentAnswer{}, ErrInvalid
	}
	return answer, nil
}

// ValidAnswer guards the existing wire response, not the authenticity of its
// references. The actual source binding is checked above and in native SQL.
func ValidAnswer(a organization.AgentAnswer, id string) bool {
	if a.OrganizationID != id || !validID(id) || a.Mode != "verified_rules" || !utf8.ValidString(a.Answer) || a.Answer == "" || len([]rune(a.Answer)) > 12000 || len(a.Sources) > 1 {
		return false
	}
	if a.Status == "unknown" {
		return len(a.Sources) == 0
	}
	if a.Status != "known" || len(a.Sources) != 1 {
		return false
	}
	s := a.Sources[0]
	if !validID(s.ID) || s.Label == "" || !utf8.ValidString(s.Label) {
		return false
	}
	switch s.Type {
	case "faq", "activity":
		return s.URL == ""
	case "profile":
		return s.ID == id && s.URL == ""
	case "link":
		u, e := url.Parse(s.URL)
		return s.ID == id && e == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && len(s.URL) <= 2048 && strings.TrimSpace(s.URL) == s.URL
	default:
		return false
	}
}
