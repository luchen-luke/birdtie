package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/jackc/pgx/v5"
)

const contextDeclarationColumns = `c.id::text,c.context_type,
    COALESCE(c.city_id,c.institution_key,c.online_key),
    COALESCE(city.name,c.institution_key,c.online_key),pc.relation,pc.visibility`

func scanContextDeclaration(row scanner) (contextgraph.Declaration, error) {
	var item contextgraph.Declaration
	err := row.Scan(&item.ContextID, &item.Type, &item.SourceKey,
		&item.Label, &item.Relation, &item.Visibility)
	return item, err
}

func (s *Store) ListOwnContextDeclarations(ctx context.Context, actor string) ([]contextgraph.Declaration, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+contextDeclarationColumns+` FROM person_contexts pc
		JOIN contexts c ON c.id=pc.context_id LEFT JOIN cities city ON city.id=c.city_id
		WHERE pc.person_account_id=$1 AND c.context_type IN ('CITY','INSTITUTION','ONLINE')
		ORDER BY pc.created_at,c.id,pc.relation`, actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []contextgraph.Declaration{}
	for rows.Next() {
		var item contextgraph.Declaration
		if err := rows.Scan(&item.ContextID, &item.Type, &item.SourceKey,
			&item.Label, &item.Relation, &item.Visibility); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) DeclareContext(ctx context.Context, actor string, input contextgraph.DeclarationInput) (contextgraph.Declaration, error) {
	input, err := contextgraph.NormalizeDeclaration(input)
	if err != nil {
		return contextgraph.Declaration{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return contextgraph.Declaration{}, err
	}
	defer tx.Rollback(ctx)
	var locked string
	if err := tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person'
		AND status='active' FOR UPDATE`, actor).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return contextgraph.Declaration{}, contextgraph.ErrUnavailable
		}
		return contextgraph.Declaration{}, err
	}
	var contextID string
	switch input.Type {
	case contextgraph.City:
		err = tx.QueryRow(ctx, `SELECT c.id FROM contexts c JOIN cities city ON city.id=c.city_id
			WHERE c.context_type='CITY' AND c.city_id=$1 AND city.publication_status='published'`,
			input.SourceKey).Scan(&contextID)
	case contextgraph.Institution:
		_, err = tx.Exec(ctx, `INSERT INTO contexts(context_type,institution_key)
			VALUES('INSTITUTION',$1) ON CONFLICT DO NOTHING`, input.SourceKey)
		if err == nil {
			err = tx.QueryRow(ctx, `SELECT id FROM contexts WHERE context_type='INSTITUTION'
				AND institution_key=$1`, input.SourceKey).Scan(&contextID)
		}
	case contextgraph.Online:
		_, err = tx.Exec(ctx, `INSERT INTO contexts(context_type,online_key)
			VALUES('ONLINE',$1) ON CONFLICT DO NOTHING`, input.SourceKey)
		if err == nil {
			err = tx.QueryRow(ctx, `SELECT id FROM contexts WHERE context_type='ONLINE'
				AND online_key=$1`, input.SourceKey).Scan(&contextID)
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return contextgraph.Declaration{}, contextgraph.ErrUnavailable
	}
	if err != nil {
		return contextgraph.Declaration{}, err
	}
	if input.Type == contextgraph.City && input.Relation == "current" {
		if _, err = tx.Exec(ctx, `DELETE FROM person_contexts pc USING contexts c
			WHERE pc.context_id=c.id AND pc.person_account_id=$1 AND pc.relation='current'
			AND c.context_type='CITY'`, actor); err != nil {
			return contextgraph.Declaration{}, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO person_contexts(person_account_id,context_id,relation,visibility)
		VALUES($1,$2,$3,'private') ON CONFLICT DO NOTHING`, actor, contextID, input.Relation); err != nil {
		return contextgraph.Declaration{}, err
	}
	item, err := scanContextDeclaration(tx.QueryRow(ctx, `SELECT `+contextDeclarationColumns+`
		FROM person_contexts pc JOIN contexts c ON c.id=pc.context_id
		LEFT JOIN cities city ON city.id=c.city_id
		WHERE pc.person_account_id=$1 AND pc.context_id=$2 AND pc.relation=$3`,
		actor, contextID, input.Relation))
	if err != nil {
		return contextgraph.Declaration{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return contextgraph.Declaration{}, err
	}
	return item, nil
}

func (s *Store) RemoveContextDeclaration(ctx context.Context, actor, contextID, relation string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM person_contexts pc USING contexts c WHERE pc.context_id=c.id AND c.context_type<>'COMMUNITY' AND pc.person_account_id=$1
		AND pc.context_id=$2 AND pc.relation=$3`, actor, contextID, relation)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return contextgraph.ErrNotFound
	}
	return nil
}
