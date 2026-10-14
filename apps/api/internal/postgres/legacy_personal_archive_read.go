package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacycollections"
	"github.com/jackc/pgx/v5"
	"time"
)

var _ legacycollections.HumanStore = (*Store)(nil)

const personalArchiveJoin = ` FROM legacy_personal_archives n JOIN legacy_personal_archive_runs r ON r.run_id=n.run_id
 JOIN legacy_entity_mappings m ON m.source_system=n.source_system AND m.entity_kind='user' AND m.source_id=n.source_owner_id AND m.target_id=n.owner_account_id AND m.owner_account_id=n.owner_account_id AND m.run_id=n.run_id
 WHERE n.owner_account_id=$1 AND n.source_system='hangzhou-civu-20261014'`
const personalArchiveSourceSQL = `SELECT coalesce(jsonb_agg(jsonb_build_array(n.id,n.xmin::text,r.projection_sha256,r.xmin::text,m.xmin::text) ORDER BY n.id),'[]'::jsonb)::text` + personalArchiveJoin
const personalArchiveColumns = `n.id,n.kind,n.source_key,n.source_revision,n.source_sha256,n.deleted,n.source_deleted_at,n.title,n.created_at`

type personalArchiveRead struct {
	base   legacyArchiveRead
	source string
	kind   string
}

func (s *Store) beginPersonalArchive(ctx context.Context, d [32]byte, a identity.Actor, kind string) (context.Context, personalArchiveRead, error) {
	ctx, b, e := s.beginLegacyArchiveRead(ctx, d, a)
	v := personalArchiveRead{base: b, kind: kind}
	if e != nil {
		return ctx, v, e
	}
	if e = b.frame.tx.QueryRow(ctx, personalArchiveSourceSQL, a.ID).Scan(&v.source); e != nil {
		legacyMigrationRollback(b.frame.tx)
		return ctx, v, identity.ErrControlUnavailable
	}
	return ctx, v, nil
}
func (v personalArchiveRead) cursorSource() string {
	return "personal-v1:" + v.kind + ":" + v.base.source + ":" + v.source
}
func (v *personalArchiveRead) finish(ctx context.Context) error {
	native, deadline, e := v.base.store.humanWriteSession(ctx, v.base.frame.tx, v.base.frame.d, v.base.frame.a, false)
	if e != nil || native != v.base.frame.session {
		return identity.ErrControlChanged
	}
	duration := time.Until(deadline)
	if duration > 30*time.Second {
		duration = 30 * time.Second
	}
	if duration <= 0 {
		return identity.ErrControlChanged
	}
	v.base.until = time.Now().Add(duration)
	return v.base.store.humanWriteFinish(ctx, v.base.frame, func() error {
		var old, current string
		if e = v.base.frame.tx.QueryRow(ctx, legacyArchiveSourceSQL, v.base.frame.a.ID).Scan(&old); e != nil {
			return identity.ErrControlUnavailable
		}
		if e = v.base.frame.tx.QueryRow(ctx, personalArchiveSourceSQL, v.base.frame.a.ID).Scan(&current); e != nil {
			return identity.ErrControlUnavailable
		}
		if old != v.base.source || current != v.source || ctx.Err() != nil {
			return identity.ErrControlChanged
		}
		return nil
	})
}

type personalArchiveProof struct {
	store                    *Store
	digest                   [32]byte
	actor                    identity.Actor
	session                  identity.HumanControlFrame
	source, baseSource, kind string
	payload                  [32]byte
	until                    time.Time
}

func (p *personalArchiveProof) RevalidateHumanControl(ctx context.Context) error {
	if p == nil || p.store == nil || ctx == nil || ctx.Err() != nil || !time.Now().Before(p.until) {
		return identity.ErrControlChanged
	}
	ctx = identity.WithHumanControlFrame(ctx, p.digest, p.actor, p.session)
	ctx, v, e := p.store.beginPersonalArchive(ctx, p.digest, p.actor, p.kind)
	if e != nil {
		return e
	}
	defer legacyMigrationRollback(v.base.frame.tx)
	if v.source != p.source || v.base.source != p.baseSource || v.base.frame.session != p.session {
		return identity.ErrControlChanged
	}
	if e = v.finish(ctx); e != nil {
		return e
	}
	if !time.Now().Before(p.until) {
		return identity.ErrControlChanged
	}
	return nil
}
func (v personalArchiveRead) proof(payload [32]byte) *personalArchiveProof {
	return &personalArchiveProof{v.base.store, v.base.frame.d, v.base.frame.a, v.base.frame.session, v.source, v.base.source, v.kind, payload, v.base.until}
}
func personalSummary(row scanner) (legacycollections.Summary, error) {
	var s legacycollections.Summary
	e := row.Scan(&s.ID, &s.Kind, &s.SourceKey, &s.SourceRevision, &s.SourceSHA256, &s.Deleted, &s.SourceDeletedAt, &s.Title, &s.ImportedAt)
	if e == nil {
		e = legacycollections.ValidateSummary(s)
	}
	return s, e
}
func (s *Store) ListHumanLegacyPersonalArchives(ctx context.Context, d [32]byte, a identity.Actor, kind, cursor string) (legacycollections.Page, error) {
	p := legacycollections.Page{Items: []legacycollections.Summary{}}
	if kind != legacycollections.Journey && kind != legacycollections.ThemeMap && kind != legacycollections.SavedPlace {
		return p, legacycollections.ErrInvalid
	}
	ctx, v, e := s.beginPersonalArchive(ctx, d, a, kind)
	if e != nil {
		return p, e
	}
	defer legacyMigrationRollback(v.base.frame.tx)
	last, e := decodeLegacyArchiveCursor(cursor, a.ID, v.cursorSource())
	if e != nil {
		return p, e
	}
	rows, e := v.base.frame.tx.Query(ctx, `SELECT `+personalArchiveColumns+personalArchiveJoin+` AND n.kind=$2 AND($3='' OR n.id>NULLIF($3,'')::uuid) ORDER BY n.id LIMIT 21 FOR SHARE OF n,r,m`, a.ID, kind, last)
	if e != nil {
		return p, identity.ErrControlUnavailable
	}
	for rows.Next() {
		item, err := personalSummary(rows)
		if err != nil {
			rows.Close()
			return p, identity.ErrControlUnavailable
		}
		p.Items = append(p.Items, item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return p, identity.ErrControlUnavailable
	}
	if len(p.Items) > 20 {
		p.Items = p.Items[:20]
		p.NextCursor = encodeLegacyArchiveCursor(a.ID, v.cursorSource(), p.Items[19].ID)
	}
	if e = v.finish(ctx); e != nil {
		return legacycollections.Page{}, e
	}
	h, e := legacyArchivePayload(p)
	if e != nil {
		return legacycollections.Page{}, e
	}
	p.ResponseProof = v.proof(h)
	return p, nil
}
func (s *Store) ReadHumanLegacyPersonalArchive(ctx context.Context, d [32]byte, a identity.Actor, id string) (legacycollections.Detail, error) {
	var result legacycollections.Detail
	if !identity.HumanControlUUID(id) {
		return result, legacycollections.ErrNotFound
	}
	ctx, v, e := s.beginPersonalArchive(ctx, d, a, "")
	if e != nil {
		return result, e
	}
	defer legacyMigrationRollback(v.base.frame.tx)
	var s1 legacycollections.Summary
	e = v.base.frame.tx.QueryRow(ctx, `SELECT `+personalArchiveColumns+`,n.payload`+personalArchiveJoin+` AND n.id=$2 FOR SHARE OF n,r,m`, a.ID, id).Scan(&s1.ID, &s1.Kind, &s1.SourceKey, &s1.SourceRevision, &s1.SourceSHA256, &s1.Deleted, &s1.SourceDeletedAt, &s1.Title, &s1.ImportedAt, &result.Payload)
	if errors.Is(e, pgx.ErrNoRows) {
		return result, legacycollections.ErrNotFound
	}
	if e != nil {
		return result, identity.ErrControlUnavailable
	}
	result.Summary = s1
	if legacycollections.ValidateDetail(result) != nil {
		return legacycollections.Detail{}, identity.ErrControlUnavailable
	}
	if e = v.finish(ctx); e != nil {
		return legacycollections.Detail{}, e
	}
	h, e := legacyArchivePayload(result)
	if e != nil {
		return legacycollections.Detail{}, e
	}
	result.ResponseProof = v.proof(h)
	return result, nil
}
func personalProof(ctx context.Context, d [32]byte, a identity.Actor, raw any, proof identity.HumanControlProof) error {
	p, ok := proof.(*personalArchiveProof)
	if !ok || p == nil || p.digest != d || p.actor.ID != a.ID || p.actor.AccountType != a.AccountType {
		return identity.ErrControlChanged
	}
	h, e := legacyArchivePayload(raw)
	if e != nil || h != p.payload {
		return identity.ErrControlChanged
	}
	return p.RevalidateHumanControl(ctx)
}
func (s *Store) FinalizeHumanLegacyPersonalArchivePage(ctx context.Context, d [32]byte, a identity.Actor, v legacycollections.Page) error {
	return personalProof(ctx, d, a, v, v.ResponseProof)
}
func (s *Store) FinalizeHumanLegacyPersonalArchiveDetail(ctx context.Context, d [32]byte, a identity.Actor, v legacycollections.Detail) error {
	return personalProof(ctx, d, a, v, v.ResponseProof)
}
