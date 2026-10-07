package agentautonomy

import (
	"errors"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentsocialpolicy"
)

func socialFixture(t *testing.T, now time.Time) *agentsocialpolicy.Store {
	t.Helper()
	rules := make([]agentsocialpolicy.Rule, 0, len(agentsocialpolicy.Categories()))
	for _, category := range agentsocialpolicy.Categories() {
		rules = append(rules, agentsocialpolicy.Rule{Category: category, Preference: agentsocialpolicy.ReviewRequired})
	}
	store, err := agentsocialpolicy.NewStore(agentFixture(), agentsocialpolicy.Specification{Rules: rules, ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	return store
}
func offlineFixture(t *testing.T, level Level, operation Operation, now time.Time) (*Store, Request, OfflineView) {
	t.Helper()
	store := storeFixture(t, level, now)
	snapshot := snapshotFixture(t, store)
	social := socialFixture(t, now)
	policy, err := social.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Purpose: EvaluateLimits, RequestID: requestID, OperationID: operationID, Actor: actorref.ActorRef{Type: actorref.Person, ID: ownerID}, Agent: agentFixture(), Operation: operation, SettingsRevision: snapshot.Revision(), Sources: []agentevent.SourceReference{{Type: agentevent.MomentSource, ID: sourceID, Owner: agentFixture().Principal, Version: agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: 2}}}, RequestedAt: now.Add(-time.Minute), ExpiresAt: now.Add(-time.Minute).Add(MaxRequestTTL)}
	descriptor, _ := Lookup(operation)
	if descriptor.NeedsHumanConfirmation {
		request.PreparationVersion = 7
	}
	if descriptor.NeedsSocialPolicy {
		request.SocialPolicyRevision = policy.Revision()
		request.Social = &agentsocialpolicy.Request{Purpose: agentsocialpolicy.EvaluateSocialPolicy, Actor: request.Actor, Agent: request.Agent, Counterparty: actorref.PrincipalRef{Type: actorref.Person, ID: peerID}, OperationID: operationID, Relations: []agentsocialpolicy.Relation{{Category: agentsocialpolicy.UnknownPerson}}, RequestedAt: request.RequestedAt, ExpiresAt: request.ExpiresAt}
	}
	view := OfflineView{CurrentSettings: snapshot, CurrentRequestDigest: RequestDigest(request), CurrentSources: append([]agentevent.SourceReference(nil), request.Sources...), SourcesExpireAt: now.Add(time.Hour), CheckedAt: now, SocialPolicy: policy}
	return store, request, view
}

func TestAutonomyEveryOriginalOperationHasOnlyOfflineLimitResult(t *testing.T) {
	now := time.Now()
	for _, level := range Levels() {
		if level == LevelDelegate {
			continue
		}
		for _, descriptor := range Operations() {
			t.Run(string(level)+"/"+string(descriptor.Operation), func(t *testing.T) {
				store, request, view := offlineFixture(t, level, descriptor.Operation, now)
				result, err := EvaluateOffline(snapshotFixture(t, store), request, view, now)
				if result.Mode() != OfflineMode || result.Authorized() {
					t.Fatal("offline result authorized an operation")
				}
				if descriptor.Operation == TakeAutonomousAction {
					if !errors.Is(err, ErrUnavailable) {
						t.Fatal(err)
					}
					return
				}
				actual, _ := levelRank(level)
				minimum, _ := levelRank(descriptor.MinimumLevel)
				if actual < minimum {
					if !errors.Is(err, ErrDenied) || result.Disposition() != Restricted {
						t.Fatal(err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if descriptor.NeedsHumanConfirmation {
					if result.Disposition() != ConfirmationRequired || result.PreparationVersion() != 7 {
						t.Fatal("concrete draft version missing")
					}
				} else if result.Disposition() != WithinLimit || result.PreparationVersion() != 0 {
					t.Fatal("assist promoted to prepared action")
				}
			})
		}
	}
}

func TestAutonomyRequestMetadataAndTimeBoundariesReject(t *testing.T) {
	now := time.Now()
	cases := map[string]struct {
		modify func(*Request)
		want   error
	}{
		"unknownOperation":        {func(r *Request) { r.Operation = "SEND_MESSAGE" }, ErrInvalid},
		"unknownPurpose":          {func(r *Request) { r.Purpose = "PRIVATE_READ_GRANTED" }, ErrInvalid},
		"badRequestID":            {func(r *Request) { r.RequestID = "?" }, ErrInvalid},
		"badOperationID":          {func(r *Request) { r.OperationID = "00000000-0000-0000-0000-000000000000" }, ErrInvalid},
		"wrongAgent":              {func(r *Request) { r.Agent.AgentID = otherAgentID }, ErrDenied},
		"wrongOwner":              {func(r *Request) { r.Agent.Principal.ID = peerID }, ErrDenied},
		"organizationActor":       {func(r *Request) { r.Actor.Type = actorref.Organization }, ErrDenied},
		"otherActor":              {func(r *Request) { r.Actor.ID = peerID }, ErrDenied},
		"noVersion":               {func(r *Request) { r.SettingsRevision = 0 }, ErrInvalid},
		"staleSettings":           {func(r *Request) { r.SettingsRevision++ }, ErrDenied},
		"noSources":               {func(r *Request) { r.Sources = nil }, ErrInvalid},
		"duplicateSource":         {func(r *Request) { r.Sources = append(r.Sources, r.Sources[0]) }, ErrInvalid},
		"crossSource":             {func(r *Request) { r.Sources[0].Owner.ID = peerID }, ErrInvalid},
		"badSourceID":             {func(r *Request) { r.Sources[0].ID = "?" }, ErrInvalid},
		"reservedVisit":           {func(r *Request) { r.Sources[0].Type = agentevent.VisitSource }, ErrInvalid},
		"inventedMemory":          {func(r *Request) { r.Sources[0].Type = "MEMORY_AUTHORIZED" }, ErrInvalid},
		"zeroRevision":            {func(r *Request) { r.Sources[0].Version.Revision = 0 }, ErrInvalid},
		"sourceVersionKind":       {func(r *Request) { r.Sources[0].Version.Kind = agentevent.NoNativeVersion }, ErrInvalid},
		"requestFuture":           {func(r *Request) { r.RequestedAt = now.Add(time.Second); r.ExpiresAt = r.RequestedAt.Add(MaxRequestTTL) }, ErrExpired},
		"ttlLong":                 {func(r *Request) { r.ExpiresAt = r.ExpiresAt.Add(time.Nanosecond) }, ErrExpired},
		"requestDeadline":         {func(r *Request) { r.RequestedAt = now.Add(-MaxRequestTTL); r.ExpiresAt = now }, ErrExpired},
		"zeroRequestedAt":         {func(r *Request) { r.RequestedAt = time.Time{} }, ErrExpired},
		"prepareVersionInObserve": {func(r *Request) { r.PreparationVersion = 1 }, ErrInvalid},
		"socialOnObserve":         {func(r *Request) { r.SocialPolicyRevision = 1 }, ErrInvalid},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			store, request, view := offlineFixture(t, LevelObserve, Observe, now)
			test.modify(&request)
			view.CurrentRequestDigest = RequestDigest(request)
			result, err := EvaluateOffline(snapshotFixture(t, store), request, view, now)
			if !errors.Is(err, test.want) || result.Authorized() || result.Disposition() != Restricted {
				t.Fatalf("%v %#v", err, result)
			}
		})
	}
}

func TestAutonomyOfflineCurrentSourceAndSettingsNotAuthority(t *testing.T) {
	now := time.Now()
	for name, modify := range map[string]func(*OfflineView){
		"oldRequest":       func(v *OfflineView) { v.CurrentRequestDigest = "old" },
		"sourceEdited":     func(v *OfflineView) { v.CurrentSources[0].Version.Revision++ },
		"sourceWithdrawn":  func(v *OfflineView) { v.CurrentSources = nil },
		"sourceReplaced":   func(v *OfflineView) { v.CurrentSources[0].ID = peerID },
		"sourceCrossOwner": func(v *OfflineView) { v.CurrentSources[0].Owner.ID = peerID },
		"extraSource":      func(v *OfflineView) { v.CurrentSources = append(v.CurrentSources, v.CurrentSources[0]) },
		"zeroSnapshot":     func(v *OfflineView) { v.CurrentSettings = Snapshot{} },
	} {
		t.Run(name, func(t *testing.T) {
			store, request, view := offlineFixture(t, LevelObserve, Observe, now)
			modify(&view)
			result, err := EvaluateOffline(snapshotFixture(t, store), request, view, now)
			if !errors.Is(err, ErrDenied) || result.Authorized() {
				t.Fatal(err)
			}
		})
	}
	for name, modify := range map[string]func(*OfflineView){
		"sourceExpires":     func(v *OfflineView) { v.SourcesExpireAt = now },
		"sourceTimeUnknown": func(v *OfflineView) { v.SourcesExpireAt = time.Time{} },
		"staleCheck":        func(v *OfflineView) { v.CheckedAt = now.Add(-time.Nanosecond) },
		"futureCheck":       func(v *OfflineView) { v.CheckedAt = now.Add(time.Nanosecond) },
	} {
		t.Run(name, func(t *testing.T) {
			store, request, view := offlineFixture(t, LevelObserve, Observe, now)
			modify(&view)
			if _, err := EvaluateOffline(snapshotFixture(t, store), request, view, now); !errors.Is(err, ErrExpired) {
				t.Fatal(err)
			}
		})
	}
	store, request, view := offlineFixture(t, LevelObserve, Observe, now)
	old := snapshotFixture(t, store)
	if e := store.Revoke(old.Revision()); e != nil {
		t.Fatal(e)
	}
	if _, e := EvaluateOffline(old, request, view, now); !errors.Is(e, ErrDenied) {
		t.Fatal("old snapshot after revoke reused")
	}
}

func TestAutonomyPreparationVersionAndSocialPreferenceAreNotApproval(t *testing.T) {
	now := time.Now()
	for _, operation := range []Operation{DraftResponse, PrepareInvitation, PrepareRegistration, SuggestMeeting} {
		t.Run(string(operation), func(t *testing.T) {
			store, request, view := offlineFixture(t, LevelPrepare, operation, now)
			request.PreparationVersion = 0
			view.CurrentRequestDigest = RequestDigest(request)
			if _, e := EvaluateOffline(snapshotFixture(t, store), request, view, now); !errors.Is(e, ErrInvalid) {
				t.Fatal("missing concrete version accepted")
			}
			request.PreparationVersion = 9
			view.CurrentRequestDigest = RequestDigest(request)
			result, e := EvaluateOffline(snapshotFixture(t, store), request, view, now)
			if e != nil || result.Authorized() || result.PreparationVersion() != 9 || result.Disposition() != ConfirmationRequired {
				t.Fatal(e)
			}
		})
	}
	store, request, view := offlineFixture(t, LevelPrepare, PrepareInvitation, now)
	policyStore := socialFixture(t, now)
	spec := agentsocialpolicy.Specification{ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	if e := policyStore.Replace(1, spec); e != nil {
		t.Fatal(e)
	}
	policy, _ := policyStore.Snapshot()
	request.SocialPolicyRevision = policy.Revision()
	view.SocialPolicy = policy
	view.CurrentRequestDigest = RequestDigest(request)
	if _, e := EvaluateOffline(snapshotFixture(t, store), request, view, now); !errors.Is(e, ErrDenied) {
		t.Fatal("prepare bypassed social disabled preference")
	}
	store, request, view = offlineFixture(t, LevelObserve, PrepareInvitation, now)
	if _, e := EvaluateOffline(snapshotFixture(t, store), request, view, now); !errors.Is(e, ErrDenied) {
		t.Fatal("social REVIEW_REQUIRED promoted autonomy ceiling")
	}
}

func TestAutonomyNativeVersionShapesAndDigestBindExactRequest(t *testing.T) {
	now := time.Now()
	store, request, view := offlineFixture(t, LevelObserve, Observe, now)
	for _, sourceType := range []agentevent.SourceType{agentevent.QuerySource, agentevent.ParticipationSource, agentevent.CommunityMembershipSource, agentevent.ProfileSource, agentevent.SavedPlaceSource, agentevent.PrivatePreferenceSource} {
		t.Run(string(sourceType), func(t *testing.T) {
			r := request
			r.Sources = append([]agentevent.SourceReference(nil), request.Sources...)
			r.Sources[0].Type = sourceType
			kind := agentevent.UpdatedAtDigestVersion
			if sourceType == agentevent.SavedPlaceSource {
				kind = agentevent.CreatedAtDigestVersion
			}
			if sourceType == agentevent.PrivatePreferenceSource {
				r.Sources[0].Version = agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: 2}
			} else {
				r.Sources[0].Version = agentevent.SourceVersion{Kind: kind, Token: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"}
			}
			v := view
			v.CurrentSources = append([]agentevent.SourceReference(nil), r.Sources...)
			v.CurrentRequestDigest = RequestDigest(r)
			result, e := EvaluateOffline(snapshotFixture(t, store), r, v, now)
			if e != nil || result.Authorized() {
				t.Fatal(e)
			}
			r.Sources[0].Version.Token = "private body is not a source token"
			if _, e := EvaluateOffline(snapshotFixture(t, store), r, v, now); !errors.Is(e, ErrInvalid) {
				t.Fatal("bad native token accepted")
			}
		})
	}
	base := RequestDigest(request)
	for name, mutate := range map[string]func(*Request){
		"requestID": func(r *Request) { r.RequestID = peerID }, "operationID": func(r *Request) { r.OperationID = peerID },
		"operation": func(r *Request) { r.Operation = Understand }, "sourceVersion": func(r *Request) { r.Sources[0].Version.Revision++ },
		"settingsRevision": func(r *Request) { r.SettingsRevision++ }, "expiry": func(r *Request) { r.ExpiresAt = r.ExpiresAt.Add(time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			copyRequest := request
			copyRequest.Sources = append([]agentevent.SourceReference(nil), request.Sources...)
			mutate(&copyRequest)
			if RequestDigest(copyRequest) == base {
				t.Fatal("digest did not bind change")
			}
		})
	}
}

func TestAutonomySourceBoundsDigestCanonicalOrderAndIndependentVersions(t *testing.T) {
	now := time.Now()
	store, request, view := offlineFixture(t, LevelObserve, Observe, now)
	first := request.Sources[0]
	second := first
	second.ID = peerID
	request.Sources = []agentevent.SourceReference{first, second}
	reordered := request
	reordered.Sources = []agentevent.SourceReference{second, first}
	if RequestDigest(request) != RequestDigest(reordered) {
		t.Fatal("source order became a new request identity")
	}
	view.CurrentSources = []agentevent.SourceReference{second, first}
	view.CurrentRequestDigest = RequestDigest(request)
	if result, e := EvaluateOffline(snapshotFixture(t, store), request, view, now); e != nil || result.Authorized() {
		t.Fatal(e)
	}
	request.Sources = make([]agentevent.SourceReference, MaxSources+1)
	for i := range request.Sources {
		request.Sources[i] = first
	}
	if _, e := EvaluateOffline(snapshotFixture(t, store), request, view, now); !errors.Is(e, ErrInvalid) {
		t.Fatal("unbounded sources")
	}
	for _, version := range []agentevent.SourceVersion{
		{Kind: agentevent.UpdatedAtDigestVersion, Token: "ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef0123456789"},
		{Kind: agentevent.UpdatedAtDigestVersion, Token: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", Revision: 1},
		{Kind: agentevent.UpdatedAtDigestVersion, Token: "gggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggg"},
		{Kind: agentevent.UpdatedAtDigestVersion},
	} {
		source := first
		source.Type = agentevent.ProfileSource
		source.Version = version
		if validSource(source, agentFixture().Principal) {
			t.Fatal("invalid native digest")
		}
	}
	preference := first
	preference.Type = agentevent.PrivatePreferenceSource
	preference.Version.Revision = 1
	if validSource(preference, agentFixture().Principal) {
		t.Fatal("metadata initial version fabricated as preferences source")
	}
	preference.Version.Revision = 2
	if !validSource(preference, agentFixture().Principal) {
		t.Fatal("native preference shape rejected")
	}
}

func TestAutonomyReusesStrict041SocialRequestShapeWithoutAllowedFacts(t *testing.T) {
	now := time.Now()
	for name, modify := range map[string]func(*Request){
		"missingSocial":         func(r *Request) { r.Social = nil },
		"missingSocialRevision": func(r *Request) { r.SocialPolicyRevision = 0 },
		"wrongSocialActor":      func(r *Request) { r.Social.Actor.ID = peerID },
		"wrongSocialAgent":      func(r *Request) { r.Social.Agent.AgentID = otherAgentID },
		"wrongSocialOperation":  func(r *Request) { r.Social.OperationID = peerID },
		"wrongSocialPurpose":    func(r *Request) { r.Social.Purpose = "APPROVED_ACTION" },
		"wrongSocialTime":       func(r *Request) { r.Social.RequestedAt = r.Social.RequestedAt.Add(time.Second) },
		"unknownCategory":       func(r *Request) { r.Social.Relations[0].Category = "MODEL_CONFIRMED_FRIEND" },
		"duplicateCategory":     func(r *Request) { r.Social.Relations = append(r.Social.Relations, r.Social.Relations[0]) },
		"unknownMixedWithKnown": func(r *Request) {
			r.Social.Relations = append(r.Social.Relations, agentsocialpolicy.Relation{Category: agentsocialpolicy.ExistingConnection})
		},
		"missingRelationshipSource": func(r *Request) { r.Social.Relations[0].Category = agentsocialpolicy.SameUniversity },
		"sameOwnerRecipient":        func(r *Request) { r.Social.Counterparty = r.Agent.Principal },
		"unknownRecipient":          func(r *Request) { r.Social.Counterparty.ID = "?" },
		"wrongOrganizationCategory": func(r *Request) { r.Social.Counterparty.Type = actorref.Organization },
		"communityNotAgent":         func(r *Request) { r.Social.Counterparty.Type = "COMMUNITY" },
	} {
		t.Run(name, func(t *testing.T) {
			store, request, view := offlineFixture(t, LevelPrepare, PrepareInvitation, now)
			modify(&request)
			view.CurrentRequestDigest = RequestDigest(request)
			result, e := EvaluateOffline(snapshotFixture(t, store), request, view, now)
			if !errors.Is(e, ErrInvalid) || result.Authorized() {
				t.Fatalf("%v %#v", e, result)
			}
		})
	}
	store, request, view := offlineFixture(t, LevelPrepare, DraftResponse, now)
	digest := RequestDigest(request)
	request.PreparationVersion++
	if RequestDigest(request) == digest {
		t.Fatal("draft version not bound")
	}
	if _, e := EvaluateOffline(snapshotFixture(t, store), request, view, now); !errors.Is(e, ErrDenied) {
		t.Fatal("old comparison digest reused for new draft")
	}
	request.SocialPolicyRevision++
	view.CurrentRequestDigest = RequestDigest(request)
	if _, e := EvaluateOffline(snapshotFixture(t, store), request, view, now); !errors.Is(e, ErrDenied) {
		t.Fatal("social settings revision confused with autonomy/source version")
	}
}
