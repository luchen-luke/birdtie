package postgres

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const agentMemoryColumns = `id,agent_id,owner_type,owner_id,version,memory_type,memory_key,summary,
structured_value,confidence,source_type,visibility,status,valid_from,valid_until,last_reinforced_at,created_at,updated_at`

func memoryError(err error) error {
	switch {
	case errors.Is(err, agentprofile.ErrInvalid):
		return agentmemory.ErrInvalid
	case errors.Is(err, agentprofile.ErrForbidden):
		return agentmemory.ErrForbidden
	case errors.Is(err, agentprofile.ErrNotFound):
		return agentmemory.ErrNotFound
	}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && (pgerr.Code == "23505" || pgerr.Code == "40001" || pgerr.Code == "40P01") {
		return agentmemory.ErrConflict
	}
	// SQL details can contain private declarations. Never expose them.
	return agentmemory.ErrUnavailable
}

func scanAgentMemory(row pgx.Row) (agentmemory.Record, error) {
	var record agentmemory.Record
	record.SchemaVersion = agentmemory.SchemaV1
	err := row.Scan(&record.ID, &record.AgentID, &record.OwnerType, &record.OwnerID, &record.Version,
		&record.MemoryType, &record.MemoryKey, &record.Summary, &record.StructuredValue, &record.Confidence,
		&record.SourceType, &record.Visibility, &record.Status, &record.ValidFrom, &record.ValidUntil,
		&record.LastReinforcedAt, &record.CreatedAt, &record.UpdatedAt)
	if err != nil {
		return agentmemory.Record{}, err
	}
	value, err := agentmemory.NormalizeStructuredValue(record.StructuredValue)
	if err != nil {
		return agentmemory.Record{}, agentmemory.ErrUnavailable
	}
	record.StructuredValue = value
	if agentmemory.ValidateRecord(record) != nil {
		return agentmemory.Record{}, agentmemory.ErrUnavailable
	}
	return record, nil
}

func (s *Store) ReadOwnMemories(ctx context.Context, access agentprofile.PrivateAccess) ([]agentmemory.Record, error) {
	if err := agentprofile.ValidatePrivateAccess(access); err != nil {
		return nil, memoryError(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, agentmemory.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	binding, err := lockOwnAgentPrivateBinding(ctx, tx, access, s.devPhoneEnabled)
	if err != nil {
		return nil, memoryError(err)
	}
	_, err = lockAgentPrivateMetadata(ctx, tx, binding, false)
	if err != nil {
		return nil, memoryError(err)
	}
	rows, err := tx.Query(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories
		WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON' AND status<>'DELETED'
		ORDER BY CASE WHEN source_type='EXPLICIT' THEN 0 ELSE 1 END,updated_at DESC,id`, binding.agentID, binding.accountID)
	if err != nil {
		return nil, agentmemory.ErrUnavailable
	}
	records := []agentmemory.Record{}
	for rows.Next() {
		record, scanErr := scanAgentMemory(rows)
		if scanErr != nil {
			rows.Close()
			return nil, agentmemory.ErrUnavailable
		}
		records = append(records, record)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, agentmemory.ErrUnavailable
	}
	// The relation query may wait across a deadline. Project expiry only after
	// all reads, using actual PG time; native contents/version/xmin stay intact.
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, agentmemory.ErrUnavailable
	}
	for i := range records {
		if !records[i].ValidUntil.After(now) {
			records[i].Status = agentmemory.StatusExpired
		}
	}
	if err = recheckAgentPrivateSession(ctx, tx, binding); err != nil {
		return nil, memoryError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, agentmemory.ErrUnavailable
	}
	return agentmemory.SortRecords(records), nil
}

func memoryContentMatches(record agentmemory.Record, input agentmemory.PutInput, valueMatches bool) bool {
	return record.SourceType == agentmemory.SourceExplicit && record.Status == agentmemory.StatusActive &&
		record.MemoryType == input.MemoryType && record.MemoryKey == input.MemoryKey && record.Summary == input.Summary &&
		valueMatches && record.Visibility == input.Visibility && record.ValidUntil.Equal(input.ValidUntil)
}

// PutOwnMemory persists only the current human's explicit declaration. The
// stable object ID is an idempotency/address key, never an owner selector.
func (s *Store) PutOwnMemory(ctx context.Context, access agentprofile.PrivateAccess, id string, input agentmemory.PutInput) (agentmemory.Record, error) {
	if err := agentprofile.ValidatePrivateAccess(access); err != nil {
		return agentmemory.Record{}, memoryError(err)
	}
	id, err := agentmemory.NormalizeMemoryID(id)
	if err != nil {
		return agentmemory.Record{}, err
	}
	if input.ExpectedVersion == math.MaxInt64 {
		return agentmemory.Record{}, agentmemory.ErrConflict
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return agentmemory.Record{}, agentmemory.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	binding, err := lockOwnAgentPrivateBinding(ctx, tx, access, s.devPhoneEnabled)
	if err != nil {
		return agentmemory.Record{}, memoryError(err)
	}
	// Migration 053 provisions metadata with native Agent creation. Missing
	// metadata denies access; Memory cannot restore deleted privacy controls.
	if _, err = lockAgentPrivateMetadata(ctx, tx, binding, true); err != nil {
		return agentmemory.Record{}, memoryError(err)
	}
	record, retry, err := putOwnMemoryInTx(ctx, tx, binding, access.WorkspacePrincipal, id, input)
	if err != nil {
		return agentmemory.Record{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		if retry {
			return agentmemory.Record{}, agentmemory.ErrUnavailable
		}
		return agentmemory.Record{}, memoryError(err)
	}
	return record, nil
}

// putOwnMemoryInTx is the original native declaration writer without commit.
// Callers must hold the actual owner binding/metadata locks. It never authorizes
// inference, changes source nature or consumes an approval on its own.
func putOwnMemoryInTx(ctx context.Context, tx pgx.Tx, binding agentPrivateBinding, owner actorref.PrincipalRef, id string, input agentmemory.PutInput) (agentmemory.Record, bool, error) {
	var err error
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return agentmemory.Record{}, false, agentmemory.ErrUnavailable
	}
	// PostgreSQL's native timestamp precision is microseconds. Canonicalize
	// before validating against the actual server time and comparing retries.
	input.ValidUntil = input.ValidUntil.UTC().Truncate(time.Microsecond)
	input, err = agentmemory.NormalizePutInput(input, now)
	if err != nil {
		return agentmemory.Record{}, false, err
	}
	// jsonb expands numeric spelling. Validate the actual persistence shape
	// before attempting a write; a bounded wire value can still exceed the
	// storage numeric/compact encoding bound. Reject it as invalid input rather
	// than misreporting database availability or returning private SQL DETAIL.
	var storageShapeValid bool
	if err = tx.QueryRow(ctx, `SELECT birdtie_agent_memory_value_valid($1::jsonb)`,
		input.StructuredValue).Scan(&storageShapeValid); err != nil {
		var pgerr *pgconn.PgError
		if errors.As(err, &pgerr) && (pgerr.Code == "22003" || pgerr.Code == "22P02") {
			return agentmemory.Record{}, false, agentmemory.ErrInvalid
		}
		return agentmemory.Record{}, false, agentmemory.ErrUnavailable
	}
	if !storageShapeValid {
		return agentmemory.Record{}, false, agentmemory.ErrInvalid
	}
	current, err := scanAgentMemory(tx.QueryRow(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories
		WHERE id=$1 AND agent_id=$2 AND owner_id=$3 AND owner_type='PERSON' FOR UPDATE`, id, binding.agentID, binding.accountID))
	missing := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !missing {
		return agentmemory.Record{}, false, agentmemory.ErrUnavailable
	}
	if missing && input.ExpectedVersion != 0 {
		return agentmemory.Record{}, false, agentmemory.ErrNotFound
	}
	if !missing {
		if current.SourceType != agentmemory.SourceExplicit {
			return agentmemory.Record{}, false, agentmemory.ErrUnavailable
		}
		if current.Status == agentmemory.StatusDeleted {
			return agentmemory.Record{}, false, agentmemory.ErrConflict
		}
	}
	// A native row lock can wait beyond the previously validated deadline.
	// Refresh the actual database clock only after obtaining that row; the
	// retry branch and the new declaration must both use this current time.
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return agentmemory.Record{}, false, agentmemory.ErrUnavailable
	}
	input, err = agentmemory.NormalizePutInput(input, now)
	if err != nil {
		return agentmemory.Record{}, false, err
	}
	if !missing {
		var valueMatches bool
		// jsonb owns the stored numeric representation. Semantic comparison
		// preserves exact decimal values without a lossy float64 conversion.
		if err = tx.QueryRow(ctx, `SELECT structured_value=$4::jsonb FROM agent_memories
			WHERE id=$1 AND agent_id=$2 AND owner_id=$3`, id, binding.agentID, binding.accountID,
			input.StructuredValue).Scan(&valueMatches); err != nil {
			return agentmemory.Record{}, false, agentmemory.ErrUnavailable
		}
		if current.ValidUntil.After(now) && memoryContentMatches(current, input, valueMatches) &&
			(current.Version == input.ExpectedVersion || (input.ExpectedVersion < math.MaxInt64 && current.Version == input.ExpectedVersion+1)) {
			if err = recheckMemoryPutBoundary(ctx, tx, binding, current.ValidUntil); err != nil {
				return agentmemory.Record{}, false, err
			}
			return current, true, nil
		}
		if input.ExpectedVersion != current.Version {
			return agentmemory.Record{}, false, agentmemory.ErrConflict
		}
	}
	createdAt := now
	if !missing {
		createdAt = current.CreatedAt
	}
	record, err := agentmemory.NewExplicit(id, binding.agentID, owner, input.ExpectedVersion+1, input, now, createdAt)
	if err != nil {
		return agentmemory.Record{}, false, err
	}
	if missing {
		record, err = scanAgentMemory(tx.QueryRow(ctx, `INSERT INTO agent_memories
			(id,agent_id,owner_type,owner_id,version,memory_type,memory_key,summary,structured_value,
			confidence,source_type,visibility,status,valid_from,valid_until,created_at,updated_at)
			VALUES($1,$2,'PERSON',$3,1,$4,$5,$6,$7::jsonb,1,'EXPLICIT',$8,'ACTIVE',$9,$10,$9,$9)
			RETURNING `+agentMemoryColumns, id, binding.agentID, binding.accountID, record.MemoryType, record.MemoryKey,
			record.Summary, record.StructuredValue, record.Visibility, now, record.ValidUntil))
	} else {
		record, err = scanAgentMemory(tx.QueryRow(ctx, `UPDATE agent_memories SET version=version+1,memory_type=$4,
			memory_key=$5,summary=$6,structured_value=$7::jsonb,visibility=$8,status='ACTIVE',valid_from=$9,
			valid_until=$10,updated_at=$9 WHERE id=$1 AND agent_id=$2 AND owner_id=$3 AND version=$11
			AND source_type='EXPLICIT' AND status<>'DELETED' RETURNING `+agentMemoryColumns,
			id, binding.agentID, binding.accountID, record.MemoryType, record.MemoryKey, record.Summary, record.StructuredValue,
			record.Visibility, now, record.ValidUntil, input.ExpectedVersion))
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return agentmemory.Record{}, false, agentmemory.ErrConflict
	}
	if err != nil {
		return agentmemory.Record{}, false, memoryError(err)
	}
	metric := "memory_corrected"
	if missing {
		metric = "memory_created"
	}
	if err = insertEnrichmentDomainAudit(ctx, tx, binding.accountID, binding.agentID, "put", "agent_memory", record.ID, "human_explicit_memory_edit", metric); err != nil {
		return agentmemory.Record{}, false, agentmemory.ErrUnavailable
	}
	if err = recheckMemoryPutBoundary(ctx, tx, binding, record.ValidUntil); err != nil {
		return agentmemory.Record{}, false, err
	}
	return record, false, nil
}

func recheckMemoryPutBoundary(ctx context.Context, tx pgx.Tx, binding agentPrivateBinding, deadline time.Time) error {
	var sessionCurrent, deadlineCurrent bool
	err := tx.QueryRow(ctx, `SELECT revoked_at IS NULL AND expires_at>clock_timestamp()
		AND idle_expires_at>clock_timestamp(),$2::timestamptz>clock_timestamp()
		FROM sessions WHERE id=$1`, binding.sessionID, deadline).Scan(&sessionCurrent, &deadlineCurrent)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !sessionCurrent) {
		return agentmemory.ErrForbidden
	}
	if err != nil {
		return agentmemory.ErrUnavailable
	}
	if !deadlineCurrent {
		return agentmemory.ErrInvalid
	}
	return nil
}

func (s *Store) DeleteOwnMemory(ctx context.Context, access agentprofile.PrivateAccess, id string, expectedVersion int64) (agentmemory.Record, error) {
	if err := agentprofile.ValidatePrivateAccess(access); err != nil {
		return agentmemory.Record{}, memoryError(err)
	}
	id, err := agentmemory.NormalizeMemoryID(id)
	if err != nil {
		return agentmemory.Record{}, err
	}
	if err = agentmemory.ValidateDeleteInput(agentmemory.DeleteInput{ExpectedVersion: expectedVersion}); err != nil {
		return agentmemory.Record{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return agentmemory.Record{}, agentmemory.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	binding, err := lockOwnAgentPrivateBinding(ctx, tx, access, s.devPhoneEnabled)
	if err != nil {
		return agentmemory.Record{}, memoryError(err)
	}
	if _, err = lockAgentPrivateMetadata(ctx, tx, binding, true); err != nil {
		return agentmemory.Record{}, memoryError(err)
	}
	record, err := deleteOwnMemoryInTx(ctx, tx, binding, id, expectedVersion)
	if err != nil {
		return agentmemory.Record{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return agentmemory.Record{}, memoryError(err)
	}
	return record, nil
}

func deleteOwnMemoryInTx(ctx context.Context, tx pgx.Tx, binding agentPrivateBinding, id string, expectedVersion int64) (agentmemory.Record, error) {
	current, err := scanAgentMemory(tx.QueryRow(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories
		WHERE id=$1 AND agent_id=$2 AND owner_id=$3 AND owner_type='PERSON' FOR UPDATE`, id, binding.agentID, binding.accountID))
	if errors.Is(err, pgx.ErrNoRows) {
		return agentmemory.Record{}, agentmemory.ErrNotFound
	}
	if err != nil {
		return agentmemory.Record{}, agentmemory.ErrUnavailable
	}
	if current.Status == agentmemory.StatusDeleted && expectedVersion < math.MaxInt64 && current.Version == expectedVersion+1 {
		if err = recheckAgentPrivateSession(ctx, tx, binding); err != nil {
			return agentmemory.Record{}, memoryError(err)
		}
		return current, nil
	}
	if current.Version != expectedVersion || current.Status == agentmemory.StatusDeleted || current.Version == math.MaxInt64 {
		return agentmemory.Record{}, agentmemory.ErrConflict
	}
	// Owner deletion can discard a reserved non-active inference without
	// confirming it. No candidate/provenance writer is opened by this operation.
	record, err := scanAgentMemory(tx.QueryRow(ctx, `UPDATE agent_memories SET version=version+1,
		status='DELETED',summary='',structured_value='{}'::jsonb,last_reinforced_at=NULL,updated_at=clock_timestamp()
		WHERE id=$1 AND agent_id=$2 AND owner_id=$3 AND version=$4 RETURNING `+agentMemoryColumns,
		id, binding.agentID, binding.accountID, expectedVersion))
	if err != nil {
		return agentmemory.Record{}, memoryError(err)
	}
	if err = insertEnrichmentDomainAudit(ctx, tx, binding.accountID, binding.agentID, "delete", "agent_memory", record.ID, "human_memory_delete", "memory_deleted"); err != nil {
		return agentmemory.Record{}, agentmemory.ErrUnavailable
	}
	if err = recheckAgentPrivateSession(ctx, tx, binding); err != nil {
		return agentmemory.Record{}, memoryError(err)
	}
	return record, nil
}
