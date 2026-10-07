package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/socialcontext"
	"github.com/jackc/pgx/v5"
)

func (s *Store) OwnSocialDisclosure(ctx context.Context, actor string) (socialcontext.Disclosure, error) {
	var out socialcontext.Disclosure
	err := s.pool.QueryRow(ctx, `SELECT coalesce(d.mutual_ties,false),
        coalesce(d.shared_communities,false),coalesce(d.shared_activities,false)
        FROM accounts a LEFT JOIN person_social_disclosure d ON d.account_id=a.id
        WHERE a.id=$1 AND a.account_type='person' AND a.status='active'`, actor).
		Scan(&out.MutualTies, &out.SharedCommunities, &out.SharedActivities)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, socialcontext.ErrNotFound
	}
	return out, err
}

func (s *Store) SetSocialDisclosure(ctx context.Context, actor string, input socialcontext.Disclosure) (socialcontext.Disclosure, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return socialcontext.Disclosure{}, err
	}
	defer tx.Rollback(ctx)
	var active bool
	if err = tx.QueryRow(ctx, `SELECT true FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, actor).Scan(&active); errors.Is(err, pgx.ErrNoRows) {
		return socialcontext.Disclosure{}, socialcontext.ErrNotFound
	} else if err != nil {
		return socialcontext.Disclosure{}, err
	}
	result, err := tx.Exec(ctx, `INSERT INTO person_social_disclosure(account_id,mutual_ties,shared_communities,shared_activities)
        SELECT id,$2,$3,$4 FROM accounts WHERE id=$1 AND account_type='person' AND status='active'
        ON CONFLICT(account_id) DO UPDATE SET mutual_ties=EXCLUDED.mutual_ties,
        shared_communities=EXCLUDED.shared_communities,shared_activities=EXCLUDED.shared_activities,updated_at=now()
        WHERE (person_social_disclosure.mutual_ties,person_social_disclosure.shared_communities,person_social_disclosure.shared_activities)
        IS DISTINCT FROM (EXCLUDED.mutual_ties,EXCLUDED.shared_communities,EXCLUDED.shared_activities)`,
		actor, input.MutualTies, input.SharedCommunities, input.SharedActivities)
	if err != nil {
		return socialcontext.Disclosure{}, err
	}
	if result.RowsAffected() == 0 {
		return input, tx.Commit(ctx)
	}
	_, err = auditExec(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)
		VALUES($1::uuid,'update','social_disclosure',$1::uuid::text,'allowed','explicit_social_disclosure')`, actor)
	if err != nil {
		return socialcontext.Disclosure{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return socialcontext.Disclosure{}, err
	}
	return input, nil
}

func (s *Store) SharedSocialContext(ctx context.Context, viewer, target string) (socialcontext.Signals, error) {
	out := socialcontext.Signals{Communities: []socialcontext.Reference{}, Activities: []socialcontext.Reference{}}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	var ties, communities, activities bool
	err = tx.QueryRow(ctx, `SELECT coalesce(vd.mutual_ties AND td.mutual_ties,false),
        coalesce(vd.shared_communities AND td.shared_communities,false),
        coalesce(vd.shared_activities AND td.shared_activities,false)
        FROM accounts v JOIN accounts t ON t.id=$2
        JOIN user_profiles p ON p.account_id=t.id AND p.visibility='public'
        LEFT JOIN person_social_disclosure vd ON vd.account_id=v.id
        LEFT JOIN person_social_disclosure td ON td.account_id=t.id
        WHERE v.id=$1 AND v.account_type='person' AND v.status='active'
        AND t.account_type='person' AND t.status='active' AND v.id<>t.id
        AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
            (b.blocker_account_id=v.id AND b.blocked_account_id=t.id) OR
            (b.blocker_account_id=t.id AND b.blocked_account_id=v.id))`, viewer, target).
		Scan(&ties, &communities, &activities)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, socialcontext.ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if ties {
		err = tx.QueryRow(ctx, `WITH viewer_ties AS (
            SELECT CASE WHEN person_a_account_id=$1 THEN person_b_account_id ELSE person_a_account_id END AS peer
            FROM person_ties WHERE status='active' AND (person_a_account_id=$1 OR person_b_account_id=$1)),
        target_ties AS (
            SELECT CASE WHEN person_a_account_id=$2 THEN person_b_account_id ELSE person_a_account_id END AS peer
            FROM person_ties WHERE status='active' AND (person_a_account_id=$2 OR person_b_account_id=$2))
        SELECT count(*) FROM viewer_ties v JOIN target_ties t USING(peer)
        JOIN accounts a ON a.id=v.peer AND a.account_type='person' AND a.status='active'
        JOIN user_profiles p ON p.account_id=a.id AND p.visibility='public'
        JOIN person_social_disclosure d ON d.account_id=a.id AND d.mutual_ties
        WHERE NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
            (b.blocker_account_id=a.id AND b.blocked_account_id IN ($1,$2)) OR
            (b.blocked_account_id=a.id AND b.blocker_account_id IN ($1,$2)))`, viewer, target).Scan(&out.MutualCount)
		if err != nil {
			return out, err
		}
	}
	if communities {
		rows, e := tx.Query(ctx, `SELECT c.id,c.name FROM community_memberships v
            JOIN community_memberships t ON t.community_id=v.community_id AND t.user_account_id=$2 AND t.status='active'
            JOIN communities c ON c.id=v.community_id AND c.visibility='public'
                AND c.publication_status='published' AND c.lifecycle_status='active'
            WHERE v.user_account_id=$1 AND v.status='active' ORDER BY c.name,c.id LIMIT 51`, viewer, target)
		if e != nil {
			return out, e
		}
		for rows.Next() {
			var ref socialcontext.Reference
			if e = rows.Scan(&ref.ID, &ref.Title); e != nil {
				rows.Close()
				return out, e
			}
			out.Communities = append(out.Communities, ref)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
		if len(out.Communities) > 50 {
			out.Communities = out.Communities[:50]
			out.CommunitiesTruncated = true
		}
	}
	if activities {
		rows, e := tx.Query(ctx, `SELECT a.id,a.title FROM activity_participations v
            JOIN activity_participations t ON t.activity_id=v.activity_id AND t.participant_account_id=$2 AND t.status='going'
            JOIN activities a ON a.id=v.activity_id AND a.visibility='public' AND a.publication_status='published'
                AND a.cancelled_at IS NULL AND (a.expires_at IS NULL OR a.expires_at>now())
            JOIN cities c ON c.id=a.city_id AND c.publication_status='published'
            WHERE v.participant_account_id=$1 AND v.status='going'
                AND birdtie_activity_visible_to(a.id,$1) AND birdtie_activity_visible_to(a.id,$2)
                AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
                    (b.blocker_account_id=a.host_account_id AND b.blocked_account_id IN ($1,$2)) OR
                    (b.blocked_account_id=a.host_account_id AND b.blocker_account_id IN ($1,$2)))
            ORDER BY a.starts_at DESC,a.id LIMIT 51`, viewer, target)
		if e != nil {
			return out, e
		}
		for rows.Next() {
			var ref socialcontext.Reference
			if e = rows.Scan(&ref.ID, &ref.Title); e != nil {
				rows.Close()
				return out, e
			}
			out.Activities = append(out.Activities, ref)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
		if len(out.Activities) > 50 {
			out.Activities = out.Activities[:50]
			out.ActivitiesTruncated = true
		}
	}
	return out, tx.Commit(ctx)
}
