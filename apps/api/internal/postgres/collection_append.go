package postgres

import (
	"bytes"
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

var _ content.HumanCollectionAppendStore = (*Store)(nil)

func collectionAppendSessionBinding(ctx context.Context, tx pgx.Tx, d [32]byte, owner string) (string, int64, error) {
	var id string
	var generation int64
	e := tx.QueryRow(ctx, `SELECT id,authorization_generation FROM sessions WHERE account_id=$1 AND token_sha256=$2`, owner, d[:]).Scan(&id, &generation)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", 0, identity.ErrUnauthorized
	}
	if e != nil {
		return "", 0, content.ErrUnavailable
	}
	return id, generation, nil
}
func (s *Store) collectionAppendCommit(ctx context.Context, tx pgx.Tx, d [32]byte, owner, id string, generation int64) error {
	if e := s.lockHumanMomentSession(ctx, tx, d, owner); e != nil {
		return e
	}
	current, gen, e := collectionAppendSessionBinding(ctx, tx, d, owner)
	if e != nil {
		return e
	}
	if current != id || gen != generation {
		return content.ErrConflict
	}
	if original, valid, present := content.OriginalCollectionSession(ctx, d, identity.Actor{ID: owner, AccountType: "person"}); present && (!valid || original.ID != current || original.Generation != gen) {
		return content.ErrConflict
	}
	return s.finishHumanMomentTx(ctx, tx, d, owner)
}
func collectionAppendReceiptTx(ctx context.Context, tx pgx.Tx, owner, op string) (content.CollectionAppendReceipt, []byte, int64, error) {
	r := content.CollectionAppendReceipt{OperationID: op}
	var digest []byte
	var expected int64
	e := tx.QueryRow(ctx, `SELECT kind,target_id,applied_revision,input_sha256,expected_revision,status FROM personal_collection_append_operations WHERE owner_account_id=$1 AND operation_id=$2`, owner, op).Scan(&r.Kind, &r.CollectionID, &r.AppliedRevision, &digest, &expected, &r.Status)
	return r, digest, expected, e
}
func collectionAppendOwnedTx(ctx context.Context, tx pgx.Tx, owner, kind, id string) error {
	table, _, _, e := collectionTables(kind)
	if e != nil || !content.ValidCollectionID(id) {
		return content.ErrInvalid
	}
	var owned string
	e = tx.QueryRow(ctx, `SELECT id FROM `+table+` WHERE id=$1 AND owner_account_id=$2 FOR SHARE`, id, owner).Scan(&owned)
	if errors.Is(e, pgx.ErrNoRows) {
		return content.ErrNotFound
	}
	if e != nil {
		return content.ErrUnavailable
	}
	return nil
}
func (s *Store) AppendHumanCollectionPlaces(ctx context.Context, d [32]byte, a identity.Actor, kind, id string, in content.CollectionAppendInput) (content.CollectionAppendReceipt, error) {
	empty := content.CollectionAppendReceipt{}
	if !content.ValidCollectionKind(kind) || !content.ValidCollectionID(id) {
		return empty, content.ErrInvalid
	}
	in, e := content.NormalizeCollectionAppend(in)
	if e != nil {
		return empty, e
	}
	ctx, tx, e := s.beginBoundCollectionTx(ctx, d, a)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(context.Background())
	session, generation, e := collectionAppendSessionBinding(ctx, tx, d, a.ID)
	if e != nil {
		return empty, e
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,136))`, a.ID+":"+in.OperationID); e != nil {
		return empty, content.ErrUnavailable
	}
	sum := content.CollectionAppendDigest(kind, id, in)
	previous, digest, expected, e := collectionAppendReceiptTx(ctx, tx, a.ID, in.OperationID)
	if e == nil {
		if previous.Kind != kind || previous.CollectionID != id || expected != in.ExpectedRevision {
			return empty, content.ErrConflict
		}
		if previous.Status != "STOPPED" && !bytes.Equal(digest, sum[:]) {
			return empty, content.ErrConflict
		}
		if e = collectionAppendOwnedTx(ctx, tx, a.ID, kind, id); e != nil {
			return empty, e
		}
		if e = s.collectionAppendCommit(ctx, tx, d, a.ID, session, generation); e != nil {
			return empty, e
		}
		return previous, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return empty, content.ErrUnavailable
	}
	table, _, _, _ := collectionTables(kind)
	if kind == "journey" {
		if rich, e := journeyRichTx(ctx, tx, a.ID, id); e != nil {
			return empty, e
		} else if rich {
			return empty, content.ErrConflict
		}
	}
	var city string
	e = tx.QueryRow(ctx, `SELECT city_id FROM `+table+` WHERE id=$1 AND owner_account_id=$2 AND revision=$3 AND status='draft' AND visibility='private' FOR UPDATE`, id, a.ID, in.ExpectedRevision).Scan(&city)
	if errors.Is(e, pgx.ErrNoRows) {
		return empty, content.ErrConflict
	}
	if e != nil {
		return empty, content.ErrUnavailable
	}
	if city != in.CityID {
		return empty, content.ErrConflict
	}
	current, e := collectionReadTx(ctx, tx, a.ID, kind, id, false)
	if e != nil {
		return empty, e
	}
	if len(current.Entries)+len(in.Entries) > 50 {
		return empty, content.ErrConflict
	}
	seen := map[string]bool{}
	combined := content.CollectionInput{Kind: kind, CityID: city, Title: current.Title, Summary: current.Summary, Entries: []content.CollectionEntry{}}
	for _, entry := range current.Entries {
		seen[entry.PlaceID] = true
		combined.Entries = append(combined.Entries, content.CollectionEntry{PlaceID: entry.PlaceID, Note: entry.Note, LocationVisibility: entry.LocationVisibility})
	}
	added := content.CollectionInput{Kind: kind, CityID: city, Title: current.Title, Summary: current.Summary, Entries: []content.CollectionEntry{}}
	for _, entry := range in.Entries {
		if seen[entry.PlaceID] {
			return empty, content.ErrConflict
		}
		seen[entry.PlaceID] = true
		v := content.CollectionEntry{PlaceID: entry.PlaceID, Note: "", LocationVisibility: "hidden"}
		added.Entries = append(added.Entries, v)
		combined.Entries = append(combined.Entries, v)
	}
	if e = collectionSourcesTx(ctx, tx, added); e != nil {
		return empty, e
	}
	if e = checkHumanMomentSession(ctx, tx, d, a.ID, s.devPhoneEnabled); e != nil {
		return empty, e
	}
	if e = collectionEntriesTx(ctx, tx, id, combined); e != nil {
		return empty, e
	}
	if _, e = tx.Exec(ctx, `UPDATE `+table+` SET revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, id); e != nil {
		return empty, content.ErrUnavailable
	}
	r := content.CollectionAppendReceipt{OperationID: in.OperationID, Kind: kind, CollectionID: id, AppliedRevision: in.ExpectedRevision + 1, Status: "SUCCEEDED"}
	if _, e = tx.Exec(ctx, `INSERT INTO personal_collection_append_operations(owner_account_id,operation_id,kind,target_id,city_id,expected_revision,applied_revision,input_sha256,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'SUCCEEDED')`, a.ID, in.OperationID, kind, id, in.CityID, in.ExpectedRevision, r.AppliedRevision, sum[:]); e != nil {
		return empty, content.ErrUnavailable
	}
	if e = collectionAuditTx(ctx, tx, a.ID, kind, id, "append_places"); e != nil {
		return empty, e
	}
	if e = collectionSourcesTx(ctx, tx, added); e != nil {
		return empty, e
	}
	if e = s.collectionAppendCommit(ctx, tx, d, a.ID, session, generation); e != nil {
		return empty, e
	}
	return r, nil
}

func (s *Store) StopHumanCollectionAppendOperation(ctx context.Context, d [32]byte, a identity.Actor, kind, id, op string, in content.CollectionAppendStopInput) (content.CollectionAppendReceipt, error) {
	empty := content.CollectionAppendReceipt{}
	if !content.ValidCollectionKind(kind) || !content.ValidCollectionID(id) || !content.ValidCollectionID(op) {
		return empty, content.ErrInvalid
	}
	in, e := content.NormalizeCollectionAppendStop(in)
	if e != nil {
		return empty, e
	}
	ctx, tx, e := s.beginBoundCollectionTx(ctx, d, a)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(context.Background())
	session, generation, e := collectionAppendSessionBinding(ctx, tx, d, a.ID)
	if e != nil {
		return empty, e
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,136))`, a.ID+":"+op); e != nil {
		return empty, content.ErrUnavailable
	}
	if e = collectionAppendOwnedTx(ctx, tx, a.ID, kind, id); e != nil {
		return empty, e
	}
	table, _, _, _ := collectionTables(kind)
	var city string
	if e = tx.QueryRow(ctx, `SELECT city_id FROM `+table+` WHERE id=$1 AND owner_account_id=$2`, id, a.ID).Scan(&city); e != nil {
		return empty, content.ErrUnavailable
	}
	if city != in.CityID {
		return empty, content.ErrConflict
	}
	r, _, expected, e := collectionAppendReceiptTx(ctx, tx, a.ID, op)
	if e == nil {
		if !r.Valid(kind, id, op) || expected != in.ExpectedRevision {
			return empty, content.ErrConflict
		}
		if e = s.collectionAppendCommit(ctx, tx, d, a.ID, session, generation); e != nil {
			return empty, e
		}
		return r, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return empty, content.ErrUnavailable
	}
	sum := content.CollectionAppendDigest(kind, id, content.CollectionAppendInput{CityID: in.CityID, ExpectedRevision: in.ExpectedRevision, OperationID: op, Entries: []content.CollectionAppendEntry{}})
	if _, e = tx.Exec(ctx, `INSERT INTO personal_collection_append_operations(owner_account_id,operation_id,kind,target_id,city_id,expected_revision,applied_revision,input_sha256,status) VALUES($1,$2,$3,$4,$5,$6,0,$7,'STOPPED')`, a.ID, op, kind, id, in.CityID, in.ExpectedRevision, sum[:]); e != nil {
		return empty, content.ErrUnavailable
	}
	r = content.CollectionAppendReceipt{OperationID: op, Kind: kind, CollectionID: id, AppliedRevision: 0, Status: "STOPPED"}
	if e = s.collectionAppendCommit(ctx, tx, d, a.ID, session, generation); e != nil {
		return empty, e
	}
	return r, nil
}
func (s *Store) GetHumanCollectionAppendOperation(ctx context.Context, d [32]byte, a identity.Actor, kind, id, op string) (content.CollectionAppendReceipt, error) {
	empty := content.CollectionAppendReceipt{}
	if !content.ValidCollectionKind(kind) || !content.ValidCollectionID(id) || !content.ValidCollectionID(op) {
		return empty, content.ErrInvalid
	}
	ctx, tx, e := s.beginBoundCollectionTx(ctx, d, a)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(context.Background())
	session, generation, e := collectionAppendSessionBinding(ctx, tx, d, a.ID)
	if e != nil {
		return empty, e
	}
	if e = collectionAppendOwnedTx(ctx, tx, a.ID, kind, id); e != nil {
		return empty, e
	}
	r, _, _, e := collectionAppendReceiptTx(ctx, tx, a.ID, op)
	if errors.Is(e, pgx.ErrNoRows) {
		return empty, content.ErrNotFound
	}
	if e != nil {
		return empty, content.ErrUnavailable
	}
	if !r.Valid(kind, id, op) {
		return empty, content.ErrNotFound
	}
	if e = s.collectionAppendCommit(ctx, tx, d, a.ID, session, generation); e != nil {
		return empty, e
	}
	return r, nil
}
