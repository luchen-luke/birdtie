package agentruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

const (
	SocialInferenceVersion            = "social-inference-v1"
	SocialInferenceMaxTTL             = 15 * time.Minute
	SocialPreferenceActivityCategory  = "ACTIVITY_CATEGORY"
	SocialPreferenceOrdinary          = "ORDINARY"
	SocialPreferenceExplicit          = "EXPLICIT"
	SocialPreferenceRuleFact          = "RULE_FACT"
	SocialPreferenceInferred          = "INFERRED"
	SocialPreferenceUncalibratedScore = "UNCALIBRATED_SCORE"
	SocialPreferenceReviewPurpose     = "REVIEW_SOCIAL_PREFERENCE"
	SocialPreferencePublishPurpose    = "PUBLISH_SOCIAL_PREFERENCE"
	SocialPreferenceAnalysisPurpose   = "SOCIAL_PREFERENCE_CANDIDATE"
	SocialActionNavigate              = "NAVIGATE"
	SocialActionRead                  = "READ"
	SocialActionPublish               = "PUBLISH"
	SocialActionHighImpact            = "HIGH_IMPACT"
)

var ErrSocialInferenceFactsJSON = errors.New("social inference facts are server-only")
var ErrInvalidSocialPreference = errors.New("invalid social preference contract")

// SocialInferenceRequest names a bounded operation. Neither this request nor
// a prompt, profile, existing discovery switch or coordination grant is consent.
// No inference route, learner, live grant store or action executor exists here.
type SocialInferenceRequest struct {
	Version        string
	RequestID      string
	TaskID         string
	AgentID        string
	OwnerPrincipal actorref.PrincipalRef
	ResourceID     string
	Action         string
	Purpose        string
	Scope          Scope
	ExpiresAt      time.Time
}

type SocialInferenceSession struct {
	Verified     bool
	Active       bool
	ID           string
	ActingUserID string
	ExpiresAt    time.Time
}

type SocialInferenceAgent struct {
	Verified       bool
	Active         bool
	OwnerActive    bool
	Role           Role
	ID             string
	OwnerPrincipal actorref.PrincipalRef
}

type SocialInferenceTask struct {
	Verified         bool
	ID               string
	ActingUserID     string
	AgentID          string
	OwnerPrincipal   actorref.PrincipalRef
	Status           string
	Revision         int64
	CurrentRevision  int64
	Action           string
	Purpose          string
	Scope            Scope
	ResourceID       string
	ResourceRevision int64
	PayloadDigest    string
}

// Source is a current, owner-authorized reference, never free text, a location,
// message body or a relationship score. Source kind is independently resolved:
// two RULE_FACT records do not become authorized inference observations.
type SocialPreferenceSource struct {
	Verified                   bool
	ID                         string
	Type                       string // USER_STATEMENT, SOCIAL_INTENT, ACTIVITY_PARTICIPATION, SAVED_PLACE, MOMENT_CONTEXT.
	Kind                       string // EXPLICIT_DECLARATION, EXPLICIT_RULE, AUTHORIZED_OBSERVATION.
	OwnerPrincipal             actorref.PrincipalRef
	Category                   string
	Sensitivity                string
	Revision                   int64
	CurrentRevision            int64
	ClusterID                  string
	IndependentClusterVerified bool
	ObservedAt                 time.Time
	ExpiresAt                  *time.Time
	Deleted                    bool
	RevokedAt                  *time.Time
	CurrentlyAuthorized        bool
	UserConfirmed              bool
	AnalysisAuthorized         bool
	AnalysisPurpose            string
	AnalysisScope              Scope
	AnalysisRevision           int64
	CurrentAnalysisRevision    int64
	AnalysisExpiresAt          time.Time
}

// Claim is an internal candidate contract, not authoritative Memory. The score
// is explicitly uncalibrated; even independent observations do not establish
// a real preference, promote Memory or permit an external action.
type SocialPreferenceClaim struct {
	Verified        bool
	ID              string
	OwnerPrincipal  actorref.PrincipalRef
	Key             string
	Value           string
	Provenance      string
	Sensitivity     string
	AssessmentKind  string
	Score           float64
	Revision        int64
	CurrentRevision int64
	ExpiresAt       time.Time
	Deleted         bool
	RevokedAt       *time.Time
	Sources         []SocialPreferenceSource
}

// Grant is hypothetical server-resolved proof for one visible version. It is
// independent of profile visibility, 048/051/052 and AGA grants. Current code
// has no resolver/persistence for it, so runtime must supply zero facts and deny.
type SocialPreferenceGrant struct {
	Verified         bool
	ID               string
	SessionID        string
	ActorID          string
	OwnerPrincipal   actorref.PrincipalRef
	AgentID          string
	RequestID        string
	TaskID           string
	TaskRevision     int64
	ResourceID       string
	ResourceRevision int64
	Action           string
	Purpose          string
	Scope            Scope
	PayloadDigest    string
	Revision         int64
	CurrentRevision  int64
	HumanConfirmed   bool
	ConfirmedAt      time.Time
	ExpiresAt        time.Time
	RevokedAt        *time.Time
}

type SocialInferenceFacts struct {
	Session SocialInferenceSession
	Agent   SocialInferenceAgent
	Task    SocialInferenceTask
	Claim   SocialPreferenceClaim
	Grant   SocialPreferenceGrant
}

func (SocialInferenceFacts) MarshalJSON() ([]byte, error) { return nil, ErrSocialInferenceFactsJSON }
func (v *SocialInferenceFacts) UnmarshalJSON([]byte) error {
	*v = SocialInferenceFacts{}
	return ErrSocialInferenceFactsJSON
}
func (SocialInferenceSession) MarshalJSON() ([]byte, error) { return nil, ErrSocialInferenceFactsJSON }
func (v *SocialInferenceSession) UnmarshalJSON([]byte) error {
	*v = SocialInferenceSession{}
	return ErrSocialInferenceFactsJSON
}
func (SocialInferenceAgent) MarshalJSON() ([]byte, error) { return nil, ErrSocialInferenceFactsJSON }
func (v *SocialInferenceAgent) UnmarshalJSON([]byte) error {
	*v = SocialInferenceAgent{}
	return ErrSocialInferenceFactsJSON
}
func (SocialInferenceTask) MarshalJSON() ([]byte, error) { return nil, ErrSocialInferenceFactsJSON }
func (v *SocialInferenceTask) UnmarshalJSON([]byte) error {
	*v = SocialInferenceTask{}
	return ErrSocialInferenceFactsJSON
}
func (SocialPreferenceClaim) MarshalJSON() ([]byte, error) { return nil, ErrSocialInferenceFactsJSON }
func (v *SocialPreferenceClaim) UnmarshalJSON([]byte) error {
	*v = SocialPreferenceClaim{}
	return ErrSocialInferenceFactsJSON
}
func (SocialPreferenceSource) MarshalJSON() ([]byte, error) { return nil, ErrSocialInferenceFactsJSON }
func (v *SocialPreferenceSource) UnmarshalJSON([]byte) error {
	*v = SocialPreferenceSource{}
	return ErrSocialInferenceFactsJSON
}
func (SocialPreferenceGrant) MarshalJSON() ([]byte, error) { return nil, ErrSocialInferenceFactsJSON }
func (v *SocialPreferenceGrant) UnmarshalJSON([]byte) error {
	*v = SocialPreferenceGrant{}
	return ErrSocialInferenceFactsJSON
}

func socialInferenceUUID(id string) bool {
	if id != strings.TrimSpace(id) || id == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	_, err := actorref.ParsePrincipal("PERSON", id)
	return err == nil
}

func socialInferencePerson(p actorref.PrincipalRef) bool {
	return p.Type == actorref.Person && socialInferenceUUID(p.ID)
}

func socialPreferenceCategory(value string) bool {
	switch value {
	case "badminton", "basketball", "football", "sports", "culture":
		return true
	default:
		return false
	}
}

// ValidateOrdinaryCandidateAttribute exposes the bounded candidate vocabulary.
// Hiking is a candidate-only addition; the separate SocialPreferenceClaim
// inference/review/publish policy retains its original five-category vocabulary.
// Passing this function is neither consent nor permission to activate Memory.
func ValidateOrdinaryCandidateAttribute(key, value string) error {
	if key != SocialPreferenceActivityCategory || (!socialPreferenceCategory(value) && value != "hiking") {
		return ErrInvalidSocialPreference
	}
	return nil
}

func socialPreferenceShape(claim SocialPreferenceClaim) bool {
	if !socialInferenceUUID(claim.ID) || !socialInferencePerson(claim.OwnerPrincipal) ||
		claim.Key != SocialPreferenceActivityCategory || !socialPreferenceCategory(claim.Value) ||
		claim.Sensitivity != SocialPreferenceOrdinary || claim.Revision <= 0 ||
		claim.CurrentRevision != claim.Revision || math.IsNaN(claim.Score) || math.IsInf(claim.Score, 0) ||
		len(claim.Sources) == 0 || len(claim.Sources) > 20 {
		return false
	}
	switch claim.Provenance {
	case SocialPreferenceExplicit, SocialPreferenceRuleFact:
		if claim.AssessmentKind != "" || claim.Score != 0 || len(claim.Sources) != 1 {
			return false
		}
	case SocialPreferenceInferred:
		if claim.AssessmentKind != SocialPreferenceUncalibratedScore || claim.Score <= 0 || claim.Score > 1 || len(claim.Sources) < 2 {
			return false
		}
	default:
		return false
	}
	seen := map[string]bool{}
	for _, src := range claim.Sources {
		id := strings.ToLower(src.ID)
		if !socialInferenceUUID(src.ID) || !socialInferencePerson(src.OwnerPrincipal) ||
			!src.OwnerPrincipal.Equal(claim.OwnerPrincipal) || src.Category != claim.Value ||
			src.Sensitivity != SocialPreferenceOrdinary || src.Revision <= 0 || src.Revision != src.CurrentRevision ||
			!socialInferenceUUID(src.ClusterID) || seen[id] {
			return false
		}
		seen[id] = true
		switch claim.Provenance {
		case SocialPreferenceExplicit:
			if src.Type != "USER_STATEMENT" || src.Kind != "EXPLICIT_DECLARATION" || !src.UserConfirmed {
				return false
			}
		case SocialPreferenceRuleFact:
			if src.Type != "SOCIAL_INTENT" || src.Kind != "EXPLICIT_RULE" {
				return false
			}
		case SocialPreferenceInferred:
			if src.Kind != "AUTHORIZED_OBSERVATION" {
				return false
			}
			switch src.Type {
			case "ACTIVITY_PARTICIPATION", "SAVED_PLACE", "MOMENT_CONTEXT":
			default:
				return false
			}
		}
	}
	return true
}

// Digest binds all disclosed candidate content and source/analysis versions.
// It is an approval binding, never an operation ID or an effect idempotency key.
// Explicit structs avoid JSON serialization of authority or private source text.
func SocialPreferencePayloadDigest(claim SocialPreferenceClaim) (string, error) {
	if !socialPreferenceShape(claim) {
		return "", ErrInvalidSocialPreference
	}
	type sourceBinding struct {
		ID, Type, Kind, OwnerID, Category, ClusterID string
		Revision, AnalysisRevision                   int64
		ObservedAt                                   time.Time
		ExpiresAt                                    *time.Time
		AnalysisPurpose                              string
		AnalysisScope                                Scope
		AnalysisExpiresAt                            time.Time
	}
	bindings := make([]sourceBinding, 0, len(claim.Sources))
	for _, s := range claim.Sources {
		var expiresAt *time.Time
		if s.ExpiresAt != nil {
			expiry := s.ExpiresAt.UTC()
			expiresAt = &expiry
		}
		bindings = append(bindings, sourceBinding{strings.ToLower(s.ID), s.Type, s.Kind,
			strings.ToLower(s.OwnerPrincipal.ID), s.Category, strings.ToLower(s.ClusterID), s.Revision, s.AnalysisRevision,
			s.ObservedAt.UTC(), expiresAt, s.AnalysisPurpose, s.AnalysisScope, s.AnalysisExpiresAt.UTC()})
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].ID < bindings[j].ID })
	payload := struct {
		Version, ID, OwnerID, Key, Value, Provenance, AssessmentKind string
		Score                                                        float64
		Revision                                                     int64
		ExpiresAt                                                    time.Time
		Sources                                                      []sourceBinding
	}{
		SocialInferenceVersion, strings.ToLower(claim.ID), strings.ToLower(claim.OwnerPrincipal.ID), claim.Key,
		claim.Value, claim.Provenance, claim.AssessmentKind, claim.Score, claim.Revision, claim.ExpiresAt.UTC(), bindings}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", ErrInvalidSocialPreference
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

// DecideSocialInference proves a pure candidate permission contract only.
// Every real use must resolve these facts again at its trusted boundary. Future
// effects require transactionally ordered confirmation consumption, resource/
// source/consent revalidation and a durable dispatch/effect ledger. This policy
// has no executor and cannot establish that an inferred preference is true.
func DecideSocialInference(now time.Time, req SocialInferenceRequest, facts SocialInferenceFacts) AccessDecision {
	if now.IsZero() || req.Version != SocialInferenceVersion || !socialInferenceUUID(req.RequestID) ||
		!socialInferenceUUID(req.TaskID) || !socialInferenceUUID(req.AgentID) || !socialInferenceUUID(req.ResourceID) ||
		!socialInferencePerson(req.OwnerPrincipal) || !req.ExpiresAt.After(now) || req.ExpiresAt.After(now.Add(SocialInferenceMaxTTL)) {
		return deny("invalid_social_inference_request")
	}
	switch req.Action {
	case SocialActionNavigate, SocialActionRead:
		if req.Scope != Private || req.Purpose != SocialPreferenceReviewPurpose {
			return deny("private_review_required")
		}
	case SocialActionPublish:
		if req.Scope != Public || req.Purpose != SocialPreferencePublishPurpose {
			return deny("explicit_public_purpose_required")
		}
	case SocialActionHighImpact:
		return deny("agent_effects_unavailable")
	default:
		return deny("unknown_social_action")
	}
	session, agent, task, claim, grant := facts.Session, facts.Agent, facts.Task, facts.Claim, facts.Grant
	if !session.Verified || !session.Active || !socialInferenceUUID(session.ID) || !session.ExpiresAt.After(now) || session.ExpiresAt.Before(req.ExpiresAt) ||
		!socialInferenceUUID(session.ActingUserID) || !strings.EqualFold(session.ActingUserID, req.OwnerPrincipal.ID) ||
		!agent.Verified || !agent.Active || !agent.OwnerActive || agent.Role != PersonalAgent ||
		!socialInferenceUUID(agent.ID) || !strings.EqualFold(agent.ID, req.AgentID) ||
		!socialInferencePerson(agent.OwnerPrincipal) || !agent.OwnerPrincipal.Equal(req.OwnerPrincipal) {
		return deny("personal_authority_required")
	}
	if !task.Verified || !socialInferenceUUID(task.ID) || !strings.EqualFold(task.ID, req.TaskID) ||
		!socialInferenceUUID(task.ActingUserID) || !strings.EqualFold(task.ActingUserID, session.ActingUserID) ||
		!socialInferenceUUID(task.AgentID) || !strings.EqualFold(task.AgentID, agent.ID) ||
		!socialInferencePerson(task.OwnerPrincipal) || !task.OwnerPrincipal.Equal(req.OwnerPrincipal) ||
		task.Status != "COMPLETED" || task.Revision <= 0 || task.Revision != task.CurrentRevision {
		return deny("current_owner_task_required")
	}
	if !claim.Verified || !socialPreferenceShape(claim) || !strings.EqualFold(claim.ID, req.ResourceID) ||
		!claim.OwnerPrincipal.Equal(req.OwnerPrincipal) || claim.Deleted || claim.RevokedAt != nil ||
		!claim.ExpiresAt.After(now) || claim.ExpiresAt.Before(req.ExpiresAt) {
		return deny("current_sourced_candidate_required")
	}
	clusters := map[string]bool{}
	for _, src := range claim.Sources {
		if !src.Verified || !src.CurrentlyAuthorized || src.Deleted || src.RevokedAt != nil || src.ObservedAt.IsZero() ||
			src.ObservedAt.After(now) || (src.ExpiresAt != nil && (!src.ExpiresAt.After(now) || src.ExpiresAt.Before(req.ExpiresAt))) {
			return deny("source_unavailable")
		}
		if claim.Provenance == SocialPreferenceInferred {
			if !src.IndependentClusterVerified || !src.AnalysisAuthorized || src.AnalysisPurpose != SocialPreferenceAnalysisPurpose ||
				src.AnalysisScope != Private || src.AnalysisRevision <= 0 || src.AnalysisRevision != src.CurrentAnalysisRevision ||
				!src.AnalysisExpiresAt.After(now) || src.AnalysisExpiresAt.Before(req.ExpiresAt) {
				return deny("independent_analysis_consent_required")
			}
			clusters[strings.ToLower(src.ClusterID)] = true
		}
	}
	if claim.Provenance == SocialPreferenceInferred && len(clusters) < 2 {
		return deny("weak_correlated_evidence")
	}
	digest, err := SocialPreferencePayloadDigest(claim)
	if err != nil || task.Action != req.Action || task.Purpose != req.Purpose || task.Scope != req.Scope ||
		!socialInferenceUUID(task.ResourceID) || !strings.EqualFold(task.ResourceID, req.ResourceID) ||
		task.ResourceRevision != claim.CurrentRevision || task.PayloadDigest != digest {
		return deny("current_task_resource_binding_required")
	}
	if !grant.Verified || !socialInferenceUUID(grant.ID) ||
		!socialInferenceUUID(grant.SessionID) || !strings.EqualFold(grant.SessionID, session.ID) ||
		!socialInferenceUUID(grant.ActorID) || !strings.EqualFold(grant.ActorID, session.ActingUserID) ||
		!socialInferencePerson(grant.OwnerPrincipal) || !grant.OwnerPrincipal.Equal(req.OwnerPrincipal) ||
		!socialInferenceUUID(grant.AgentID) || !strings.EqualFold(grant.AgentID, req.AgentID) ||
		!socialInferenceUUID(grant.RequestID) || !strings.EqualFold(grant.RequestID, req.RequestID) ||
		!socialInferenceUUID(grant.TaskID) || !strings.EqualFold(grant.TaskID, req.TaskID) || grant.TaskRevision != task.CurrentRevision ||
		!socialInferenceUUID(grant.ResourceID) || !strings.EqualFold(grant.ResourceID, req.ResourceID) || grant.ResourceRevision != claim.CurrentRevision ||
		grant.Action != req.Action || grant.Purpose != req.Purpose || grant.Scope != req.Scope || grant.PayloadDigest != digest ||
		grant.Revision <= 0 || grant.CurrentRevision != grant.Revision || !grant.HumanConfirmed || grant.ConfirmedAt.IsZero() ||
		grant.ConfirmedAt.After(now) || !grant.ExpiresAt.After(now) || grant.ExpiresAt.Before(req.ExpiresAt) ||
		!grant.ExpiresAt.After(grant.ConfirmedAt) || grant.RevokedAt != nil {
		return deny("independent_bound_human_consent_required")
	}
	return allow()
}
