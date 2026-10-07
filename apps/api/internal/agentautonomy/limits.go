package agentautonomy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentsocialpolicy"
)

const EvaluateLimits = "AUTONOMY_LIMIT_EVALUATION"
const OfflineMode = "OFFLINE_LIMIT_ONLY_NOT_AUTHORIZATION"

// Request contains metadata references, not source content or authorization.
// PreparationVersion only describes a proposed draft version. It is not a
// human approval receipt and Service cannot produce Prepared or success.
type Request struct {
	Purpose              string
	RequestID            string
	OperationID          string
	Actor                actorref.ActorRef
	Agent                agentcognitive.AgentReference
	Operation            Operation
	SettingsRevision     uint64
	PreparationVersion   uint64
	Sources              []agentevent.SourceReference
	RequestedAt          time.Time
	ExpiresAt            time.Time
	SocialPolicyRevision uint64
	Social               *agentsocialpolicy.Request
}

func (Request) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (r *Request) UnmarshalJSON([]byte) error {
	if r != nil {
		*r = Request{}
	}
	return ErrServerOnly
}

func validSource(source agentevent.SourceReference, owner actorref.PrincipalRef) bool {
	if !validID(source.ID) || source.Owner != owner {
		return false
	}
	var kind agentevent.VersionKind
	switch source.Type {
	case agentevent.MomentSource, agentevent.PrivatePreferenceSource:
		kind = agentevent.RevisionVersion
	case agentevent.QuerySource, agentevent.ParticipationSource, agentevent.CommunityMembershipSource, agentevent.ProfileSource:
		kind = agentevent.UpdatedAtDigestVersion
	case agentevent.SavedPlaceSource:
		kind = agentevent.CreatedAtDigestVersion
	default:
		return false
	}
	if source.Version.Kind != kind {
		return false
	}
	if kind == agentevent.RevisionVersion {
		minimum := int64(1)
		if source.Type == agentevent.PrivatePreferenceSource {
			minimum = 2 // Original preference event contract starts after metadata v1.
		}
		return source.Version.Revision >= minimum && source.Version.Token == ""
	}
	if source.Version.Revision != 0 || len(source.Version.Token) != sha256.Size*2 || strings.ToLower(source.Version.Token) != source.Version.Token {
		return false
	}
	decoded, err := hex.DecodeString(source.Version.Token)
	return err == nil && len(decoded) == sha256.Size
}

func validateRequest(snapshot Snapshot, request Request, now time.Time) error {
	descriptor, known := Lookup(request.Operation)
	if request.Purpose != EvaluateLimits || !known || !validTime(now) || !validAgent(request.Agent) || !validAgent(snapshot.agent) ||
		!validID(request.RequestID) || !validID(request.OperationID) || request.SettingsRevision == 0 || snapshot.revision == 0 ||
		len(request.Sources) == 0 || len(request.Sources) > MaxSources {
		return ErrInvalid
	}
	if request.Agent != snapshot.agent || request.Actor.Type != actorref.Person || request.Actor.ID != snapshot.agent.Principal.ID ||
		request.SettingsRevision != snapshot.revision || snapshot.revoked {
		return ErrDenied
	}
	if !validTime(request.RequestedAt) || !validTime(request.ExpiresAt) || request.RequestedAt.After(now) ||
		!request.ExpiresAt.Equal(request.RequestedAt.Add(MaxRequestTTL)) || !request.ExpiresAt.After(now) ||
		now.Before(snapshot.spec.ValidFrom) || !now.Before(snapshot.spec.ExpiresAt) {
		return ErrExpired
	}
	if descriptor.NeedsHumanConfirmation != (request.PreparationVersion > 0) {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, source := range request.Sources {
		key := string(source.Type) + ":" + source.ID
		if !validSource(source, snapshot.agent.Principal) || seen[key] {
			return ErrInvalid
		}
		seen[key] = true
	}
	if descriptor.NeedsSocialPolicy {
		if request.Social == nil || request.SocialPolicyRevision == 0 {
			return ErrInvalid
		}
		social := request.Social
		if social.Agent != request.Agent || social.Actor != request.Actor || social.OperationID != request.OperationID ||
			social.Purpose != agentsocialpolicy.EvaluateSocialPolicy || !social.RequestedAt.Equal(request.RequestedAt) || !social.ExpiresAt.Equal(request.ExpiresAt) ||
			len(social.Relations) == 0 || len(social.Relations) > len(agentsocialpolicy.Categories()) {
			return ErrInvalid
		}
		counterparty, err := actorref.ParsePrincipal(string(social.Counterparty.Type), social.Counterparty.ID)
		if err != nil || counterparty != social.Counterparty || !validID(counterparty.ID) || counterparty == request.Agent.Principal {
			return ErrInvalid
		}
	} else if request.Social != nil || request.SocialPolicyRevision != 0 {
		return ErrInvalid
	}
	return nil
}

// Digest binds only described metadata. It is not an effect key or approval.
func RequestDigest(request Request) string {
	sources := append([]agentevent.SourceReference(nil), request.Sources...)
	sort.Slice(sources, func(i, j int) bool {
		a, b := sources[i], sources[j]
		if a.Type == b.Type {
			return a.ID < b.ID
		}
		return a.Type < b.Type
	})
	socialDigest := ""
	if request.Social != nil {
		socialDigest = agentsocialpolicy.RequestDigest(*request.Social)
	}
	data, _ := json.Marshal(struct {
		Purpose, RequestID, OperationID                            string
		Actor                                                      actorref.ActorRef
		Agent                                                      agentcognitive.AgentReference
		Operation                                                  Operation
		SettingsRevision, PreparationVersion, SocialPolicyRevision uint64
		Sources                                                    []agentevent.SourceReference
		RequestedAt, ExpiresAt                                     time.Time
		SocialDigest                                               string
	}{request.Purpose, request.RequestID, request.OperationID, request.Actor, request.Agent, request.Operation, request.SettingsRevision,
		request.PreparationVersion, request.SocialPolicyRevision, sources, request.RequestedAt.UTC(), request.ExpiresAt.UTC(), socialDigest})
	sum := sha256.Sum256(append([]byte("birdtie.autonomy.limits.request.v1\x00"), data...))
	return hex.EncodeToString(sum[:])
}

// OfflineView is a caller-supplied comparison fixture ONLY. It is not consumed
// by Service and cannot stand in for a current source/purpose/approval resolver.
type OfflineView struct {
	CurrentSettings      Snapshot
	CurrentRequestDigest string
	CurrentSources       []agentevent.SourceReference
	SourcesExpireAt      time.Time
	CheckedAt            time.Time
	SocialPolicy         agentsocialpolicy.Policy
}

func (OfflineView) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (v *OfflineView) UnmarshalJSON([]byte) error {
	if v != nil {
		*v = OfflineView{}
	}
	return ErrServerOnly
}

type Disposition string

const (
	WithinLimit          Disposition = "WITHIN_CONFIGURED_LIMIT"
	ConfirmationRequired Disposition = "DRAFT_VERSION_REQUIRES_HUMAN_CONFIRMATION"
	Restricted           Disposition = "RESTRICTED"
)

// Assessment is internal/offline, not a product DTO or a prepared action. Its
// Authorization method always returns false, even for a within-limit result.
type Assessment struct {
	disposition     Disposition
	reason          string
	settingRevision uint64
	draftVersion    uint64
}

func (Assessment) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (a *Assessment) UnmarshalJSON([]byte) error {
	if a != nil {
		*a = Assessment{}
	}
	return ErrServerOnly
}
func (Assessment) Mode() string                 { return OfflineMode }
func (Assessment) Authorized() bool             { return false }
func (a Assessment) Disposition() Disposition   { return a.disposition }
func (a Assessment) Reason() string             { return a.reason }
func (a Assessment) SettingsRevision() uint64   { return a.settingRevision }
func (a Assessment) PreparationVersion() uint64 { return a.draftVersion }

func sameSources(expected, current []agentevent.SourceReference) bool {
	if len(expected) != len(current) {
		return false
	}
	used := make([]bool, len(current))
	for _, source := range expected {
		found := false
		for i, candidate := range current {
			if !used[i] && candidate == source {
				used[i], found = true, true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func checkSocialPolicy(policy agentsocialpolicy.Policy, request Request, now time.Time) error {
	if policy.Agent() != request.Agent || policy.Revision() == 0 || policy.Revoked() {
		return ErrDenied
	}
	spec := policy.Specification()
	if !validTime(spec.ValidFrom) || !validTime(spec.ExpiresAt) || now.Before(spec.ValidFrom) || !now.Before(spec.ExpiresAt) {
		return ErrExpired
	}
	if request.Social == nil {
		return nil
	}
	if policy.Revision() != request.SocialPolicyRevision {
		return ErrDenied
	}
	// Reuse 041's public strict request validation through its explicitly
	// unavailable offline boundary. No allowed/verified/consent facts are
	// supplied. Unavailable is expected and only confirms metadata shape;
	// this call never serves as current source proof or actual authorization.
	_, shapeErr := agentsocialpolicy.EvaluateOffline(policy, *request.Social,
		agentsocialpolicy.OfflineBoundary{State: agentsocialpolicy.BoundaryUnavailable}, now)
	if shapeErr != agentsocialpolicy.ErrUnavailable {
		if shapeErr == agentsocialpolicy.ErrExpired {
			return ErrExpired
		}
		if shapeErr == agentsocialpolicy.ErrInvalid {
			return ErrInvalid
		}
		return ErrDenied
	}
	seen := map[agentsocialpolicy.Category]bool{}
	for _, relation := range request.Social.Relations {
		preference, err := policy.Preference(relation.Category)
		if err != nil || seen[relation.Category] {
			return ErrInvalid
		}
		seen[relation.Category] = true
		if preference != agentsocialpolicy.ReviewRequired {
			return ErrDenied
		}
	}
	return nil
}

// EvaluateOffline assesses a configured ceiling only. Synthetic currentSources
// are not source proof; even all checks passing can never grant an operation.
func EvaluateOffline(snapshot Snapshot, request Request, view OfflineView, now time.Time) (Assessment, error) {
	restricted := Assessment{Restricted, "配置比较未通过；不能读取、准备或执行。", snapshot.revision, 0}
	if err := validateRequest(snapshot, request, now); err != nil {
		return restricted, err
	}
	if snapshot.origin == nil || !snapshot.origin.Current(snapshot, now) || view.CurrentSettings != snapshot ||
		view.CurrentRequestDigest != RequestDigest(request) || !sameSources(request.Sources, view.CurrentSources) {
		return restricted, ErrDenied
	}
	if !validTime(view.CheckedAt) || !view.CheckedAt.Equal(now) || !validTime(view.SourcesExpireAt) || !view.SourcesExpireAt.After(now) {
		return restricted, ErrExpired
	}
	if err := checkSocialPolicy(view.SocialPolicy, request, now); err != nil {
		return restricted, err
	}
	if request.Operation == TakeAutonomousAction {
		return restricted, ErrUnavailable
	}
	actual, _ := levelRank(snapshot.spec.Level)
	descriptor, _ := Lookup(request.Operation)
	minimum, _ := levelRank(descriptor.MinimumLevel)
	if actual < minimum {
		return restricted, ErrDenied
	}
	if descriptor.NeedsHumanConfirmation {
		return Assessment{ConfirmationRequired, "仅比较草稿版本上限；该版本必须本人确认，尚无批准或提交。", snapshot.revision, request.PreparationVersion}, nil
	}
	return Assessment{WithinLimit, "仅在配置范围内；没有当前来源用途授权，不能读取或执行。", snapshot.revision, 0}, nil
}
