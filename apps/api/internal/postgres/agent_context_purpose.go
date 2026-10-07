package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
)

var _ acb.PurposeStore = (*Store)(nil)

func contextPurposeHash(v any) string {
	raw, _ := json.Marshal(v)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func contextPurposeLimit(current *time.Time, t time.Time) {
	if !t.IsZero() && t.Before(*current) {
		*current = t.UTC()
	}
}

// Same native transaction as the consuming Builder. Captures references and
// exact domain versions; no source body is returned or persisted by this port.
func contextPurposeCaptureTx(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, selection acb.PurposeSelection) (acb.PurposeCapture, error) {
	s, e := acb.NormalizePurposeSelection(selection)
	if e != nil {
		return acb.PurposeCapture{}, e
	}
	if s.AgentID != b.agent {
		return acb.PurposeCapture{}, acb.ErrDenied
	}
	var requestAt time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&requestAt); e != nil || ctx.Err() != nil {
		return acb.PurposeCapture{}, acb.ErrUnavailable
	}
	if !s.DeadlineAt.After(requestAt) || s.DeadlineAt.Sub(requestAt) > acb.MaxDeadline {
		return acb.PurposeCapture{}, acb.ErrExpired
	}
	out := acb.PurposeCapture{BoundSelection: s, ExpiresAt: s.DeadlineAt, Sources: []acb.Source{}}
	contextPurposeLimit(&out.ExpiresAt, b.limit)
	// Ordinary Authenticate may extend idle_expires_at. Its mutable xmin is
	// deliberately excluded; absolute session identity and all actor generations
	// are bound. There is no Session restore API or invented restore entitlement.
	var authority []byte
	var sessionEnd time.Time
	e = tx.QueryRow(ctx, `SELECT jsonb_build_object('session',se.id,'created',se.created_at,'method',se.authentication_method,'absoluteExpiry',se.expires_at,'digest',encode(se.token_sha256,'hex'),
 'owner',a.id,'ownerRow',a.xmin::text,'agent',ag.id,'agentRow',ag.xmin::text,'metadataRow',ap.xmin::text,'metadataVersion',ap.profile_version),LEAST(se.expires_at,se.idle_expires_at)
 FROM accounts a JOIN sessions se ON se.account_id=a.id JOIN agents ag ON ag.principal_account_id=a.id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 WHERE a.id=$1 AND a.account_type='person' AND a.status='active' AND se.id=$2 AND se.revoked_at IS NULL AND se.expires_at>clock_timestamp() AND se.idle_expires_at>clock_timestamp()
 AND ag.id=$3 AND ag.agent_type='personal' AND ag.status='active' AND ap.profile_version>0`, b.owner, b.session, b.agent).Scan(&authority, &sessionEnd)
	if e != nil {
		return acb.PurposeCapture{}, contextBuilderError(e)
	}
	out.Authority = contextPurposeHash(json.RawMessage(authority))
	contextPurposeLimit(&out.ExpiresAt, sessionEnd)
	addOpaque := func(kind, id string, at time.Time, token string, raw []byte) {
		out.Sources = append(out.Sources, acb.Source{Kind: kind, ID: id, NativeTime: at.UTC(), RowToken: token, Version: agentevent.SourceVersion{Kind: agentevent.UpdatedAtDigestVersion, Token: contextPurposeHash(json.RawMessage(raw))}})
	}
	addRevision := func(kind, id string, at time.Time, token string, version int64) {
		out.Sources = append(out.Sources, acb.Source{Kind: kind, ID: id, NativeTime: at.UTC(), RowToken: token, Version: agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: version}})
	}
	var at time.Time
	var raw []byte
	var token, query string
	e = tx.QueryRow(ctx, `SELECT t.updated_at,t.xmin::text,COALESCE(NULLIF(t.filters->>'currentQuery',''),t.query),jsonb_build_object('id',t.id,'owner',t.owner_account_id,'city',t.city_context_id,'context',t.context_id,'status',t.status,'intent',t.intent,'queryDigest',encode(sha256(convert_to(COALESCE(NULLIF(t.filters->>'currentQuery',''),t.query),'UTF8')),'hex'),'updatedAt',t.updated_at,'row',t.xmin::text)
 FROM agent_tasks t JOIN contexts cx ON cx.id=t.context_id AND cx.context_type='CITY' AND cx.city_id=$3
 WHERE t.id=$1 AND t.owner_account_id=$2 AND t.principal_type='person' AND t.acting_user_account_id=$2 AND t.context_type='CITY' AND t.city_context_id=$3 AND t.status='ACTIVE' AND t.updated_at<=clock_timestamp() FOR SHARE OF t,cx`, s.TaskID, b.owner, s.CityID).Scan(&at, &token, &query, &raw)
	if e != nil {
		return acb.PurposeCapture{}, contextBuilderError(e)
	}
	if !at.Equal(s.TaskUpdatedAt) || acb.PurposeQueryDigest(query) != s.QueryDigest || (s.CurrentQuery != "" && s.CurrentQuery != query) {
		return acb.PurposeCapture{}, acb.ErrDenied
	}
	out.BoundSelection.CurrentQuery = query
	addOpaque("CURRENT_TASK_REQUEST", s.TaskID, at, token, raw)
	var expiry *time.Time
	e = tx.QueryRow(ctx, `SELECT c.updated_at,c.xmin::text,c.expires_at,jsonb_build_object('id',c.id,'timeZone',c.time_zone,'updatedAt',c.updated_at,'expiry',c.expires_at,'cityRow',c.xmin::text,'context',cx.id,'contextRow',cx.xmin::text,'stateRow',cc.xmin::text)
 FROM cities c JOIN contexts cx ON cx.city_id=c.id AND cx.context_type='CITY' JOIN city_contexts cc ON cc.city_id=c.id
 WHERE c.id=$1 AND c.publication_status='published' AND cc.status='active' AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp()) AND c.updated_at<=clock_timestamp() AND cx.created_at<=clock_timestamp() AND cc.updated_at<=clock_timestamp() FOR SHARE OF c,cx,cc`, s.CityID).Scan(&at, &token, &expiry, &raw)
	if e != nil {
		return acb.PurposeCapture{}, contextBuilderError(e)
	}
	addOpaque("PUBLIC_CITY", s.CityID, at, token, raw)
	if expiry != nil {
		contextPurposeLimit(&out.ExpiresAt, *expiry)
	}
	if len(s.ProfileFields) > 0 {
		var revision int64
		var validFields bool
		e = tx.QueryRow(ctx, `SELECT written_profile_version,updated_at,xmin::text,fields ?& $3::text[] FROM agent_private_profiles WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON' AND written_profile_version>1 AND updated_at<=clock_timestamp() FOR SHARE`, b.agent, b.owner, s.ProfileFields).Scan(&revision, &at, &token, &validFields)
		if e != nil {
			return acb.PurposeCapture{}, contextBuilderError(e)
		}
		if !validFields {
			return acb.PurposeCapture{}, acb.ErrDenied
		}
		addRevision("PURPOSE_PRIVATE_PROFILE", b.agent, at, token, revision)
	}
	for _, id := range s.MemoryIDs {
		var revision int64
		var end time.Time
		e = tx.QueryRow(ctx, `SELECT version,updated_at,xmin::text,valid_until FROM agent_memories WHERE id=$1 AND agent_id=$2 AND owner_id=$3 AND owner_type='PERSON' AND status='ACTIVE' AND source_type='EXPLICIT' AND visibility='PRIVATE' AND version>0 AND valid_from<=clock_timestamp() AND valid_until>clock_timestamp() AND updated_at<=clock_timestamp() FOR SHARE`, id, b.agent, b.owner).Scan(&revision, &at, &token, &end)
		if e != nil {
			return acb.PurposeCapture{}, contextBuilderError(e)
		}
		addRevision("PURPOSE_EXPLICIT_MEMORY", id, at, token, revision)
		contextPurposeLimit(&out.ExpiresAt, end)
	}
	for _, family := range s.PolicyFamilies {
		var revision int64
		var end time.Time
		e = tx.QueryRow(ctx, `SELECT native_revision,updated_at,xmin::text,expires_at FROM agent_policy_settings WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON' AND family=$3 AND native_revision>0 AND valid_from<=clock_timestamp() AND expires_at>clock_timestamp() AND updated_at<=clock_timestamp() FOR SHARE`, b.agent, b.owner, family).Scan(&revision, &at, &token, &end)
		if e != nil {
			return acb.PurposeCapture{}, contextBuilderError(e)
		}
		addRevision("PURPOSE_POLICY_SETTINGS", b.agent+":"+string(family), at, token, revision)
		contextPurposeLimit(&out.ExpiresAt, end)
	}
	for _, id := range s.PlaceIDs {
		e = tx.QueryRow(ctx, `SELECT updated_at,xmin::text,expires_at,jsonb_build_object('id',id,'name',name,'category',category_code,'updatedAt',updated_at,'expiry',expires_at,'row',xmin::text) FROM places WHERE id=$1 AND city_id=$2 AND publication_status='published' AND (expires_at IS NULL OR expires_at>clock_timestamp()) AND updated_at<=clock_timestamp() FOR SHARE`, id, s.CityID).Scan(&at, &token, &expiry, &raw)
		if e != nil {
			return acb.PurposeCapture{}, contextBuilderError(e)
		}
		addOpaque("PUBLIC_PLACE", id, at, token, raw)
		if expiry != nil {
			contextPurposeLimit(&out.ExpiresAt, *expiry)
		}
	}
	for _, id := range s.ActivityIDs {
		var end time.Time
		e = tx.QueryRow(ctx, `SELECT a.updated_at,a.xmin::text,a.expires_at,a.ends_at,jsonb_build_object('id',a.id,'title',a.title,'category',a.category_code,'starts',a.starts_at,'ends',a.ends_at,'updatedAt',a.updated_at,'expiry',a.expires_at,'activityRow',a.xmin::text,'organizerPerson',ao.person_account_id,'organizerCommunity',ao.community_id,'organizerOrganization',ao.organization_id,'organizerRow',ao.xmin::text)
 FROM activities a JOIN activity_organizers ao ON ao.activity_id=a.id WHERE a.id=$1 AND a.city_id=$2 AND a.publication_status='published' AND a.visibility='public' AND a.cancelled_at IS NULL AND a.ends_at>clock_timestamp() AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND a.updated_at<=clock_timestamp() AND birdtie_activity_visible_to(a.id,$3::uuid)
 AND NOT EXISTS(SELECT 1 FROM account_blocks blocked WHERE (blocked.blocker_account_id=$3 AND blocked.blocked_account_id=a.host_account_id) OR (blocked.blocked_account_id=$3 AND blocked.blocker_account_id=a.host_account_id)) FOR SHARE OF a,ao`, id, s.CityID, b.owner).Scan(&at, &token, &expiry, &end, &raw)
		if e != nil {
			return acb.PurposeCapture{}, contextBuilderError(e)
		}
		addOpaque("PUBLIC_ACTIVITY", id, at, token, raw)
		contextPurposeLimit(&out.ExpiresAt, end)
		if expiry != nil {
			contextPurposeLimit(&out.ExpiresAt, *expiry)
		}
	}
	for _, id := range s.RelationshipTieIDs {
		e = tx.QueryRow(ctx, `SELECT t.updated_at,t.xmin::text,jsonb_build_object('id',t.id,'peerAccountId',peer.id,'state','accepted','tieRow',t.xmin::text,'requestRow',fr.xmin::text,'peerRow',peer.xmin::text,'consentRow',co.xmin::text)
 FROM person_ties t JOIN connection_requests fr ON fr.id=t.request_id AND fr.scope='friend' AND fr.state='accepted' AND LEAST(fr.sender_account_id,fr.recipient_account_id)=t.person_a_account_id AND GREATEST(fr.sender_account_id,fr.recipient_account_id)=t.person_b_account_id
 JOIN accounts peer ON peer.id=CASE WHEN t.person_a_account_id=$2 THEN t.person_b_account_id ELSE t.person_a_account_id END AND peer.account_type='person' AND peer.status='active'
 JOIN person_agent_relationship_consent co ON co.account_id=$2 AND co.enabled
 WHERE t.id=$1 AND $2 IN(t.person_a_account_id,t.person_b_account_id) AND t.status='active' AND t.updated_at<=clock_timestamp() AND co.updated_at<=clock_timestamp()
 AND NOT EXISTS(SELECT 1 FROM account_blocks block WHERE (block.blocker_account_id=$2 AND block.blocked_account_id=peer.id) OR (block.blocked_account_id=$2 AND block.blocker_account_id=peer.id)) FOR SHARE OF t,fr,peer,co`, id, b.owner).Scan(&at, &token, &raw)
		if e != nil {
			return acb.PurposeCapture{}, contextBuilderError(e)
		}
		addOpaque("PURPOSE_RELATIONSHIP_TIE", id, at, token, raw)
	}
	out.ObservedAt, e = contextPurposeFinalTx(ctx, tx, b, s, out.ExpiresAt, "", 0)
	if e != nil {
		return acb.PurposeCapture{}, e
	}
	if !out.ExpiresAt.After(out.ObservedAt) || s.DeadlineAt.Sub(out.ObservedAt) > acb.MaxDeadline {
		return acb.PurposeCapture{}, acb.ErrExpired
	}
	for _, src := range out.Sources {
		if src.NativeTime.After(out.ObservedAt) {
			return acb.PurposeCapture{}, acb.ErrDenied
		}
	}
	return out, nil
}

// All current ACLs, native source windows, exact Task and optional grant share
// the FINAL statement after earlier source/table waits. Row locks do not prove
// phantom Block absence or stop the clock from expiring a selected source.
func contextPurposeFinalTx(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, s acb.PurposeSelection, expires time.Time, grantID string, revision int64) (time.Time, error) {
	var at time.Time
	var valid bool
	e := tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at)
 SELECT stamp.at,stamp.at<$4::timestamptz
 AND EXISTS(SELECT 1 FROM sessions se JOIN accounts a ON a.id=se.account_id JOIN agents ag ON ag.principal_account_id=a.id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 WHERE se.id=$1 AND a.id=$2 AND ag.id=$3 AND a.account_type='person' AND a.status='active' AND ag.agent_type='personal' AND ag.status='active' AND se.revoked_at IS NULL AND se.expires_at>stamp.at AND se.idle_expires_at>stamp.at AND ap.profile_version>0)
 AND EXISTS(SELECT 1 FROM agent_tasks t JOIN contexts cx ON cx.id=t.context_id AND cx.context_type='CITY' AND cx.city_id=$6 JOIN cities c ON c.id=cx.city_id JOIN city_contexts cc ON cc.city_id=c.id
 WHERE t.id=$5 AND t.owner_account_id=$2 AND t.principal_type='person' AND t.acting_user_account_id=$2 AND t.context_type='CITY' AND t.city_context_id=$6 AND t.status='ACTIVE' AND t.updated_at=$7 AND t.updated_at<=stamp.at
 AND encode(sha256(convert_to(COALESCE(NULLIF(t.filters->>'currentQuery',''),t.query),'UTF8')),'hex')=$8
 AND c.publication_status='published' AND cc.status='active' AND (c.expires_at IS NULL OR c.expires_at>stamp.at) AND c.updated_at<=stamp.at AND cc.updated_at<=stamp.at AND cx.created_at<=stamp.at)
 AND (cardinality($9::text[])=0 OR EXISTS(SELECT 1 FROM agent_private_profiles p WHERE p.agent_id=$3 AND p.owner_id=$2 AND p.owner_type='PERSON' AND p.fields ?& $9 AND p.written_profile_version>1 AND p.updated_at<=stamp.at))
 AND NOT EXISTS(SELECT unnest($10::uuid[]) EXCEPT SELECT id FROM agent_memories m WHERE m.agent_id=$3 AND m.owner_id=$2 AND m.owner_type='PERSON' AND m.source_type='EXPLICIT' AND m.visibility='PRIVATE' AND m.status='ACTIVE' AND m.valid_from<=stamp.at AND m.valid_until>stamp.at AND m.updated_at<=stamp.at)
 AND NOT EXISTS(SELECT unnest($11::text[]) EXCEPT SELECT family FROM agent_policy_settings p WHERE p.agent_id=$3 AND p.owner_id=$2 AND p.owner_type='PERSON' AND p.valid_from<=stamp.at AND p.expires_at>stamp.at AND p.updated_at<=stamp.at)
 AND NOT EXISTS(SELECT unnest($12::uuid[]) EXCEPT SELECT id FROM places p WHERE p.city_id=$6 AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>stamp.at) AND p.updated_at<=stamp.at)
 AND NOT EXISTS(SELECT unnest($13::uuid[]) EXCEPT SELECT a.id FROM activities a JOIN activity_organizers ao ON ao.activity_id=a.id WHERE a.city_id=$6 AND a.publication_status='published' AND a.visibility='public' AND a.cancelled_at IS NULL AND a.ends_at>stamp.at AND (a.expires_at IS NULL OR a.expires_at>stamp.at) AND a.updated_at<=stamp.at AND birdtie_activity_visible_to(a.id,$2::uuid)
 AND NOT EXISTS(SELECT 1 FROM account_blocks blocked WHERE (blocked.blocker_account_id=$2 AND blocked.blocked_account_id=a.host_account_id) OR (blocked.blocked_account_id=$2 AND blocked.blocker_account_id=a.host_account_id)))
 AND NOT EXISTS(SELECT unnest($14::uuid[]) EXCEPT SELECT t.id FROM person_ties t JOIN connection_requests fr ON fr.id=t.request_id AND fr.scope='friend' AND fr.state='accepted' AND LEAST(fr.sender_account_id,fr.recipient_account_id)=t.person_a_account_id AND GREATEST(fr.sender_account_id,fr.recipient_account_id)=t.person_b_account_id
 JOIN accounts peer ON peer.id=CASE WHEN t.person_a_account_id=$2 THEN t.person_b_account_id ELSE t.person_a_account_id END AND peer.account_type='person' AND peer.status='active'
 JOIN person_agent_relationship_consent co ON co.account_id=$2 AND co.enabled WHERE $2 IN(t.person_a_account_id,t.person_b_account_id) AND t.status='active' AND t.updated_at<=stamp.at AND co.updated_at<=stamp.at
 AND NOT EXISTS(SELECT 1 FROM account_blocks block WHERE (block.blocker_account_id=$2 AND block.blocked_account_id=peer.id) OR (block.blocked_account_id=$2 AND block.blocker_account_id=peer.id)))
 AND ($15::text='' OR EXISTS(SELECT 1 FROM consent_grants g JOIN agent_context_purpose_bindings gb ON gb.grant_id=g.id JOIN agent_context_purpose_previews p ON p.id=gb.preview_id WHERE g.id=$16::uuid AND g.revision=$17 AND g.revoked_at IS NULL AND g.expires_at>stamp.at AND g.owner_account_id=$2 AND g.recipient_account_id=$2 AND g.resource_type='agent_context' AND g.resource_id=p.id::text AND g.purpose='TASK_CONTEXT_READ' AND g.actions=ARRAY['read']::text[] AND p.owner_id=$2 AND p.agent_id=$3 AND p.session_id=$1)) FROM stamp`,
		b.session, b.owner, b.agent, expires, s.TaskID, s.CityID, s.TaskUpdatedAt, s.QueryDigest, s.ProfileFields, s.MemoryIDs, s.PolicyFamilies, s.PlaceIDs, s.ActivityIDs, s.RelationshipTieIDs, grantID, func() any {
			if grantID == "" {
				return nil
			}
			return grantID
		}(), revision).Scan(&at, &valid)
	if e != nil || ctx.Err() != nil {
		return time.Time{}, acb.ErrUnavailable
	}
	if !valid {
		return time.Time{}, acb.ErrDenied
	}
	return at.UTC(), nil
}

func contextPurposeCaptureEqual(a acb.PurposeCapture, authority string, sources []acb.Source) bool {
	return a.Authority == authority && reflect.DeepEqual(a.Sources, sources)
}
func contextPurposeDecode(selection, sources []byte) (acb.PurposeSelection, []acb.Source, error) {
	var s acb.PurposeSelection
	var refs []acb.Source
	if json.Unmarshal(selection, &s) != nil || json.Unmarshal(sources, &refs) != nil {
		return s, nil, acb.ErrUnavailable
	}
	s, e := acb.NormalizePurposeSelection(s)
	return s, refs, e
}

func (s *Store) PreviewOwnContextPurpose(ctx context.Context, access agentprofile.PrivateAccess, selection acb.PurposeSelection) (acb.PurposePreview, error) {
	tx, b, e := s.beginContextBuilder(ctx, access)
	if e != nil {
		return acb.PurposePreview{}, e
	}
	defer tx.Rollback(context.Background())
	// Serialize only this owner's preview/approval writes without upgrading its
	// Account SHARE lock; unrelated grants and native readers remain independent.
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,76033))`, b.owner); e != nil {
		return acb.PurposePreview{}, acb.ErrUnavailable
	}
	c, e := contextPurposeCaptureTx(ctx, tx, b, selection)
	if e != nil {
		return acb.PurposePreview{}, e
	}
	review, e := s.contextPurposeReviewTx(ctx, tx, b, c)
	if e != nil {
		return acb.PurposePreview{}, e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM agent_context_purpose_previews p WHERE p.owner_id=$1 AND p.expires_at<=clock_timestamp() AND NOT EXISTS(SELECT 1 FROM agent_context_purpose_bindings b WHERE b.preview_id=p.id)`, b.owner); e != nil {
		return acb.PurposePreview{}, acb.ErrUnavailable
	}
	var count int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM agent_context_purpose_previews p WHERE owner_id=$1 AND NOT EXISTS(SELECT 1 FROM agent_context_purpose_bindings b WHERE b.preview_id=p.id)`, b.owner).Scan(&count); e != nil {
		return acb.PurposePreview{}, acb.ErrUnavailable
	}
	if count >= 128 {
		return acb.PurposePreview{}, acb.ErrUnavailable
	}
	raw, _ := acb.PurposeSelectionBytes(c.BoundSelection)
	refs, _ := json.Marshal(c.Sources)
	expires := c.ObservedAt.Add(acb.PurposePreviewTTL)
	contextPurposeLimit(&expires, c.ExpiresAt)
	var id string
	if e = tx.QueryRow(ctx, `INSERT INTO agent_context_purpose_previews(id,owner_id,agent_id,session_id,task_id,selection,sources,authority,observed_at,expires_at) VALUES(gen_random_uuid(),$1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, b.owner, b.agent, b.session, c.BoundSelection.TaskID, raw, refs, c.Authority, c.ObservedAt, expires).Scan(&id); e != nil {
		return acb.PurposePreview{}, acb.ErrUnavailable
	}
	// Recheck after INSERT/table waits; newly inserted phantom Block or a natural
	// expiry cannot be hidden by the earlier source snapshot.
	last, e := contextPurposeCaptureTx(ctx, tx, b, c.BoundSelection)
	if e != nil {
		return acb.PurposePreview{}, e
	}
	if !contextPurposeCaptureEqual(last, c.Authority, c.Sources) || !expires.After(last.ObservedAt) {
		return acb.PurposePreview{}, acb.ErrDenied
	}
	if e = tx.Commit(ctx); e != nil {
		return acb.PurposePreview{}, acb.ErrUnavailable
	}
	return acb.PurposePreview{ID: id, Purpose: acb.TaskContextRead, Selection: c.BoundSelection, Sources: c.Sources, ObservedAt: c.ObservedAt, ExpiresAt: expires, Review: review}, nil
}

func contextPurposeReadGrantTx(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, id string, lock bool) (acb.PurposeGrant, string, error) {
	var g acb.PurposeGrant
	var authority string
	var sel, refs []byte
	q := `SELECT g.id,g.revision,g.created_at,g.expires_at,g.revoked_at,p.selection,p.sources,p.authority FROM consent_grants g JOIN agent_context_purpose_bindings binding ON binding.grant_id=g.id JOIN agent_context_purpose_previews p ON p.id=binding.preview_id
 WHERE g.id=$1 AND g.owner_account_id=$2 AND g.recipient_account_id=$2 AND g.resource_type='agent_context' AND g.resource_id=p.id::text AND g.purpose='TASK_CONTEXT_READ' AND g.actions=ARRAY['read']::text[] AND g.expires_at IS NOT NULL AND p.owner_id=$2 AND p.agent_id=$3 AND p.session_id=$4`
	if lock {
		q += ` FOR SHARE OF g,p,binding`
	}
	if e := tx.QueryRow(ctx, q, id, b.owner, b.agent, b.session).Scan(&g.ID, &g.Revision, &g.CreatedAt, &g.ExpiresAt, &g.RevokedAt, &sel, &refs, &authority); e != nil {
		return acb.PurposeGrant{}, "", contextBuilderError(e)
	}
	var e error
	g.Selection, g.Sources, e = contextPurposeDecode(sel, refs)
	if e != nil {
		return acb.PurposeGrant{}, "", e
	}
	g.Purpose = acb.TaskContextRead
	return g, authority, nil
}

// Resolve is a bounded read control, not a model/candidate/Memory capability.
func contextPurposeResolveTx(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, id string, selection acb.PurposeSelection) (acb.PurposeResolution, error) {
	g, authority, e := contextPurposeReadGrantTx(ctx, tx, b, id, true)
	if e != nil {
		return acb.PurposeResolution{}, e
	}
	if g.RevokedAt != nil || !acb.SamePurposeSelection(selection, g.Selection) {
		return acb.PurposeResolution{}, acb.ErrDenied
	}
	c, e := contextPurposeCaptureTx(ctx, tx, b, selection)
	if e != nil {
		return acb.PurposeResolution{}, e
	}
	if !contextPurposeCaptureEqual(c, authority, g.Sources) {
		return acb.PurposeResolution{}, acb.ErrDenied
	}
	contextPurposeLimit(&c.ExpiresAt, g.ExpiresAt)
	// FINAL SQL is after all possible source/pool/table waits. Lifecycle still
	// belongs only to consent_grants, evaluated with this current PG timestamp.
	at, e := contextPurposeFinalTx(ctx, tx, b, c.BoundSelection, c.ExpiresAt, g.ID, g.Revision)
	if e != nil {
		return acb.PurposeResolution{}, e
	}
	return acb.PurposeResolution{GrantID: g.ID, GrantRevision: g.Revision, BoundSelection: c.BoundSelection, Sources: c.Sources, Authority: c.Authority, ObservedAt: at.UTC(), ExpiresAt: c.ExpiresAt}, nil
}

func (s *Store) ApproveOwnContextPurpose(ctx context.Context, access agentprofile.PrivateAccess, previewID string) (acb.PurposeGrant, error) {
	tx, b, e := s.beginContextBuilder(ctx, access)
	if e != nil {
		return acb.PurposeGrant{}, e
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,76033))`, b.owner); e != nil {
		return acb.PurposeGrant{}, acb.ErrUnavailable
	}
	var sel, refs []byte
	var authority string
	var previewExpiry time.Time
	e = tx.QueryRow(ctx, `SELECT selection,sources,authority,expires_at FROM agent_context_purpose_previews WHERE id=$1 AND owner_id=$2 AND agent_id=$3 AND session_id=$4 FOR UPDATE`, previewID, b.owner, b.agent, b.session).Scan(&sel, &refs, &authority, &previewExpiry)
	if e != nil {
		return acb.PurposeGrant{}, contextBuilderError(e)
	}
	selection, sources, e := contextPurposeDecode(sel, refs)
	if e != nil {
		return acb.PurposeGrant{}, e
	}
	var existing string
	e = tx.QueryRow(ctx, `SELECT grant_id FROM agent_context_purpose_bindings WHERE preview_id=$1`, previewID).Scan(&existing)
	if e == nil {
		resolution, e := contextPurposeResolveTx(ctx, tx, b, existing, selection)
		if e != nil {
			return acb.PurposeGrant{}, e
		}
		g, _, e := contextPurposeReadGrantTx(ctx, tx, b, existing, true)
		if e != nil {
			return acb.PurposeGrant{}, e
		}
		g.Selection = resolution.BoundSelection
		if e = tx.Commit(ctx); e != nil {
			return acb.PurposeGrant{}, acb.ErrUnavailable
		}
		return g, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return acb.PurposeGrant{}, acb.ErrUnavailable
	}
	c, e := contextPurposeCaptureTx(ctx, tx, b, selection)
	if e != nil {
		return acb.PurposeGrant{}, e
	}
	if !previewExpiry.After(c.ObservedAt) || !contextPurposeCaptureEqual(c, authority, sources) {
		return acb.PurposeGrant{}, acb.ErrDenied
	}
	// The human approved this concrete preview's displayed original deadline.
	// Authenticate may have since extended idle_expires_at; neither that touch
	// nor a later approval can expand the exact preview's finite consent window.
	contextPurposeLimit(&c.ExpiresAt, previewExpiry)
	var id string
	e = tx.QueryRow(ctx, `INSERT INTO consent_grants(id,owner_account_id,recipient_account_id,resource_type,resource_id,purpose,actions,revision,expires_at,created_at) VALUES(gen_random_uuid(),$1,$1,'agent_context',$2,'TASK_CONTEXT_READ',ARRAY['read']::text[],1,$3,$4) RETURNING id`, b.owner, previewID, c.ExpiresAt, c.ObservedAt).Scan(&id)
	if e != nil {
		return acb.PurposeGrant{}, acb.ErrUnavailable
	}
	if _, e = tx.Exec(ctx, `INSERT INTO agent_context_purpose_bindings(grant_id,preview_id) VALUES($1,$2)`, id, previewID); e != nil {
		return acb.PurposeGrant{}, acb.ErrUnavailable
	}
	resolution, e := contextPurposeResolveTx(ctx, tx, b, id, selection)
	if e != nil {
		return acb.PurposeGrant{}, e
	}
	if !previewExpiry.After(resolution.ObservedAt) {
		return acb.PurposeGrant{}, acb.ErrDenied
	}
	if e = insertDomainAudit(ctx, tx, b.owner, "grant", "agent_context", id, "TASK_CONTEXT_READ", &id); e != nil {
		return acb.PurposeGrant{}, acb.ErrUnavailable
	}
	// Audit-table wait cannot turn an expired/revoked source into a successful
	// approval; final fresh resolver runs after this last potentially blocking IO.
	resolution, e = contextPurposeResolveTx(ctx, tx, b, id, selection)
	if e != nil {
		return acb.PurposeGrant{}, e
	}
	if !previewExpiry.After(resolution.ObservedAt) {
		return acb.PurposeGrant{}, acb.ErrDenied
	}
	g, _, e := contextPurposeReadGrantTx(ctx, tx, b, id, true)
	if e != nil {
		return acb.PurposeGrant{}, e
	}
	g.Selection = resolution.BoundSelection
	if e = tx.Commit(ctx); e != nil {
		return acb.PurposeGrant{}, acb.ErrUnavailable
	}
	return g, nil
}

func (s *Store) ReadOwnContextPurpose(ctx context.Context, access agentprofile.PrivateAccess, id string) (acb.PurposeGrant, error) {
	tx, b, e := s.beginContextBuilder(ctx, access)
	if e != nil {
		return acb.PurposeGrant{}, e
	}
	defer tx.Rollback(context.Background())
	g, _, e := contextPurposeReadGrantTx(ctx, tx, b, id, true)
	if e != nil {
		return acb.PurposeGrant{}, e
	}
	c, e := contextPurposeCaptureTx(ctx, tx, b, g.Selection)
	if e != nil {
		return acb.PurposeGrant{}, e
	}
	g.Selection = c.BoundSelection
	// A revoked grant may be inspected to reconcile an explicit revoke, but a
	// changed identity/source is not disclosed through an old approval receipt.
	_, authority, e := contextPurposeReadGrantTx(ctx, tx, b, id, true)
	if e != nil || !contextPurposeCaptureEqual(c, authority, g.Sources) {
		return acb.PurposeGrant{}, acb.ErrDenied
	}
	if _, e = contextPurposeFinalTx(ctx, tx, b, c.BoundSelection, c.ExpiresAt, "", 0); e != nil {
		return acb.PurposeGrant{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return acb.PurposeGrant{}, acb.ErrUnavailable
	}
	return g, nil
}
func (s *Store) ResolveOwnContextPurpose(ctx context.Context, access agentprofile.PrivateAccess, id string, selection acb.PurposeSelection) (acb.PurposeResolution, error) {
	tx, b, e := s.beginContextBuilder(ctx, access)
	if e != nil {
		return acb.PurposeResolution{}, e
	}
	defer tx.Rollback(context.Background())
	r, e := contextPurposeResolveTx(ctx, tx, b, id, selection)
	if e != nil {
		return acb.PurposeResolution{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return acb.PurposeResolution{}, acb.ErrUnavailable
	}
	return r, nil
}
func (s *Store) RevokeOwnContextPurpose(ctx context.Context, access agentprofile.PrivateAccess, id string, revision int64) (acb.PurposeGrant, error) {
	if revision < 1 {
		return acb.PurposeGrant{}, acb.ErrInvalid
	}
	tx, b, e := s.beginContextBuilder(ctx, access)
	if e != nil {
		return acb.PurposeGrant{}, e
	}
	defer tx.Rollback(context.Background())
	// Native owner/session controls revoke; an expired source cannot prevent
	// revocation. Binding and stable current owner are checked without source read.
	g, authority, e := contextPurposeReadGrantTx(ctx, tx, b, id, false)
	if e != nil {
		return acb.PurposeGrant{}, e
	}
	_ = authority
	var actual int64
	var at *time.Time
	e = tx.QueryRow(ctx, `SELECT revision,revoked_at FROM consent_grants WHERE id=$1 FOR UPDATE`, id).Scan(&actual, &at)
	if e != nil {
		return acb.PurposeGrant{}, contextBuilderError(e)
	}
	if at != nil && actual == revision+1 {
		g.Revision = actual
		g.RevokedAt = at
	} else {
		if at != nil || actual != revision || revision == int64(^uint64(0)>>1) {
			return acb.PurposeGrant{}, acb.ErrDenied
		}
		e = tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE consent_grants g SET revoked_at=stamp.at,revision=g.revision+1 FROM stamp WHERE g.id=$1 AND g.revision=$2 AND g.revoked_at IS NULL RETURNING g.revision,g.revoked_at`, id, revision).Scan(&g.Revision, &g.RevokedAt)
		if e != nil {
			return acb.PurposeGrant{}, contextBuilderError(e)
		}
		if e = insertDomainAudit(ctx, tx, b.owner, "revoke", "agent_context", id, "TASK_CONTEXT_READ", &id); e != nil {
			return acb.PurposeGrant{}, acb.ErrUnavailable
		}
	}
	if _, e = s.finishContextBuilder(ctx, tx, b, nil); e != nil {
		return acb.PurposeGrant{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return acb.PurposeGrant{}, acb.ErrUnavailable
	}
	return g, nil
}
