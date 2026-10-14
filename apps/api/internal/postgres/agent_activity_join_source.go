package postgres

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentautonomy"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/jackc/pgx/v5"
)

type joinReplySource struct {
	current  currentConfigurationTask
	selector *nativeReplySelector
	target   ea.Receipt
	review   *aa.ActivityJoinReview
	policy   *nativeToolPolicy
	until    time.Time
}

// Only the current ordinary activity result round is a proposal source. An
// arbitrary ACTIVE task, an older assistant turn or a client membership can
// neither choose the target nor confer permission.
func latestActivityJoinReply(t agentworkspace.Task, owner, target string) (*nativeReplySelector, error) {
	i := len(t.Conversation) - 1
	if t.PrincipalType != "person" || t.PrincipalID != owner || t.ActingUserID != owner || t.ContextType != "CITY" || t.Status != agentworkspace.TaskCompleted || i < 1 || t.Conversation[i].Role != "assistant" || t.Conversation[i-1].Role != "user" || agentworkspace.ValidateReplyMembership(t, i) != nil {
		return nil, aa.ErrDenied
	}
	if t.Intent != agentworkspace.FindActivity && !(t.Intent == agentworkspace.RefineResults && t.Filters["targetIntent"] == agentworkspace.FindActivity) {
		return nil, aa.ErrDenied
	}
	query := t.Filters["currentQuery"]
	if query == "" {
		query = t.Query
	}
	if strings.TrimSpace(query) == "" || t.Conversation[i-1].Text != query {
		return nil, aa.ErrChanged
	}
	m := *t.Conversation[i].ResultMembership
	if m.Kind != "activity" || m.CityID != t.CityID || m.TaskID != t.ID {
		return nil, aa.ErrDenied
	}
	found := false
	for _, r := range m.Refs {
		found = found || r.Type == "activity" && r.ID == target
	}
	if !found {
		return nil, aa.ErrDenied
	}
	// Licensed place context belongs to its original place-only contract.
	if t.Conversation[i].LicensedPlaceModelEvidence != nil {
		return nil, aa.ErrDenied
	}
	return &nativeReplySelector{index: i, membership: m}, nil
}

func joinOptional(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	q := s
	return &q
}
func nativeJoinReview(a foundation.Activity, targetVersion string) (*aa.ActivityJoinReview, error) {
	r := &aa.ActivityJoinReview{SchemaVersion: aa.ActivityJoinReviewSchema, ActivityID: a.ID, TargetVersion: targetVersion, Title: a.Title, Summary: a.Summary, Description: a.Description, HostLabel: joinOptional(a.HostLabel), Organizer: aa.ActivityJoinOrganizer{Type: a.Organizer.Type, ID: a.Organizer.ID, Name: joinOptional(a.Organizer.Name)}, StartsAt: a.StartsAt.UTC(), EndsAt: a.EndsAt.UTC(), TimeZone: a.TimeZone, Modality: a.Modality, LocationLabel: joinOptional(a.PlaceName), PhysicalPlaceStatus: joinOptional(a.PhysicalPlaceStatus), Eligibility: joinOptional(a.Eligibility), Visibility: a.Visibility, Capacity: a.Capacity, ParticipantCount: a.ParticipantCount}
	// The legacy price column has a default zero. Without a native currency it
	// does not establish a free ticket; retain an explicit unknown pair.
	if aa.ActivityJoinCurrencyValid(a.Currency) {
		price, currency := a.PriceMinor, a.Currency
		r.PriceMinor = &price
		r.Currency = &currency
	}
	if aa.ValidateActivityJoinReview(r, a.ID) != nil {
		return nil, aa.ErrUnavailable
	}
	return r, nil
}

func (g *AgentActivityJoin) captureJoinReply(ctx context.Context, tx pgx.Tx, a agentprofile.PrivateAccess, input aa.ActivityJoinInput) (joinReplySource, error) {
	var out joinReplySource
	cur, e := lockModelConfigurationTask(ctx, tx, agentevent.Access{SessionDigest: a.SessionDigest}, input.TaskID, g.store.devPhoneEnabled)
	if e != nil || cur.agent.Principal != a.WorkspacePrincipal || cur.status != agentworkspace.TaskCompleted {
		return out, aa.ErrDenied
	}
	task, e := scanAgentTask(tx.QueryRow(ctx, `SELECT `+agentTaskColumns+` FROM agent_tasks WHERE id=$1 AND owner_account_id=$2 AND principal_type='person' AND acting_user_account_id=$2 FOR SHARE`, input.TaskID, a.WorkspacePrincipal.ID))
	if e != nil {
		return out, aa.ErrDenied
	}
	selector, e := latestActivityJoinReply(task, a.WorkspacePrincipal.ID, input.ActivityID)
	if e != nil {
		return out, e
	}
	expected, e := time.Parse(time.RFC3339Nano, input.ExpectedTaskUpdatedAt)
	if e != nil || !input.CurrentSourceValid() || !expected.Equal(task.UpdatedAt) || input.ReplyTurnDigest != selector.membership.TurnDigest {
		return out, aa.ErrChanged
	}
	target, e := g.source(ctx, tx, a, input.ActivityID)
	if e != nil {
		return out, e
	}
	policy, e := captureNativeToolPolicy(ctx, tx, agentPrivateBinding{cur.sessionID, cur.agent.Principal.ID, cur.agent.AgentID}, target.View.ValidUntil)
	if e != nil || policy.level != agentautonomy.LevelPrepare || agenttool.Restrict(agenttool.ActivityJoin, actorref.Person, policy.level).Denied {
		return out, aa.ErrDenied
	}
	raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(task))
	if e != nil {
		return out, aa.ErrUnavailable
	}
	access := arp.Access{Actor: joinHumanAccess(a).Actor, SessionDigest: a.SessionDigest, TaskID: task.ID, ExpectedTask: raw}
	receipt, e := g.store.captureAgentResultProjectionSelectedTx(ctx, tx, access, arp.Query{CityID: task.CityID, Kind: "activity", CompareIDs: []string{}}, nil, policy, selector)
	if e != nil {
		return out, aa.ErrChanged
	}
	// Local enrichment is still governed by the original native evidence and
	// immutable source generation. It supplies no new execution authority.
	if task.Conversation[selector.index].LocalReplyEvidence != nil {
		binding, e := lockOwnAgentPrivateBinding(ctx, tx, a, g.store.devPhoneEnabled)
		if e != nil {
			return out, aa.ErrDenied
		}
		_, until, e := validateLocalReplyEvidenceTx(ctx, tx, binding, task.Conversation[selector.index], cur.now)
		if e != nil {
			return out, aa.ErrChanged
		}
		receipt.ValidUntil = retryMinimum(receipt.ValidUntil, until)
	}
	allowed := false
	for _, item := range receipt.Items {
		if item.Entity.Type == "activity" && item.Entity.ID == input.ActivityID {
			for _, d := range item.Actions {
				if d.Kind == ea.Join && d.State == ea.Available {
					for _, op := range d.AllowedOperations {
						allowed = allowed || op == "JOIN"
					}
				}
			}
		}
	}
	if !allowed {
		return out, aa.ErrChanged
	}
	var review *aa.ActivityJoinReview
	for _, activity := range receipt.Activities {
		if activity.ID == input.ActivityID && activity.CityID == task.CityID && activity.Visibility == "public" {
			review, e = nativeJoinReview(activity, target.Proof)
			if e != nil {
				return out, e
			}
			break
		}
	}
	if review == nil || !receipt.ValidUntil.After(receipt.ObservedAt) {
		return out, aa.ErrDenied
	}
	// These original host field/community leases can change a visible label or
	// organizer's live status without an UPDATE. Freeze their current deadlines
	// so a deferred trigger wait cannot outlive the facts the person reviewed.
	if e = tx.QueryRow(ctx, `SELECT least($1::timestamptz,(SELECT min(g.expires_at) FROM consent_grants g WHERE g.owner_account_id IN(SELECT person_account_id FROM activity_organizers WHERE activity_id=$2) AND g.recipient_account_id=$3 AND g.resource_type='profile' AND g.purpose='profile_view' AND g.revoked_at IS NULL AND g.expires_at>clock_timestamp()),(SELECT min(c.expires_at) FROM communities c WHERE c.id IN(SELECT community_id FROM activity_organizers WHERE activity_id=$2) OR c.id::text IN(SELECT scope.value#>>'{}' FROM agent_profile_field_visibility v CROSS JOIN LATERAL jsonb_array_elements(coalesce(v.rules->'displayName'->'communityIds','[]'::jsonb)) scope(value) WHERE v.owner_id IN(SELECT person_account_id FROM activity_organizers WHERE activity_id=$2))))`, receipt.ValidUntil, input.ActivityID, a.WorkspacePrincipal.ID).Scan(&receipt.ValidUntil); e != nil || !receipt.ValidUntil.After(receipt.ObservedAt) {
		return out, aa.ErrChanged
	}
	return joinReplySource{current: cur, selector: selector, target: target, review: review, policy: policy, until: receipt.ValidUntil}, nil
}

func joinReplyBindingMatches(p joinApproval, s joinReplySource) bool {
	return p.b.CurrentReplySource() && s.current.agent.AgentID == p.b.AgentID && s.current.sessionID == p.b.SessionID && s.current.version.Token == p.b.SourceVersion && p.b.ReplyIndex == strconv.Itoa(s.selector.index) && p.b.ReplyTurnDigest == s.selector.membership.TurnDigest && p.b.ReplyResultSetID == s.selector.membership.ResultSetID && p.b.ReviewDigest == aa.ActivityJoinReviewDigest(*s.review) && p.b.TargetVersion == s.target.Proof && p.b.PolicyVersion == s.policy.token && !p.b.ExpiresAt.After(s.until)
}
func (g *AgentActivityJoin) previewForApproval(ctx context.Context, tx pgx.Tx, a agentprofile.PrivateAccess, p joinApproval) (aa.ActivityJoinPreview, error) {
	v := joinPreview(p)
	if p.state == aa.Cancelled {
		return v, nil
	}
	s, e := g.captureJoinReply(ctx, tx, a, p.p)
	if e != nil {
		return v, e
	}
	if !joinReplyBindingMatches(p, s) {
		return v, aa.ErrChanged
	}
	v.Review = s.review
	return v, nil
}
