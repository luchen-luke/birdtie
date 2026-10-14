package postgres

import (
	"context"
	"reflect"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

var _ agentworkspace.OwnOnlinePlaceReplyStore = (*Store)(nil)
var _ agentworkspace.OwnOnlinePlaceFollowupStore = (*Store)(nil)

func bindExternalPlaceReply(t agentworkspace.Task, search agentworkspace.OnlinePlaceSearch, message string, now time.Time) (agentworkspace.Task, bool, error) {
	if t.PrincipalType != "person" || t.PrincipalID == "" || t.ActingUserID != t.PrincipalID || t.ContextType != "CITY" || t.Intent != agentworkspace.FindPlace ||
		search.CityID != t.CityID || !externalPlaceSearchValid(search, now, true) || !egressUUID(search.SnapshotID) ||
		!cityseed.SourceTextValid(search.RequestID, 200) || !cityseed.SourceTextValid(message, 2000) || (search.TaskID != "" && search.TaskID != t.ID) {
		return t, false, arp.ErrInvalid
	}
	query := t.Filters["currentQuery"]
	if query == "" {
		query = t.Query
	}
	if query == "" || search.Query != query || len(query) > 240 || search.TurnDigest != agentworkspace.OnlinePlaceTurnDigest(query) ||
		(search.Origin != "LIVE" && search.Origin != "CACHE") || (search.Freshness != "FRESH" && search.Freshness != "STALE") {
		return t, false, arp.ErrChanged
	}
	index := len(t.Conversation)
	boundIndex := index
	if t.Status == agentworkspace.TaskCompleted {
		boundIndex--
	}
	if search.MessageIndex != 1 && search.MessageIndex != boundIndex {
		return t, false, arp.ErrInvalid
	}
	if t.Status == agentworkspace.TaskCompleted && index > 0 {
		last := t.Conversation[index-1]
		if last.Role == "assistant" && last.ExternalPlaceReply != nil && last.ExternalPlaceReply.SnapshotID == search.SnapshotID && last.Text == message {
			want, e := copyExternalPlaceSearch(search)
			if e != nil {
				return t, false, e
			}
			want.TaskID, want.MessageIndex, want.TurnDigest = t.ID, index-1, agentworkspace.OnlinePlaceTurnDigest(query)
			if reflect.DeepEqual(want, *last.ExternalPlaceReply) {
				return t, false, nil
			}
		}
		return t, false, arp.ErrChanged
	}
	if t.Status != agentworkspace.TaskActive || index == 0 || t.Conversation[index-1].Role != "user" || t.Conversation[index-1].Text != query {
		return t, false, arp.ErrChanged
	}
	copy, e := copyExternalPlaceSearch(search)
	if e != nil {
		return t, false, e
	}
	copy.TaskID, copy.MessageIndex, copy.TurnDigest = t.ID, index, agentworkspace.OnlinePlaceTurnDigest(query)
	t.Conversation = append(append([]agentworkspace.Message(nil), t.Conversation...), agentworkspace.Message{Role: "assistant", Text: message, ExternalPlaceReply: &copy})
	t.Status = agentworkspace.TaskCompleted
	return t, true, nil
}

// Native ordinary-human storage only. The caller supplies a validated public
// snapshot after its cache/network read; no private Task lock spans that read.
func (s *Store) CompleteOwnOnlinePlaceReply(ctx context.Context, a arp.Access, search agentworkspace.OnlinePlaceSearch, message string) (agentworkspace.Task, error) {
	return s.completeOwnOnlinePlaceReply(ctx, a, search, message, "")
}

func (s *Store) CompleteOwnOnlinePlaceFollowup(ctx context.Context, a arp.Access, query string, search agentworkspace.OnlinePlaceSearch, message string) (agentworkspace.Task, error) {
	if query == "" {
		return agentworkspace.Task{}, arp.ErrInvalid
	}
	return s.completeOwnOnlinePlaceReply(ctx, a, search, message, query)
}

func (s *Store) completeOwnOnlinePlaceReply(ctx context.Context, a arp.Access, search agentworkspace.OnlinePlaceSearch, message, followup string) (agentworkspace.Task, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return agentworkspace.Task{}, arp.ErrUnavailable
	}
	if !a.Valid() {
		return agentworkspace.Task{}, arp.ErrDenied
	}
	tx, e := s.beginHumanMomentTx(ctx, a.SessionDigest, a.Actor)
	if e != nil {
		return agentworkspace.Task{}, e
	}
	defer tx.Rollback(context.Background())
	var agent string
	if e = tx.QueryRow(ctx, `SELECT id::text FROM agents WHERE principal_account_id=$1 AND agent_type='personal' AND status='active' FOR SHARE`, a.Actor.ID).Scan(&agent); e != nil {
		return agentworkspace.Task{}, e
	}
	t, e := ownReplyTaskTx(ctx, tx, a, true)
	if e != nil {
		return t, e
	}
	var city string
	if e = tx.QueryRow(ctx, `SELECT c.id FROM cities c JOIN city_contexts cc ON cc.city_id=c.id
 JOIN contexts cx ON cx.id=$2 AND cx.context_type='CITY' AND cx.city_id=c.id
 WHERE c.id=$1 AND c.publication_status='published' AND cc.status='active'
 AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp()) FOR SHARE OF c,cc,cx`, t.CityID, t.ContextID).Scan(&city); e != nil {
		return t, e
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return t, e
	}
	if followup != "" {
		t, e = prepareExternalPlaceFollowup(t, followup, search)
		if e != nil {
			return t, e
		}
	}
	bound, changed, e := bindExternalPlaceReply(t, search, message, now)
	if e != nil {
		return t, e
	}
	if changed {
		bound, e = s.updateTaskInTx(ctx, tx, bound)
		if e != nil {
			return t, e
		}
	}
	if e = s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID); e != nil {
		return t, e
	}
	if e = finalExternalPlaceReplyAuthority(ctx, tx, a, bound, agent, s.devPhoneEnabled); e != nil {
		return t, e
	}
	if e = tx.Commit(ctx); e != nil {
		return t, e
	}
	return bound, ctx.Err()
}

func prepareExternalPlaceFollowup(t agentworkspace.Task, query string, search agentworkspace.OnlinePlaceSearch) (agentworkspace.Task, error) {
	last := len(t.Conversation) - 1
	if t.Status != agentworkspace.TaskCompleted || t.Intent != agentworkspace.FindPlace || t.PrincipalType != "person" || t.PrincipalID == "" || t.ActingUserID != t.PrincipalID || t.ContextType != "CITY" || last < 1 || t.Conversation[last].Role != "assistant" || t.Conversation[last-1].Role != "user" || query == "" || len(query) > 240 || search.Query != query || search.TurnDigest != agentworkspace.OnlinePlaceTurnDigest(query) {
		return t, arp.ErrChanged
	}
	previous := t.Conversation[last].ExternalPlaceReply
	if previous == nil || previous.Schema != "birdtie-online-places-v2" || previous.TaskID != t.ID || previous.MessageIndex != last || previous.Query != t.Conversation[last-1].Text || previous.TurnDigest != agentworkspace.OnlinePlaceTurnDigest(previous.Query) || previous.CacheVersion < 1 || previous.CacheVersion != search.CacheVersion || previous.CityID != t.CityID || search.CityID != t.CityID || previous.Scope != search.Scope || !previous.ObservedAt.Equal(search.ObservedAt) || !previous.ExpiresAt.Equal(search.ExpiresAt) || !reflect.DeepEqual(previous.Places, search.Places) {
		return t, arp.ErrChanged
	}
	filters := make(map[string]string, len(t.Filters)+4)
	for k, v := range t.Filters {
		filters[k] = v
	}
	t.Filters = filters
	agentworkspace.ClearPlaceFollowupContext(&t)
	t.Filters["currentQuery"], t.Filters["targetIntent"] = query, agentworkspace.FindPlace
	t.Filters["category"], t.Filters["searchTerm"], t.Filters["locationPreference"], t.Filters["reason"] = "restaurant", "", search.Scope, agentworkspace.RestaurantDiscovery
	t.Status = agentworkspace.TaskActive
	t.Conversation = append(append([]agentworkspace.Message(nil), t.Conversation...), agentworkspace.Message{Role: "user", Text: query})
	return t, nil
}

func finalExternalPlaceReplyAuthority(ctx context.Context, tx pgx.Tx, a arp.Access, t agentworkspace.Task, agent string, dev bool) error {
	var session, current bool
	e := tx.QueryRow(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() at)
 SELECT EXISTS(SELECT 1 FROM sessions se CROSS JOIN n WHERE se.token_sha256=$1 AND se.account_id=$2
 AND se.revoked_at IS NULL AND se.expires_at>n.at AND se.idle_expires_at>n.at AND ($6::boolean OR se.authentication_method<>'dev_phone')),
 EXISTS(SELECT 1 FROM accounts ac JOIN agents ag ON ag.principal_account_id=ac.id JOIN agent_tasks t ON t.owner_account_id=ac.id
 JOIN cities c ON c.id=t.city_context_id JOIN city_contexts cc ON cc.city_id=c.id JOIN contexts cx ON cx.id=t.context_id CROSS JOIN n
 WHERE ac.id=$2 AND ac.status='active' AND ac.account_type='person' AND ag.id=$5 AND ag.status='active' AND ag.agent_type='personal'
 AND t.id=$3 AND t.updated_at=$4 AND t.principal_type='person' AND t.acting_user_account_id=ac.id AND t.context_type='CITY'
 AND cx.context_type='CITY' AND cx.city_id=c.id AND cc.status='active' AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>n.at))`,
		a.SessionDigest[:], a.Actor.ID, t.ID, t.UpdatedAt, agent, dev).Scan(&session, &current)
	if e != nil || ctx.Err() != nil {
		return arp.ErrUnavailable
	}
	if !session {
		return identity.ErrUnauthorized
	}
	if !current {
		return arp.ErrChanged
	}
	return nil
}
