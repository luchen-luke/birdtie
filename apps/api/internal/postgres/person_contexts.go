package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
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

func declarationEffectTx(ctx context.Context, tx pgx.Tx, actor string, input contextgraph.DeclarationInput) (contextgraph.Declaration, error) {
	var locked string
	if err := tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person'
		AND status='active' FOR UPDATE`, actor).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return contextgraph.Declaration{}, contextgraph.ErrUnavailable
		}
		return contextgraph.Declaration{}, err
	}
	var contextID string
	var err error
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
	return item, nil
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
	defer tx.Rollback(context.Background())
	item, err := declarationEffectTx(ctx, tx, actor, input)
	if err != nil {
		return item, err
	}
	if err = tx.Commit(ctx); err != nil {
		return contextgraph.Declaration{}, err
	}
	return item, nil
}
func removeDeclarationEffectTx(ctx context.Context, tx pgx.Tx, actor, id, relation string) error {
	r, e := tx.Exec(ctx, `DELETE FROM person_contexts pc USING contexts c WHERE pc.context_id=c.id AND c.context_type IN('CITY','INSTITUTION','ONLINE') AND pc.person_account_id=$1 AND pc.context_id=$2 AND pc.relation=$3`, actor, id, relation)
	if e != nil {
		return e
	}
	if r.RowsAffected() != 1 {
		return contextgraph.ErrNotFound
	}
	return nil
}
func (s *Store) RemoveContextDeclaration(ctx context.Context, actor, id, relation string) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	if e = removeDeclarationEffectTx(ctx, tx, actor, id, relation); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

var _ contextgraph.DeclarationControlStore = (*Store)(nil)

func declarationControlSnapshot(ctx context.Context, f humanWriteFrame) (int64, []contextgraph.Declaration, string, error) {
	var version int64
	var epoch string
	e := f.tx.QueryRow(ctx, `SELECT COALESCE((SELECT version FROM person_declaration_control_epochs WHERE owner_account_id=$1),0),COALESCE((SELECT xmin::text FROM person_declaration_control_epochs WHERE owner_account_id=$1),'none')`, f.a.ID).Scan(&version, &epoch)
	if e != nil {
		return 0, nil, "", identity.ErrControlUnavailable
	}
	rows, e := f.tx.Query(ctx, `SELECT c.id::text,c.context_type,COALESCE(c.city_id,c.institution_key,c.online_key),COALESCE(CASE WHEN city.publication_status='published' AND(city.expires_at IS NULL OR city.expires_at>clock_timestamp()) THEN city.name END,c.institution_key,c.online_key,'城市资料暂不可用'),pc.relation,pc.visibility,concat_ws(':',pc.xmin::text,c.xmin::text,city.xmin::text) FROM person_contexts pc JOIN contexts c ON c.id=pc.context_id LEFT JOIN cities city ON city.id=c.city_id WHERE pc.person_account_id=$1 AND c.context_type IN('CITY','INSTITUTION','ONLINE') ORDER BY pc.created_at,c.id,pc.relation`, f.a.ID)
	if e != nil {
		return 0, nil, "", identity.ErrControlUnavailable
	}
	defer rows.Close()
	items := []contextgraph.Declaration{}
	epochs := []string{epoch}
	for rows.Next() {
		var v contextgraph.Declaration
		var rowEpoch string
		if e = rows.Scan(&v.ContextID, &v.Type, &v.SourceKey, &v.Label, &v.Relation, &v.Visibility, &rowEpoch); e != nil {
			return 0, nil, "", identity.ErrControlUnavailable
		}
		items = append(items, v)
		epochs = append(epochs, rowEpoch)
	}
	if e = rows.Err(); e != nil {
		return 0, nil, "", identity.ErrControlUnavailable
	}
	return version, items, humanWriteHashJSON([]any{version, items, epochs}), nil
}
func declarationControlDeadline(ctx context.Context, f humanWriteFrame) (*time.Time, error) {
	var until *time.Time
	e := f.tx.QueryRow(ctx, `SELECT min(city.expires_at) FROM person_contexts pc JOIN contexts c ON c.id=pc.context_id JOIN cities city ON city.id=c.city_id WHERE pc.person_account_id=$1 AND c.context_type='CITY' AND city.publication_status='published' AND city.expires_at>clock_timestamp()`, f.a.ID).Scan(&until)
	if e != nil {
		return nil, identity.ErrControlUnavailable
	}
	return until, nil
}
func declarationControlSource(ctx context.Context, f humanWriteFrame, in contextgraph.DeclarationControlInput) (string, string, *time.Time, error) {
	version, items, snapshot, e := declarationControlSnapshot(ctx, f)
	if e != nil {
		return "", "", nil, e
	}
	if version != in.ExpectedVersion {
		return "", "", nil, identity.ErrControlChanged
	}
	until, e := declarationControlDeadline(ctx, f)
	if e != nil {
		return "", "", nil, e
	}
	if in.Action == "REMOVE" {
		for _, v := range items {
			if v.ContextID == in.ContextID && v.Relation == in.Relation {
				return snapshot, "", until, nil
			}
		}
		return "", "", nil, identity.ErrNotFound
	}
	var contextID, authority string
	var deadline *time.Time
	if in.Type == contextgraph.City {
		e = f.tx.QueryRow(ctx, `SELECT c.id,concat_ws(':',c.xmin::text,city.xmin::text),city.expires_at FROM contexts c JOIN cities city ON city.id=c.city_id WHERE c.context_type='CITY' AND c.city_id=$1 AND city.publication_status='published' AND(city.expires_at IS NULL OR city.expires_at>clock_timestamp())`, in.SourceKey).Scan(&contextID, &authority, &deadline)
		if errors.Is(e, pgx.ErrNoRows) {
			return "", "", nil, identity.ErrNotFound
		}
		if e != nil {
			return "", "", nil, identity.ErrControlUnavailable
		}
	} else {
		var id, epoch *string
		column := "institution_key"
		if in.Type == contextgraph.Online {
			column = "online_key"
		}
		e = f.tx.QueryRow(ctx, `SELECT id,xmin::text FROM contexts WHERE context_type=$1 AND `+column+`=$2`, in.Type, in.SourceKey).Scan(&id, &epoch)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return "", "", nil, identity.ErrControlUnavailable
		}
		if id != nil {
			contextID = *id
			authority = *epoch
		} else {
			authority = "absent"
		}
	}
	for _, v := range items {
		if v.ContextID == contextID && v.Relation == in.Relation {
			return "", "", nil, identity.ErrControlChanged
		}
	}
	if until != nil && (deadline == nil || until.Before(*deadline)) {
		deadline = until
	}
	return identity.HumanControlTextHash(snapshot + ":" + authority), authority, deadline, nil
}
func declarationControlCityAuthority(ctx context.Context, f humanWriteFrame, in contextgraph.DeclarationControlInput) (string, error) {
	if in.Action != "DECLARE" || in.Type != contextgraph.City {
		return "", nil
	}
	var authority string
	e := f.tx.QueryRow(ctx, `SELECT concat_ws(':',c.xmin::text,city.xmin::text) FROM contexts c JOIN cities city ON city.id=c.city_id WHERE c.context_type='CITY' AND c.city_id=$1 AND city.publication_status='published' AND(city.expires_at IS NULL OR city.expires_at>clock_timestamp()) FOR SHARE OF c,city`, in.SourceKey).Scan(&authority)
	if e != nil {
		return "", identity.ErrControlChanged
	}
	return authority, nil
}
func (s *Store) ReadDeclarationControl(ctx context.Context, d [32]byte, a identity.Actor) (contextgraph.DeclarationControlState, error) {
	ctx, f, e := s.humanWriteBegin(ctx, d, a, "CONTEXT", "")
	if e != nil {
		return contextgraph.DeclarationControlState{}, e
	}
	defer f.tx.Rollback(context.Background())
	version, items, source, e := declarationControlSnapshot(ctx, f)
	if e != nil {
		return contextgraph.DeclarationControlState{}, e
	}
	sourceDeadline, e := declarationControlDeadline(ctx, f)
	if e != nil {
		return contextgraph.DeclarationControlState{}, e
	}
	now, until, e := s.humanWriteNow(ctx, f, sourceDeadline)
	if e != nil {
		return contextgraph.DeclarationControlState{}, e
	}
	check := func(ctx context.Context, next humanWriteFrame) error {
		_, _, current, e := declarationControlSnapshot(ctx, next)
		if e != nil {
			return e
		}
		if current != source {
			return identity.ErrControlChanged
		}
		return nil
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, until, func() error { return check(ctx, f) })); e != nil {
		return contextgraph.DeclarationControlState{}, e
	}
	return contextgraph.DeclarationControlState{Version: version, Items: items, ObservedAt: now, ExpiresAt: until, ResponseProof: humanWriteAttachProof(s, f, until, check)}, nil
}
func (s *Store) PreviewDeclarationControl(ctx context.Context, d [32]byte, a identity.Actor, in contextgraph.DeclarationControlInput) (contextgraph.DeclarationControlPreview, error) {
	in, e := contextgraph.NormalizeDeclarationControl(in, false)
	if e != nil {
		return contextgraph.DeclarationControlPreview{}, e
	}
	ctx, f, e := s.humanWriteBegin(ctx, d, a, "CONTEXT", in.OperationID)
	if e != nil {
		return contextgraph.DeclarationControlPreview{}, e
	}
	defer f.tx.Rollback(context.Background())
	if _, e = humanWriteReceiptTx(ctx, f); e == nil {
		return contextgraph.DeclarationControlPreview{}, identity.ErrControlChanged
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return contextgraph.DeclarationControlPreview{}, identity.ErrControlUnavailable
	}
	source, _, deadline, e := declarationControlSource(ctx, f, in)
	if e != nil {
		return contextgraph.DeclarationControlPreview{}, e
	}
	base, e := s.humanWritePreview(ctx, f, "", in.Action, contextgraph.DeclarationControlDigest(in), source, deadline)
	if e != nil {
		return contextgraph.DeclarationControlPreview{}, e
	}
	check := func(ctx context.Context, next humanWriteFrame) error {
		current, _, _, e := declarationControlSource(ctx, next, in)
		if e != nil {
			return e
		}
		if current != source {
			return identity.ErrControlChanged
		}
		return nil
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, base.ExpiresAt, func() error { return check(ctx, f) })); e != nil {
		return contextgraph.DeclarationControlPreview{}, e
	}
	base.ResponseProof = humanWriteAttachProof(s, f, base.ExpiresAt, check)
	return contextgraph.DeclarationControlPreview{HumanControlPreview: base, ExpectedVersion: in.ExpectedVersion, ContextID: in.ContextID, Type: in.Type, SourceKey: in.SourceKey, Relation: in.Relation}, nil
}
func (s *Store) CommitDeclarationControl(ctx context.Context, d [32]byte, a identity.Actor, in contextgraph.DeclarationControlInput) (identity.HumanControlReceipt, error) {
	in, e := contextgraph.NormalizeDeclarationControl(in, true)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	ctx, f, e := s.humanWriteBegin(ctx, d, a, "CONTEXT", in.OperationID)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	digest := contextgraph.DeclarationControlDigest(in)
	if r, done, e := s.humanWriteReplay(ctx, f, "", digest); done {
		return r, e
	}
	var owner string
	e = f.tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR UPDATE`, a.ID).Scan(&owner)
	if e != nil {
		return identity.HumanControlReceipt{}, identity.ErrControlChanged
	}
	source, authority, sourceDeadline, e := declarationControlSource(ctx, f, in)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	deadline, e := s.humanWriteApprove(ctx, f, "", in.Action, digest, source, in.Approval, sourceDeadline)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	id := in.ContextID
	if in.Action == "DECLARE" {
		v, e := declarationEffectTx(ctx, f.tx, a.ID, contextgraph.DeclarationInput{Type: in.Type, SourceKey: in.SourceKey, Relation: in.Relation})
		if e != nil {
			return identity.HumanControlReceipt{}, e
		}
		id = v.ContextID
	} else {
		e := removeDeclarationEffectTx(ctx, f.tx, a.ID, id, in.Relation)
		if e != nil {
			return identity.HumanControlReceipt{}, identity.ErrControlChanged
		}
	}
	version, _, post, e := declarationControlSnapshot(ctx, f)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	if version <= in.ExpectedVersion && in.ExpectedVersion < identity.HumanControlMaxVersion {
		return identity.HumanControlReceipt{}, identity.ErrControlChanged
	}
	if _, e = auditExec(ctx, f.tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)VALUES($1,$2,'context',$3,'allowed','personal_content')`, a.ID, in.Action, id); e != nil {
		return identity.HumanControlReceipt{}, identity.ErrControlUnavailable
	}
	r, e := humanWriteSaveReceipt(ctx, f, "", in.Action, id, id, digest, version)
	if e != nil {
		return r, e
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, deadline, func() error {
		_, _, current, e := declarationControlSnapshot(ctx, f)
		if e != nil {
			return e
		}
		if current != post {
			return identity.ErrControlChanged
		}
		if in.Action == "DECLARE" && in.Type == contextgraph.City {
			next, e := declarationControlCityAuthority(ctx, f, in)
			if e != nil || next != authority {
				return identity.ErrControlChanged
			}
		}
		return nil
	})); e != nil {
		return identity.HumanControlReceipt{}, e
	}
	return r, nil
}
func (s *Store) ReadDeclarationControlOperation(ctx context.Context, d [32]byte, a identity.Actor, op string) (identity.HumanControlReceipt, error) {
	return s.humanWriteHistory(ctx, d, a, "CONTEXT", op, "", false)
}
func (s *Store) StopDeclarationControlOperation(ctx context.Context, d [32]byte, a identity.Actor, op string) (identity.HumanControlReceipt, error) {
	return s.humanWriteHistory(ctx, d, a, "CONTEXT", op, "", true)
}
