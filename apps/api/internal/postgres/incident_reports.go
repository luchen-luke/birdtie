package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/activitychat"
	"github.com/birdtie/birdtie/apps/api/internal/communitychat"
	"github.com/birdtie/birdtie/apps/api/internal/safety"
	"github.com/jackc/pgx/v5"
)

func scanReport(row scanner) (safety.Report, error) {
	var item safety.Report
	err := row.Scan(&item.ID, &item.TargetType, &item.TargetID, &item.Reason,
		&item.Details, &item.Status, &item.CreatedAt)
	return item, err
}

const reportColumns = `id,target_type,target_id,reason,details,status,created_at`

func (s *Store) CreateReport(ctx context.Context, reporterID string, input safety.ReportInput) (safety.Report, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return safety.Report{}, err
	}
	defer tx.Rollback(ctx)
	var locked string
	if err := tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR UPDATE`, reporterID).Scan(&locked); err != nil {
		return safety.Report{}, err
	}
	var recent int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM incident_reports
		WHERE reporter_account_id=$1 AND created_at>now()-interval '24 hours'`, reporterID).Scan(&recent); err != nil {
		return safety.Report{}, err
	}
	if recent >= 5 {
		return safety.Report{}, safety.ErrRateLimited
	}
	var targetID any
	communityMessageAllowed := false
	if input.TargetType == "community_message" {
		var communityID string
		err := tx.QueryRow(ctx, `SELECT c.community_id FROM community_conversation_messages m
			JOIN community_conversations c ON c.id=m.conversation_id
			JOIN community_conversation_members member ON member.conversation_id=c.id
				AND member.account_id=$2 AND member.status='active'
			WHERE m.id=$1 AND m.sender_account_id<>$2 AND m.removed_at IS NULL
			AND EXISTS(SELECT 1 FROM accounts WHERE id=m.sender_account_id AND status='active')
			AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
				(b.blocker_account_id=$2 AND b.blocked_account_id=m.sender_account_id) OR
				(b.blocker_account_id=m.sender_account_id AND b.blocked_account_id=$2))`, input.TargetID, reporterID).Scan(&communityID)
		if errors.Is(err, pgx.ErrNoRows) {
			return safety.Report{}, safety.ErrTargetNotFound
		}
		if err != nil {
			return safety.Report{}, err
		}
		state, err := communityChatState(ctx, tx, reporterID, communityID)
		if errors.Is(err, communitychat.ErrNotFound) || (err == nil && !state.Joined) {
			return safety.Report{}, safety.ErrTargetNotFound
		}
		if err != nil {
			return safety.Report{}, err
		}
		communityMessageAllowed = true
	}
	activityMessageAllowed := false
	if input.TargetType == "activity_message" {
		var activityID string
		err := tx.QueryRow(ctx, `SELECT c.activity_id FROM activity_conversation_messages m
			JOIN activity_conversations c ON c.id=m.conversation_id
			JOIN activity_conversation_members member ON member.conversation_id=c.id
				AND member.account_id=$2 AND member.status='active'
			WHERE m.id=$1 AND m.sender_account_id<>$2 AND m.removed_at IS NULL
			AND EXISTS(SELECT 1 FROM accounts WHERE id=m.sender_account_id AND status='active')
			AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
				(b.blocker_account_id=$2 AND b.blocked_account_id=m.sender_account_id) OR
				(b.blocker_account_id=m.sender_account_id AND b.blocked_account_id=$2))`, input.TargetID, reporterID).Scan(&activityID)
		if errors.Is(err, pgx.ErrNoRows) {
			return safety.Report{}, safety.ErrTargetNotFound
		}
		if err != nil {
			return safety.Report{}, err
		}
		state, err := chatState(ctx, tx, reporterID, activityID)
		if errors.Is(err, activitychat.ErrNotFound) || (err == nil && !state.Joined) {
			return safety.Report{}, safety.ErrTargetNotFound
		}
		if err != nil {
			return safety.Report{}, err
		}
		activityMessageAllowed = true
	}
	if input.TargetID != "" {
		targetID = input.TargetID
	}
	item, err := scanReport(tx.QueryRow(ctx, `INSERT INTO incident_reports
		(reporter_account_id,target_type,target_id,reason,details)
		SELECT $1,$2,$3,$4,$5 WHERE
		($2='general' OR ($2='activity_message' AND $6::boolean) OR ($2='community_message' AND $7::boolean) OR
		 ($2='activity' AND EXISTS (SELECT 1 FROM activities a WHERE a.id=$3 AND a.publication_status='published')) OR
		 ($2='organization' AND EXISTS (SELECT 1 FROM organizations o WHERE o.id=$3 AND o.status='active')) OR
		 ($2='account' AND EXISTS (SELECT 1 FROM accounts a WHERE a.id=$3 AND a.status='active')) OR
		 ($2='message' AND EXISTS (SELECT 1 FROM conversation_messages m
		   JOIN conversations c ON c.id=m.conversation_id
		   WHERE m.id=$3 AND m.sender_account_id<>$1
		   AND $1 IN (c.member_a_account_id,c.member_b_account_id))) OR
		 ($2='community' AND EXISTS (SELECT 1 FROM communities c WHERE c.id=$3
		   AND c.lifecycle_status='active' AND
		   ((c.publication_status='published' AND c.visibility<>'hidden') OR
		    EXISTS (SELECT 1 FROM community_memberships cm WHERE cm.community_id=c.id
		      AND cm.user_account_id=$1 AND cm.status='active')))) OR
		 ($2='business' AND EXISTS (SELECT 1 FROM businesses b
		   JOIN accounts principal ON principal.id=b.account_id
		   WHERE b.id=$3 AND b.status='active' AND b.claim_status='verified'
		   AND principal.status='active')))
		RETURNING `+reportColumns, reporterID, input.TargetType, targetID, input.Reason, input.Details, activityMessageAllowed, communityMessageAllowed))
	if errors.Is(err, pgx.ErrNoRows) {
		return safety.Report{}, safety.ErrTargetNotFound
	}
	if err != nil {
		return safety.Report{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return safety.Report{}, err
	}
	return item, nil
}

func (s *Store) ListOwnReports(ctx context.Context, reporterID string) ([]safety.Report, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+reportColumns+` FROM incident_reports
		WHERE reporter_account_id=$1 ORDER BY created_at DESC,id DESC LIMIT 50`, reporterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []safety.Report{}
	for rows.Next() {
		item, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
