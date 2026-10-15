package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
	"github.com/jackc/pgx/v5"

	"time"
)

var _ content.HumanReferenceImageStore = (*Store)(nil)
var _ content.HumanReferenceImageTargetsStore = (*Store)(nil)

// Every current read requires an immutable source owner, the exact original
// native target mapping and a still-readable material. A Place UUID alone is
// not a media grant. System artwork is deliberately outside this relation.
func referenceImageSQL() string {
	return referenceImageReadSQL(false)
}

func referenceImageReadSQL(targets bool) string {
	projection, target, order := "", " AND r.target_id=$3::uuid", "r.source_position,r.id"
	if targets {
		projection, target, order = "r.target_id::text,p.name,", "", "r.target_id,r.source_position,r.id"
	}
	return `SELECT ` + projection + `r.id::text,r.kind,l.revision,r.source_position,m.mime_type,m.width,m.height,m.byte_size,m.sha256,r.source_type,r.review_status,r.source_is_cover,r.rights_basis,r.attribution,r.source_publishable,
 jsonb_build_array(r.xmin::text,l.xmin::text,m.xmin::text,af.xmin::text,ir.xmin::text,original.xmin::text,u.xmin::text,p.xmin::text,mo.xmin::text,j.xmin::text,backing.xmin::text,bm.xmin::text,ma.xmin::text,CASE WHEN p.id IS NOT NULL THEN ` + retainedPlaceReadBindingSQL("p") + ` END)::text,
 least(backing.retain_until,p.expires_at,CASE WHEN p.id IS NOT NULL THEN ` + currentRetainedPlaceDeadlineSQL("p", "clock_timestamp()") + ` END)
 FROM legacy_reference_media_relations r JOIN private_reference_image_links l ON l.id=r.id AND l.status='ACTIVE'
 JOIN private_reference_image_materials m ON m.id=l.material_id AND m.owner_account_id=r.owner_account_id AND m.status='READY_PRIVATE'
 JOIN legacy_reference_media_assets af ON af.run_id=r.run_id AND af.source_media_id=r.source_media_id AND af.material_id=m.id AND af.owner_account_id=r.owner_account_id AND NOT af.system_archive
 JOIN legacy_reference_media_runs ir ON ir.run_id=r.run_id
 JOIN legacy_import_runs original ON original.run_id=ir.run_id AND original.source_file_sha256=ir.source_file_sha256 AND original.manifest_file_sha256=ir.manifest_file_sha256 AND original.snapshot_sha256=ir.snapshot_sha256
 JOIN legacy_entity_mappings u ON u.run_id=r.run_id AND u.entity_kind='user' AND u.source_id=r.source_owner_id AND u.target_id=r.owner_account_id AND u.owner_account_id=r.owner_account_id
 LEFT JOIN places p ON r.target_type='PLACE' AND p.id=r.target_id
 LEFT JOIN moments mo ON r.target_type='MOMENT' AND mo.id=r.target_id AND mo.author_account_id=r.owner_account_id
 LEFT JOIN journeys j ON r.target_type='JOURNEY' AND j.id=r.target_id AND j.owner_account_id=r.owner_account_id
 LEFT JOIN private_moment_images backing ON backing.id=m.backing_image_id AND backing.owner_account_id=m.owner_account_id
 LEFT JOIN moments bm ON bm.id=backing.moment_id AND bm.author_account_id=backing.owner_account_id
 LEFT JOIN media_assets ma ON ma.id=backing.id AND ma.owner_account_id=backing.owner_account_id
 WHERE r.owner_account_id=$1 AND r.target_type=$2` + target + ` AND NOT r.tombstone AND r.review_status<>'removed'
 AND((m.backing_image_id IS NULL AND octet_length(m.content)=m.byte_size AND encode(sha256(m.content),'hex')=m.sha256)
 OR(m.backing_image_id IS NOT NULL AND backing.status='ready_private' AND backing.retain_until>clock_timestamp() AND ma.state='ready_private' AND ma.byte_size=m.byte_size AND ma.sha256_hex=m.sha256 AND backing.derivative_sha256=m.sha256 AND bm.status='draft' AND bm.visibility='private' AND octet_length(backing.derivative)=m.byte_size AND encode(sha256(backing.derivative),'hex')=m.sha256
 AND EXISTS(SELECT 1 FROM(SELECT run_id,source_media_id,source_media_sha256,owner_account_id,image_id FROM legacy_private_media_import_items UNION ALL SELECT run_id,source_media_id,source_media_sha256,owner_account_id,image_id FROM legacy_private_image_recovery_items) bf WHERE bf.run_id=r.run_id AND bf.source_media_id=af.source_media_id AND bf.source_media_sha256=af.source_media_sha256 AND bf.image_id=backing.id AND bf.owner_account_id=r.owner_account_id)))
 AND((r.target_type='PLACE' AND p.publication_status IN('draft','published') AND(p.expires_at IS NULL OR p.expires_at>clock_timestamp()) AND ` + currentRetainedPlaceVisibilitySQL("p", "clock_timestamp()") + `
 AND EXISTS(SELECT 1 FROM(SELECT run_id,source_key,source_sha256,target_id FROM legacy_native_adaptation_entities WHERE kind='place' UNION ALL SELECT run_id,source_key,source_sha256,target_id::text FROM legacy_native_context_entities WHERE kind='unplaced_place') pm WHERE pm.run_id=r.run_id AND pm.source_key=r.source_parent_id::text AND pm.source_sha256=r.source_parent_sha256 AND pm.target_id=p.id::text))
 OR(r.target_type='MOMENT' AND mo.status<>'withdrawn' AND EXISTS(SELECT 1 FROM legacy_native_adaptation_entities mm JOIN legacy_note_archives na ON na.run_id=mm.run_id AND na.source_note_id::text=mm.source_key AND na.owner_account_id=mm.owner_account_id AND na.source_sha256=mm.source_sha256 AND NOT na.deleted WHERE mm.run_id=r.run_id AND mm.kind='moment' AND mm.source_key=r.source_parent_id::text AND mm.source_sha256=r.source_parent_sha256 AND mm.target_id=mo.id::text AND mm.owner_account_id=r.owner_account_id))
 OR(r.target_type='JOURNEY' AND j.status<>'withdrawn' AND EXISTS(SELECT 1 FROM legacy_journey_adaptations jm JOIN legacy_personal_archives ar ON ar.run_id=jm.run_id AND ar.kind='journey' AND ar.source_key=jm.source_key AND ar.source_sha256=jm.source_sha256 AND ar.owner_account_id=jm.owner_account_id AND NOT ar.deleted WHERE jm.run_id=r.run_id AND jm.source_key=r.source_parent_id::text AND jm.source_sha256=r.source_parent_sha256 AND jm.target_id=j.id AND jm.owner_account_id=r.owner_account_id)))
 ORDER BY ` + order + ` LIMIT 10001`
}
func referenceQueryHash(q content.ReferenceImageQuery) string {
	return content.MomentControlHash([]any{"private-reference-images-v1", q.TargetType, q.TargetID})
}

func readReferenceTargetsTx(ctx context.Context, f humanWriteFrame, q content.ReferenceImageTargetsQuery) (content.ReferenceImageTargetsPage, error) {
	p := content.ReferenceImageTargetsPage{SchemaVersion: content.ReferenceImageTargetsSchema, TargetType: q.TargetType, Items: []content.ReferenceImageTarget{}}
	if f.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&p.ObservedAt) != nil {
		return p, content.ErrUnavailable
	}
	p.ObservedAt = p.ObservedAt.UTC()
	p.ValidUntil = p.ObservedAt.Add(30 * time.Second)
	if e := ownedContextLeaseTx(ctx, f, &p.ValidUntil); e != nil {
		return p, e
	}
	var principal string
	if f.tx.QueryRow(ctx, `SELECT concat_ws(':',id::text,account_type,status,xmin::text) FROM accounts WHERE id=$1 AND account_type='person' AND status='active'`, f.a.ID).Scan(&principal) != nil {
		return p, identity.ErrControlChanged
	}
	cursor, e := decodeJourneyHistoryCursor(q.Cursor)
	if e != nil {
		return p, e
	}
	// The exact same immutable source-owner, target mapping, review, parent,
	// retention and current-byte gates apply to discovery and actual images.
	rows, e := f.tx.Query(ctx, referenceImageReadSQL(true), f.a.ID, q.TargetType)
	if e != nil {
		return p, content.ErrUnavailable
	}
	all := []content.ReferenceImageTarget{}
	facts := []any{}
	count := 0
	for rows.Next() {
		var target, name, source string
		var image content.ReferenceImage
		var until *time.Time
		e = rows.Scan(&target, &name, &image.ID, &image.Kind, &image.Revision, &image.Position, &image.MIMEType, &image.Width, &image.Height, &image.ByteCount, &image.SHA256, &image.SourceType, &image.SourceReviewStatus, &image.SourceIsCover, &image.SourceRightsBasis, &image.SourceAttribution, &image.SourcePublishable, &source, &until)
		if e != nil {
			break
		}
		count++
		facts = append(facts, []any{target, name, image, source})
		if len(all) == 0 || all[len(all)-1].TargetID != target {
			all = append(all, content.ReferenceImageTarget{TargetID: target, Name: name})
		}
		all[len(all)-1].ImageCount++
		if until != nil && until.Before(p.ValidUntil) {
			p.ValidUntil = until.UTC()
		}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil || count > 10000 {
		return p, content.ErrUnavailable
	}
	query := content.MomentControlHash([]any{content.ReferenceImageTargetsSchema, q.TargetType})
	p.SourceVersion = content.MomentControlHash([]any{query, historyReader(f), principal, facts})
	start := 0
	if cursor != nil {
		if cursor.Owner != f.a.ID || cursor.Reader != historyReader(f) || cursor.Query != query || cursor.Source != p.SourceVersion || !cursor.Until.After(p.ObservedAt) {
			return p, identity.ErrControlChanged
		}
		found := false
		for i, item := range all {
			if item.TargetID == cursor.Before {
				start, found = i+1, true
				break
			}
		}
		if !found {
			return p, identity.ErrControlChanged
		}
		if cursor.Until.Before(p.ValidUntil) {
			p.ValidUntil = cursor.Until
		}
	}
	end := min(start+20, len(all))
	p.Items = append(p.Items, all[start:end]...)
	if end < len(all) {
		next, e := encodeJourneyHistoryCursor(journeyHistoryCursor{Version: 1, Owner: f.a.ID, Reader: historyReader(f), Query: query, Source: p.SourceVersion, Before: p.Items[len(p.Items)-1].TargetID, Until: p.ValidUntil})
		if e != nil {
			return p, e
		}
		p.NextCursor = &next
	}
	if !p.Valid(time.Now().UTC()) {
		return p, identity.ErrControlChanged
	}
	return p, nil
}

type referenceTargetsProof struct {
	s      *Store
	d      [32]byte
	a      identity.Actor
	q      content.ReferenceImageTargetsQuery
	source string
	until  time.Time
}

func (p *referenceTargetsProof) RevalidateHumanControl(ctx context.Context) error {
	if !p.until.After(time.Now().UTC()) {
		return identity.ErrControlChanged
	}
	fresh, e := p.s.ReadHumanReferenceImageTargets(ctx, p.d, p.a, p.q)
	if e != nil {
		return e
	}
	if fresh.SourceVersion != p.source || fresh.ValidUntil.Before(p.until) {
		return identity.ErrControlChanged
	}
	return nil
}

func (s *Store) ReadHumanReferenceImageTargets(ctx context.Context, d [32]byte, a identity.Actor, q content.ReferenceImageTargetsQuery) (content.ReferenceImageTargetsPage, error) {
	if !q.Valid() {
		return content.ReferenceImageTargetsPage{}, content.ErrInvalid
	}
	ctx, frame, e := s.humanWriteBound(ctx, d, a)
	if e != nil {
		return content.ReferenceImageTargetsPage{}, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return content.ReferenceImageTargetsPage{}, content.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	f := humanWriteFrame{tx: tx, d: d, a: a, session: frame}
	p, e := readReferenceTargetsTx(ctx, f, q)
	if e != nil {
		return p, e
	}
	e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, p.ValidUntil, func() error {
		fresh, e := readReferenceTargetsTx(ctx, f, q)
		if e != nil {
			return e
		}
		if fresh.SourceVersion != p.SourceVersion || fresh.ValidUntil.Before(p.ValidUntil) {
			return identity.ErrControlChanged
		}
		return nil
	}))
	if e != nil {
		return content.ReferenceImageTargetsPage{}, e
	}
	p.ResponseProof = &referenceTargetsProof{s, d, a, q, p.SourceVersion, p.ValidUntil}
	return p, nil
}
func readReferenceImagesTx(ctx context.Context, f humanWriteFrame, q content.ReferenceImageQuery) (content.ReferenceImagePage, error) {
	p := content.ReferenceImagePage{SchemaVersion: content.ReferenceImageSchema, TargetType: q.TargetType, TargetID: q.TargetID, Items: []content.ReferenceImage{}}
	if f.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&p.ObservedAt) != nil {
		return p, content.ErrUnavailable
	}
	p.ObservedAt = p.ObservedAt.UTC()
	p.ValidUntil = p.ObservedAt.Add(30 * time.Second)
	var principal string
	if f.tx.QueryRow(ctx, `SELECT concat_ws(':',id::text,account_type,status,xmin::text) FROM accounts WHERE id=$1 AND account_type='person' AND status='active'`, f.a.ID).Scan(&principal) != nil {
		return p, identity.ErrControlChanged
	}
	if e := ownedContextLeaseTx(ctx, f, &p.ValidUntil); e != nil {
		return p, e
	}
	cursor, e := decodeJourneyHistoryCursor(q.Cursor)
	if e != nil {
		return p, e
	}
	rows, e := f.tx.Query(ctx, referenceImageSQL(), f.a.ID, q.TargetType, q.TargetID)
	if e != nil {
		return p, content.ErrUnavailable
	}
	all := []content.ReferenceImage{}
	sources := []string{}
	for rows.Next() {
		var i content.ReferenceImage
		var raw string
		var until *time.Time
		if e = rows.Scan(&i.ID, &i.Kind, &i.Revision, &i.Position, &i.MIMEType, &i.Width, &i.Height, &i.ByteCount, &i.SHA256, &i.SourceType, &i.SourceReviewStatus, &i.SourceIsCover, &i.SourceRightsBasis, &i.SourceAttribution, &i.SourcePublishable, &raw, &until); e != nil {
			break
		}
		i.SourceVersion = content.MomentControlHash([]any{"reference-image-v1", historyReader(f), principal, referenceQueryHash(q), i, raw})
		i.ContentPath = fmt.Sprintf("/v1/me/reference-images/%s/content?revision=%d&sourceVersion=%s", i.ID, i.Revision, i.SourceVersion)
		if until != nil && until.Before(p.ValidUntil) {
			p.ValidUntil = until.UTC()
		}
		all = append(all, i)
		sources = append(sources, raw)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil || len(all) > 10000 {
		return p, content.ErrUnavailable
	}
	p.SourceVersion = content.MomentControlHash([]any{"reference-page-v1", historyReader(f), principal, referenceQueryHash(q), all, sources})
	start := 0
	if cursor != nil {
		if cursor.Owner != f.a.ID || cursor.Reader != historyReader(f) || cursor.Query != referenceQueryHash(q) || cursor.Source != p.SourceVersion || !cursor.Until.After(p.ObservedAt) {
			return p, identity.ErrControlChanged
		}
		found := false
		for n, i := range all {
			if i.ID == cursor.Before {
				start = n + 1
				found = true
				break
			}
		}
		if !found {
			return p, identity.ErrControlChanged
		}
		if cursor.Until.Before(p.ValidUntil) {
			p.ValidUntil = cursor.Until
		}
	}
	end := start + 20
	if end > len(all) {
		end = len(all)
	}
	p.Items = append(p.Items, all[start:end]...)
	if end < len(all) {
		c, e := encodeJourneyHistoryCursor(journeyHistoryCursor{Version: 1, Owner: f.a.ID, Reader: historyReader(f), Query: referenceQueryHash(q), Source: p.SourceVersion, Before: p.Items[len(p.Items)-1].ID, Until: p.ValidUntil})
		if e != nil {
			return p, e
		}
		p.NextCursor = &c
	}
	if !p.Valid(time.Now().UTC()) {
		return p, identity.ErrControlChanged
	}
	return p, nil
}

type referenceImageProof struct {
	s      *Store
	d      [32]byte
	a      identity.Actor
	q      content.ReferenceImageQuery
	source string
	until  time.Time
}

func (p *referenceImageProof) RevalidateHumanControl(ctx context.Context) error {
	if !p.until.After(time.Now().UTC()) {
		return identity.ErrControlChanged
	}
	v, e := p.s.ReadHumanReferenceImages(ctx, p.d, p.a, p.q)
	if e != nil {
		return e
	}
	if v.SourceVersion != p.source || v.ValidUntil.Before(p.until) {
		return identity.ErrControlChanged
	}
	return nil
}
func (s *Store) ReadHumanReferenceImages(ctx context.Context, d [32]byte, a identity.Actor, q content.ReferenceImageQuery) (content.ReferenceImagePage, error) {
	if !q.Valid() {
		return content.ReferenceImagePage{}, content.ErrInvalid
	}
	ctx, frame, e := s.humanWriteBound(ctx, d, a)
	if e != nil {
		return content.ReferenceImagePage{}, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return content.ReferenceImagePage{}, content.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	f := humanWriteFrame{tx: tx, d: d, a: a, session: frame}
	p, e := readReferenceImagesTx(ctx, f, q)
	if e != nil {
		return p, e
	}
	e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, p.ValidUntil, func() error {
		fresh, e := readReferenceImagesTx(ctx, f, q)
		if e != nil {
			return e
		}
		if fresh.SourceVersion != p.SourceVersion || fresh.ValidUntil.Before(p.ValidUntil) {
			return identity.ErrControlChanged
		}
		return nil
	}))
	if e != nil {
		return content.ReferenceImagePage{}, e
	}
	p.ResponseProof = &referenceImageProof{s, d, a, q, p.SourceVersion, p.ValidUntil}
	return p, nil
}
func referenceImageQueryForID(ctx context.Context, f humanWriteFrame, id string) (content.ReferenceImageQuery, error) {
	q := content.ReferenceImageQuery{}
	e := f.tx.QueryRow(ctx, `SELECT target_type,target_id::text FROM legacy_reference_media_relations WHERE id=$1 AND owner_account_id=$2`, id, f.a.ID).Scan(&q.TargetType, &q.TargetID)
	if errors.Is(e, pgx.ErrNoRows) {
		return q, content.ErrNotFound
	}
	if e != nil {
		return q, content.ErrUnavailable
	}
	return q, nil
}

// Excludes only the two current rows this DELETE itself is allowed to retire.
// All independent principal, target, reused-source and provider epochs remain
// in the final comparison, even when the target changed away and back.
func referenceMutationEpoch(ctx context.Context, f humanWriteFrame, id string) (string, error) {
	var source string
	e := f.tx.QueryRow(ctx, `SELECT jsonb_build_array(own.xmin::text,p.xmin::text,mo.xmin::text,j.xmin::text,b.xmin::text,bm.xmin::text,ma.xmin::text,CASE WHEN p.id IS NOT NULL THEN `+retainedPlaceReadBindingSQL("p")+` END)::text FROM legacy_reference_media_relations r JOIN private_reference_image_links l ON l.id=r.id JOIN private_reference_image_materials m ON m.id=l.material_id JOIN accounts own ON own.id=r.owner_account_id LEFT JOIN places p ON r.target_type='PLACE' AND p.id=r.target_id LEFT JOIN moments mo ON r.target_type='MOMENT' AND mo.id=r.target_id LEFT JOIN journeys j ON r.target_type='JOURNEY' AND j.id=r.target_id LEFT JOIN private_moment_images b ON b.id=m.backing_image_id LEFT JOIN moments bm ON bm.id=b.moment_id LEFT JOIN media_assets ma ON ma.id=b.id WHERE r.id=$1 AND r.owner_account_id=$2`, id, f.a.ID).Scan(&source)
	if e != nil {
		return "", content.ErrUnavailable
	}
	return source, nil
}
func referenceItem(ctx context.Context, f humanWriteFrame, id string) (content.ReferenceImagePage, content.ReferenceImage, error) {
	q, e := referenceImageQueryForID(ctx, f, id)
	if e != nil {
		return content.ReferenceImagePage{}, content.ReferenceImage{}, e
	}
	for {
		p, e := readReferenceImagesTx(ctx, f, q)
		if e != nil {
			return p, content.ReferenceImage{}, e
		}
		for _, v := range p.Items {
			if v.ID == id {
				return p, v, nil
			}
		}
		if p.NextCursor == nil {
			return p, content.ReferenceImage{}, content.ErrNotFound
		}
		q.Cursor = *p.NextCursor
	}
}
func (s *Store) ReadHumanReferenceImageContent(ctx context.Context, d [32]byte, a identity.Actor, id string, revision int64, source string) (content.ReferenceImageContent, error) {
	empty := content.ReferenceImageContent{}
	if !identity.HumanControlUUID(id) || revision < 1 || !content.MomentControlHex(source) {
		return empty, content.ErrInvalid
	}
	ctx, frame, e := s.humanWriteBound(ctx, d, a)
	if e != nil {
		return empty, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return empty, content.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	f := humanWriteFrame{tx: tx, d: d, a: a, session: frame}
	p, i, e := referenceItem(ctx, f, id)
	if e != nil {
		return empty, e
	}
	if i.Revision != revision || i.SourceVersion != source {
		return empty, identity.ErrControlChanged
	}
	var body []byte
	e = tx.QueryRow(ctx, `SELECT coalesce(m.content,b.derivative) FROM private_reference_image_links l JOIN private_reference_image_materials m ON m.id=l.material_id LEFT JOIN private_moment_images b ON b.id=m.backing_image_id WHERE l.id=$1 AND l.status='ACTIVE' AND m.owner_account_id=$2 AND m.status='READY_PRIVATE'`, id, a.ID).Scan(&body)
	if e != nil || len(body) != i.ByteCount || legacymedia.Hash(body) != i.SHA256 {
		clear(body)
		return empty, content.ErrUnavailable
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, p.ValidUntil, func() error {
		fresh, fi, e := referenceItem(ctx, f, id)
		if e != nil {
			return e
		}
		if fi.SourceVersion != source || fresh.ValidUntil.Before(p.ValidUntil) {
			return identity.ErrControlChanged
		}
		return nil
	})); e != nil {
		clear(body)
		return empty, e
	}
	q, _ := referenceImageQueryForProof(p)
	return content.ReferenceImageContent{Image: i, Bytes: body, ValidUntil: p.ValidUntil, ResponseProof: &referenceImageProof{s, d, a, q, p.SourceVersion, p.ValidUntil}}, nil
}
func referenceImageQueryForProof(p content.ReferenceImagePage) (content.ReferenceImageQuery, error) {
	return content.ReferenceImageQuery{TargetType: p.TargetType, TargetID: p.TargetID}, nil
}

type referenceReceiptProof struct {
	s         *Store
	d         [32]byte
	a         identity.Actor
	operation string
}

func (p *referenceReceiptProof) RevalidateHumanControl(ctx context.Context) error {
	_, e := p.s.ReadHumanReferenceImageOperation(ctx, p.d, p.a, p.operation)
	return e
}
func readReferenceOperation(ctx context.Context, tx pgx.Tx, owner, operation string) (content.ReferenceImageReceipt, string, error) {
	var r content.ReferenceImageReceipt
	var raw []byte
	var hash string
	e := tx.QueryRow(ctx, `SELECT receipt,input_sha256 FROM private_reference_image_operations WHERE owner_account_id=$1 AND operation_id=$2`, owner, operation).Scan(&raw, &hash)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, "", content.ErrNotFound
	}
	if e != nil || json.Unmarshal(raw, &r) != nil {
		return r, "", content.ErrUnavailable
	}
	return r, hash, nil
}
func (s *Store) ReadHumanReferenceImageOperation(ctx context.Context, d [32]byte, a identity.Actor, operation string) (content.ReferenceImageReceipt, error) {
	var empty content.ReferenceImageReceipt
	if !identity.HumanControlUUID(operation) {
		return empty, content.ErrInvalid
	}
	ctx, frame, e := s.humanWriteBound(ctx, d, a)
	if e != nil {
		return empty, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return empty, content.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	f := humanWriteFrame{tx: tx, d: d, a: a, session: frame}
	v, _, e := readReferenceOperation(ctx, tx, a.ID, operation)
	if e != nil {
		return empty, e
	}
	if e = s.humanWriteFinish(ctx, f); e != nil {
		return empty, e
	}
	v.ResponseProof = &referenceReceiptProof{s, d, a, operation}
	return v, nil
}
func (s *Store) DeleteHumanReferenceImage(ctx context.Context, d [32]byte, a identity.Actor, id string, in content.ReferenceImageDelete) (content.ReferenceImageReceipt, error) {
	var empty content.ReferenceImageReceipt
	if !identity.HumanControlUUID(id) || !in.Valid() {
		return empty, content.ErrInvalid
	}
	ctx, frame, e := s.humanWriteBound(ctx, d, a)
	if e != nil {
		return empty, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return empty, content.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	f := humanWriteFrame{tx: tx, d: d, a: a, session: frame}
	lockedFrame, _, lockErr := s.humanWriteSession(ctx, tx, d, a, true)
	if lockErr != nil {
		return empty, lockErr
	}
	if lockedFrame != frame {
		return empty, identity.ErrControlChanged
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,167))`, a.ID+":"+in.OperationID); e != nil {
		return empty, content.ErrUnavailable
	}
	hash := content.MomentControlHash([]any{"delete-reference-image-v1", id, in})
	old, oldHash, e := readReferenceOperation(ctx, tx, a.ID, in.OperationID)
	if e == nil {
		if oldHash != hash {
			return empty, identity.ErrControlChanged
		}
		if e = s.humanWriteFinish(ctx, f); e != nil {
			return empty, e
		}
		old.ResponseProof = &referenceReceiptProof{s, d, a, in.OperationID}
		return old, nil
	}
	if !errors.Is(e, content.ErrNotFound) {
		return empty, e
	}
	var material string
	e = tx.QueryRow(ctx, `SELECT l.material_id::text FROM private_reference_image_links l JOIN legacy_reference_media_relations r ON r.id=l.id WHERE l.id=$1 AND r.owner_account_id=$2 AND l.status='ACTIVE'`, id, a.ID).Scan(&material)
	if errors.Is(e, pgx.ErrNoRows) {
		return empty, content.ErrNotFound
	}
	if e != nil {
		return empty, content.ErrUnavailable
	}
	var locked string
	if tx.QueryRow(ctx, `SELECT id::text FROM private_reference_image_materials WHERE id=$1 AND owner_account_id=$2 AND status='READY_PRIVATE' FOR UPDATE`, material, a.ID).Scan(&locked) != nil {
		return empty, identity.ErrControlChanged
	}
	if tx.QueryRow(ctx, `SELECT id::text FROM private_reference_image_links WHERE id=$1 AND status='ACTIVE' FOR UPDATE`, id).Scan(&locked) != nil {
		return empty, identity.ErrControlChanged
	}
	epoch, e := referenceMutationEpoch(ctx, f, id)
	if e != nil {
		return empty, e
	}
	page, i, e := referenceItem(ctx, f, id)
	if e != nil {
		return empty, e
	}
	if i.Revision != in.ExpectedRevision || i.SourceVersion != in.ExpectedSourceVersion {
		return empty, identity.ErrControlChanged
	}
	if _, e = tx.Exec(ctx, `UPDATE private_reference_image_links SET status='DELETED',revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, id); e != nil {
		return empty, content.ErrUnavailable
	}
	// Retire only this binding. A reused Note image is neither overwritten nor
	// deleted, and shared new materials live until their last purpose is removed.
	if _, e = tx.Exec(ctx, `UPDATE private_reference_image_materials m SET status='DELETED',content=NULL,revision=revision+1,updated_at=clock_timestamp() WHERE m.id=$1 AND m.status='READY_PRIVATE' AND m.backing_image_id IS NULL AND NOT EXISTS(SELECT 1 FROM private_reference_image_links l WHERE l.material_id=m.id AND l.status='ACTIVE')`, material); e != nil {
		return empty, content.ErrUnavailable
	}
	receipt := content.ReferenceImageReceipt{SchemaVersion: "owned-reference-image-operation-v1", OperationID: in.OperationID, ImageID: id, Revision: in.ExpectedRevision + 1, Status: "COMMITTED", Effect: "DELETED"}
	if tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&receipt.CommittedAt) != nil {
		return empty, content.ErrUnavailable
	}
	receipt.CommittedAt = receipt.CommittedAt.UTC()
	raw, _ := json.Marshal(receipt)
	if _, e = tx.Exec(ctx, `INSERT INTO private_reference_image_operations(owner_account_id,operation_id,image_id,input_sha256,receipt)VALUES($1,$2,$3,$4,$5)`, a.ID, in.OperationID, id, hash, raw); e != nil {
		return empty, content.ErrUnavailable
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, page.ValidUntil, func() error {
		fresh, e := referenceMutationEpoch(ctx, f, id)
		if e != nil {
			return e
		}
		if fresh != epoch {
			return identity.ErrControlChanged
		}
		return nil
	})); e != nil {
		return empty, e
	}
	receipt.ResponseProof = &referenceReceiptProof{s, d, a, in.OperationID}
	return receipt, nil
}
