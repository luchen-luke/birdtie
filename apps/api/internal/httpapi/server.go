package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activeintent"
	"github.com/birdtie/birdtie/apps/api/internal/activitychat"
	"github.com/birdtie/birdtie/apps/api/internal/activityparticipation"
	"github.com/birdtie/birdtie/apps/api/internal/activityplan"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentcandidatepipeline"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/agentmulticandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotificationschedule"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/birdtie/birdtie/apps/api/internal/agentseed"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/analytics"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/birdtie/birdtie/apps/api/internal/communitychat"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/birdtie/birdtie/apps/api/internal/devauth"
	"github.com/birdtie/birdtie/apps/api/internal/follow"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	"github.com/birdtie/birdtie/apps/api/internal/intent"
	"github.com/birdtie/birdtie/apps/api/internal/intentconversion"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"github.com/birdtie/birdtie/apps/api/internal/nowcontextselection"
	"github.com/birdtie/birdtie/apps/api/internal/oidcauth"
	"github.com/birdtie/birdtie/apps/api/internal/opportunity"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/birdtie/birdtie/apps/api/internal/placematch"
	"github.com/birdtie/birdtie/apps/api/internal/relationshipcontext"
	"github.com/birdtie/birdtie/apps/api/internal/safety"
	"github.com/birdtie/birdtie/apps/api/internal/saved"
	"github.com/birdtie/birdtie/apps/api/internal/socialcontext"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/birdtie/birdtie/apps/api/internal/venue"
)

type pinger interface {
	Ping(context.Context) error
}

type server struct {
	sandboxRecovery        aa.HumanRecoveryGateway
	humanIntentConversions intentconversion.Gateway
	catalog                foundation.PublicCatalog
	access                 identity.AccessStore
	humanProfiles          identity.HumanProfileStore
	agentSeeds             agentseed.HumanStore
	seed                   cityseed.Store
	content                content.MomentStore
	agent                  agentworkspace.Store
	communities            community.Store
	socialCommunities      community.SocialStore
	inbox                  inbox.Store
	intents                intent.Store
	socialIntents          socialintent.Store
	opportunities          opportunity.Store
	placeMatches           placematch.Store
	venues                 venue.Store
	saved                  saved.Store
	activityPlans          activityplan.Store
	participations         activityparticipation.Store
	connections            connection.Store
	devPhone               devauth.Store
	devPhoneEnabled        bool
	oidc                   *oidcauth.Service
	db                     pinger
	organizations          organization.Store
	memberships            organization.MembershipStore
	mapLocations           organization.MapLocationStore
	faqs                   organization.FAQStore
	activityPublish        activitypublish.Store
	socialActivityPublish  activitypublish.SocialStore
	analytics              analytics.Store
	safety                 safety.Store
	contextDeclarations    contextgraph.DeclarationStore
	follows                follow.Store
	socialContext          socialcontext.Store
	activityChat           activitychat.Store
	communityChat          communitychat.Store
	relationshipContext    relationshipcontext.Store
	newPeople              newpeople.Store
	privateProfiles        privateAgentProfileStore
	profileVisibility      agentProfileVisibilityStore
	memories               agentmemory.Store
	memoryEvidence         agentmemory.EvidenceStore
	notificationPolicies   agentnotification.Store
	notificationSchedules  agentnotificationschedule.Store
	policySettings         agentpolicysettings.Store
	messagePolicies        agentmessagepolicy.Store
	memoryCandidates       agentmemorycandidate.HumanGateway
	candidatePipeline      agentcandidatepipeline.Gateway
	multiCandidates        agentmulticandidate.Gateway
	humanActiveIntents     activeintent.Gateway
	agentRuns              agentrun.Gateway
	nowContextSelection    nowcontextselection.Gateway
	liveAnswers            agentworkspace.LiveAnswers
}

// Options are trusted startup dependencies, never client feature overrides.
type Option func(*server)

func WithSandboxRecovery(gateway aa.HumanRecoveryGateway) Option {
	return func(s *server) { s.sandboxRecovery = gateway }
}

func WithMemoryCandidates(gateway agentmemorycandidate.HumanGateway) Option {
	return func(s *server) { s.memoryCandidates = gateway }
}

func WithCandidatePipeline(gateway agentcandidatepipeline.Gateway) Option {
	return func(s *server) { s.candidatePipeline = gateway }
}

func WithMultiCandidates(gateway agentmulticandidate.Gateway) Option {
	return func(s *server) { s.multiCandidates = gateway }
}

func WithHumanIntentConversions(g intentconversion.Gateway) Option {
	return func(s *server) { s.humanIntentConversions = g }
}
func WithHumanActiveIntents(gateway activeintent.Gateway) Option {
	return func(s *server) { s.humanActiveIntents = gateway }
}

func WithAgentRuns(gateway agentrun.Gateway) Option {
	return func(s *server) { s.agentRuns = gateway }
}

func WithNowContextSelection(gateway nowcontextselection.Gateway) Option {
	return func(s *server) { s.nowContextSelection = gateway }
}

var uuidPath = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func New(catalog foundation.PublicCatalog, access identity.AccessStore, seed cityseed.Store, contentStore content.MomentStore, agentStore agentworkspace.Store, communityStore community.Store, inboxStore inbox.Store, intentStore intent.Store, savedStore saved.Store, activityPlans activityplan.Store, connectionStore connection.Store, devPhoneStore devauth.Store, devPhoneEnabled bool, oidc *oidcauth.Service, db pinger, allowedOrigins []string, options ...Option) http.Handler {
	orgStore, _ := catalog.(organization.Store)
	membershipStore, _ := catalog.(organization.MembershipStore)
	mapLocationStore, _ := catalog.(organization.MapLocationStore)
	faqStore, _ := catalog.(organization.FAQStore)
	publishStore, _ := catalog.(activitypublish.Store)
	socialPublishStore, _ := catalog.(activitypublish.SocialStore)
	participationStore, _ := catalog.(activityparticipation.Store)
	analyticsStore, _ := catalog.(analytics.Store)
	safetyStore, _ := catalog.(safety.Store)
	socialCommunityStore, _ := communityStore.(community.SocialStore)
	socialIntentStore, _ := intentStore.(socialintent.Store)
	opportunityStore, _ := catalog.(opportunity.Store)
	placeMatchStore, _ := catalog.(placematch.Store)
	venueStore, _ := catalog.(venue.Store)
	s := &server{catalog: catalog, access: access, seed: seed, content: contentStore, agent: agentStore, communities: communityStore, inbox: inboxStore, intents: intentStore, saved: savedStore, activityPlans: activityPlans, participations: participationStore, connections: connectionStore, devPhone: devPhoneStore, devPhoneEnabled: devPhoneEnabled, oidc: oidc, db: db, organizations: orgStore, memberships: membershipStore, mapLocations: mapLocationStore, faqs: faqStore, activityPublish: publishStore, analytics: analyticsStore, safety: safetyStore}
	s.socialCommunities = socialCommunityStore
	s.socialIntents = socialIntentStore
	s.opportunities = opportunityStore
	s.placeMatches = placeMatchStore
	s.venues = venueStore
	s.contextDeclarations, _ = catalog.(contextgraph.DeclarationStore)
	s.follows, _ = catalog.(follow.Store)
	s.socialContext, _ = catalog.(socialcontext.Store)
	s.relationshipContext, _ = catalog.(relationshipcontext.Store)
	s.newPeople, _ = catalog.(newpeople.Store)
	s.privateProfiles, _ = catalog.(privateAgentProfileStore)
	s.humanProfiles, _ = access.(identity.HumanProfileStore)
	s.agentSeeds, _ = catalog.(agentseed.HumanStore)
	s.profileVisibility, _ = catalog.(agentProfileVisibilityStore)
	s.memories, _ = catalog.(agentmemory.Store)
	s.memoryEvidence, _ = catalog.(agentmemory.EvidenceStore)
	s.notificationPolicies, _ = catalog.(agentnotification.Store)
	s.notificationSchedules, _ = catalog.(agentnotificationschedule.Store)
	s.policySettings, _ = catalog.(agentpolicysettings.Store)
	s.messagePolicies, _ = connectionStore.(agentmessagepolicy.Store)
	if s.messagePolicies == nil {
		s.messagePolicies, _ = catalog.(agentmessagepolicy.Store)
	}
	s.activityChat, _ = catalog.(activitychat.Store)
	s.communityChat, _ = communityStore.(communitychat.Store)
	s.socialActivityPublish = socialPublishStore
	for _, option := range options {
		if option != nil {
			option(s)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/me/agent-sandbox-approvals/{approvalID}/reconcile", s.recoverOwnSandboxApproval)
	mux.HandleFunc("POST /v1/me/moments/{momentID}/private-image-previews", s.previewPrivateMomentImage)
	mux.HandleFunc("PUT /v1/me/moments/{momentID}/private-image-previews/{imageID}/content", s.savePrivateMomentImage)
	mux.HandleFunc("GET /v1/me/moments/{momentID}/private-image-previews/{imageID}", s.readPrivateMomentImageReceipt)
	mux.HandleFunc("GET /v1/me/moments/{momentID}/private-image-operations/{imageID}", s.readPrivateMomentImageOperation)
	mux.HandleFunc("GET /v1/me/moments/{momentID}/private-images", s.readPrivateMomentImages)
	mux.HandleFunc("GET /v1/me/moments/{momentID}/private-images/{imageID}/content", s.readPrivateMomentImageBytes)
	mux.HandleFunc("DELETE /v1/me/moments/{momentID}/private-images/{imageID}", s.deletePrivateMomentImage)
	mux.HandleFunc("GET /v1/businesses/{businessID}", s.getPublicBusiness)
	mux.HandleFunc("GET /v1/me/entity-actions/{entityType}/{entityID}", s.getEntityActions)
	mux.HandleFunc("GET /v1/public/entity-actions/{entityType}/{entityID}", s.getPublicEntityActions)
	mux.HandleFunc("GET /v1/me/businesses/{businessID}/public-profile/permission", s.getBusinessPublicPermission)
	mux.HandleFunc("PUT /v1/me/businesses/{businessID}/public-profile/permission", s.changeBusinessPublicPermission)
	mux.HandleFunc("GET /v1/me/agent-seed", s.getOwnAgentSeed)
	mux.HandleFunc("PUT /v1/me/agent-seed", s.saveOwnAgentSeed)
	mux.HandleFunc("GET /v1/me/agent-policies", s.getOwnAgentPolicies)
	mux.HandleFunc("GET /v1/me/message-request-policy", s.getOwnMessagePolicy)
	mux.HandleFunc("PUT /v1/me/message-request-policy", s.putOwnMessagePolicy)
	mux.HandleFunc("GET /v1/me/message-request-policy/decisions/{personID}", s.getMessagePolicyDecision)
	mux.HandleFunc("PUT /v1/me/agent-policies/attention", func(w http.ResponseWriter, r *http.Request) { s.putOwnAgentPolicy(w, r, agentpolicysettings.Attention) })
	mux.HandleFunc("PUT /v1/me/agent-policies/social", func(w http.ResponseWriter, r *http.Request) { s.putOwnAgentPolicy(w, r, agentpolicysettings.Social) })
	mux.HandleFunc("PUT /v1/me/agent-policies/autonomy", func(w http.ResponseWriter, r *http.Request) { s.putOwnAgentPolicy(w, r, agentpolicysettings.Autonomy) })
	mux.HandleFunc("GET /v1/me/notification-policy", s.getOwnNotificationPolicy)
	mux.HandleFunc("PUT /v1/me/notification-policy", s.putOwnNotificationPolicy)
	mux.HandleFunc("GET /v1/me/notification-schedule", s.getOwnNotificationSchedule)
	mux.HandleFunc("PUT /v1/me/notification-schedule", s.putOwnNotificationSchedule)
	mux.HandleFunc("POST /v1/me/agent-candidate-pipeline/stage", s.stageOwnMomentCandidate)
	mux.HandleFunc("GET /v1/me/agent-candidate-pipeline/grants/{grantID}/receipt", s.getOwnMomentCandidateReceipt)
	mux.HandleFunc("POST /v1/me/agent-multi-candidates/previews", s.previewOwnMultiCandidate)
	mux.HandleFunc("GET /v1/me/agent-multi-candidates/previews/{previewID}", s.readOwnMultiCandidatePreview)
	mux.HandleFunc("GET /v1/me/agent-multi-candidates/previews/{previewID}/receipt", s.readOwnMultiCandidatePreviewReceipt)
	mux.HandleFunc("POST /v1/me/agent-multi-candidates/previews/{previewID}/approve", s.approveOwnMultiCandidate)
	mux.HandleFunc("GET /v1/me/agent-multi-candidates/grants/{grantID}", s.readOwnMultiCandidateGrant)
	mux.HandleFunc("DELETE /v1/me/agent-multi-candidates/grants/{grantID}", s.revokeOwnMultiCandidate)
	mux.HandleFunc("POST /v1/me/agent-multi-candidates/stage", s.stageOwnMultiCandidate)
	mux.HandleFunc("GET /v1/me/agent-multi-candidates/grants/{grantID}/receipt", s.readOwnMultiCandidateReceipt)
	mux.HandleFunc("GET /v1/me/agent-memories", s.listOwnAgentMemories)
	mux.HandleFunc("GET /v1/me/agent-memories/{memoryID}", s.getOwnAgentMemoryDetail)
	mux.HandleFunc("POST /v1/me/agent-memories/{memoryID}/reject", s.rejectOwnAgentMemory)
	mux.HandleFunc("POST /v1/me/agent-memory-corrections/previews", s.previewOwnMemoryCorrection)
	mux.HandleFunc("POST /v1/me/agent-memory-corrections/{operationID}/confirm", s.confirmOwnMemoryCorrection)
	mux.HandleFunc("GET /v1/me/agent-memory-corrections/{operationID}", s.readOwnMemoryCorrection)
	mux.HandleFunc("GET /v1/me/agent-memory-candidates", s.listOwnMemoryCandidates)
	mux.HandleFunc("POST /v1/me/agent-memory-candidates", s.saveOwnMemoryCandidate)
	mux.HandleFunc("GET /v1/me/agent-memory-candidates/{candidateID}", s.getOwnMemoryCandidate)
	mux.HandleFunc("POST /v1/me/agent-memory-candidates/{candidateID}/preview", s.previewOwnMemoryCandidate)
	mux.HandleFunc("POST /v1/me/agent-memory-candidates/{candidateID}/accept", s.acceptOwnMemoryCandidate)
	mux.HandleFunc("POST /v1/me/agent-memory-candidates/{candidateID}/reject", s.rejectOwnMemoryCandidate)
	mux.HandleFunc("GET /v1/me/places/{placeID}/memory", s.getOwnPlaceMemory)
	mux.HandleFunc("GET /v1/me/places/{placeID}/declarations", s.getOwnPlaceDeclarationControls)
	mux.HandleFunc("PUT /v1/me/place-declarations/{memoryID}", s.putOwnPlaceDeclaration)
	mux.HandleFunc("DELETE /v1/me/place-declarations/{memoryID}", s.deleteOwnPlaceDeclaration)
	mux.HandleFunc("PUT /v1/me/agent-memories/{memoryID}", s.putOwnAgentMemory)
	mux.HandleFunc("DELETE /v1/me/agent-memories/{memoryID}", s.deleteOwnAgentMemory)
	mux.HandleFunc("GET /v1/me/organizations/{organizationID}/agent-memories", s.listOrganizationAgentMemories)
	mux.HandleFunc("PUT /v1/me/organizations/{organizationID}/agent-memories/{memoryID}", s.putOrganizationAgentMemory)
	mux.HandleFunc("DELETE /v1/me/organizations/{organizationID}/agent-memories/{memoryID}", s.deleteOrganizationAgentMemory)
	mux.HandleFunc("PUT /v1/me/organizations/{organizationID}/agent-memories/{memoryID}/evidence/{evidenceID}", s.putOrganizationMemoryEvidence)
	mux.HandleFunc("DELETE /v1/me/organizations/{organizationID}/agent-memories/{memoryID}/evidence/{evidenceID}", s.removeOrganizationMemoryEvidence)
	mux.HandleFunc("GET /v1/me/organizations/{organizationID}/agent-memories/{memoryID}/provenance", s.readOrganizationMemoryProvenance)
	mux.HandleFunc("GET /v1/me/organizations/{organizationID}/announcements", s.listOrganizationAnnouncements)
	mux.HandleFunc("GET /v1/me/organizations/{organizationID}/announcements/{announcementID}", s.readOrganizationAnnouncement)
	mux.HandleFunc("PUT /v1/me/organizations/{organizationID}/announcements/{announcementID}", s.putOrganizationAnnouncement)
	mux.HandleFunc("POST /v1/me/organizations/{organizationID}/announcements/{announcementID}/publication-preview", s.previewOrganizationAnnouncement)
	mux.HandleFunc("POST /v1/me/organizations/{organizationID}/announcements/{announcementID}/publish", s.publishOrganizationAnnouncement)
	mux.HandleFunc("POST /v1/me/organizations/{organizationID}/announcements/{announcementID}/withdraw", s.withdrawOrganizationAnnouncement)
	mux.HandleFunc("GET /v1/organizations/{organizationID}/announcements/{announcementID}", s.readPublicOrganizationAnnouncement)
	mux.HandleFunc("GET /v1/me/agent-memories/{memoryID}/provenance", s.readOwnMemoryProvenance)
	mux.HandleFunc("PUT /v1/me/agent-memories/{memoryID}/evidence/{evidenceID}", s.putOwnMemoryEvidence)
	mux.HandleFunc("DELETE /v1/me/agent-memories/{memoryID}/evidence/{evidenceID}", s.removeOwnMemoryEvidence)
	mux.HandleFunc("GET /v1/me/agent-private-profile", s.getOwnAgentPrivateProfile)
	mux.HandleFunc("PUT /v1/me/agent-private-profile", s.replaceOwnAgentPrivateProfile)
	mux.HandleFunc("GET /v1/me/agent-profile-completion/suggestions", s.getOwnProfileCompletionSuggestions)
	mux.HandleFunc("POST /v1/me/agent-profile-completion/previews", s.previewOwnProfileCompletion)
	mux.HandleFunc("GET /v1/me/agent-profile-completion/previews/{previewID}", s.getOwnProfileCompletion)
	mux.HandleFunc("POST /v1/me/agent-profile-completion/previews/{previewID}/accept", s.acceptOwnProfileCompletion)
	mux.HandleFunc("POST /v1/me/agent-context/previews", s.previewContextPurpose)
	mux.HandleFunc("POST /v1/me/agent-context/self-review", s.humanSelfReviewContext)
	mux.HandleFunc("POST /v1/me/agent-context/approvals", s.approveContextPurpose)
	mux.HandleFunc("GET /v1/me/agent-context/grants/{grantID}", s.readContextPurpose)
	mux.HandleFunc("GET /v1/me/agent-context/grants", s.listOwnContextPurposes)
	mux.HandleFunc("DELETE /v1/me/agent-context/grants/{grantID}", s.revokeContextPurpose)
	mux.HandleFunc("POST /v1/me/agent-context/runtime", s.runtimeContextPurpose)
	mux.HandleFunc("POST /v1/me/agent-enrichment-purpose/previews", s.previewOwnEnrichmentPurpose)
	mux.HandleFunc("GET /v1/me/agent-enrichment-purpose/previews/{previewID}", s.getOwnEnrichmentPurposePreview)
	mux.HandleFunc("GET /v1/me/agent-enrichment-purpose/previews/{previewID}/receipt", s.getOwnEnrichmentPurposePreviewReceipt)
	mux.HandleFunc("POST /v1/me/agent-enrichment-purpose/previews/{previewID}/approve", s.approveOwnEnrichmentPurpose)
	mux.HandleFunc("GET /v1/me/agent-enrichment-purpose/grants/{grantID}", s.getOwnEnrichmentPurpose)
	mux.HandleFunc("GET /v1/me/agent-enrichment-purpose/grants", s.listOwnEnrichmentPurposes)
	mux.HandleFunc("DELETE /v1/me/agent-enrichment-purpose/grants/{grantID}", s.revokeOwnEnrichmentPurpose)
	mux.HandleFunc("POST /v1/me/agent-candidate-retention/previews", s.previewOwnCandidateRetention)
	mux.HandleFunc("GET /v1/me/agent-candidate-retention/previews/{previewID}", s.getOwnCandidateRetentionPreview)
	mux.HandleFunc("POST /v1/me/agent-candidate-retention/previews/{previewID}/approve", s.approveOwnCandidateRetention)
	mux.HandleFunc("GET /v1/me/agent-candidate-retention/grants/{grantID}", s.getOwnCandidateRetention)
	mux.HandleFunc("DELETE /v1/me/agent-candidate-retention/grants/{grantID}", s.revokeOwnCandidateRetention)
	mux.HandleFunc("POST /v1/me/model-egress/previews", s.previewModelEgress)
	mux.HandleFunc("GET /v1/me/model-egress/options", s.listModelEgressOptions)
	mux.HandleFunc("GET /v1/me/model-egress/previews", s.listModelEgressReceipts)
	mux.HandleFunc("GET /v1/me/model-egress/previews/{previewID}", s.readModelEgressReceipt)
	mux.HandleFunc("POST /v1/me/model-egress/approvals", s.approveModelEgress)
	mux.HandleFunc("DELETE /v1/me/model-egress/previews/{previewID}", s.revokeModelEgress)
	mux.HandleFunc("GET /v1/me/model-egress/roots/{rootID}/tasks/{taskID}/budget", s.readModelEgressBudget)
	mux.HandleFunc("GET /v1/me/agent-profile-visibility", s.ownProfileVisibility)
	mux.HandleFunc("PUT /v1/me/agent-profile-visibility", s.replaceProfileVisibility)
	mux.HandleFunc("GET /v1/accounts/{accountID}/agent-profile-fields", s.profileFields)
	mux.HandleFunc("GET /v1/me/new-people/consent", s.ownNewPeopleConsent)
	mux.HandleFunc("PUT /v1/me/new-people/consent", s.setNewPeopleConsent)
	mux.HandleFunc("GET /v1/me/new-people/intents", s.listOwnNewPeopleIntents)
	mux.HandleFunc("POST /v1/me/new-people/intents", s.createNewPeopleIntent)
	mux.HandleFunc("GET /v1/me/new-people/candidates", s.newPeopleCandidates)
	mux.HandleFunc("GET /v1/me/agent-introductions", s.ownAgentIntroductionSuggestions)
	mux.HandleFunc("GET /v1/me/community-interests", s.getOwnCommunityInterests)
	mux.HandleFunc("GET /v1/me/community-interests/options", s.getOwnCommunityInterestOptions)
	mux.HandleFunc("POST /v1/me/community-interests/preview", s.previewOwnCommunityInterest)
	mux.HandleFunc("POST /v1/me/community-interests/approve", s.approveOwnCommunityInterest)
	mux.HandleFunc("GET /v1/me/activity-participation-disclosures", s.getOwnParticipationDisclosures)
	mux.HandleFunc("GET /v1/me/activity-participation-disclosures/options", s.getOwnParticipationDisclosureOptions)
	mux.HandleFunc("POST /v1/me/activity-participation-disclosures/preview", s.previewOwnParticipationDisclosure)
	mux.HandleFunc("POST /v1/me/activity-participation-disclosures/approve", s.approveOwnParticipationDisclosure)
	mux.HandleFunc("POST /v1/me/new-people/invitations", s.createNewPeopleInvitation)
	mux.HandleFunc("GET /v1/activities/{activityID}/conversation", s.getActivityChat)
	mux.HandleFunc("GET /v1/communities/{communityID}/conversation", s.getCommunityChat)
	mux.HandleFunc("POST /v1/communities/{communityID}/conversation", s.joinCommunityChat)
	mux.HandleFunc("DELETE /v1/communities/{communityID}/conversation", s.leaveCommunityChat)
	mux.HandleFunc("GET /v1/communities/{communityID}/conversation/messages", s.listCommunityChatMessages)
	mux.HandleFunc("POST /v1/communities/{communityID}/conversation/messages", s.sendCommunityChatMessage)
	mux.HandleFunc("DELETE /v1/communities/{communityID}/conversation/messages/{messageID}", s.removeCommunityChatMessage)
	mux.HandleFunc("POST /v1/activities/{activityID}/conversation", s.joinActivityChat)
	mux.HandleFunc("DELETE /v1/activities/{activityID}/conversation", s.leaveActivityChat)
	mux.HandleFunc("GET /v1/activities/{activityID}/conversation/messages", s.listActivityChatMessages)
	mux.HandleFunc("POST /v1/activities/{activityID}/conversation/messages", s.sendActivityChatMessage)
	mux.HandleFunc("DELETE /v1/activities/{activityID}/conversation/messages/{messageID}", s.removeActivityChatMessage)
	mux.HandleFunc("GET /v1/me/social-disclosure", s.ownSocialDisclosure)
	mux.HandleFunc("GET /v1/me/agent-relationship-consent", s.ownRelationshipConsent)
	mux.HandleFunc("PUT /v1/me/agent-relationship-consent", s.setRelationshipConsent)
	mux.HandleFunc("GET /v1/me/agent-relationship-context", s.ownRelationshipContext)
	mux.HandleFunc("PUT /v1/me/social-disclosure", s.setSocialDisclosure)
	mux.HandleFunc("GET /v1/accounts/{accountID}/shared-context", s.sharedSocialContext)
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /v1/cities", s.listCities)
	mux.HandleFunc("GET /v1/me/social-intents/{intentID}/activity-conversion", s.getOwnIntentActivityConversion)
	mux.HandleFunc("POST /v1/me/social-intents/{intentID}/activity-conversion/preview", s.previewOwnIntentActivityConversion)
	mux.HandleFunc("POST /v1/me/social-intents/{intentID}/activity-conversion/approve", s.approveOwnIntentActivityConversion)
	mux.HandleFunc("GET /v1/cities/{cityID}", s.getCity)
	mux.HandleFunc("GET /v1/cities/{cityID}/places", s.listPlaces)
	mux.HandleFunc("GET /v1/cities/{cityID}/map-layers", s.getPublicMapLayers)
	mux.HandleFunc("POST /v1/me/agent-runs", s.scheduleOwnAgentRun)
	mux.HandleFunc("GET /v1/me/agent-runs/{runID}", s.readOwnAgentRun)
	mux.HandleFunc("POST /v1/me/agent-runs/{runID}/cancel", s.cancelOwnAgentRun)
	mux.HandleFunc("POST /v1/me/agent-runs/{runID}/retention-grant", s.attachOwnAgentRunGrant)
	mux.HandleFunc("GET /v1/me/agent-runs/failures", s.listOwnAgentRunFailures)
	mux.HandleFunc("GET /v1/me/agent-runs/{runID}/failure", s.readOwnAgentRunFailure)
	mux.HandleFunc("POST /v1/me/agent-runs/{runID}/recover", s.recoverOwnAgentRun)
	mux.HandleFunc("GET /v1/me/active-social-intents", s.listOwnActiveSocialIntents)
	mux.HandleFunc("GET /v1/me/active-social-intents/options", s.getActiveSocialIntentOptions)
	mux.HandleFunc("GET /v1/me/active-social-intents/{intentID}", s.getOwnActiveSocialIntent)
	mux.HandleFunc("POST /v1/me/active-social-intents/{intentID}/preview", s.previewOwnActiveSocialIntent)
	mux.HandleFunc("POST /v1/me/active-social-intents/{intentID}/approve", s.approveOwnActiveSocialIntent)
	mux.HandleFunc("GET /v1/me/cities/{cityID}/map-opportunities", s.getOwnMapOpportunityLayer)
	mux.HandleFunc("GET /v1/me/online-social-opportunities/options", s.getOwnOnlineSocialOpportunityOptions)
	mux.HandleFunc("GET /v1/me/online-social-opportunities/{intentID}", s.getOwnOnlineSocialOpportunities)
	mux.HandleFunc("GET /v1/me/now/context-selection/options", s.getNowContextSelectionOptions)
	mux.HandleFunc("POST /v1/me/now/context-selection/resolve", s.resolveNowContextSelection)
	mux.HandleFunc("GET /v1/cities/{cityID}/organizations/map", s.listPublicOrganizationMapPins)
	mux.HandleFunc("GET /v1/places/{placeID}", s.getPlace)
	mux.HandleFunc("GET /v1/places/{placeID}/profile", s.getPublicPlaceProfile)
	mux.HandleFunc("GET /v1/places/{placeID}/social-history", s.getPublicPlaceSocialHistory)
	mux.HandleFunc("GET /v1/moments/{momentID}", s.getPublicMoment)
	mux.HandleFunc("POST /v1/me/conversations/{conversationID}/entity-shares", s.shareHumanEntity)
	mux.HandleFunc("GET /v1/me/conversations/{conversationID}/entity-shares/{operationID}", s.getHumanEntityShare)
	mux.HandleFunc("POST /v1/cities/{cityID}/places/{placeID}/profile-candidates", s.submitPlaceProfileCandidate)
	mux.HandleFunc("GET /v1/cities/{cityID}/place-profile-candidates", s.listPlaceProfileCandidates)
	mux.HandleFunc("POST /v1/place-profile-candidates/{candidateID}/review", s.reviewPlaceProfileCandidate)
	mux.HandleFunc("POST /v1/places/{placeID}/profile/withdraw", s.withdrawPlaceProfile)
	mux.HandleFunc("GET /v1/places/{placeID}/activities", s.listPlaceActivities)
	mux.HandleFunc("GET /v1/places/{placeID}/venue", s.getPublicVenue)
	mux.HandleFunc("POST /v1/places/{placeID}/booking-events", s.postExternalBookingEvent)
	mux.HandleFunc("POST /v1/cities/{cityID}/places/{placeID}/venue-candidates", s.submitVenueCandidate)
	mux.HandleFunc("GET /v1/cities/{cityID}/venue-candidates", s.listVenueCandidates)
	mux.HandleFunc("POST /v1/venue-candidates/{candidateID}/review", s.reviewVenueCandidate)
	mux.HandleFunc("GET /v1/cities/{cityID}/activities", s.listActivities)
	mux.HandleFunc("GET /v1/cities/{cityID}/pulse", s.areaPulse)
	mux.HandleFunc("GET /v1/activities/{activityID}", s.getActivity)
	mux.HandleFunc("POST /v1/activities/{activityID}/analytics/events", s.recordActivityView)
	mux.HandleFunc("GET /v1/activities/{activityID}/participations/me", s.getOwnParticipation)
	mux.HandleFunc("GET /v1/me/participations", s.listOwnParticipations)
	mux.HandleFunc("POST /v1/activities/{activityID}/participations", s.joinActivity)
	mux.HandleFunc("DELETE /v1/activities/{activityID}/participations/me", s.cancelOwnParticipation)
	mux.HandleFunc("POST /v1/cities/{cityID}/agent/tasks", s.createAgentTask)
	mux.HandleFunc("GET /v1/me/agent-tasks", s.listAgentTasks)
	mux.HandleFunc("GET /v1/me/agent-tasks/{taskID}", s.getAgentTask)
	mux.HandleFunc("POST /v1/cities/{cityID}/communities", s.submitCommunity)
	mux.HandleFunc("GET /v1/communities", s.discoverSocialCommunities)
	mux.HandleFunc("POST /v1/communities", s.createSocialCommunity)
	mux.HandleFunc("GET /v1/communities/{communityID}", s.getSocialCommunity)
	mux.HandleFunc("PATCH /v1/communities/{communityID}", s.updateSocialCommunity)
	mux.HandleFunc("POST /v1/communities/{communityID}/archive", s.archiveSocialCommunity)
	mux.HandleFunc("POST /v1/communities/{communityID}/join", s.joinSocialCommunity)
	mux.HandleFunc("POST /v1/communities/{communityID}/leave", s.leaveSocialCommunity)
	mux.HandleFunc("GET /v1/communities/{communityID}/members", s.listSocialMembers)
	mux.HandleFunc("GET /v1/communities/{communityID}/requests", s.listSocialRequests)
	mux.HandleFunc("POST /v1/communities/{communityID}/requests/{requestID}/approve", s.approveSocialRequest)
	mux.HandleFunc("POST /v1/communities/{communityID}/requests/{requestID}/reject", s.rejectSocialRequest)
	mux.HandleFunc("POST /v1/communities/{communityID}/invitations", s.inviteSocialMember)
	mux.HandleFunc("PUT /v1/communities/{communityID}/members/{memberID}/role", s.changeSocialMemberRole)
	mux.HandleFunc("DELETE /v1/communities/{communityID}/members/{memberID}", s.removeSocialMember)
	mux.HandleFunc("POST /v1/communities/{communityID}/transfer-owner", s.transferSocialOwner)
	mux.HandleFunc("GET /v1/me/social-communities", s.listMySocialCommunities)
	mux.HandleFunc("GET /v1/me/communities", s.listOwnCommunities)
	mux.HandleFunc("POST /v1/me/communities/{communityID}/withdraw", s.withdrawCommunity)
	mux.HandleFunc("GET /v1/me/saved", s.listSaved)
	mux.HandleFunc("POST /v1/me/reports", s.createIncidentReport)
	mux.HandleFunc("GET /v1/me/reports", s.listOwnIncidentReports)
	mux.HandleFunc("POST /v1/me/saved", s.saveItem)
	mux.HandleFunc("DELETE /v1/me/saved/{savedID}", s.removeSaved)
	mux.HandleFunc("GET /v1/me/activity-plans", s.listActivityPlans)
	mux.HandleFunc("POST /v1/me/activity-plans", s.planActivity)
	mux.HandleFunc("DELETE /v1/me/activity-plans/{planID}", s.removeActivityPlan)
	mux.HandleFunc("GET /v1/me/connection-requests", s.listConnectionRequests)
	mux.HandleFunc("POST /v1/me/connection-requests", s.createConnectionRequest)
	mux.HandleFunc("POST /v1/me/connection-requests/{requestID}/decision", s.decideConnectionRequest)
	mux.HandleFunc("GET /v1/me/connection-requests/{requestID}/decision-operations/{operationID}", s.getConnectionDecisionOperation)
	mux.HandleFunc("GET /v1/me/ties", s.listPersonTies)
	mux.HandleFunc("DELETE /v1/me/ties/{tieID}", s.removePersonTie)
	mux.HandleFunc("POST /v1/me/ties/{tieID}/conversation", s.startFriendConversation)
	mux.HandleFunc("GET /v1/me/conversations", s.listConversations)
	mux.HandleFunc("GET /v1/me/conversations/{conversationID}/messages", s.listMessages)
	mux.HandleFunc("POST /v1/me/conversations/{conversationID}/read", s.markConversationRead)
	mux.HandleFunc("POST /v1/me/conversations/{conversationID}/messages", s.sendMessage)
	mux.HandleFunc("POST /v1/me/conversations/{conversationID}/message-operations", s.sendHumanMessageOperation)
	mux.HandleFunc("GET /v1/me/conversations/{conversationID}/message-operations/{operationID}", s.readHumanMessageOperation)
	mux.HandleFunc("GET /v1/me/inbox", s.listInbox)
	mux.HandleFunc("POST /v1/me/inbox/{itemID}/read", s.markInboxRead)
	mux.HandleFunc("GET /v1/cities/{cityID}/activity-candidates", s.listActivityCandidates)
	mux.HandleFunc("POST /v1/cities/{cityID}/activity-candidates", s.submitActivityCandidate)
	mux.HandleFunc("POST /v1/activity-candidates/{candidateID}/review", s.reviewActivityCandidate)
	mux.HandleFunc("GET /v1/me", s.me)
	mux.HandleFunc("GET /v1/me/contexts", s.listOwnContexts)
	mux.HandleFunc("GET /v1/me/now/online-contexts", s.listOwnNowOnlineContexts)
	mux.HandleFunc("POST /v1/me/now/online/tasks", s.createOwnNowOnlineTask)
	mux.HandleFunc("GET /v1/me/now/online/tasks/{taskID}", s.getOwnNowOnlineTask)
	mux.HandleFunc("GET /v1/me/now/online/intents/{intentID}", s.getOwnNowOnlineIntent)
	mux.HandleFunc("GET /v1/me/follows", s.listOwnFollows)
	mux.HandleFunc("POST /v1/me/follows", s.followTarget)
	mux.HandleFunc("DELETE /v1/me/follows/{targetType}/{targetID}", s.unfollowTarget)
	mux.HandleFunc("POST /v1/me/contexts", s.declareOwnContext)
	mux.HandleFunc("DELETE /v1/me/contexts/{contextID}/{relation}", s.removeOwnContext)
	mux.HandleFunc("GET /v1/me/organizations", s.listOrganizations)
	mux.HandleFunc("GET /v1/me/organization-invitations", s.listOrganizationInvitations)
	mux.HandleFunc("POST /v1/me/organization-invitations/{membershipID}/accept", s.acceptOrganizationInvitation)
	mux.HandleFunc("GET /v1/me/organizations/{organizationID}/members", s.listOrganizationMembers)
	mux.HandleFunc("POST /v1/me/organizations/{organizationID}/members", s.inviteOrganizationMember)
	mux.HandleFunc("PUT /v1/me/organizations/{organizationID}/members/{membershipID}/role", s.changeOrganizationMemberRole)
	mux.HandleFunc("DELETE /v1/me/organizations/{organizationID}/members/{membershipID}", s.revokeOrganizationMember)
	mux.HandleFunc("GET /v1/me/organizations/{organizationID}/map-location", s.getOrganizationMapLocation)
	mux.HandleFunc("PUT /v1/me/organizations/{organizationID}/map-location", s.submitOrganizationMapLocation)
	mux.HandleFunc("DELETE /v1/me/organizations/{organizationID}/map-location", s.hideOrganizationMapLocation)
	mux.HandleFunc("POST /v1/me/organizations/{organizationID}/map-location/review", s.reviewOrganizationMapLocation)
	mux.HandleFunc("GET /v1/organizations/{organizationID}", s.getPublicOrganization)
	mux.HandleFunc("POST /v1/organizations/{organizationID}/agent/ask", s.askOrganizationAgent)
	mux.HandleFunc("GET /v1/me/organizations/{organizationID}/faqs", s.listOrganizationFAQs)
	mux.HandleFunc("POST /v1/me/organizations/{organizationID}/faqs", s.createOrganizationFAQ)
	mux.HandleFunc("PUT /v1/me/organizations/{organizationID}/faqs/{faqID}", s.updateOrganizationFAQ)
	mux.HandleFunc("DELETE /v1/me/organizations/{organizationID}/faqs/{faqID}", s.deleteOrganizationFAQ)
	mux.HandleFunc("POST /v1/me/organizations", s.createOrganization)
	mux.HandleFunc("PUT /v1/me/organizations/{organizationID}/profile", s.updateOrganizationProfile)
	mux.HandleFunc("GET /v1/me/organizations/{organizationID}/activities", s.listManagedActivities)
	mux.HandleFunc("GET /v1/me/activities", s.listSocialManagedActivities)
	mux.HandleFunc("GET /v1/me/activity-organizers/businesses", s.listManagedBusinessOrganizers)
	mux.HandleFunc("GET /v1/me/businesses", s.listBusinessConsoles)
	mux.HandleFunc("GET /v1/me/businesses/{businessID}/console", s.getBusinessConsole)
	mux.HandleFunc("PUT /v1/me/businesses/{businessID}/claim", s.submitBusinessClaim)
	mux.HandleFunc("POST /v1/me/businesses/{businessID}/claim/review", s.reviewBusinessClaim)
	mux.HandleFunc("PUT /v1/me/businesses/{businessID}/profile", s.putBusinessProfile)
	mux.HandleFunc("POST /v1/me/businesses/{businessID}/profile/review", s.reviewBusinessProfile)
	mux.HandleFunc("PUT /v1/me/businesses/{businessID}/venues/{placeID}/facts", s.putBusinessVenueFacts)
	mux.HandleFunc("POST /v1/me/businesses/{businessID}/venues/{placeID}/facts/review", s.reviewBusinessVenueFacts)
	mux.HandleFunc("PUT /v1/me/businesses/{businessID}/members", s.changeBusinessMember)
	mux.HandleFunc("POST /v1/me/businesses/{businessID}/knowledge/ask", s.askOwnBusinessKnowledge)
	mux.HandleFunc("GET /v1/me/businesses/{businessID}/agent-identity", s.getBusinessAgentIdentity)
	mux.HandleFunc("POST /v1/me/businesses/{businessID}/agent-identity", s.establishBusinessAgentIdentity)
	mux.HandleFunc("POST /v1/businesses/{businessID}/sponsored-opportunities", s.submitSponsoredOpportunity)
	mux.HandleFunc("GET /v1/businesses/{businessID}/sponsored-opportunities", s.listOwnSponsoredOpportunities)
	mux.HandleFunc("GET /v1/cities/{cityID}/sponsored-opportunity-review", s.listSponsoredOpportunityReview)
	mux.HandleFunc("POST /v1/sponsored-opportunities/{declarationID}/review", s.reviewSponsoredOpportunity)
	mux.HandleFunc("POST /v1/sponsored-opportunities/{declarationID}/revoke", s.revokeSponsoredOpportunity)
	mux.HandleFunc("POST /v1/me/activities", s.createSocialActivityDraft)
	mux.HandleFunc("PUT /v1/me/activities/{activityID}", s.updateSocialActivity)
	mux.HandleFunc("POST /v1/me/activities/{activityID}/publish", s.publishSocialActivity)
	mux.HandleFunc("POST /v1/me/activities/{activityID}/cancel", s.cancelSocialActivity)
	mux.HandleFunc("POST /v1/me/activities/{activityID}/invitations", s.inviteSocialActivityPerson)
	mux.HandleFunc("GET /v1/me/organizations/{organizationID}/analytics", s.organizationActivityMetrics)
	mux.HandleFunc("POST /v1/me/organizations/{organizationID}/activities", s.createActivityDraft)
	mux.HandleFunc("PUT /v1/me/organizations/{organizationID}/activities/{activityID}", s.updateManagedActivity)
	mux.HandleFunc("POST /v1/me/organizations/{organizationID}/activities/{activityID}/publish", s.publishManagedActivity)
	mux.HandleFunc("POST /v1/me/organizations/{organizationID}/activities/{activityID}/cancel", s.cancelManagedActivity)
	mux.HandleFunc("PUT /v1/me/profile", s.updateOwnProfile)
	mux.HandleFunc("POST /v1/cities/{cityID}/intents", s.submitIntent)
	mux.HandleFunc("GET /v1/me/intents", s.listOwnIntents)
	mux.HandleFunc("POST /v1/me/social-intents", s.createSocialIntentDraft)
	mux.HandleFunc("GET /v1/me/social-intent-creations/{operationID}", s.getOwnSocialIntentCreation)
	mux.HandleFunc("GET /v1/me/agent-tasks/{taskID}/social-intent-draft", s.getOwnTaskSocialIntentDraft)
	mux.HandleFunc("POST /v1/me/agent-tasks/{taskID}/social-intent-drafts", s.createSocialIntentDraftFromTask)
	mux.HandleFunc("GET /v1/me/social-intents", s.listOwnSocialIntents)
	mux.HandleFunc("GET /v1/me/opportunities", s.listOwnOpportunities)
	mux.HandleFunc("GET /v1/me/social-intents/{intentID}/place-matches", s.listOwnPlaceMatches)
	mux.HandleFunc("GET /v1/me/social-intents/{intentID}", s.getOwnSocialIntent)
	mux.HandleFunc("POST /v1/me/social-intents/{intentID}/activate", s.activateSocialIntent)
	mux.HandleFunc("POST /v1/me/social-intents/{intentID}/cancel", s.cancelSocialIntent)
	mux.HandleFunc("GET /v1/social-intents", s.listVisibleSocialIntents)
	mux.HandleFunc("GET /v1/social-intents/{intentID}", s.getVisibleSocialIntent)
	mux.HandleFunc("POST /v1/me/intents/{intentID}/withdraw", s.withdrawIntent)
	mux.HandleFunc("GET /v1/me/moments", s.listOwnMoments)
	mux.HandleFunc("POST /v1/me/moments", s.createMomentDraft)
	mux.HandleFunc("GET /v1/me/moments/{momentID}", s.getOwnMoment)
	mux.HandleFunc("GET /v1/me/moments/{momentID}/publication", s.previewMomentPublication)
	mux.HandleFunc("POST /v1/me/moments/{momentID}/publication", s.publishMomentPublication)
	mux.HandleFunc("PUT /v1/me/moments/{momentID}", s.updateMomentDraft)
	mux.HandleFunc("DELETE /v1/me/moments/{momentID}", s.withdrawMoment)
	mux.HandleFunc("GET /v1/me/blocks", s.listBlocks)
	mux.HandleFunc("POST /v1/me/blocks", s.blockAccount)
	mux.HandleFunc("DELETE /v1/me/blocks/{accountID}", s.unblockAccount)
	mux.HandleFunc("POST /v1/session/logout", s.logout)
	mux.HandleFunc("GET /v1/me/consents", s.listConsents)
	mux.HandleFunc("POST /v1/me/consents", s.grantConsent)
	mux.HandleFunc("DELETE /v1/me/consents/{grantID}", s.revokeConsent)
	mux.HandleFunc("GET /v1/accounts/{accountID}/profile", s.getProfile)
	mux.HandleFunc("GET /v1/cities/{cityID}/place-candidates", s.listPlaceCandidates)
	mux.HandleFunc("POST /v1/cities/{cityID}/place-candidates", s.submitPlaceCandidate)
	mux.HandleFunc("POST /v1/place-candidates/{candidateID}/review", s.reviewPlaceCandidate)
	mux.HandleFunc("GET /v1/auth/oidc/start", s.startOIDC)
	mux.HandleFunc("GET /v1/auth/oidc/status", s.oidcStatus)
	mux.HandleFunc("GET /v1/auth/oidc/callback", s.completeOIDC)
	mux.HandleFunc("POST /v1/auth/oidc/exchange", s.exchangeOIDC)
	mux.HandleFunc("GET /v1/auth/dev-phone/status", s.devPhoneStatus)
	mux.HandleFunc("POST /v1/auth/dev-phone/code", s.requestDevPhoneCode)
	mux.HandleFunc("POST /v1/auth/dev-phone/verify", s.verifyDevPhoneCode)
	return requestTrace(cors(mux, allowedOrigins))
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	respond(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		respondError(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	respond(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *server) listCities(w http.ResponseWriter, r *http.Request) {
	cities, err := s.catalog.ListCities(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": cities})
}

func (s *server) getCity(w http.ResponseWriter, r *http.Request) {
	city, err := s.catalog.GetCity(r.Context(), r.PathValue("cityID"))
	if handleReadError(w, err) {
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": city})
}

func (s *server) listPlaces(w http.ResponseWriter, r *http.Request) {
	cityID := r.PathValue("cityID")
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) > 240 {
		respondError(w, http.StatusBadRequest, "invalid_place_query")
		return
	}
	_, err := s.catalog.GetCity(r.Context(), cityID)
	if handleReadError(w, err) {
		return
	}
	places, err := s.catalog.ListPlaces(r.Context(), cityID, query)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": places})
}

func (s *server) getPlace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("placeID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_place_id")
		return
	}
	place, err := s.catalog.GetPlace(r.Context(), id)
	if handleReadError(w, err) {
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": place})
}

func (s *server) listPlaceActivities(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("placeID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_place_id")
		return
	}
	actor, _, err := s.actor(r, false)
	if authFailed(w, err) {
		return
	}
	if _, err := s.catalog.GetPlace(r.Context(), id); handleReadError(w, err) {
		return
	}
	catalog, ok := s.catalog.(foundation.PlaceActivityCatalog)
	if !ok {
		respondError(w, http.StatusServiceUnavailable, "place_activity_catalog_unavailable")
		return
	}
	items, err := catalog.ListPlaceActivities(r.Context(), id, actor.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": items})
}

func (s *server) listActivities(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, false)
	if authFailed(w, err) {
		return
	}
	cityID := r.PathValue("cityID")
	_, err = s.catalog.GetCity(r.Context(), cityID)
	if handleReadError(w, err) {
		return
	}
	filter, filtered, code := parseActivitySearch(r)
	if code != "" {
		respondError(w, http.StatusBadRequest, code)
		return
	}
	var activities []foundation.Activity
	if filtered {
		activities, err = s.catalog.FindActivities(r.Context(), cityID, actor.ID, filter)
	} else {
		activities, err = s.catalog.ListActivities(r.Context(), cityID, actor.ID)
	}
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": activities})
}

func (s *server) areaPulse(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, false)
	if authFailed(w, err) {
		return
	}
	cityID := r.PathValue("cityID")
	if _, err := s.catalog.GetCity(r.Context(), cityID); handleReadError(w, err) {
		return
	}
	filter, _, code := parseActivitySearch(r)
	if code != "" {
		respondError(w, http.StatusBadRequest, code)
		return
	}
	if filter.West == nil || r.URL.Query().Has("category") {
		respondError(w, http.StatusBadRequest, "pulse_bounds_required")
		return
	}
	pulse, err := s.catalog.AreaPulse(r.Context(), cityID, actor.ID, filter)
	if err != nil {
		serverError(w, err)
		return
	}
	activities, err := s.catalog.FindActivities(r.Context(), cityID, actor.ID, filter)
	if err != nil {
		serverError(w, err)
		return
	}
	pulse.Bounds = [4]float64{*filter.West, *filter.South, *filter.East, *filter.North}
	pulse.From, pulse.To = filter.From, filter.To
	pulse.Activities = activities
	pulse.Truncated = pulse.Total > int64(len(activities))
	respond(w, http.StatusOK, map[string]any{"data": pulse})
}

var activityCategory = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

func parseActivitySearch(r *http.Request) (foundation.ActivitySearchFilter, bool, string) {
	query := r.URL.Query()
	filter := foundation.ActivitySearchFilter{Category: strings.TrimSpace(query.Get("category"))}
	filtered := query.Has("bounds") || query.Has("from") || query.Has("to") || query.Has("category")
	if filter.Category != "" && !activityCategory.MatchString(filter.Category) {
		return filter, filtered, "invalid_category"
	}
	if query.Has("bounds") {
		parts := strings.Split(query.Get("bounds"), ",")
		if len(parts) != 4 {
			return filter, filtered, "invalid_bounds"
		}
		values := [4]float64{}
		for index, part := range parts {
			value, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return filter, filtered, "invalid_bounds"
			}
			values[index] = value
		}
		if values[0] < -180 || values[2] > 180 || values[1] < -90 || values[3] > 90 ||
			values[0] >= values[2] || values[1] >= values[3] {
			return filter, filtered, "invalid_bounds"
		}
		filter.West, filter.South, filter.East, filter.North = &values[0], &values[1], &values[2], &values[3]
	}
	for _, field := range []struct {
		name string
		into **time.Time
	}{{"from", &filter.From}, {"to", &filter.To}} {
		if !query.Has(field.name) {
			continue
		}
		value, err := time.Parse(time.RFC3339, query.Get(field.name))
		if err != nil {
			return filter, filtered, "invalid_time_range"
		}
		*field.into = &value
	}
	if filter.From != nil && filter.To != nil && !filter.From.Before(*filter.To) {
		return filter, filtered, "invalid_time_range"
	}
	return filter, filtered, ""
}

func (s *server) getActivity(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, false)
	if authFailed(w, err) {
		return
	}
	id := r.PathValue("activityID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_activity_id")
		return
	}
	activity, err := s.catalog.GetActivity(r.Context(), id, actor.ID)
	if handleReadError(w, err) {
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": activity})
}

func handleReadError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, foundation.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else {
		serverError(w, err)
	}
	return true
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("request_id=%s server error: %v", w.Header().Get("X-Request-ID"), err)
	respondError(w, http.StatusInternalServerError, "internal_error")
}

func respondError(w http.ResponseWriter, status int, code string) {
	respond(w, status, map[string]any{"error": map[string]string{"code": code}})
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func cors(next http.Handler, allowedOrigins []string) http.Handler {
	allowed := make(map[string]bool)
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			allowed[origin] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Add("Vary", "Origin")
			if allowed[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID, X-Birdtie-Entry-Source")
				w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
			} else if r.Method == http.MethodOptions {
				respondError(w, http.StatusForbidden, "origin_not_allowed")
				return
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
