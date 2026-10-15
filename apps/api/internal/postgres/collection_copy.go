package postgres

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

var _ content.CollectionCopyStore = (*Store)(nil)
var _ content.CollectionCopySessionStore = (*Store)(nil)
var collectionCopySigning struct {
	sync.Once
	key [32]byte
	err error
}

func (s *Store) CaptureCollectionSession(ctx context.Context, d [32]byte, a identity.Actor) (content.CollectionSessionFrame, error) {
	var out content.CollectionSessionFrame
	if ctx == nil || a.AccountType != "person" || !content.ValidCollectionID(a.ID) || d == ([32]byte{}) {
		return out, identity.ErrUnauthorized
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, content.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	first, e := collectionSessionEpoch(ctx, tx, d, a.ID)
	if e != nil {
		return out, e
	}
	if e = checkHumanMomentSession(ctx, tx, d, a.ID, s.devPhoneEnabled); e != nil {
		return out, e
	}
	if e = s.lockHumanMomentSession(ctx, tx, d, a.ID); e != nil {
		return out, e
	}
	fresh, e := collectionSessionEpoch(ctx, tx, d, a.ID)
	if e != nil {
		return out, e
	}
	if first != fresh {
		return out, content.ErrConflict
	}
	if e = checkHumanMomentSession(ctx, tx, d, a.ID, s.devPhoneEnabled); e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, content.ErrUnavailable
	}
	return content.CollectionSessionFrame{ID: first.id, Generation: first.generation}, nil
}
func (s *Store) ValidateCollectionSession(ctx context.Context, d [32]byte, a identity.Actor, f content.CollectionSessionFrame) error {
	fresh, e := s.CaptureCollectionSession(ctx, d, a)
	if e != nil {
		return e
	}
	if f != fresh {
		return content.ErrConflict
	}
	return nil
}
func (s *Store) collectionBoundContext(ctx context.Context, d [32]byte, a identity.Actor) (context.Context, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return ctx, content.ErrUnavailable
	}
	f, valid, present := content.OriginalCollectionSession(ctx, d, a)
	if present {
		if !valid {
			return ctx, content.ErrConflict
		}
		if !content.ValidCollectionID(f.ID) || f.Generation < 1 {
			return ctx, content.ErrConflict
		}
		return ctx, nil
	}
	f, e := s.CaptureCollectionSession(ctx, d, a)
	if e != nil {
		return ctx, e
	}
	return content.WithCollectionSession(ctx, d, a, f), nil
}
func (s *Store) beginBoundCollectionTx(ctx context.Context, d [32]byte, a identity.Actor) (context.Context, pgx.Tx, error) {
	ctx, e := s.collectionBoundContext(ctx, d, a)
	if e != nil {
		return ctx, nil, e
	}
	tx, e := s.beginHumanMomentTx(ctx, d, a)
	return ctx, tx, e
}
func (s *Store) collectionCopyBegin(ctx context.Context, d [32]byte, a identity.Actor, op string) (context.Context, pgx.Tx, collectionSessionFence, error) {
	ctx, e := s.collectionBoundContext(ctx, d, a)
	if e != nil {
		return ctx, nil, collectionSessionFence{}, e
	}
	f, _, _ := content.OriginalCollectionSession(ctx, d, a)
	tx, e := s.beginHumanMomentTx(ctx, d, a)
	if e != nil {
		return ctx, nil, collectionSessionFence{}, e
	}
	fence := collectionSessionFence{f.ID, f.Generation}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,141))`, a.ID+":collection-copy:"+op); e != nil {
		_ = tx.Rollback(context.Background())
		return ctx, nil, fence, content.ErrUnavailable
	}
	return ctx, tx, fence, nil
}
func (s *Store) collectionCopyFinish(ctx context.Context, tx pgx.Tx, d [32]byte, a identity.Actor, f collectionSessionFence, gates ...func() error) error {
	if e := s.lockHumanMomentSession(ctx, tx, d, a.ID); e != nil {
		return e
	}
	current, e := collectionSessionEpoch(ctx, tx, d, a.ID)
	if e != nil {
		return e
	}
	if current != f {
		return content.ErrConflict
	}
	for _, gate := range gates {
		if e = gate(); e != nil {
			return e
		}
	}
	return s.finishHumanMomentTx(ctx, tx, d, a.ID)
}
func (s *Store) collectionCopySourceFinish(ctx context.Context, tx pgx.Tx, d [32]byte, a identity.Actor, f collectionSessionFence, r content.CollectionRef, in content.CollectionCopyInput, v content.PublicCollection, authority string, deadline time.Time) error {
	return s.collectionCopyFinish(ctx, tx, d, a, f, func() error {
		current, currentAuth, e := s.collectionCopySource(ctx, tx, d, a, r, in, false, &v.SourceExpiresAt)
		if e != nil {
			return e
		}
		if current.SourceBinding != v.SourceBinding || currentAuth != authority || !deadline.After(time.Now().UTC()) {
			return content.ErrConflict
		}
		return nil
	})
}

// A permanent origin contains no author text or coordinates. The retained
// source, consent, publication and Block/Unblock generations are fingerprinted
// independently of a transient reader's Session and observed lease.
func collectionCopyAuthoritySQL(kind, source, viewer string) string {
	table, entries, key, e := collectionTables(kind)
	if e != nil {
		return "NULL::text"
	}
	safety := strings.ReplaceAll(strings.ReplaceAll(mapPublicAuthorSafetyGenerationSQL, "$6", viewer), "a.id", "origin_author.id")
	retained := currentRetainedPlaceVisibilitySQL("origin_place", "clock_timestamp()")
	return `(SELECT encode(sha256(convert_to(jsonb_build_array(origin.xmin::text,origin_author.xmin::text,origin_city.xmin::text,copy_grant.xmin::text,` + safety + `,
 (SELECT jsonb_agg(jsonb_build_array(origin_entry.sequence_no,origin_entry.place_id,origin_entry.xmin::text,origin_place.xmin::text,` + retainedPlaceReadBindingSQL("origin_place") + `) ORDER BY origin_entry.sequence_no)
 FROM ` + entries + ` origin_entry JOIN places origin_place ON origin_place.id=origin_entry.place_id AND origin_place.city_id=origin_entry.city_id WHERE origin_entry.` + key + `=origin.id))::text,'UTF8')),'hex')
 FROM ` + table + ` origin JOIN accounts origin_author ON origin_author.id=origin.owner_account_id JOIN cities origin_city ON origin_city.id=origin.city_id
 JOIN personal_collection_copy_grants copy_grant ON copy_grant.kind='` + kind + `' AND copy_grant.target_id=origin.id AND copy_grant.owner_account_id=origin.owner_account_id AND copy_grant.publication_revision=origin.revision AND copy_grant.allow_private_place_copy
 WHERE origin.id=` + source + ` AND origin.status='published' AND origin.visibility='public' AND origin.owner_confirmed_at IS NOT NULL AND origin.published_at IS NOT NULL
 AND origin_author.account_type='person' AND origin_author.status='active' AND origin_city.publication_status='published' AND(origin_city.expires_at IS NULL OR origin_city.expires_at>clock_timestamp())
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE ` + viewer + `::uuid IS NOT NULL AND((b.blocker_account_id=` + viewer + `::uuid AND b.blocked_account_id=origin_author.id)OR(b.blocked_account_id=` + viewer + `::uuid AND b.blocker_account_id=origin_author.id)))
 AND (SELECT count(*) FROM ` + entries + ` WHERE ` + key + `=origin.id) BETWEEN 1 AND 50
 AND NOT EXISTS(SELECT 1 FROM ` + entries + ` origin_entry LEFT JOIN places origin_place ON origin_place.id=origin_entry.place_id AND origin_place.city_id=origin_entry.city_id
 WHERE origin_entry.` + key + `=origin.id AND(origin_place.id IS NULL OR origin_place.publication_status<>'published' OR(origin_place.expires_at IS NOT NULL AND origin_place.expires_at<=clock_timestamp()) OR NOT (` + retained + `))))`
}
func collectionCopyBorrowedSQL(kind, destination, place, viewer string) string {
	return `(NOT EXISTS(SELECT 1 FROM personal_collection_copy_borrowed_places borrowed WHERE borrowed.destination_kind='` + kind + `' AND borrowed.destination_id=` + destination + ` AND borrowed.place_id=` + place + `)
 OR EXISTS(SELECT 1 FROM personal_collection_copy_origins original WHERE original.destination_kind='` + kind + `' AND original.destination_id=` + destination + ` AND original.receiver_account_id=` + viewer + `::uuid
 AND original.origin_authority=` + collectionCopyAuthoritySQL(kind, "original.source_id", viewer) + `
 AND EXISTS(SELECT 1 FROM personal_collection_copy_grants original_grant WHERE original_grant.kind=original.source_kind AND original_grant.target_id=original.source_id AND original_grant.publication_revision=original.publication_revision AND original_grant.grant_version=original.grant_version AND original_grant.allow_private_place_copy)))`
}
func collectionCopyBorrowedDeadlineSQL(kind, destination, place string) string {
	table, entries, key, _ := collectionTables(kind)
	return `(SELECT min(least(origin_city.expires_at,origin_place.expires_at,` + currentRetainedPlaceDeadlineSQL("origin_place", "clock_timestamp()") + `)) FROM personal_collection_copy_origins original JOIN personal_collection_copy_borrowed_places borrowed ON borrowed.destination_kind=original.destination_kind AND borrowed.destination_id=original.destination_id JOIN ` + table + ` origin ON origin.id=original.source_id JOIN cities origin_city ON origin_city.id=origin.city_id JOIN ` + entries + ` origin_entry ON origin_entry.` + key + `=origin.id JOIN places origin_place ON origin_place.id=origin_entry.place_id AND origin_place.city_id=origin_entry.city_id WHERE original.destination_kind='` + kind + `' AND original.destination_id=` + destination + ` AND borrowed.place_id=` + place + `)`
}
func collectionCopyGrantSQL(kind, id, revision, until, items string) string {
	return `(SELECT CASE WHEN jsonb_array_length(eligible.ids)>0 THEN jsonb_build_object('sourceRef',jsonb_build_object('kind','` + kind + `','id',copy_grant.target_id),'publicationRevision',copy_grant.publication_revision,'grantVersion',copy_grant.grant_version,'eligiblePlaceIds',eligible.ids,'expiresAt',` + until + `) END
 FROM personal_collection_copy_grants copy_grant CROSS JOIN LATERAL(SELECT coalesce(jsonb_agg(item->'placeId' ORDER BY position),'[]'::jsonb) ids FROM jsonb_array_elements(` + items + `) WITH ORDINALITY x(item,position) WHERE item->>'locationVisibility'='place' AND item ? 'placeId')eligible
 WHERE copy_grant.kind='` + kind + `' AND copy_grant.target_id=` + id + ` AND copy_grant.publication_revision=` + revision + ` AND copy_grant.allow_private_place_copy)`
}
func decodeCollectionCopyGrant(raw []byte, v *content.PublicCollection) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var g content.CollectionCopyGrant
	if json.Unmarshal(raw, &g) != nil {
		return content.ErrUnavailable
	}
	g.ExpiresAt = g.ExpiresAt.UTC()
	g.OrderedPlaceIDsSHA256 = content.CollectionCopyPlacesDigest(g.EligiblePlaceIDs)
	v.CopyGrant = &g
	return nil
}
func collectionCopyPublicTx(ctx context.Context, tx pgx.Tx, s *Store, d [32]byte, a identity.Actor, r content.CollectionRef, until *time.Time) (content.PublicCollection, error) {
	var v content.PublicCollection
	var raw, grant []byte
	var token string
	e := tx.QueryRow(ctx, collectionPublicReadSQL(r.Kind), r.ID, a.ID, d[:], s.devPhoneEnabled, until).Scan(&v.ID, &v.CityID, &v.Title, &v.Summary, &v.Revision, &raw, &v.SourceExpiresAt, &token, &grant)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, content.ErrNotFound
	}
	if e != nil || json.Unmarshal(raw, &v.Entries) != nil {
		return v, content.ErrUnavailable
	}
	v.Kind = r.Kind
	v.SourceExpiresAt = v.SourceExpiresAt.UTC()
	h := sha256.Sum256([]byte(token))
	v.SourceBinding = hex.EncodeToString(h[:])
	if e = decodeCollectionCopyGrant(grant, &v); e != nil {
		return v, e
	}
	return v, content.ValidatePublicCollection(v, r.Kind, r.ID)
}
func (s *Store) collectionCopySource(ctx context.Context, tx pgx.Tx, d [32]byte, a identity.Actor, r content.CollectionRef, in content.CollectionCopyInput, lock bool, until *time.Time) (content.PublicCollection, string, error) {
	table, _, _, _ := collectionTables(r.Kind)
	if lock {
		var id string
		if e := tx.QueryRow(ctx, `SELECT id FROM `+table+` WHERE id=$1 FOR SHARE`, r.ID).Scan(&id); e != nil {
			return content.PublicCollection{}, "", content.ErrNotFound
		}
		if e := tx.QueryRow(ctx, `SELECT target_id FROM personal_collection_copy_grants WHERE kind=$1 AND target_id=$2 FOR SHARE`, r.Kind, r.ID).Scan(&id); e != nil {
			return content.PublicCollection{}, "", content.ErrConflict
		}
	}
	v, e := collectionCopyPublicTx(ctx, tx, s, d, a, r, until)
	if e != nil {
		return v, "", e
	}
	g := v.CopyGrant
	if g == nil || v.Revision != in.PublicationRevision || g.GrantVersion != in.GrantVersion || !content.CollectionCopySubsequence(in.OrderedPlaceIDs, g.EligiblePlaceIDs) {
		return v, "", content.ErrConflict
	}
	places := content.CollectionInput{Kind: r.Kind, CityID: v.CityID, Entries: []content.CollectionEntry{}}
	for _, id := range in.OrderedPlaceIDs {
		places.Entries = append(places.Entries, content.CollectionEntry{PlaceID: id, LocationVisibility: "hidden"})
	}
	if lock {
		if e = collectionSourcesTx(ctx, tx, places); e != nil {
			return v, "", e
		}
	}
	var authority *string
	e = tx.QueryRow(ctx, `SELECT `+collectionCopyAuthoritySQL(r.Kind, "$1::uuid", "$2"), r.ID, a.ID).Scan(&authority)
	if e != nil {
		return v, "", content.ErrUnavailable
	}
	if authority == nil {
		return v, "", content.ErrConflict
	}
	return v, *authority, nil
}
func collectionCopyMaterial(a identity.Actor, f collectionSessionFence, r content.CollectionRef, in content.CollectionCopyInput, v content.PublicCollection, authority string) string {
	b, _ := json.Marshal([]any{"private-copy-approval-v1", a.ID, f.id, f.generation, content.CollectionCopyDigest(r, in), v.SourceBinding, authority})
	return string(b)
}
func collectionCopyToken(material string, issued, end int64) (string, error) {
	collectionCopySigning.Do(func() { _, collectionCopySigning.err = rand.Read(collectionCopySigning.key[:]) })
	if collectionCopySigning.err != nil {
		return "", content.ErrUnavailable
	}
	h := hmac.New(sha256.New, collectionCopySigning.key[:])
	fmt.Fprintf(h, "%s:%d:%d", material, issued, end)
	return "cc1." + strconv.FormatInt(issued, 10) + "." + strconv.FormatInt(end, 10) + "." + hex.EncodeToString(h.Sum(nil)), nil
}
func collectionCopyTokenDeadline(material, token string, now time.Time) (time.Time, bool) {
	p := strings.Split(token, ".")
	if len(p) != 4 || p[0] != "cc1" {
		return time.Time{}, false
	}
	issued, e := strconv.ParseInt(p[1], 10, 64)
	if e != nil {
		return time.Time{}, false
	}
	end, e := strconv.ParseInt(p[2], 10, 64)
	if e != nil || issued > now.UnixNano() || end <= now.UnixNano() || end-issued <= 0 || end-issued > int64(30*time.Second) {
		return time.Time{}, false
	}
	expected, e := collectionCopyToken(material, issued, end)
	return time.Unix(0, end).UTC(), e == nil && hmac.Equal([]byte(token), []byte(expected))
}

type collectionCopyPreviewProof struct {
	s                 *Store
	d                 [32]byte
	a                 identity.Actor
	r                 content.CollectionRef
	in                content.CollectionCopyInput
	out               content.CollectionCopyPreview
	frame             collectionSessionFence
	source, authority string
	until             time.Time
}

func (p *collectionCopyPreviewProof) RevalidateCollection(ctx context.Context) error {
	ctx = content.WithCollectionSession(ctx, p.d, p.a, content.CollectionSessionFrame{ID: p.frame.id, Generation: p.frame.generation})
	ctx, tx, f, e := p.s.collectionCopyBegin(ctx, p.d, p.a, p.in.OperationID)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	v, authority, e := p.s.collectionCopySource(ctx, tx, p.d, p.a, p.r, p.in, false, &p.until)
	if e != nil {
		return e
	}
	if v.SourceBinding != p.source || authority != p.authority || !p.out.ExpiresAt.After(time.Now().UTC()) {
		return content.ErrConflict
	}
	return p.s.collectionCopySourceFinish(ctx, tx, p.d, p.a, f, p.r, p.in, v, authority, p.out.ExpiresAt)
}
func (s *Store) PreviewHumanCollectionCopy(ctx context.Context, d [32]byte, a identity.Actor, r content.CollectionRef, in content.CollectionCopyInput) (content.CollectionCopyPreview, error) {
	in, e := content.NormalizeCollectionCopy(r, in, false)
	if e != nil {
		return content.CollectionCopyPreview{}, e
	}
	ctx, tx, f, e := s.collectionCopyBegin(ctx, d, a, in.OperationID)
	if e != nil {
		return content.CollectionCopyPreview{}, e
	}
	defer tx.Rollback(context.Background())
	if _, _, e = collectionCopyReceiptTx(ctx, tx, a.ID, in.OperationID); e == nil {
		return content.CollectionCopyPreview{}, content.ErrConflict
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return content.CollectionCopyPreview{}, e
	}
	v, authority, e := s.collectionCopySource(ctx, tx, d, a, r, in, true, nil)
	if e != nil {
		return content.CollectionCopyPreview{}, e
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return content.CollectionCopyPreview{}, content.ErrUnavailable
	}
	end := now.Add(30 * time.Second)
	if v.SourceExpiresAt.Before(end) {
		end = v.SourceExpiresAt
	}
	token, e := collectionCopyToken(collectionCopyMaterial(a, f, r, in, v, authority), now.UnixNano(), end.UnixNano())
	if e != nil {
		return content.CollectionCopyPreview{}, e
	}
	out := content.CollectionCopyPreview{SourceRef: r, OperationID: in.OperationID, PublicationRevision: in.PublicationRevision, GrantVersion: in.GrantVersion, OrderedPlaceIDsSHA256: in.OrderedPlaceIDsSHA256, EntryCount: len(in.OrderedPlaceIDs), Title: in.Title, InputSHA256: content.CollectionCopyDigest(r, in), Approval: token, ExpiresAt: end.UTC()}
	next, nextAuth, e := s.collectionCopySource(ctx, tx, d, a, r, in, false, &v.SourceExpiresAt)
	if e != nil {
		return out, e
	}
	if next.SourceBinding != v.SourceBinding || nextAuth != authority {
		return out, content.ErrConflict
	}
	if e = s.collectionCopySourceFinish(ctx, tx, d, a, f, r, in, v, authority, end); e != nil {
		return content.CollectionCopyPreview{}, e
	}
	out.ResponseProof = &collectionCopyPreviewProof{s, d, a, r, in, out, f, v.SourceBinding, authority, v.SourceExpiresAt}
	return out, nil
}
func collectionCopyReceiptTx(ctx context.Context, tx pgx.Tx, actor, op string) (content.CollectionCopyReceipt, []byte, error) {
	out := content.CollectionCopyReceipt{OperationID: op}
	var dk, di, sk, si *string
	var pr, gv *int64
	var ids, digest []byte
	e := tx.QueryRow(ctx, `SELECT status,result_revision,destination_kind,destination_id,source_kind,source_id,publication_revision,grant_version,ordered_place_ids_sha256,input_sha256 FROM personal_collection_copy_operations WHERE receiver_account_id=$1 AND operation_id=$2`, actor, op).Scan(&out.Status, &out.ResultRevision, &dk, &di, &sk, &si, &pr, &gv, &ids, &digest)
	if e != nil {
		return out, nil, e
	}
	if out.Status == "STOPPED" {
		return out, nil, nil
	}
	if dk == nil || di == nil || sk == nil || si == nil || pr == nil || gv == nil || len(ids) != 32 || len(digest) != 32 || out.Status != "SUCCEEDED" || out.ResultRevision != 1 {
		return out, nil, content.ErrUnavailable
	}
	out.DestinationRef = &content.CollectionRef{*dk, *di}
	out.SourceRef = &content.CollectionRef{*sk, *si}
	out.PublicationRevision = *pr
	out.GrantVersion = *gv
	out.OrderedPlaceIDsSHA256 = hex.EncodeToString(ids)
	return out, digest, nil
}
func (s *Store) CopyHumanCollection(ctx context.Context, d [32]byte, a identity.Actor, r content.CollectionRef, in content.CollectionCopyInput) (content.CollectionCopyReceipt, error) {
	in, e := content.NormalizeCollectionCopy(r, in, true)
	if e != nil {
		return content.CollectionCopyReceipt{}, e
	}
	ctx, tx, f, e := s.collectionCopyBegin(ctx, d, a, in.OperationID)
	if e != nil {
		return content.CollectionCopyReceipt{}, e
	}
	defer tx.Rollback(context.Background())
	out, previous, e := collectionCopyReceiptTx(ctx, tx, a.ID, in.OperationID)
	sum, _ := hex.DecodeString(content.CollectionCopyDigest(r, in))
	if e == nil {
		if out.Status == "SUCCEEDED" && (!bytes.Equal(previous, sum) || out.SourceRef == nil || *out.SourceRef != r) {
			return content.CollectionCopyReceipt{}, content.ErrConflict
		}
		if e = s.collectionCopyFinish(ctx, tx, d, a, f); e != nil {
			return content.CollectionCopyReceipt{}, e
		}
		return out, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, content.ErrUnavailable
	}
	v, authority, e := s.collectionCopySource(ctx, tx, d, a, r, in, true, nil)
	if e != nil {
		return out, e
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return out, content.ErrUnavailable
	}
	deadline, valid := collectionCopyTokenDeadline(collectionCopyMaterial(a, f, r, in, v, authority), in.Approval, now)
	if !valid || deadline.After(v.SourceExpiresAt) {
		return out, content.ErrConflict
	}
	table, _, _, _ := collectionTables(r.Kind)
	var id string
	e = tx.QueryRow(ctx, `INSERT INTO `+table+`(owner_account_id,city_id,title,summary,visibility,status,revision) VALUES($1,$2,$3,'','private','draft',1) RETURNING id`, a.ID, v.CityID, in.Title).Scan(&id)
	if e != nil {
		return out, content.ErrUnavailable
	}
	if id == in.OperationID || id == r.ID {
		return out, content.ErrUnavailable
	}
	dest := content.CollectionInput{Kind: r.Kind, CityID: v.CityID, Title: in.Title, Summary: "", Entries: []content.CollectionEntry{}}
	for _, place := range in.OrderedPlaceIDs {
		dest.Entries = append(dest.Entries, content.CollectionEntry{PlaceID: place, LocationVisibility: "hidden"})
	}
	if e = collectionEntriesTx(ctx, tx, id, dest); e != nil {
		return out, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO personal_collection_copy_origins(destination_kind,destination_id,receiver_account_id,source_kind,source_id,publication_revision,grant_version,origin_authority)VALUES($1,$2,$3,$1,$4,$5,$6,$7)`, r.Kind, id, a.ID, r.ID, in.PublicationRevision, in.GrantVersion, authority)
	if e != nil {
		return out, content.ErrUnavailable
	}
	for _, place := range in.OrderedPlaceIDs {
		if _, e = tx.Exec(ctx, `INSERT INTO personal_collection_copy_borrowed_places(destination_kind,destination_id,place_id)VALUES($1,$2,$3)`, r.Kind, id, place); e != nil {
			return out, content.ErrUnavailable
		}
	}
	idsSum, _ := hex.DecodeString(in.OrderedPlaceIDsSHA256)
	_, e = tx.Exec(ctx, `INSERT INTO personal_collection_copy_operations(receiver_account_id,operation_id,status,destination_kind,destination_id,source_kind,source_id,publication_revision,grant_version,ordered_place_ids_sha256,input_sha256,result_revision)VALUES($1,$2,'SUCCEEDED',$3,$4,$3,$5,$6,$7,$8,$9,1)`, a.ID, in.OperationID, r.Kind, id, r.ID, in.PublicationRevision, in.GrantVersion, idsSum, sum)
	if e != nil {
		return out, content.ErrUnavailable
	}
	if e = collectionAuditTx(ctx, tx, a.ID, r.Kind, id, "copy_public_places"); e != nil {
		return out, e
	}
	next, nextAuth, e := s.collectionCopySource(ctx, tx, d, a, r, in, false, &v.SourceExpiresAt)
	if e != nil {
		return out, e
	}
	if next.SourceBinding != v.SourceBinding || nextAuth != authority || !deadline.After(time.Now().UTC()) {
		return out, content.ErrConflict
	}
	out, _, e = collectionCopyReceiptTx(ctx, tx, a.ID, in.OperationID)
	if e != nil {
		return out, content.ErrUnavailable
	}
	if e = s.collectionCopySourceFinish(ctx, tx, d, a, f, r, in, v, authority, deadline); e != nil {
		return content.CollectionCopyReceipt{}, e
	}
	return out, nil
}
func (s *Store) collectionCopyHistory(ctx context.Context, d [32]byte, a identity.Actor, op string, stop bool) (content.CollectionCopyReceipt, error) {
	if !content.ValidCollectionID(op) || op != strings.ToLower(op) {
		return content.CollectionCopyReceipt{}, content.ErrInvalid
	}
	ctx, tx, f, e := s.collectionCopyBegin(ctx, d, a, op)
	if e != nil {
		return content.CollectionCopyReceipt{}, e
	}
	defer tx.Rollback(context.Background())
	out, _, e := collectionCopyReceiptTx(ctx, tx, a.ID, op)
	if errors.Is(e, pgx.ErrNoRows) {
		if !stop {
			return out, content.ErrNotFound
		}
		if _, e = tx.Exec(ctx, `INSERT INTO personal_collection_copy_operations(receiver_account_id,operation_id,status,result_revision)VALUES($1,$2,'STOPPED',0)`, a.ID, op); e != nil {
			return out, content.ErrUnavailable
		}
		out = content.CollectionCopyReceipt{OperationID: op, Status: "STOPPED"}
	}
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return out, content.ErrUnavailable
	}
	if e = s.collectionCopyFinish(ctx, tx, d, a, f); e != nil {
		return content.CollectionCopyReceipt{}, e
	}
	return out, nil
}
func (s *Store) GetHumanCollectionCopyOperation(ctx context.Context, d [32]byte, a identity.Actor, op string) (content.CollectionCopyReceipt, error) {
	return s.collectionCopyHistory(ctx, d, a, op, false)
}
func (s *Store) StopHumanCollectionCopyOperation(ctx context.Context, d [32]byte, a identity.Actor, op string) (content.CollectionCopyReceipt, error) {
	return s.collectionCopyHistory(ctx, d, a, op, true)
}

type collectionPrivateCopyProof struct {
	s          *Store
	d          [32]byte
	a          identity.Actor
	frame      content.CollectionSessionFrame
	collection content.PersonalCollection
	wire       []byte
}

func collectionPrivateCopyWire(v content.PersonalCollection) []byte {
	v.ResponseProof = nil
	b, _ := json.Marshal(v)
	return b
}
func (p *collectionPrivateCopyProof) RevalidateCollection(ctx context.Context) error {
	ctx = content.WithCollectionSession(ctx, p.d, p.a, p.frame)
	tx, e := p.s.beginHumanMomentTx(ctx, p.d, p.a)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	if _, e = collectionReadTx(ctx, tx, p.a.ID, p.collection.Kind, p.collection.ID, true); e != nil {
		return e
	}
	if e = p.s.lockHumanMomentSession(ctx, tx, p.d, p.a.ID); e != nil {
		return e
	}
	fresh, e := collectionReadTx(ctx, tx, p.a.ID, p.collection.Kind, p.collection.ID, false)
	if e != nil {
		return e
	}
	fresh.PublicationOperation = p.collection.PublicationOperation
	if len(fresh.Entries) != len(p.collection.Entries) {
		return content.ErrConflict
	}
	for i := range fresh.Entries {
		if old := p.collection.Entries[i].LegacyReference; old != nil {
			next := fresh.Entries[i].LegacyReference
			if next == nil || next.Reference != old.Reference || next.SourceVersion != old.SourceVersion || !old.Valid(time.Now().UTC()) || next.ValidUntil.Before(old.ValidUntil) {
				return content.ErrConflict
			}
			fixed := *old
			fresh.Entries[i].LegacyReference = &fixed
		}
		old := p.collection.Entries[i].SourceExpiresAt
		next := fresh.Entries[i].SourceExpiresAt
		if old != nil {
			if next == nil || !old.After(time.Now().UTC()) || next.Before(*old) {
				return content.ErrConflict
			}
			fixed := *old
			fresh.Entries[i].SourceExpiresAt = &fixed
		}
	}
	if !bytes.Equal(p.wire, collectionPrivateCopyWire(fresh)) {
		return content.ErrConflict
	}
	return p.s.collectionCommit(ctx, tx, p.d, p.a.ID)
}
func (s *Store) attachCollectionCopyReadProof(ctx context.Context, d [32]byte, a identity.Actor, v *content.PersonalCollection) {
	f, valid, _ := content.OriginalCollectionSession(ctx, d, a)
	if valid {
		frozen := *v
		frozen.Entries = append(v.Entries[:0:0], v.Entries...)
		if v.JourneyDetails != nil {
			raw, e := json.Marshal(v.JourneyDetails)
			if e != nil {
				return
			}
			var copy content.JourneyDetails
			if json.Unmarshal(raw, &copy) != nil {
				return
			}
			frozen.JourneyDetails = &copy
		}
		v.ResponseProof = &collectionPrivateCopyProof{s, d, a, f, frozen, collectionPrivateCopyWire(frozen)}
	}
}
