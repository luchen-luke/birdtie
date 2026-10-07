package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/follow"
	"github.com/jackc/pgx/v5/pgconn"
)

func followColumn(kind string) string {
	switch kind {
	case follow.Person:
		return "person_account_id"
	case follow.Organization:
		return "organization_id"
	case follow.Community:
		return "community_id"
	case follow.Business:
		return "business_id"
	default:
		return ""
	}
}

func followTargetVisible(kind string) string {
	switch kind {
	case follow.Person:
		return `EXISTS(SELECT 1 FROM accounts a JOIN user_profiles p ON p.account_id=a.id
			WHERE a.id=$2 AND a.account_type='person' AND a.status='active' AND p.visibility='public'
			AND a.id<>$1 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
			(b.blocker_account_id=$1 AND b.blocked_account_id=a.id) OR
			(b.blocker_account_id=a.id AND b.blocked_account_id=$1)))`
	case follow.Organization:
		return `EXISTS(SELECT 1 FROM organizations o JOIN accounts a ON a.id=o.account_id
			WHERE o.id=$2 AND o.status='active' AND a.status='active')`
	case follow.Community:
		return `EXISTS(SELECT 1 FROM communities c WHERE c.id=$2 AND c.lifecycle_status='active'
			AND c.publication_status='published' AND c.visibility='public')`
	case follow.Business:
		return `EXISTS(SELECT 1 FROM businesses b JOIN accounts a ON a.id=b.account_id
			WHERE b.id=$2 AND b.status='active' AND b.claim_status='verified' AND a.status='active')`
	default:
		return "FALSE"
	}
}

func (s *Store) Follow(ctx context.Context, actor string, target follow.Target) (follow.Record, error) {
	target, err := follow.Normalize(target)
	if err != nil {
		return follow.Record{}, err
	}
	column := followColumn(target.Type)
	if column == "" {
		return follow.Record{}, follow.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return follow.Record{}, err
	}
	defer tx.Rollback(ctx)
	var allowed bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE id=$1
		AND account_type='person' AND status='active') AND `+followTargetVisible(target.Type),
		actor, target.ID).Scan(&allowed)
	if err != nil {
		return follow.Record{}, err
	}
	if !allowed {
		return follow.Record{}, follow.ErrUnavailable
	}
	_, err = tx.Exec(ctx, `INSERT INTO follows(follower_account_id,`+column+`)
		VALUES($1,$2) ON CONFLICT DO NOTHING`, actor, target.ID)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "P0001" {
		return follow.Record{}, follow.ErrUnavailable
	}
	if err != nil {
		return follow.Record{}, err
	}
	var item follow.Record
	item.Target = target
	err = tx.QueryRow(ctx, `SELECT id,created_at FROM follows WHERE follower_account_id=$1
		AND `+column+`=$2`, actor, target.ID).Scan(&item.ID, &item.CreatedAt)
	if err != nil {
		return follow.Record{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return follow.Record{}, err
	}
	return item, nil
}

func (s *Store) Unfollow(ctx context.Context, actor string, target follow.Target) error {
	target, err := follow.Normalize(target)
	if err != nil {
		return err
	}
	column := followColumn(target.Type)
	if column == "" {
		return follow.ErrInvalid
	}
	result, err := s.pool.Exec(ctx, `DELETE FROM follows WHERE follower_account_id=$1
		AND `+column+`=$2`, actor, target.ID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return follow.ErrNotFound
	}
	return nil
}

func (s *Store) ListOwnFollows(ctx context.Context, actor string) ([]follow.Record, error) {
	var installed bool
	if err := s.pool.QueryRow(ctx, `SELECT to_regclass('public.follows') IS NOT NULL`).Scan(&installed); err != nil {
		return nil, err
	}
	if !installed {
		return []follow.Record{}, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT f.id,
		CASE WHEN f.person_account_id IS NOT NULL THEN 'PERSON'
			WHEN f.organization_id IS NOT NULL THEN 'ORGANIZATION'
			WHEN f.community_id IS NOT NULL THEN 'COMMUNITY' ELSE 'BUSINESS' END,
		COALESCE(f.person_account_id,f.organization_id,f.community_id,f.business_id)::text,
		CASE WHEN f.person_account_id IS NOT NULL THEN
            CASE WHEN birdtie_agent_profile_field_allowed(person.id,$1::uuid,'displayName')
                THEN COALESCE(NULLIF(p.display_name,''),NULLIF(person.handle,''),'Birdtie 成员')
                ELSE 'Birdtie 成员' END
            ELSE COALESCE(o.name,c.name,b.name,'Birdtie 成员') END,
		f.created_at
		FROM follows f
		LEFT JOIN accounts person ON person.id=f.person_account_id AND person.status='active'
		LEFT JOIN user_profiles p ON p.account_id=person.id AND p.visibility='public'
		LEFT JOIN organizations o ON o.id=f.organization_id AND o.status='active'
		LEFT JOIN communities c ON c.id=f.community_id AND c.lifecycle_status='active'
			AND c.publication_status='published' AND c.visibility='public'
		LEFT JOIN businesses b ON b.id=f.business_id AND b.status='active' AND b.claim_status='verified'
		WHERE f.follower_account_id=$1 AND
		 ((p.account_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM account_blocks block WHERE
			(block.blocker_account_id=$1 AND block.blocked_account_id=person.id) OR
			(block.blocker_account_id=person.id AND block.blocked_account_id=$1))) OR
		  (o.id IS NOT NULL AND EXISTS(SELECT 1 FROM accounts a WHERE a.id=o.account_id AND a.status='active')) OR
		  c.id IS NOT NULL OR
		  (b.id IS NOT NULL AND EXISTS(SELECT 1 FROM accounts a WHERE a.id=b.account_id AND a.status='active')))
		ORDER BY f.created_at DESC,f.id DESC`, actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []follow.Record{}
	for rows.Next() {
		var item follow.Record
		if err := rows.Scan(&item.ID, &item.Target.Type, &item.Target.ID,
			&item.Label, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
