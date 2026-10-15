package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	lpr "github.com/birdtie/birdtie/apps/api/internal/legacyplacereference"
	"github.com/jackc/pgx/v5"
	"time"
)

var _ content.OwnedMomentMediaContextStore = (*Store)(nil)

func ownedMediaContextsTx(ctx context.Context, f humanWriteFrame, mid string) (content.OwnedMomentMediaContexts, error) {
	v := content.OwnedMomentMediaContexts{SchemaVersion: content.OwnedMomentMediaContextSchema, MomentID: mid, Items: []content.OwnedMediaContext{}}
	var parent string
	e := f.tx.QueryRow(ctx, `SELECT revision,xmin::text,clock_timestamp() FROM moments WHERE id=$1 AND author_account_id=$2 AND status<>'withdrawn'`, mid, f.a.ID).Scan(&v.MomentRevision, &parent, &v.ObservedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, content.ErrNotFound
	}
	if e != nil {
		return v, content.ErrUnavailable
	}
	v.ObservedAt = v.ObservedAt.UTC()
	v.ValidUntil = v.ObservedAt.Add(30 * time.Second)
	if e = ownedContextLeaseTx(ctx, f, &v.ValidUntil); e != nil {
		return v, e
	}
	proof := []any{parent, v.MomentRevision}
	rows, e := f.tx.Query(ctx, `SELECT c.id,c.asset_kind,coalesce(c.image_id,c.video_id),c.revision,c.source_position,c.captured_date,c.original_map_enabled,c.source_latitude::text,c.source_longitude::text,least(i.retain_until,video.retain_until),jsonb_build_array(c.xmin::text,e.xmin::text,n.xmin::text,i.xmin::text,video.xmin::text,a.xmin::text)::text FROM private_moment_media_contexts c JOIN legacy_native_context_entities e ON e.kind='media_context' AND e.target_id=c.id AND e.owner_account_id=c.owner_account_id JOIN legacy_note_archives n ON n.run_id=e.run_id AND n.source_note_id::text=e.source_parent_key AND n.source_sha256=e.source_parent_sha256 AND n.owner_account_id=c.owner_account_id AND NOT n.deleted JOIN legacy_native_adaptation_entities original ON original.run_id=n.run_id AND original.kind='moment' AND original.source_key=n.source_note_id::text AND original.source_sha256=n.source_sha256 AND original.target_id=c.moment_id::text AND original.owner_account_id=n.owner_account_id JOIN media_assets a ON a.id=coalesce(c.image_id,c.video_id) AND a.owner_account_id=c.owner_account_id AND a.state='ready_private' LEFT JOIN private_moment_images i ON i.id=c.image_id AND i.moment_id=c.moment_id AND i.owner_account_id=c.owner_account_id AND i.status='ready_private' AND i.derivative IS NOT NULL AND i.retain_until>clock_timestamp() LEFT JOIN private_moment_videos video ON video.id=c.video_id AND video.moment_id=c.moment_id AND video.owner_account_id=c.owner_account_id AND video.status='ready_private' AND video.content IS NOT NULL AND video.retain_until>clock_timestamp() WHERE c.moment_id=$1 AND c.owner_account_id=$2 AND c.status='ACTIVE' AND(i.id IS NOT NULL OR video.id IS NOT NULL) ORDER BY c.source_position,c.id LIMIT 181`, mid, f.a.ID)
	if e != nil {
		return v, content.ErrUnavailable
	}
	type item struct {
		v     content.OwnedMediaContext
		stamp string
		until time.Time
	}
	items := []item{}
	for rows.Next() {
		var x item
		var lat, lon *string
		if e = rows.Scan(&x.v.ID, &x.v.AssetKind, &x.v.AssetID, &x.v.Revision, &x.v.SourcePosition, &x.v.CapturedDate, &x.v.OriginalMapEnabled, &lat, &lon, &x.until, &x.stamp); e != nil {
			break
		}
		x.v.CoordinateStatus = "ABSENT"
		if lat != nil {
			n := json.Number(*lat)
			x.v.SourceLatitude = &n
			x.v.CoordinateStatus = "UNSPECIFIED"
		}
		if lon != nil {
			n := json.Number(*lon)
			x.v.SourceLongitude = &n
			x.v.CoordinateStatus = "UNSPECIFIED"
		}
		x.v.Locations = []content.OwnedMediaContextPlace{}
		items = append(items, x)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil || len(items) > 180 {
		return v, content.ErrUnavailable
	}
	for _, x := range items {
		if x.until.Before(v.ValidUntil) {
			v.ValidUntil = x.until.UTC()
		}
		places, e := f.tx.Query(ctx, `SELECT id,position,place_id,xmin::text FROM private_moment_media_context_places WHERE context_id=$1 ORDER BY position LIMIT 51`, x.v.ID)
		if e != nil {
			return v, content.ErrUnavailable
		}
		type place struct {
			p     content.OwnedMediaContextPlace
			stamp string
		}
		ps := []place{}
		for places.Next() {
			var p place
			if e = places.Scan(&p.p.ID, &p.p.Position, &p.p.PlaceID, &p.stamp); e != nil {
				break
			}
			ps = append(ps, p)
		}
		if e == nil {
			e = places.Err()
		}
		places.Close()
		if e != nil || len(ps) > 50 {
			return v, content.ErrUnavailable
		}
		for _, p := range ps {
			detail, e := legacyPlaceReferenceTx(ctx, f.tx, f.a.ID, lpr.Reference{Kind: "moment", RecordID: mid, PlaceID: p.p.PlaceID}, v.ObservedAt)
			if e == nil {
				p.p.LegacyReference = &detail.Reference
				if detail.Reference.ValidUntil.Before(v.ValidUntil) {
					v.ValidUntil = detail.Reference.ValidUntil
				}
			} else if !errors.Is(e, lpr.ErrNotFound) {
				return v, e
			}
			privateProof := ""
			if p.p.LegacyReference != nil {
				privateProof = p.p.LegacyReference.SourceVersion
			}
			proof = append(proof, []any{p.stamp, p.p.ID, p.p.Position, p.p.PlaceID, privateProof})
			x.v.Locations = append(x.v.Locations, p.p)
		}
		stable := x.v
		stable.Locations = nil
		proof = append(proof, []any{x.stamp, stable})
		v.Items = append(v.Items, x.v)
	}
	v.SourceVersion = content.MomentControlHash(proof)
	return v, nil
}

type ownedMediaContextProof struct {
	s     *Store
	d     [32]byte
	a     identity.Actor
	frame identity.HumanControlFrame
	v     content.OwnedMomentMediaContexts
}

func (p *ownedMediaContextProof) RevalidateHumanControl(ctx context.Context) error {
	if !p.v.ValidUntil.After(time.Now().UTC()) {
		return identity.ErrControlChanged
	}
	ctx = identity.WithHumanControlFrame(ctx, p.d, p.a, p.frame)
	_, e := p.s.readOwnedMediaContexts(ctx, p.d, p.a, p.v.MomentID, &p.v)
	return e
}
func (s *Store) readOwnedMediaContexts(ctx context.Context, d [32]byte, a identity.Actor, mid string, expected *content.OwnedMomentMediaContexts) (content.OwnedMomentMediaContexts, error) {
	if !identity.HumanControlUUID(mid) {
		return content.OwnedMomentMediaContexts{}, content.ErrInvalid
	}
	ctx, frame, e := s.humanWriteBound(ctx, d, a)
	if e != nil {
		return content.OwnedMomentMediaContexts{}, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return content.OwnedMomentMediaContexts{}, content.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	f := humanWriteFrame{tx: tx, d: d, a: a, session: frame}
	v, e := ownedMediaContextsTx(ctx, f, mid)
	if e != nil {
		return v, e
	}
	if expected != nil {
		if expected.SourceVersion != v.SourceVersion || expected.MomentRevision != v.MomentRevision || !expected.ValidUntil.After(v.ObservedAt) || v.ValidUntil.Before(expected.ValidUntil) {
			return v, identity.ErrControlChanged
		}
		v.ObservedAt, v.ValidUntil = expected.ObservedAt, expected.ValidUntil
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, v.ValidUntil, func() error {
		fresh, e := ownedMediaContextsTx(ctx, f, mid)
		if e != nil {
			return e
		}
		if fresh.SourceVersion != v.SourceVersion || fresh.ValidUntil.Before(v.ValidUntil) {
			return identity.ErrControlChanged
		}
		return nil
	})); e != nil {
		return content.OwnedMomentMediaContexts{}, e
	}
	v.ResponseProof = &ownedMediaContextProof{s, d, a, frame, v}
	return v, nil
}
func (s *Store) ReadOwnedMomentMediaContexts(ctx context.Context, d [32]byte, a identity.Actor, mid string) (content.OwnedMomentMediaContexts, error) {
	return s.readOwnedMediaContexts(ctx, d, a, mid, nil)
}

func ownedContextLeaseTx(ctx context.Context, f humanWriteFrame, until *time.Time) error {
	var bound time.Time
	e := f.tx.QueryRow(ctx, `SELECT least(expires_at,idle_expires_at)FROM sessions WHERE token_sha256=$1 AND account_id=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_expires_at>clock_timestamp()`, f.d[:], f.a.ID).Scan(&bound)
	if e != nil {
		return identity.ErrUnauthorized
	}
	if bound.Before(*until) {
		*until = bound.UTC()
	}
	return nil
}
