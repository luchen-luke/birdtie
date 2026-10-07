package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5"
)

const socialIntentColumns = `id,creator_account_id,intent_type,title,constraints,
	audience,modality,context_id,status,expires_at,created_at,updated_at,converted_activity_id,converted_participation_id,converted_at`

func scanSocialIntent(row scanner) (socialintent.Record, error) {
	var item socialintent.Record
	var constraints []byte
	err := row.Scan(&item.ID, &item.CreatorID, &item.Type, &item.Title,
		&constraints, &item.Audience, &item.Modality, &item.ContextID,
		&item.Status, &item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt, &item.ConvertedActivityID, &item.ConvertedParticipationID, &item.ConvertedAt)
	item.Constraints = json.RawMessage(constraints)
	item.Status = socialintent.EffectiveStatus(item.Status, item.ExpiresAt, time.Now())
	return item, err
}

func (s *Store) CreateSocialIntentDraft(ctx context.Context, creatorID string, input socialintent.DraftInput) (socialintent.Record, error) {
	if input.OperationID != "" {
		return socialintent.Record{}, socialintent.ErrCreationInvalid
	}
	return s.createSocialIntentDraft(ctx, creatorID, "", input)
}

func (s *Store) CreateSocialIntentDraftFromTask(ctx context.Context, creatorID, taskID string, input socialintent.DraftInput) (socialintent.Record, error) {
	if input.OperationID != "" {
		return socialintent.Record{}, socialintent.ErrCreationInvalid
	}
	return s.createSocialIntentDraft(ctx, creatorID, taskID, input)
}

func (s *Store) createSocialIntentDraft(ctx context.Context, creatorID, taskID string, input socialintent.DraftInput) (socialintent.Record, error) {
	constraints, normalized, err := socialintent.ParseConstraints(input.Constraints, input.Modality)
	if err != nil {
		return socialintent.Record{}, err
	}
	input.Constraints = normalized
	if constraints.PlaceID != "" {
		var published bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM places
			WHERE id=$1 AND publication_status='published'
			AND (expires_at IS NULL OR expires_at>now()))`, constraints.PlaceID).Scan(&published); err != nil {
			return socialintent.Record{}, err
		}
		if !published {
			return socialintent.Record{}, socialintent.ErrInvalidConstraints
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return socialintent.Record{}, err
	}
	defer tx.Rollback(ctx)
	item, err := s.createSocialIntentDraftInTx(ctx, tx, creatorID, taskID, input)
	if err != nil {
		return socialintent.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return socialintent.Record{}, err
	}
	return item, nil
}

// Both the unchanged legacy adapter and keyed native create use the original
// insert, audience validation and audit in the caller's ONE transaction.
func (s *Store) createSocialIntentDraftInTx(ctx context.Context, tx pgx.Tx, creatorID, taskID string, input socialintent.DraftInput) (socialintent.Record, error) {
	constraints, normalized, err := socialintent.ParseConstraints(input.Constraints, input.Modality)
	if err != nil {
		return socialintent.Record{}, err
	}
	input.Constraints = normalized
	if constraints.PlaceID != "" {
		var published bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM places WHERE id=$1 AND publication_status='published' AND(expires_at IS NULL OR expires_at>clock_timestamp()))`, constraints.PlaceID).Scan(&published); err != nil {
			return socialintent.Record{}, err
		}
		if !published {
			return socialintent.Record{}, socialintent.ErrInvalidConstraints
		}
	}
	var contextID any
	if input.ContextID != "" {
		contextID = input.ContextID
	}
	if taskID != "" {
		var eligible bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_tasks t
			JOIN accounts a ON a.id=t.owner_account_id AND a.account_type='person' AND a.status='active'
			WHERE t.id=$1 AND t.owner_account_id=$2 AND t.acting_user_account_id=$2
			AND t.principal_type='person' AND t.intent='FIND_ACTIVITY' AND t.status='COMPLETED')`, taskID, creatorID).Scan(&eligible); err != nil {
			return socialintent.Record{}, err
		}
		if !eligible {
			return socialintent.Record{}, socialintent.ErrNotFound
		}
	}
	if input.Audience == "LOCAL" {
		var allowed bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cities
			WHERE id=$1 AND publication_status='published')`, input.CityID).Scan(&allowed); err != nil {
			return socialintent.Record{}, err
		}
		if !allowed {
			return socialintent.Record{}, socialintent.ErrInvalidAudience
		}
	} else if input.Audience == "COMMUNITY" {
		var allowed bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM communities c
			JOIN community_memberships m ON m.community_id=c.id AND m.user_account_id=$2 AND m.status='active'
			WHERE c.id=$1 AND c.lifecycle_status='active')`, input.CommunityID, creatorID).Scan(&allowed); err != nil {
			return socialintent.Record{}, err
		}
		if !allowed {
			return socialintent.Record{}, socialintent.ErrInvalidAudience
		}
	} else if input.Audience == "INVITE_ONLY" {
		for _, inviteeID := range input.InviteeIDs {
			if inviteeID == creatorID {
				return socialintent.Record{}, socialintent.ErrInvalidAudience
			}
			var allowed bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts a
				WHERE a.id=$1 AND a.account_type='person' AND a.status='active'
				AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
				 (b.blocker_account_id=$1 AND b.blocked_account_id=$2) OR
				 (b.blocker_account_id=$2 AND b.blocked_account_id=$1)))`, inviteeID, creatorID).Scan(&allowed); err != nil {
				return socialintent.Record{}, err
			}
			if !allowed {
				return socialintent.Record{}, socialintent.ErrInvalidAudience
			}
		}
	}
	insert := `INSERT INTO social_intents
		(creator_account_id,intent_type,title,constraints,audience,modality,context_id,expires_at)
		SELECT a.id,$2,$3,$4::jsonb,$5,$6,$7,$8 FROM accounts a
		WHERE a.id=$1 AND a.account_type='person' AND a.status='active'
		RETURNING ` + socialIntentColumns
	args := []any{creatorID, input.Type, input.Title, []byte(input.Constraints), input.Audience,
		input.Modality, contextID, input.ExpiresAt}
	if taskID != "" {
		insert = `INSERT INTO social_intents
			(creator_account_id,intent_type,title,constraints,audience,modality,context_id,expires_at,source_agent_task_id)
			SELECT a.id,$2,$3,$4::jsonb,$5,$6,$7,$8,$9 FROM accounts a
			WHERE a.id=$1 AND a.account_type='person' AND a.status='active'
			ON CONFLICT (source_agent_task_id) WHERE source_agent_task_id IS NOT NULL DO NOTHING
			RETURNING ` + socialIntentColumns
		args = append(args, taskID)
	}
	item, err := scanSocialIntent(tx.QueryRow(ctx, insert, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		if taskID != "" {
			return socialintent.Record{}, socialintent.ErrConflict
		}
		return socialintent.Record{}, socialintent.ErrNotFound
	}
	if err != nil {
		return socialintent.Record{}, err
	}
	if input.Audience == "LOCAL" || input.Audience == "COMMUNITY" {
		var cityID, communityID any
		if input.CityID != "" {
			cityID = input.CityID
		}
		if input.CommunityID != "" {
			communityID = input.CommunityID
		}
		if _, err := tx.Exec(ctx, `INSERT INTO social_intent_audience_targets(intent_id,city_id,community_id)
			VALUES($1,$2,$3)`, item.ID, cityID, communityID); err != nil {
			return socialintent.Record{}, err
		}
	}
	for _, inviteeID := range input.InviteeIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO social_intent_invitations(intent_id,invitee_account_id)
			VALUES($1,$2)`, item.ID, inviteeID); err != nil {
			return socialintent.Record{}, err
		}
	}
	if err := insertDomainAudit(ctx, tx, creatorID, "create", "social_intent", item.ID, "human_social_intent_draft", nil); err != nil {
		return socialintent.Record{}, err
	}
	item.CityID, item.CommunityID, item.InviteeIDs = input.CityID, input.CommunityID, input.InviteeIDs
	return item, nil
}

func (s *Store) ListOwnSocialIntents(ctx context.Context, creatorID string) ([]socialintent.Record, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+socialIntentColumns+`
		FROM social_intents WHERE creator_account_id=$1
		AND EXISTS(SELECT 1 FROM accounts WHERE id=$1 AND account_type='person' AND status='active')
		ORDER BY created_at DESC,id DESC LIMIT 100`, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []socialintent.Record{}
	for rows.Next() {
		item, err := scanSocialIntent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range items {
		if err := s.fillSocialIntentAudience(ctx, &items[i]); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *Store) GetOwnSocialIntent(ctx context.Context, creatorID, id string) (socialintent.Record, error) {
	item, err := scanSocialIntent(s.pool.QueryRow(ctx, `SELECT `+socialIntentColumns+`
		FROM social_intents WHERE id=$1 AND creator_account_id=$2
		AND EXISTS(SELECT 1 FROM accounts WHERE id=$2 AND account_type='person' AND status='active')`, id, creatorID))
	if errors.Is(err, pgx.ErrNoRows) {
		return socialintent.Record{}, socialintent.ErrNotFound
	}
	if err == nil {
		err = s.fillSocialIntentAudience(ctx, &item)
	}
	return item, err
}

func (s *Store) fillSocialIntentAudience(ctx context.Context, item *socialintent.Record) error {
	switch item.Audience {
	case "LOCAL", "COMMUNITY":
		var cityID, communityID *string
		if err := s.pool.QueryRow(ctx, `SELECT city_id,community_id
			FROM social_intent_audience_targets WHERE intent_id=$1`, item.ID).Scan(&cityID, &communityID); err != nil {
			return err
		}
		if cityID != nil {
			item.CityID = *cityID
		}
		if communityID != nil {
			item.CommunityID = *communityID
		}
	case "INVITE_ONLY":
		rows, err := s.pool.Query(ctx, `SELECT invitee_account_id FROM social_intent_invitations
			WHERE intent_id=$1 AND status='invited' ORDER BY invitee_account_id`, item.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		item.InviteeIDs = []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			item.InviteeIDs = append(item.InviteeIDs, id)
		}
		return rows.Err()
	}
	return nil
}

func (s *Store) ListVisibleSocialIntents(ctx context.Context, viewerID string) ([]socialintent.Record, error) {
	var viewer any
	if viewerID != "" {
		viewer = viewerID
	}
	rows, err := s.pool.Query(ctx, `SELECT `+socialIntentColumns+`
		FROM social_intents WHERE birdtie_social_intent_visible_to(id,$1::uuid)
		ORDER BY created_at DESC,id DESC LIMIT 50`, viewer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []socialintent.Record{}
	for rows.Next() {
		item, err := scanSocialIntent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetVisibleSocialIntent(ctx context.Context, viewerID, id string) (socialintent.Record, error) {
	var viewer any
	if viewerID != "" {
		viewer = viewerID
	}
	item, err := scanSocialIntent(s.pool.QueryRow(ctx, `SELECT `+socialIntentColumns+`
		FROM social_intents WHERE id=$1 AND birdtie_social_intent_visible_to(id,$2::uuid)`, id, viewer))
	if errors.Is(err, pgx.ErrNoRows) {
		return socialintent.Record{}, socialintent.ErrNotFound
	}
	return item, err
}

func (s *Store) ActivateSocialIntent(ctx context.Context, ownerID, id string) (socialintent.Record, error) {
	return s.transitionSocialIntent(ctx, ownerID, id, socialintent.Active)
}

func (s *Store) CancelSocialIntent(ctx context.Context, ownerID, id string) (socialintent.Record, error) {
	return s.transitionSocialIntent(ctx, ownerID, id, socialintent.Cancelled)
}

func (s *Store) transitionSocialIntent(ctx context.Context, ownerID, id, target string) (socialintent.Record, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return socialintent.Record{}, err
	}
	defer tx.Rollback(ctx)
	var status, audience string
	var expiresAt time.Time
	var constraints []byte
	err = tx.QueryRow(ctx, `SELECT i.status,i.expires_at,i.audience,i.constraints
		FROM social_intents i JOIN accounts owner ON owner.id=i.creator_account_id
		AND owner.account_type='person' AND owner.status='active'
		WHERE i.id=$1 AND i.creator_account_id=$2 FOR UPDATE OF i`, id, ownerID).
		Scan(&status, &expiresAt, &audience, &constraints)
	if errors.Is(err, pgx.ErrNoRows) {
		return socialintent.Record{}, socialintent.ErrNotFound
	}
	if err != nil {
		return socialintent.Record{}, err
	}
	if !expiresAt.After(time.Now()) || !socialintent.CanTransition(status, target) {
		return socialintent.Record{}, socialintent.ErrConflict
	}
	if target == socialintent.Active {
		if err := validateSocialIntentActivation(ctx, tx, ownerID, id, audience, constraints); err != nil {
			return socialintent.Record{}, err
		}
	}
	item, err := scanSocialIntent(tx.QueryRow(ctx, `UPDATE social_intents
		SET status=$3,updated_at=now() WHERE id=$1 AND creator_account_id=$2
		RETURNING `+socialIntentColumns, id, ownerID, target))
	if err != nil {
		return socialintent.Record{}, err
	}
	action := "cancel"
	purpose := "owner_request"
	if target == socialintent.Active {
		action, purpose = "activate", "owner_confirmed"
	}
	if _, err := auditExec(ctx, tx, `INSERT INTO audit_events
		(actor_account_id,action,resource_type,resource_id,decision,purpose)
		VALUES($1,$2,'social_intent',$3,'allowed',$4)`, ownerID, action, id, purpose); err != nil {
		return socialintent.Record{}, err
	}
	if target == socialintent.Active {
		if err := routeOpportunityIntentOwner(ctx, tx, ownerID); err != nil {
			return socialintent.Record{}, err
		}
	}
	if err := s.fillSocialIntentAudience(ctx, &item); err != nil {
		return socialintent.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return socialintent.Record{}, err
	}
	return item, nil
}

func validateSocialIntentActivation(ctx context.Context, tx pgx.Tx, ownerID, id, audience string, raw []byte) error {
	// Activation repeats checks that may have changed since the draft was made.
	var modality string
	if err := tx.QueryRow(ctx, `SELECT modality FROM social_intents WHERE id=$1`, id).Scan(&modality); err != nil {
		return err
	}
	constraints, _, err := socialintent.ParseConstraints(json.RawMessage(raw), modality)
	if err != nil {
		return socialintent.ErrConflict
	}
	if constraints.PlaceID != "" {
		var published bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM places WHERE id=$1
			AND publication_status='published' AND (expires_at IS NULL OR expires_at>now()))`, constraints.PlaceID).Scan(&published); err != nil {
			return err
		}
		if !published {
			return socialintent.ErrConflict
		}
	}
	var allowed bool
	switch audience {
	case "PUBLIC", "LOCAL":
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_profiles
			WHERE account_id=$1 AND visibility='public')`, ownerID).Scan(&allowed); err != nil {
			return err
		}
		if !allowed {
			return socialintent.ErrConflict
		}
	}
	switch audience {
	case "LOCAL":
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM social_intent_audience_targets target
			JOIN cities city ON city.id=target.city_id AND city.publication_status='published'
			WHERE target.intent_id=$1)`, id).Scan(&allowed)
	case "COMMUNITY":
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM social_intent_audience_targets target
			JOIN communities c ON c.id=target.community_id AND c.lifecycle_status='active'
			JOIN community_memberships m ON m.community_id=c.id AND m.user_account_id=$2 AND m.status='active'
			WHERE target.intent_id=$1)`, id, ownerID).Scan(&allowed)
	case "INVITE_ONLY":
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM social_intent_invitations invitation
			JOIN accounts invitee ON invitee.id=invitation.invitee_account_id
			AND invitee.account_type='person' AND invitee.status='active'
			WHERE invitation.intent_id=$1 AND invitation.status='invited'
			AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
			 (b.blocker_account_id=$2 AND b.blocked_account_id=invitee.id) OR
			 (b.blocker_account_id=invitee.id AND b.blocked_account_id=$2)))`, id, ownerID).Scan(&allowed)
	default:
		allowed = true
	}
	if err != nil {
		return err
	}
	if !allowed {
		return socialintent.ErrConflict
	}
	return nil
}
