package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

var _ content.HumanMomentStore = (*Store)(nil)

// Ordinary human content has no dependency on Agent/Profile existence. Account
// SHARE is compatible with the existing outbox's Moment -> Account SHARE;
// Session is locked after resource waits so a committed revoke is observable.
func (s *Store) beginHumanMomentTx(ctx context.Context, digest [32]byte, actor identity.Actor) (pgx.Tx, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return nil, content.ErrUnavailable
	}
	principal, err := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	if err != nil || actor.AccountType != "person" || principal.Type != actorref.Person || principal.ID != actor.ID || digest == ([32]byte{}) {
		return nil, identity.ErrUnauthorized
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, content.ErrUnavailable
	}
	fail := func(e error) (pgx.Tx, error) { _ = tx.Rollback(context.Background()); return nil, e }
	if _, err = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); err != nil {
		return fail(content.ErrUnavailable)
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, actor.ID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return fail(identity.ErrUnauthorized)
	}
	if err != nil {
		return fail(content.ErrUnavailable)
	}
	// A current optional enrichment identity shares its metadata lock before
	// waiting on Moment. Candidate/Evidence already use metadata -> source;
	// capturing the old outbox later must not invert that order. Missing Agent
	// or metadata is a normal human-only path, not a permission failure.
	var optionalAgent string
	err = tx.QueryRow(ctx, `SELECT ag.id FROM agents ag JOIN agent_profiles ap
	 ON ap.agent_id=ag.id AND ap.owner_id=$1 AND ap.owner_type='PERSON'
	 WHERE ag.principal_account_id=$1 AND ag.agent_type='personal' AND ag.status='active'
	 FOR SHARE OF ag,ap`, actor.ID).Scan(&optionalAgent)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fail(content.ErrUnavailable)
	}
	// No session row lock before a Moment wait. This is only an early negative
	// check; authorization still requires the locked final current-clock gate.
	if err = checkHumanMomentSession(ctx, tx, digest, actor.ID, s.devPhoneEnabled); err != nil {
		return fail(err)
	}
	return tx, nil
}

const humanMomentCurrentSession = `SELECT EXISTS(SELECT 1 FROM sessions s JOIN accounts a ON a.id=s.account_id
 WHERE s.token_sha256=$1 AND a.id=$2 AND a.account_type='person' AND a.status='active'
 AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND s.idle_expires_at>clock_timestamp()
 AND ($3::boolean OR s.authentication_method<>'dev_phone'))`

func checkHumanMomentSession(ctx context.Context, tx pgx.Tx, digest [32]byte, owner string, dev bool) error {
	var current bool
	if err := tx.QueryRow(ctx, humanMomentCurrentSession, digest[:], owner, dev).Scan(&current); err != nil || ctx.Err() != nil {
		return content.ErrUnavailable
	}
	if !current {
		return identity.ErrUnauthorized
	}
	return nil
}
func (s *Store) lockHumanMomentSession(ctx context.Context, tx pgx.Tx, digest [32]byte, owner string) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM sessions WHERE token_sha256=$1 AND account_id=$2
 AND revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_expires_at>clock_timestamp()
 AND ($3::boolean OR authentication_method<>'dev_phone') FOR SHARE`, digest[:], owner, s.devPhoneEnabled).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrUnauthorized
	}
	if err != nil || ctx.Err() != nil {
		return content.ErrUnavailable
	}
	return nil
}
func (s *Store) finishHumanMomentTx(ctx context.Context, tx pgx.Tx, digest [32]byte, owner string) error {
	if err := checkHumanMomentSession(ctx, tx, digest, owner, s.devPhoneEnabled); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return content.ErrUnavailable
	}
	return nil
}
func (s *Store) normalizeHumanMomentTx(ctx context.Context, tx pgx.Tx, input content.MomentInput) (content.MomentInput, error) {
	input, err := content.NormalizeMomentInput(input)
	if err != nil {
		return content.MomentInput{}, err
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT $1::timestamptz IS NULL OR $1::timestamptz<=clock_timestamp()+interval '24 hours'`, input.OccurredAt).Scan(&valid); err != nil || ctx.Err() != nil {
		return content.MomentInput{}, content.ErrUnavailable
	}
	if !valid {
		return content.MomentInput{}, content.ErrInvalid
	}
	return input, nil
}
func humanMomentFailure(err error) error {
	if errors.Is(err, content.ErrConflict) || errors.Is(err, content.ErrNotFound) || errors.Is(err, content.ErrInvalid) || errors.Is(err, identity.ErrUnauthorized) {
		return err
	}
	return content.ErrUnavailable
}
func validHumanMomentID(id string) bool {
	p, e := actorref.ParsePrincipal("person", id)
	return e == nil && p.ID == id
}

func (s *Store) CreateHumanMomentDraft(ctx context.Context, digest [32]byte, actor identity.Actor, input content.MomentInput) (content.Moment, error) {
	tx, err := s.beginHumanMomentTx(ctx, digest, actor)
	if err != nil {
		return content.Moment{}, err
	}
	defer tx.Rollback(context.Background())
	input, err = s.normalizeHumanMomentTx(ctx, tx, input)
	if err != nil {
		return content.Moment{}, err
	}
	m, err := createMomentDraftInTx(ctx, tx, actor.ID, input)
	if err != nil {
		return content.Moment{}, humanMomentFailure(err)
	}
	if err = s.lockHumanMomentSession(ctx, tx, digest, actor.ID); err != nil {
		return content.Moment{}, err
	}
	if err = s.finishHumanMomentTx(ctx, tx, digest, actor.ID); err != nil {
		return content.Moment{}, err
	}
	return m, nil
}
func (s *Store) UpdateHumanMomentDraft(ctx context.Context, digest [32]byte, actor identity.Actor, id string, revision int64, input content.MomentInput) (content.Moment, error) {
	if !validHumanMomentID(id) || revision < 1 {
		return content.Moment{}, content.ErrInvalid
	}
	tx, err := s.beginHumanMomentTx(ctx, digest, actor)
	if err != nil {
		return content.Moment{}, err
	}
	defer tx.Rollback(context.Background())
	input, err = s.normalizeHumanMomentTx(ctx, tx, input)
	if err != nil {
		return content.Moment{}, err
	}
	m, err := updateMomentDraftInTx(ctx, tx, actor.ID, id, revision, input)
	if err != nil {
		return content.Moment{}, humanMomentFailure(err)
	}
	if err = s.lockHumanMomentSession(ctx, tx, digest, actor.ID); err != nil {
		return content.Moment{}, err
	}
	if err = s.finishHumanMomentTx(ctx, tx, digest, actor.ID); err != nil {
		return content.Moment{}, err
	}
	return m, nil
}
func (s *Store) WithdrawHumanMoment(ctx context.Context, digest [32]byte, actor identity.Actor, id string, revision int64) error {
	if !validHumanMomentID(id) || revision < 1 {
		return content.ErrInvalid
	}
	tx, err := s.beginHumanMomentTx(ctx, digest, actor)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = withdrawMomentInTx(ctx, tx, actor.ID, id, revision); err != nil {
		return humanMomentFailure(err)
	}
	if err = s.lockHumanMomentSession(ctx, tx, digest, actor.ID); err != nil {
		return err
	}
	return s.finishHumanMomentTx(ctx, tx, digest, actor.ID)
}
func (s *Store) GetHumanMoment(ctx context.Context, digest [32]byte, actor identity.Actor, id string) (content.Moment, error) {
	if !validHumanMomentID(id) {
		return content.Moment{}, content.ErrInvalid
	}
	tx, err := s.beginHumanMomentTx(ctx, digest, actor)
	if err != nil {
		return content.Moment{}, err
	}
	defer tx.Rollback(context.Background())
	var locked string
	err = tx.QueryRow(ctx, `SELECT id FROM moments WHERE id=$1 AND author_account_id=$2 FOR SHARE`, id, actor.ID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return content.Moment{}, content.ErrNotFound
	}
	if err != nil {
		return content.Moment{}, content.ErrUnavailable
	}
	if err = s.lockHumanMomentSession(ctx, tx, digest, actor.ID); err != nil {
		return content.Moment{}, err
	}
	m, err := scanMoment(tx.QueryRow(ctx, `SELECT `+momentColumns+` FROM moments m WHERE m.id=$1 AND m.author_account_id=$2
 AND EXISTS(SELECT 1 FROM sessions ss WHERE ss.token_sha256=$3 AND ss.account_id=$2 AND ss.revoked_at IS NULL
 AND ss.expires_at>clock_timestamp() AND ss.idle_expires_at>clock_timestamp() AND ($4::boolean OR ss.authentication_method<>'dev_phone'))`, id, actor.ID, digest[:], s.devPhoneEnabled))
	if err != nil {
		if e := checkHumanMomentSession(ctx, tx, digest, actor.ID, s.devPhoneEnabled); e != nil {
			return content.Moment{}, e
		}
		return content.Moment{}, humanMomentFailure(err)
	}
	if err = s.finishHumanMomentTx(ctx, tx, digest, actor.ID); err != nil {
		return content.Moment{}, err
	}
	return m, nil
}
func (s *Store) ListHumanMoments(ctx context.Context, digest [32]byte, actor identity.Actor) ([]content.Moment, error) {
	tx, err := s.beginHumanMomentTx(ctx, digest, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	rows, err := tx.Query(ctx, `SELECT id::text FROM moments WHERE author_account_id=$1 AND status<>'withdrawn' ORDER BY updated_at DESC,id DESC LIMIT 100 FOR SHARE`, actor.ID)
	if err != nil {
		return nil, content.ErrUnavailable
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, content.ErrUnavailable
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, content.ErrUnavailable
	}
	if err = s.lockHumanMomentSession(ctx, tx, digest, actor.ID); err != nil {
		return nil, err
	}
	rows, err = tx.Query(ctx, `SELECT `+momentColumns+` FROM moments m WHERE m.id=ANY($1::uuid[]) AND m.author_account_id=$2 AND m.status<>'withdrawn'
 AND EXISTS(SELECT 1 FROM sessions ss WHERE ss.token_sha256=$3 AND ss.account_id=$2 AND ss.revoked_at IS NULL
 AND ss.expires_at>clock_timestamp() AND ss.idle_expires_at>clock_timestamp() AND ($4::boolean OR ss.authentication_method<>'dev_phone')) ORDER BY m.updated_at DESC,m.id DESC`, ids, actor.ID, digest[:], s.devPhoneEnabled)
	if err != nil {
		return nil, content.ErrUnavailable
	}
	result := make([]content.Moment, 0, len(ids))
	for rows.Next() {
		m, e := scanMoment(rows)
		if e != nil {
			rows.Close()
			return nil, content.ErrUnavailable
		}
		result = append(result, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, content.ErrUnavailable
	}
	if err = s.finishHumanMomentTx(ctx, tx, digest, actor.ID); err != nil {
		return nil, err
	}
	return result, nil
}
