package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5"
)

func (s *Store) GetNewPeopleConsent(ctx context.Context, owner string) (newpeople.Consent, error) {
	var out newpeople.Consent
	err := s.pool.QueryRow(ctx, `SELECT coalesce(c.enabled,false) FROM accounts a
 LEFT JOIN person_new_people_consent c ON c.account_id=a.id
 WHERE a.id=$1 AND a.account_type='person' AND a.status='active'`, owner).Scan(&out.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, newpeople.ErrNotFound
	}
	return out, err
}

func (s *Store) SetNewPeopleConsent(ctx context.Context, owner string, enabled bool) (newpeople.Consent, error) {
	out := newpeople.Consent{Enabled: enabled}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	// All invitations lock both accounts in UUID order; changing a choice locks
	// its own account first, including the initially missing consent row.
	var id string
	err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR NO KEY UPDATE`, owner).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, newpeople.ErrNotFound
	}
	if err != nil {
		return out, err
	}
	changed, err := tx.Exec(ctx, `INSERT INTO person_new_people_consent(account_id,enabled) VALUES($1,$2)
 ON CONFLICT(account_id) DO UPDATE SET enabled=EXCLUDED.enabled,updated_at=now()
 WHERE person_new_people_consent.enabled IS DISTINCT FROM EXCLUDED.enabled`, owner, enabled)
	if err != nil {
		return out, err
	}
	if changed.RowsAffected() == 0 {
		return out, tx.Commit(ctx)
	}
	_, err = auditExec(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)
 VALUES($1,'update','new_people_consent',$1::uuid::text,'allowed',CASE WHEN $2 THEN 'explicit_enable' ELSE 'explicit_revoke' END)`, owner, enabled)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func (s *Store) CreateNewPeopleIntent(ctx context.Context, owner string, in newpeople.DraftInput) (socialintent.Record, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Category = strings.TrimSpace(in.Category)
	in.CityID = strings.TrimSpace(in.CityID)
	if len([]rune(in.Title)) < 1 || len([]rune(in.Title)) > 160 || in.Category == "" || len(in.CityID) > 80 ||
		!in.ExpiresAt.After(time.Now().Add(time.Minute)) || in.ExpiresAt.After(time.Now().Add(90*24*time.Hour)) {
		return socialintent.Record{}, newpeople.ErrInvalid
	}
	constraints, err := json.Marshal(socialintent.Constraints{Category: in.Category, PlaceID: in.PlaceID,
		AreaLabel: in.AreaLabel, OnlinePlatform: in.OnlinePlatform, MinParticipants: in.MinParticipants, MaxParticipants: in.MaxParticipants})
	if err != nil {
		return socialintent.Record{}, err
	}
	parsed, normalized, err := socialintent.ParseConstraints(constraints, in.Modality)
	if err != nil {
		return socialintent.Record{}, newpeople.ErrInvalid
	}
	draft := socialintent.DraftInput{Type: "FIND_COMPANION", Title: in.Title, Constraints: normalized, Audience: "PUBLIC", Modality: in.Modality, ExpiresAt: in.ExpiresAt}
	if in.Modality == "ONLINE" {
		if in.CityID != "" {
			return socialintent.Record{}, newpeople.ErrInvalid
		}
	} else {
		if in.CityID == "" {
			return socialintent.Record{}, newpeople.ErrInvalid
		}
		err = s.pool.QueryRow(ctx, `SELECT x.id FROM contexts x JOIN cities c ON c.id=x.city_id AND c.publication_status='published'
   WHERE x.context_type='CITY' AND x.city_id=$1`, in.CityID).Scan(&draft.ContextID)
		if errors.Is(err, pgx.ErrNoRows) {
			return socialintent.Record{}, newpeople.ErrInvalid
		}
		if err != nil {
			return socialintent.Record{}, err
		}
		if parsed.PlaceID != "" {
			var valid bool
			err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM places WHERE id=$1 AND city_id=$2 AND publication_status='published'
    AND (expires_at IS NULL OR expires_at>now()))`, parsed.PlaceID, in.CityID).Scan(&valid)
			if err != nil {
				return socialintent.Record{}, err
			}
			if !valid {
				return socialintent.Record{}, newpeople.ErrInvalid
			}
		}
	}
	// No private Person Context declaration or inferred location is created.
	item, err := s.CreateSocialIntentDraft(ctx, owner, draft)
	if err == nil && in.Modality != "ONLINE" {
		item.CityID = in.CityID
	}
	return item, err
}

func (s *Store) ListNewPeopleIntents(ctx context.Context, owner string) ([]socialintent.Record, error) {
	if _, err := s.GetNewPeopleConsent(ctx, owner); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+socialIntentColumns+` FROM social_intents
 WHERE creator_account_id=$1 AND intent_type='FIND_COMPANION' ORDER BY created_at DESC,id DESC LIMIT 100`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []socialintent.Record{}
	for rows.Next() {
		item, e := scanSocialIntent(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range out {
		if err = s.fillSocialIntentAudience(ctx, &out[i]); err != nil {
			return nil, err
		}
		if out[i].CityID == "" && out[i].ContextID != nil && out[i].Modality != "ONLINE" {
			var city *string
			err = s.pool.QueryRow(ctx, `SELECT city_id FROM contexts WHERE id=$1`, *out[i].ContextID).Scan(&city)
			if err != nil {
				return nil, err
			}
			if city != nil {
				out[i].CityID = *city
			}
		}
	}
	return out, nil
}

// Scope references are taken only from the Intent, never Person Context.
// The raw constraints remain inside this Store; candidate JSON contains no
// title, platform, coarse area, coordinates or audience membership list.
const newPeopleSignalColumns = `i.id,i.creator_account_id,
    CASE WHEN birdtie_agent_profile_field_allowed(i.creator_account_id,$1::uuid,'displayName')
        THEN COALESCE(NULLIF(p.display_name,''),NULLIF(a.handle,''),'Birdtie 成员')
        ELSE 'Birdtie 成员' END,i.modality,i.constraints,
 coalesce(city.id,''),coalesce(place.id::text,'')`
const newPeopleSignalJoins = ` JOIN accounts a ON a.id=i.creator_account_id AND a.account_type='person' AND a.status='active'
 JOIN user_profiles p ON p.account_id=a.id AND p.visibility='public'
 JOIN person_new_people_consent opt ON opt.account_id=a.id AND opt.enabled
 LEFT JOIN contexts ic ON ic.id=i.context_id AND ic.context_type='CITY'
 LEFT JOIN social_intent_audience_targets target ON target.intent_id=i.id
 LEFT JOIN cities city ON city.id=coalesce(target.city_id,ic.city_id) AND city.publication_status='published' AND (city.expires_at IS NULL OR city.expires_at>statement_timestamp())
 LEFT JOIN places place ON lower(place.id::text)=lower(i.constraints->>'placeId') AND place.publication_status='published'
 AND (place.expires_at IS NULL OR place.expires_at>statement_timestamp())
 AND (coalesce(target.city_id,ic.city_id) IS NULL OR place.city_id=city.id)
 AND EXISTS(SELECT 1 FROM cities pc WHERE pc.id=place.city_id AND pc.publication_status='published')`
const newPeopleActive = ` i.intent_type='FIND_COMPANION' AND i.status='ACTIVE' AND i.expires_at>statement_timestamp() AND i.audience<>'PRIVATE'`

func scanNewPeopleSignal(row scanner) (newpeople.Signal, error) {
	var out newpeople.Signal
	var raw []byte
	var validPlace string
	err := row.Scan(&out.IntentID, &out.AccountID, &out.DisplayName, &out.Modality, &raw, &out.CityID, &validPlace)
	if err != nil {
		return out, err
	}
	return decodeNewPeopleSignal(out, raw, validPlace)
}

func decodeNewPeopleSignal(out newpeople.Signal, raw []byte, validPlace string) (newpeople.Signal, error) {
	c, _, err := socialintent.ParseConstraints(raw, out.Modality)
	if err != nil {
		return out, newpeople.ErrInvalid
	}
	if c.PlaceID != "" && !strings.EqualFold(c.PlaceID, validPlace) {
		return out, newpeople.ErrInvalid
	}
	if out.Modality != "ONLINE" && c.PlaceID == "" && out.CityID == "" {
		return out, newpeople.ErrInvalid
	}
	out.Category = c.Category
	out.PlaceID = c.PlaceID
	out.AreaLabel = c.AreaLabel
	out.OnlinePlatform = c.OnlinePlatform
	out.MinParticipants = c.MinParticipants
	out.MaxParticipants = c.MaxParticipants
	if strings.TrimSpace(out.Category) == "" {
		return out, newpeople.ErrInvalid
	}
	return out, nil
}

func newPeopleSource(ctx context.Context, tx pgx.Tx, owner, sourceID string) (newpeople.Signal, error) {
	out, err := scanNewPeopleSignal(tx.QueryRow(ctx, `SELECT `+newPeopleSignalColumns+` FROM social_intents i `+newPeopleSignalJoins+`
 WHERE i.id=$2 AND i.creator_account_id=$1 AND `+newPeopleActive, owner, sourceID))
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, newpeople.ErrInvalid) {
		return out, newpeople.ErrNotFound
	}
	return out, err
}

const newPeopleCandidateGate = ` AND i.creator_account_id<>$1
 AND EXISTS(SELECT 1 FROM social_intents selected WHERE selected.id=$2 AND selected.expires_at>statement_timestamp())
 AND birdtie_social_intent_visible_to(i.id,$1) AND birdtie_social_intent_visible_to($2,i.creator_account_id)
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE (b.blocker_account_id=$1 AND b.blocked_account_id=i.creator_account_id)
 OR (b.blocker_account_id=i.creator_account_id AND b.blocked_account_id=$1))
 AND NOT EXISTS(SELECT 1 FROM person_ties t WHERE t.status='active' AND t.person_a_account_id=LEAST($1::uuid,i.creator_account_id)
 AND t.person_b_account_id=GREATEST($1::uuid,i.creator_account_id))
 AND NOT EXISTS(SELECT 1 FROM connection_requests r WHERE r.state='pending' AND r.expires_at>statement_timestamp() AND
 ((r.sender_account_id=$1 AND r.recipient_account_id=i.creator_account_id) OR
 (r.recipient_account_id=$1 AND r.sender_account_id=i.creator_account_id)))`

func (s *Store) FindNewPeople(ctx context.Context, owner, sourceID string) (newpeople.Response, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return newpeople.Response{}, err
	}
	defer tx.Rollback(ctx)
	out, _, _, err := s.findNewPeopleTx(ctx, tx, owner, sourceID)
	if err != nil {
		return newpeople.Response{}, err
	}
	return out, tx.Commit(ctx)
}

func (s *Store) findNewPeopleTx(ctx context.Context, tx pgx.Tx, owner, sourceID string, native ...humanNewPeopleBinding) (newpeople.Response, string, time.Time, error) {
	var err error
	var tokens []string
	var deadline time.Time
	if _, err = newPeopleSource(ctx, tx, owner, sourceID); err != nil {
		return newpeople.Response{}, "", time.Time{}, err
	}
	// Source constraints and peer values are projected at the same current
	// statement snapshot. The preliminary source read is not reusable authority.
	var digest []byte
	var devPhone bool
	nativeRead := len(native) == 1
	if nativeRead {
		digest = native[0].Digest[:]
		devPhone = native[0].DevPhone
	}
	policyColumns, policyWhere := "", ""
	args := []any{owner, sourceID, digest, devPhone, nativeRead}
	if nativeRead && native[0].Policy != nil {
		policyColumns, policyWhere = currentMatchPolicyFence(native[0].Policy)
		p := native[0].Policy
		args = append(args, p.until, p.token, p.owner, p.agent)
	}
	rows, err := tx.Query(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at),
 current_owner AS MATERIALIZED(SELECT a.id FROM accounts a JOIN sessions se ON se.account_id=a.id CROSS JOIN stamp
 WHERE a.id=$1 AND a.account_type='person' AND a.status='active' AND se.token_sha256=$3 AND se.revoked_at IS NULL AND se.expires_at>stamp.at AND se.idle_expires_at>stamp.at AND ($4::boolean OR se.authentication_method<>'dev_phone')),
 current_source(intent_id,account_id,display_name,modality,constraints,city_id,place_id,native_token,deadline) AS MATERIALIZED (
 SELECT `+newPeopleSignalColumns+`,`+humanNewPeopleVersionSQL+`,least(i.expires_at,city.expires_at,place.expires_at,(SELECT c.expires_at FROM communities c WHERE c.id=target.community_id)) FROM social_intents i `+newPeopleSignalJoins+`
 WHERE i.id=$2 AND i.creator_account_id=$1 AND `+newPeopleActive+`),
 current_candidates(intent_id,account_id,display_name,modality,constraints,city_id,place_id,native_token,deadline) AS (
 SELECT `+newPeopleSignalColumns+`,`+humanNewPeopleVersionSQL+`,least(i.expires_at,city.expires_at,place.expires_at,(SELECT c.expires_at FROM communities c WHERE c.id=target.community_id)) FROM social_intents i `+newPeopleSignalJoins+`
 WHERE `+newPeopleActive+newPeopleCandidateGate+` AND EXISTS(SELECT 1 FROM current_source))
 SELECT source.*,candidate.*`+policyColumns+` FROM current_source source LEFT JOIN current_candidates candidate ON true
 WHERE (NOT $5::boolean OR EXISTS(SELECT 1 FROM current_owner)) AND EXISTS(SELECT 1 FROM stamp WHERE source.deadline>stamp.at AND (candidate.deadline IS NULL OR candidate.deadline>stamp.at))
 `+policyWhere+` ORDER BY candidate.account_id,candidate.intent_id`, args...)
	if err != nil {
		return newpeople.Response{}, "", time.Time{}, err
	}
	peers := []newpeople.Signal{}
	var source newpeople.Signal
	foundSource := false
	for rows.Next() {
		var rawSource, rawPeer []byte
		var sourcePlace, sourceToken string
		var sourceDeadline time.Time
		var peerToken *string
		var peerDeadline *time.Time
		var peerID, peerAccountID, peerName, peerModality, peerCity, peerPlace *string
		dest := []any{&source.IntentID, &source.AccountID, &source.DisplayName, &source.Modality,
			&rawSource, &source.CityID, &sourcePlace, &sourceToken, &sourceDeadline, &peerID, &peerAccountID, &peerName, &peerModality,
			&rawPeer, &peerCity, &peerPlace, &peerToken, &peerDeadline}
		if nativeRead && native[0].Policy != nil {
			if native[0].Observed == nil {
				rows.Close()
				return newpeople.Response{}, "", time.Time{}, newpeople.ErrForbidden
			}
			dest = append(dest, native[0].Observed)
		}
		if err = rows.Scan(dest...); err != nil {
			rows.Close()
			return newpeople.Response{}, "", time.Time{}, err
		}
		source, err = decodeNewPeopleSignal(source, rawSource, sourcePlace)
		if err != nil {
			rows.Close()
			return newpeople.Response{}, "", time.Time{}, newpeople.ErrNotFound
		}
		foundSource = true
		tokens = append(tokens, sourceToken)
		if deadline.IsZero() || sourceDeadline.Before(deadline) {
			deadline = sourceDeadline
		}
		if peerToken != nil {
			tokens = append(tokens, *peerToken)
		}
		if peerDeadline != nil && peerDeadline.Before(deadline) {
			deadline = *peerDeadline
		}
		if peerID == nil {
			continue
		}
		if peerAccountID == nil || peerName == nil || peerModality == nil || peerCity == nil || peerPlace == nil {
			rows.Close()
			return newpeople.Response{}, "", time.Time{}, newpeople.ErrInvalid
		}
		peer, e := decodeNewPeopleSignal(newpeople.Signal{IntentID: *peerID, AccountID: *peerAccountID,
			DisplayName: *peerName, Modality: *peerModality, CityID: *peerCity}, rawPeer, *peerPlace)
		if errors.Is(e, newpeople.ErrInvalid) {
			continue
		}
		if e != nil {
			rows.Close()
			return newpeople.Response{}, "", time.Time{}, e
		}
		peers = append(peers, peer)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return newpeople.Response{}, "", time.Time{}, err
	}
	if !foundSource {
		return newpeople.Response{}, "", time.Time{}, newpeople.ErrNotFound
	}
	raw, err := json.Marshal(tokens)
	if err != nil {
		return newpeople.Response{}, "", time.Time{}, err
	}
	return newpeople.BuildResponse(source, peers), humanNewPeopleDigest(raw), deadline, nil
}

func (s *Store) InviteNewPeople(ctx context.Context, owner, sourceID, candidateID, note string) (connection.Request, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return connection.Request{}, err
	}
	defer tx.Rollback(ctx)
	if err = messageAuthenticateCurrent(ctx, tx, owner); err != nil {
		return connection.Request{}, err
	}
	var peerID string
	err = tx.QueryRow(ctx, `SELECT creator_account_id FROM social_intents WHERE id=$1`, candidateID).Scan(&peerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return connection.Request{}, newpeople.ErrNotFound
	}
	if err != nil {
		return connection.Request{}, err
	}
	if peerID == owner {
		return connection.Request{}, newpeople.ErrNotFound
	}
	// Serialize concurrent choices, profile/account changes and Blocks. Existing
	// Block's target FOR SHARE waits here, then ends any request created before it.
	if err = lockNewPeopleRows(ctx, tx, `SELECT id FROM accounts WHERE id=ANY($1::uuid[]) ORDER BY id FOR NO KEY UPDATE`, []string{owner, peerID}); err != nil {
		return connection.Request{}, err
	}
	if err = lockNewPeopleRows(ctx, tx, `SELECT account_id FROM user_profiles WHERE account_id=ANY($1::uuid[]) ORDER BY account_id FOR SHARE`, []string{owner, peerID}); err != nil {
		return connection.Request{}, err
	}
	if err = lockNewPeopleRows(ctx, tx, `SELECT account_id FROM person_new_people_consent WHERE account_id=ANY($1::uuid[]) ORDER BY account_id FOR SHARE`, []string{owner, peerID}); err != nil {
		return connection.Request{}, err
	}
	if err = lockNewPeopleRows(ctx, tx, `SELECT id FROM social_intents WHERE id=ANY($1::uuid[]) ORDER BY id FOR SHARE`, []string{sourceID, candidateID}); err != nil {
		return connection.Request{}, err
	}
	// Target revocation and Community membership changes cannot pass a stale
	// visibility check during the actual invitation write.
	if err = lockNewPeopleRows(ctx, tx, `SELECT intent_id FROM social_intent_audience_targets WHERE intent_id=ANY($1::uuid[]) ORDER BY intent_id FOR SHARE`, []string{sourceID, candidateID}); err != nil {
		return connection.Request{}, err
	}
	if err = lockNewPeopleRows(ctx, tx, `SELECT intent_id FROM social_intent_invitations WHERE intent_id=ANY($1::uuid[]) ORDER BY intent_id,invitee_account_id FOR SHARE`, []string{sourceID, candidateID}); err != nil {
		return connection.Request{}, err
	}
	if err = lockNewPeopleRows(ctx, tx, `SELECT user_account_id FROM community_memberships WHERE user_account_id=ANY($1::uuid[]) ORDER BY community_id,user_account_id FOR SHARE`, []string{owner, peerID}); err != nil {
		return connection.Request{}, err
	}
	for _, query := range []string{
		`SELECT id FROM contexts WHERE id IN(SELECT context_id FROM social_intents WHERE id=ANY($1::uuid[])) ORDER BY id FOR SHARE`,
		`SELECT id FROM cities WHERE id IN(SELECT coalesce(t.city_id,c.city_id) FROM social_intents i
 LEFT JOIN contexts c ON c.id=i.context_id AND c.context_type='CITY'
 LEFT JOIN social_intent_audience_targets t ON t.intent_id=i.id WHERE i.id=ANY($1::uuid[]))
 OR id IN(SELECT p.city_id FROM places p JOIN social_intents i ON lower(p.id::text)=lower(i.constraints->>'placeId') WHERE i.id=ANY($1::uuid[])) ORDER BY id FOR SHARE`,
		`SELECT id FROM places WHERE lower(id::text) IN(SELECT lower(constraints->>'placeId') FROM social_intents WHERE id=ANY($1::uuid[])) ORDER BY id FOR SHARE`,
		`SELECT id FROM communities WHERE id IN(SELECT community_id FROM social_intent_audience_targets WHERE intent_id=ANY($1::uuid[])) ORDER BY id FOR SHARE`,
	} {
		if err = lockNewPeopleRows(ctx, tx, query, []string{sourceID, candidateID}); err != nil {
			return connection.Request{}, err
		}
	}
	source, err := newPeopleSource(ctx, tx, owner, sourceID)
	if err != nil {
		return connection.Request{}, err
	}
	peer, err := scanNewPeopleSignal(tx.QueryRow(ctx, `SELECT `+newPeopleSignalColumns+` FROM social_intents i `+newPeopleSignalJoins+`
 WHERE i.id=$3 AND `+newPeopleActive+newPeopleCandidateGate, owner, sourceID, candidateID))
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, newpeople.ErrInvalid) {
		return connection.Request{}, newpeople.ErrNotFound
	}
	if err != nil {
		return connection.Request{}, err
	}
	if _, compatible := newpeople.Match(source, peer); !compatible {
		return connection.Request{}, newpeople.ErrNotFound
	}
	routeFence, err := startMessageRoute(ctx, tx, owner, peer.AccountID)
	if err != nil {
		return connection.Request{}, err
	}
	if !messageRequestAllowed(routeFence) {
		return connection.Request{}, connection.ErrNotFound
	}
	// Retain expiry of both original matched intents and their published scope.
	// The helper's own pending Request must not cause a false source re-read.
	var bothSources bool
	var sourceDeadline time.Time
	err = tx.QueryRow(ctx, `SELECT count(DISTINCT i.id)=2,min(LEAST(i.expires_at,city.expires_at,place.expires_at,(SELECT pc.expires_at FROM cities pc WHERE pc.id=place.city_id))) FROM social_intents i `+newPeopleSignalJoins+` WHERE i.id=ANY($1::uuid[]) AND `+newPeopleActive, []string{sourceID, candidateID}).Scan(&bothSources, &sourceDeadline)
	if err != nil {
		return connection.Request{}, err
	}
	if !bothSources {
		return connection.Request{}, newpeople.ErrNotFound
	}
	if sourceDeadline.Before(routeFence.until) {
		routeFence.until = sourceDeadline
	}
	request, err := createFriendRequestWithFenceTx(ctx, tx, owner, peer.AccountID, strings.TrimSpace(note), routeFence)
	if err != nil {
		return connection.Request{}, err
	}
	_, err = auditExec(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)
 VALUES($1,'invite','social_intent',$2,'allowed','explicit_new_people_invitation')`, owner, candidateID)
	if err != nil {
		return connection.Request{}, err
	}
	if err = finishMessageRoute(ctx, tx, routeFence); err != nil {
		return connection.Request{}, err
	}
	return request, tx.Commit(ctx)
}

// This is the same original intent/consent invitation writer, with a captured
// actual human Session. It does not route through a generic friend endpoint.
func (s *Store) InviteNewPeopleCurrent(ctx context.Context, a ea.Access, source, candidate, note, version string) (connection.Request, error) {
	ctx, e := s.messageCurrentAccess(ctx, a, version)
	if e != nil {
		return connection.Request{}, e
	}
	return s.InviteNewPeople(ctx, a.Actor.ID, source, candidate, note)
}

func lockNewPeopleRows(ctx context.Context, tx pgx.Tx, query string, ids []string) error {
	rows, err := tx.Query(ctx, query, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return err
		}
	}
	return rows.Err()
}
