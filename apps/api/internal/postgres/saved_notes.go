package postgres

import (
	"bytes"
	"context"
	"errors"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/saved"
	"github.com/jackc/pgx/v5"
)

var _ saved.HumanNoteStore = (*Store)(nil)

func savedNoteFailure(e error) error {
	if errors.Is(e, identity.ErrUnauthorized) || errors.Is(e, saved.ErrNotFound) || errors.Is(e, saved.ErrInvalidNote) || errors.Is(e, saved.ErrNoteChanged) {
		return e
	}
	return saved.ErrNoteUnavailable
}

func lockOwnSavedNote(ctx context.Context, tx pgx.Tx, owner, id string, write bool) error {
	suffix := " FOR SHARE"
	if write {
		suffix = " FOR UPDATE"
	}
	var found string
	e := tx.QueryRow(ctx, `SELECT id FROM saved_items WHERE id=$1 AND owner_account_id=$2`+suffix, id, owner).Scan(&found)
	if errors.Is(e, pgx.ErrNoRows) {
		return saved.ErrNotFound
	}
	return savedNoteFailureIfAny(e)
}

func savedNoteFailureIfAny(e error) error {
	if e == nil {
		return nil
	}
	return savedNoteFailure(e)
}

func (s *Store) finishSavedNote(ctx context.Context, tx pgx.Tx, d [32]byte, owner string) error {
	// Resource and trigger waits precede the locked current Session check. A
	// revoke committed while waiting must roll back both note and receipt.
	if e := s.lockHumanMomentSession(ctx, tx, d, owner); e != nil {
		return savedNoteFailure(e)
	}
	return savedNoteFailureIfAny(s.finishHumanMomentTx(ctx, tx, d, owner))
}

func (s *Store) GetHumanSavedNote(ctx context.Context, d [32]byte, a identity.Actor, id string) (saved.Note, error) {
	if !saved.ValidNoteID(id) {
		return saved.Note{}, saved.ErrInvalidNote
	}
	id = strings.ToLower(id)
	tx, e := s.beginHumanMomentTx(ctx, d, a)
	if e != nil {
		return saved.Note{}, savedNoteFailure(e)
	}
	defer tx.Rollback(context.Background())
	if e = lockOwnSavedNote(ctx, tx, a.ID, id, false); e != nil {
		return saved.Note{}, e
	}
	v := saved.Note{SavedID: id}
	e = tx.QueryRow(ctx, `SELECT note,revision FROM private_saved_notes WHERE saved_item_id=$1`, id).Scan(&v.Note, &v.Revision)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return saved.Note{}, saved.ErrNoteUnavailable
	}
	if e = s.finishSavedNote(ctx, tx, d, a.ID); e != nil {
		return saved.Note{}, e
	}
	return v, nil
}

func (s *Store) GetHumanSavedNoteOperation(ctx context.Context, d [32]byte, a identity.Actor, id, op string) (saved.NoteOperation, error) {
	if !saved.ValidNoteID(id) || !saved.ValidNoteID(op) {
		return saved.NoteOperation{}, saved.ErrInvalidNote
	}
	id, op = strings.ToLower(id), strings.ToLower(op)
	tx, e := s.beginHumanMomentTx(ctx, d, a)
	if e != nil {
		return saved.NoteOperation{}, savedNoteFailure(e)
	}
	defer tx.Rollback(context.Background())
	if e = lockOwnSavedNote(ctx, tx, a.ID, id, false); e != nil {
		return saved.NoteOperation{}, e
	}
	v := saved.NoteOperation{SavedID: id, OperationID: op, Status: "SUCCEEDED"}
	e = tx.QueryRow(ctx, `SELECT result_revision FROM private_saved_note_operations WHERE saved_item_id=$1 AND operation_id=$2`, id, op).Scan(&v.Revision)
	if errors.Is(e, pgx.ErrNoRows) {
		return saved.NoteOperation{}, saved.ErrNotFound
	}
	if e != nil {
		return saved.NoteOperation{}, saved.ErrNoteUnavailable
	}
	if e = s.finishSavedNote(ctx, tx, d, a.ID); e != nil {
		return saved.NoteOperation{}, e
	}
	return v, nil
}

func (s *Store) UpdateHumanSavedNote(ctx context.Context, d [32]byte, a identity.Actor, id string, in saved.NoteInput) (saved.NoteOperation, error) {
	if !saved.ValidNoteID(id) {
		return saved.NoteOperation{}, saved.ErrInvalidNote
	}
	var e error
	in, e = saved.NormalizeNoteInput(in)
	if e != nil {
		return saved.NoteOperation{}, e
	}
	id = strings.ToLower(id)
	tx, e := s.beginHumanMomentTx(ctx, d, a)
	if e != nil {
		return saved.NoteOperation{}, savedNoteFailure(e)
	}
	defer tx.Rollback(context.Background())
	// Serialize by the immutable bookmark. Locking does not UPDATE it, so its
	// original source token, xmin and Memory invalidation history stay intact.
	if e = lockOwnSavedNote(ctx, tx, a.ID, id, true); e != nil {
		return saved.NoteOperation{}, e
	}
	digest := saved.NoteInputDigest(id, in)
	v := saved.NoteOperation{SavedID: id, OperationID: in.OperationID, Status: "SUCCEEDED"}
	var previous []byte
	e = tx.QueryRow(ctx, `SELECT input_sha256,result_revision FROM private_saved_note_operations WHERE saved_item_id=$1 AND operation_id=$2`, id, in.OperationID).Scan(&previous, &v.Revision)
	if e == nil {
		if !bytes.Equal(previous, digest[:]) {
			return saved.NoteOperation{}, saved.ErrNoteChanged
		}
		if e = s.finishSavedNote(ctx, tx, d, a.ID); e != nil {
			return saved.NoteOperation{}, e
		}
		return v, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return saved.NoteOperation{}, saved.ErrNoteUnavailable
	}
	var current int64
	e = tx.QueryRow(ctx, `SELECT revision FROM private_saved_notes WHERE saved_item_id=$1 FOR UPDATE`, id).Scan(&current)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return saved.NoteOperation{}, saved.ErrNoteUnavailable
	}
	if current != in.ExpectedRevision {
		return saved.NoteOperation{}, saved.ErrNoteChanged
	}
	v.Revision = current + 1
	if current == 0 {
		_, e = tx.Exec(ctx, `INSERT INTO private_saved_notes(saved_item_id,note,revision,updated_at) VALUES($1,$2,$3,clock_timestamp())`, id, in.Note, v.Revision)
	} else {
		var actual int64
		e = tx.QueryRow(ctx, `UPDATE private_saved_notes SET note=$2,revision=$3,updated_at=clock_timestamp() WHERE saved_item_id=$1 AND revision=$4 RETURNING revision`, id, in.Note, v.Revision, current).Scan(&actual)
		if errors.Is(e, pgx.ErrNoRows) || (e == nil && actual != v.Revision) {
			return saved.NoteOperation{}, saved.ErrNoteChanged
		}
	}
	if e != nil {
		return saved.NoteOperation{}, saved.ErrNoteUnavailable
	}
	if _, e = tx.Exec(ctx, `INSERT INTO private_saved_note_operations(saved_item_id,operation_id,input_sha256,result_revision) VALUES($1,$2,$3,$4)`, id, in.OperationID, digest[:], v.Revision); e != nil {
		return saved.NoteOperation{}, saved.ErrNoteUnavailable
	}
	if e = insertDomainAudit(ctx, tx, a.ID, "update", "saved_item", id, "private_saved_note", nil); e != nil {
		return saved.NoteOperation{}, saved.ErrNoteUnavailable
	}
	if e = s.finishSavedNote(ctx, tx, d, a.ID); e != nil {
		return saved.NoteOperation{}, e
	}
	return v, nil
}
