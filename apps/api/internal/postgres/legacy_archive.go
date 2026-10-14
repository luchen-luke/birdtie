package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacyarchive"
	"github.com/jackc/pgx/v5"
)

var _ legacyarchive.HumanStore = (*Store)(nil)

// Source metadata is private. No source credential, public_url, pixel, current
// Place or Agent grant is joined here. An empty archive still has a current
// Account/Session/source lease, including suspension/reactivation ABA.
const legacyArchiveSourceSQL = `SELECT jsonb_build_array(a.id,a.xmin::text,
 (SELECT coalesce(jsonb_agg(jsonb_build_array(m.source_id,m.target_id,m.owner_account_id,m.run_id,m.source_version,m.source_sha256,m.xmin::text,r.xmin::text,r.snapshot_sha256) ORDER BY m.source_id),'[]'::jsonb)
 FROM legacy_entity_mappings m JOIN legacy_import_runs r ON r.run_id=m.run_id
 WHERE m.source_system='hangzhou-civu-20261014' AND m.entity_kind='user' AND m.target_id=a.id AND m.owner_account_id=a.id),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(n.id,n.xmin::text,n.run_id,n.source_version,n.source_sha256,n.deleted,m.xmin::text,r.xmin::text) ORDER BY n.id),'[]'::jsonb)
 FROM legacy_note_archives n JOIN legacy_entity_mappings m ON m.source_system=n.source_system AND m.entity_kind='note' AND m.source_id=n.source_note_id AND m.target_id=n.id AND m.owner_account_id=n.owner_account_id
 JOIN legacy_import_runs r ON r.run_id=n.run_id
 WHERE n.owner_account_id=a.id AND n.source_system='hangzhou-civu-20261014'))::text
 FROM accounts a WHERE a.id=$1 AND a.account_type='person' AND a.status='active'`

const legacyArchiveColumns = `n.id::text,n.source_system,n.source_note_id::text,n.source_version,n.source_sha256,n.deleted,n.title,n.recorded_start_date,n.recorded_end_date,n.source_visibility,n.source_moderation_status,n.created_at`

type legacyArchiveCursor struct {
	Schema string `json:"schema"`
	Owner  string `json:"owner"`
	Source string `json:"source"`
	Last   string `json:"last"`
}

func legacyArchiveSourceHash(source string) string {
	h := sha256.Sum256([]byte(source))
	return hex.EncodeToString(h[:])
}

func decodeLegacyArchiveCursor(raw, owner, source string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if len(raw) > 1024 {
		return "", legacyarchive.ErrInvalid
	}
	decoded, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil || base64.RawURLEncoding.EncodeToString(decoded) != raw {
		return "", legacyarchive.ErrInvalid
	}
	var v legacyArchiveCursor
	if e = json.Unmarshal(decoded, &v); e != nil || v.Schema != "legacy-owner-page-v1" || v.Owner != owner || v.Source != legacyArchiveSourceHash(source) || !legacyarchive.UUID(v.Last) {
		return "", identity.ErrControlChanged
	}
	expected, _ := json.Marshal(v)
	if string(decoded) != string(expected) {
		return "", legacyarchive.ErrInvalid
	}
	return v.Last, nil
}

func encodeLegacyArchiveCursor(owner, source, last string) string {
	raw, _ := json.Marshal(legacyArchiveCursor{"legacy-owner-page-v1", owner, legacyArchiveSourceHash(source), last})
	return base64.RawURLEncoding.EncodeToString(raw)
}

type legacyArchiveRead struct {
	store  *Store
	frame  humanWriteFrame
	source string
	until  time.Time
}

func (s *Store) beginLegacyArchiveRead(ctx context.Context, d [32]byte, a identity.Actor) (context.Context, legacyArchiveRead, error) {
	bound := ctx
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return bound, legacyArchiveRead{}, identity.ErrControlUnavailable
	}
	first, valid, present := identity.OriginalHumanControlFrame(ctx, d, a)
	v := legacyArchiveRead{store: s, frame: humanWriteFrame{d: d, a: a, session: first}}
	if a.AccountType != "person" || !legacyarchive.UUID(a.ID) || d == ([32]byte{}) {
		return bound, v, identity.ErrUnauthorized
	}
	if present && (!valid || !identity.HumanControlUUID(first.ID) || first.Generation < 1) {
		return bound, v, identity.ErrControlChanged
	}
	var e error
	v.frame.tx, e = s.pool.BeginTx(bound, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return bound, v, identity.ErrControlUnavailable
	}
	fail := func(e error) (context.Context, legacyArchiveRead, error) {
		_ = v.frame.tx.Rollback(context.Background())
		return bound, v, e
	}
	if !present {
		// An ordinary direct reader also captures its original Session before
		// waiting. Do not let the generic capture's account lock hide an ABA
		// before this archive's source frame has been read.
		first, _, e = s.humanWriteSession(bound, v.frame.tx, d, a, false)
		if e != nil {
			return fail(e)
		}
		v.frame.session = first
		bound = identity.WithHumanControlFrame(bound, d, a, first)
	}
	if e = v.frame.tx.QueryRow(bound, legacyArchiveSourceSQL, a.ID).Scan(&v.source); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return fail(identity.ErrUnauthorized)
		}
		return fail(identity.ErrControlUnavailable)
	}
	var id string
	if e = v.frame.tx.QueryRow(bound, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, a.ID).Scan(&id); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return fail(identity.ErrControlChanged)
		}
		return fail(identity.ErrControlUnavailable)
	}
	// Re-read after the account lock wait; returning to active cannot revive the
	// old metadata frame. Note/map/run rows are immutable under migration146.
	var current string
	if e = v.frame.tx.QueryRow(bound, legacyArchiveSourceSQL, a.ID).Scan(&current); e != nil || current != v.source {
		return fail(identity.ErrControlChanged)
	}
	return bound, v, nil
}

func (v *legacyArchiveRead) finish(ctx context.Context) error {
	current, deadline, e := v.store.humanWriteSession(ctx, v.frame.tx, v.frame.d, v.frame.a, true)
	if e != nil {
		return e
	}
	if current != v.frame.session {
		return identity.ErrControlChanged
	}
	var nativeNow time.Time
	if e = v.frame.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&nativeNow); e != nil {
		return identity.ErrControlUnavailable
	}
	duration := deadline.Sub(nativeNow)
	if duration > 30*time.Second {
		duration = 30 * time.Second
	}
	if duration <= 0 {
		return identity.ErrControlChanged
	}
	v.until = time.Now().Add(duration)
	return v.store.humanWriteFinish(ctx, v.frame, func() error {
		var source string
		if e := v.frame.tx.QueryRow(ctx, legacyArchiveSourceSQL, v.frame.a.ID).Scan(&source); e != nil {
			return identity.ErrControlUnavailable
		}
		if source != v.source || ctx.Err() != nil || !time.Now().Before(v.until) {
			return identity.ErrControlChanged
		}
		return nil
	})
}

func scanLegacyArchiveSummary(row scanner) (legacyarchive.Summary, error) {
	var v legacyarchive.Summary
	e := row.Scan(&v.ID, &v.Source.System, &v.Source.ID, &v.SourceVersion, &v.SourceSHA256, &v.Deleted, &v.Title, &v.RecordedStartDate, &v.RecordedEndDate, &v.SourceVisibility, &v.SourceModerationStatus, &v.ImportedAt)
	v.Source.Kind = "note"
	if e == nil {
		e = legacyarchive.ValidateSummary(v)
	}
	return v, e
}

type legacyArchiveProof struct {
	store   *Store
	digest  [32]byte
	actor   identity.Actor
	session identity.HumanControlFrame
	source  string
	ids     []string
	kind    string
	payload [32]byte
	until   time.Time
}

func legacyArchivePayload(v any) ([32]byte, error) {
	raw, e := json.Marshal(v)
	if e != nil {
		return [32]byte{}, identity.ErrControlUnavailable
	}
	return sha256.Sum256(raw), nil
}

func (p *legacyArchiveProof) RevalidateHumanControl(ctx context.Context) error {
	if p == nil || p.store == nil || ctx == nil || ctx.Err() != nil || !time.Now().Before(p.until) {
		return identity.ErrControlChanged
	}
	ctx, v, e := p.store.beginLegacyArchiveRead(ctx, p.digest, p.actor)
	if e != nil {
		return e
	}
	defer v.frame.tx.Rollback(context.Background())
	if v.source != p.source || v.frame.session != p.session {
		return identity.ErrControlChanged
	}
	for _, id := range p.ids {
		var current string
		if e = v.frame.tx.QueryRow(ctx, `SELECT id FROM legacy_note_archives WHERE id=$1 AND owner_account_id=$2 AND source_system='hangzhou-civu-20261014' FOR SHARE`, id, p.actor.ID).Scan(&current); e != nil {
			return identity.ErrControlChanged
		}
	}
	if e = v.finish(ctx); e != nil {
		return e
	}
	if !time.Now().Before(p.until) {
		return identity.ErrControlChanged
	}
	return nil
}

func (s *Store) ListHumanLegacyArchives(ctx context.Context, d [32]byte, a identity.Actor, cursor string) (legacyarchive.Page, error) {
	ctx, v, e := s.beginLegacyArchiveRead(ctx, d, a)
	if e != nil {
		return legacyarchive.Page{}, e
	}
	defer v.frame.tx.Rollback(context.Background())
	last, e := decodeLegacyArchiveCursor(cursor, a.ID, v.source)
	if e != nil {
		return legacyarchive.Page{}, e
	}
	rows, e := v.frame.tx.Query(ctx, `SELECT `+legacyArchiveColumns+` FROM legacy_note_archives n
 JOIN legacy_entity_mappings m ON m.source_system=n.source_system AND m.entity_kind='note' AND m.source_id=n.source_note_id AND m.target_id=n.id AND m.owner_account_id=n.owner_account_id AND m.run_id=n.run_id AND m.source_sha256=n.source_sha256 AND m.source_version=n.source_version
 WHERE n.owner_account_id=$1 AND n.source_system='hangzhou-civu-20261014' AND ($2='' OR n.id>NULLIF($2,'')::uuid)
 ORDER BY n.id LIMIT 21 FOR SHARE OF n,m`, a.ID, last)
	if e != nil {
		return legacyarchive.Page{}, identity.ErrControlUnavailable
	}
	page := legacyarchive.Page{Items: []legacyarchive.Summary{}}
	ids := []string{}
	for rows.Next() {
		item, e := scanLegacyArchiveSummary(rows)
		if e != nil {
			rows.Close()
			return legacyarchive.Page{}, identity.ErrControlUnavailable
		}
		page.Items = append(page.Items, item)
		ids = append(ids, item.ID)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return legacyarchive.Page{}, identity.ErrControlUnavailable
	}
	if len(page.Items) > legacyarchive.PageSize {
		page.Items = page.Items[:legacyarchive.PageSize]
		page.NextCursor = encodeLegacyArchiveCursor(a.ID, v.source, page.Items[len(page.Items)-1].ID)
	}
	payload, e := legacyArchivePayload(page)
	if e != nil {
		return legacyarchive.Page{}, e
	}
	if e = v.finish(ctx); e != nil {
		return legacyarchive.Page{}, e
	}
	page.ResponseProof = &legacyArchiveProof{s, d, a, v.frame.session, v.source, ids, "page", payload, v.until}
	return page, nil
}

func (s *Store) ReadHumanLegacyArchive(ctx context.Context, d [32]byte, a identity.Actor, id string) (legacyarchive.Detail, error) {
	if !legacyarchive.UUID(id) {
		return legacyarchive.Detail{}, legacyarchive.ErrInvalid
	}
	ctx, v, e := s.beginLegacyArchiveRead(ctx, d, a)
	if e != nil {
		return legacyarchive.Detail{}, e
	}
	defer v.frame.tx.Rollback(context.Background())
	var locked string
	e = v.frame.tx.QueryRow(ctx, `SELECT n.id FROM legacy_note_archives n JOIN legacy_entity_mappings m ON m.source_system=n.source_system AND m.entity_kind='note' AND m.source_id=n.source_note_id AND m.target_id=n.id AND m.owner_account_id=n.owner_account_id AND m.run_id=n.run_id AND m.source_sha256=n.source_sha256 AND m.source_version=n.source_version WHERE n.id=$1 AND n.owner_account_id=$2 AND n.source_system='hangzhou-civu-20261014' FOR SHARE OF n,m`, id, a.ID).Scan(&locked)
	if errors.Is(e, pgx.ErrNoRows) {
		if e = v.finish(ctx); e != nil {
			return legacyarchive.Detail{}, e
		}
		return legacyarchive.Detail{}, legacyarchive.ErrNotFound
	}
	if e != nil {
		return legacyarchive.Detail{}, identity.ErrControlUnavailable
	}
	summary, e := scanLegacyArchiveSummary(v.frame.tx.QueryRow(ctx, `SELECT `+legacyArchiveColumns+` FROM legacy_note_archives n WHERE n.id=$1 AND n.owner_account_id=$2`, id, a.ID))
	if e != nil {
		return legacyarchive.Detail{}, identity.ErrControlUnavailable
	}
	detail := legacyarchive.Detail{Summary: summary}
	var relationships []byte
	e = v.frame.tx.QueryRow(ctx, `SELECT body,source_relationships FROM legacy_note_archives WHERE id=$1 AND owner_account_id=$2`, id, a.ID).Scan(&detail.Body, &relationships)
	if e != nil {
		return legacyarchive.Detail{}, identity.ErrControlUnavailable
	}
	if !summary.Deleted {
		decoded, e := legacyarchive.DecodeRelationships(relationships)
		if e != nil {
			return legacyarchive.Detail{}, identity.ErrControlUnavailable
		}
		detail.Relationships = &decoded
	} else if detail.Body != "" || relationships != nil {
		return legacyarchive.Detail{}, identity.ErrControlUnavailable
	}
	if e = legacyarchive.ValidateDetail(detail); e != nil {
		return legacyarchive.Detail{}, identity.ErrControlUnavailable
	}
	payload, e := legacyArchivePayload(detail)
	if e != nil {
		return legacyarchive.Detail{}, e
	}
	if e = v.finish(ctx); e != nil {
		return legacyarchive.Detail{}, e
	}
	detail.ResponseProof = &legacyArchiveProof{s, d, a, v.frame.session, v.source, []string{id}, "detail", payload, v.until}
	return detail, nil
}

func (s *Store) finalizeLegacyArchive(ctx context.Context, d [32]byte, a identity.Actor, kind string, value any, proof identity.HumanControlProof) error {
	p, ok := proof.(*legacyArchiveProof)
	if !ok || p == nil || p.store != s || p.digest != d || p.actor.ID != a.ID || p.actor.AccountType != a.AccountType || p.kind != kind {
		return identity.ErrControlChanged
	}
	hash, e := legacyArchivePayload(value)
	if e != nil {
		return e
	}
	if hash != p.payload {
		return identity.ErrControlChanged
	}
	return p.RevalidateHumanControl(ctx)
}

func (s *Store) FinalizeHumanLegacyArchiveRead(ctx context.Context, d [32]byte, a identity.Actor, v legacyarchive.Detail) error {
	if e := legacyarchive.ValidateDetail(v); e != nil {
		return identity.ErrControlChanged
	}
	return s.finalizeLegacyArchive(ctx, d, a, "detail", v, v.ResponseProof)
}

func (s *Store) FinalizeHumanLegacyArchivePage(ctx context.Context, d [32]byte, a identity.Actor, v legacyarchive.Page) error {
	if v.Items == nil || len(v.Items) > legacyarchive.PageSize {
		return identity.ErrControlChanged
	}
	for _, item := range v.Items {
		if e := legacyarchive.ValidateSummary(item); e != nil {
			return identity.ErrControlChanged
		}
	}
	return s.finalizeLegacyArchive(ctx, d, a, "page", v, v.ResponseProof)
}
