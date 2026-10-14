package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

var _ content.CollectionPublicationStore = (*Store)(nil)

type collectionSessionFence struct {
	id         string
	generation int64
}

func collectionSessionEpoch(ctx context.Context, tx pgx.Tx, d [32]byte, owner string) (collectionSessionFence, error) {
	var f collectionSessionFence
	e := tx.QueryRow(ctx, `SELECT id,authorization_generation FROM sessions WHERE account_id=$1 AND token_sha256=$2 AND revoked_at IS NULL`, owner, d[:]).Scan(&f.id, &f.generation)
	if errors.Is(e, pgx.ErrNoRows) {
		return f, identity.ErrUnauthorized
	}
	if e != nil {
		return f, content.ErrUnavailable
	}
	return f, nil
}
func (s *Store) collectionPublicationFinish(ctx context.Context, tx pgx.Tx, d [32]byte, owner string, f collectionSessionFence, source *content.CollectionInput) error {
	if e := s.lockHumanMomentSession(ctx, tx, d, owner); e != nil {
		return e
	}
	live, e := collectionSessionEpoch(ctx, tx, d, owner)
	if e != nil {
		return e
	}
	if live != f {
		return identity.ErrUnauthorized
	}
	if original, valid, present := content.OriginalCollectionSession(ctx, d, identity.Actor{ID: owner, AccountType: "person"}); present && (!valid || original.ID != live.id || original.Generation != live.generation) {
		return content.ErrConflict
	}
	// All resource/receipt waits precede this last source and Session gate.
	if source != nil {
		if e = collectionPublicationSourcesCurrent(ctx, tx, *source); e != nil {
			return e
		}
	}
	return s.finishHumanMomentTx(ctx, tx, d, owner)
}
func collectionPublicationSourcesCurrent(ctx context.Context, tx pgx.Tx, in content.CollectionInput) error {
	ids := make([]string, 0, len(in.Entries))
	for _, e := range in.Entries {
		ids = append(ids, e.PlaceID)
	}
	var eligible bool
	e := tx.QueryRow(ctx, `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at)
 SELECT EXISTS(SELECT 1 FROM cities c CROSS JOIN cl WHERE c.id=$1 AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>cl.at))
 AND (SELECT count(*) FROM places p CROSS JOIN cl WHERE p.id=ANY($2::uuid[]) AND p.city_id=$1 AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>cl.at) AND `+currentRetainedPlaceVisibilitySQL("p", "cl.at")+`)=$3`, in.CityID, ids, len(ids)).Scan(&eligible)
	if e != nil {
		return content.ErrUnavailable
	}
	if !eligible {
		return content.ErrConflict
	}
	return nil
}
func readCollectionPublicationOperation(ctx context.Context, tx pgx.Tx, owner, op string) (kind, id string, meta content.CollectionPublicationOperation, digest []byte, err error) {
	meta.OperationID = op
	var result *int64
	err = tx.QueryRow(ctx, `SELECT kind,target_id,action,outcome,expected_revision,result_revision,input_sha256 FROM personal_collection_publication_operations WHERE owner_account_id=$1 AND operation_id=$2`, owner, op).Scan(&kind, &id, &meta.Action, &meta.Outcome, &meta.ExpectedRevision, &result, &digest)
	if err == nil {
		switch meta.Outcome {
		case "COMMITTED":
			if result == nil || *result != meta.ExpectedRevision+1 || len(digest) != 32 {
				err = content.ErrUnavailable
			} else {
				meta.ResultRevision = *result
			}
		case "NO_EFFECT":
			if result != nil || digest != nil {
				err = content.ErrUnavailable
			}
		default:
			err = content.ErrUnavailable
		}
	}
	return
}
func (s *Store) PublishHumanCollection(ctx context.Context, d [32]byte, a identity.Actor, kind, id string, in content.CollectionPublicationInput) (content.PersonalCollection, error) {
	in, e := content.NormalizeCollectionPublication(in)
	if e != nil {
		return content.PersonalCollection{}, e
	}
	return s.mutateCollectionPublication(ctx, d, a, kind, id, in.OperationID, in.Revision, "PUBLISH", in.Entries, in.AllowPrivatePlaceCopy)
}
func (s *Store) UnpublishHumanCollection(ctx context.Context, d [32]byte, a identity.Actor, kind, id string, in content.CollectionUnpublishInput) (content.PersonalCollection, error) {
	if !content.ValidCollectionID(in.OperationID) || in.Revision < 1 || in.Revision >= math.MaxInt64 {
		return content.PersonalCollection{}, content.ErrInvalid
	}
	return s.mutateCollectionPublication(ctx, d, a, kind, id, strings.ToLower(in.OperationID), in.Revision, "UNPUBLISH", nil, false)
}
func (s *Store) mutateCollectionPublication(ctx context.Context, d [32]byte, a identity.Actor, kind, id, op string, revision int64, action string, entries []content.CollectionPublicationEntry, allowCopy bool) (content.PersonalCollection, error) {
	table, entryTable, key, e := collectionTables(kind)
	if e != nil || !content.ValidCollectionID(id) {
		return content.PersonalCollection{}, content.ErrInvalid
	}
	id = strings.ToLower(id)
	ctx, tx, e := s.beginBoundCollectionTx(ctx, d, a)
	if e != nil {
		return content.PersonalCollection{}, e
	}
	defer tx.Rollback(context.Background())
	fence, e := collectionSessionEpoch(ctx, tx, d, a.ID)
	if e != nil {
		return content.PersonalCollection{}, e
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, a.ID+":collection-publication:"+op); e != nil {
		return content.PersonalCollection{}, content.ErrUnavailable
	}
	digest := content.CollectionPublicationCopyDigest(kind, id, action, revision, entries, allowCopy)
	oldKind, oldID, meta, oldDigest, e := readCollectionPublicationOperation(ctx, tx, a.ID, op)
	if e == nil {
		// A terminal NO_EFFECT reservation forbids all late payloads for this
		// operation ID, including the originally approved payload.
		if oldKind != kind || oldID != id || meta.Action != action || meta.ExpectedRevision != revision || meta.Outcome != "COMMITTED" || !bytes.Equal(oldDigest, digest[:]) {
			return content.PersonalCollection{}, content.ErrConflict
		}
		v, e := collectionReadTx(ctx, tx, a.ID, kind, id, true)
		if e != nil {
			return v, e
		}
		if v.Revision < meta.ResultRevision {
			return content.PersonalCollection{}, content.ErrUnavailable
		}
		v.PublicationOperation = &meta
		if e = s.collectionPublicationFinish(ctx, tx, d, a.ID, fence, nil); e != nil {
			return content.PersonalCollection{}, e
		}
		s.attachCollectionCopyReadProof(ctx, d, a, &v)
		return v, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return content.PersonalCollection{}, content.ErrUnavailable
	}
	var locked string
	e = tx.QueryRow(ctx, `SELECT id FROM `+table+` WHERE id=$1 AND owner_account_id=$2 FOR UPDATE`, id, a.ID).Scan(&locked)
	if errors.Is(e, pgx.ErrNoRows) {
		return content.PersonalCollection{}, content.ErrNotFound
	}
	if e != nil {
		return content.PersonalCollection{}, content.ErrUnavailable
	}
	v, e := collectionReadTx(ctx, tx, a.ID, kind, id, false)
	if e != nil {
		return v, e
	}
	if v.Revision != revision {
		return content.PersonalCollection{}, content.ErrConflict
	}
	var source *content.CollectionInput
	if action == "PUBLISH" {
		if v.Status != "draft" || v.Visibility != "private" || len(entries) != len(v.Entries) {
			return content.PersonalCollection{}, content.ErrConflict
		}
		// The author's grant permits a receiver's private draft only. Public
		// redistribution requires an independent canonical selection.
		var borrowed bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM personal_collection_copy_borrowed_places WHERE destination_kind=$1 AND destination_id=$2)`, kind, id).Scan(&borrowed); e != nil {
			return content.PersonalCollection{}, content.ErrUnavailable
		}
		if borrowed {
			return content.PersonalCollection{}, content.ErrConflict
		}
		for i, entry := range entries {
			if entry.PlaceID != v.Entries[i].PlaceID {
				return content.PersonalCollection{}, content.ErrConflict
			}
		}
		in := v.CollectionInput
		source = &in
		if e = collectionSourcesTx(ctx, tx, in); e != nil {
			return content.PersonalCollection{}, e
		}
		if e = collectionPublicationSourcesCurrent(ctx, tx, in); e != nil {
			return content.PersonalCollection{}, e
		}
		for i, entry := range entries {
			tag, e := tx.Exec(ctx, `UPDATE `+entryTable+` SET location_visibility=$3 WHERE `+key+`=$1 AND sequence_no=$2 AND place_id=$4`, id, i+1, entry.LocationVisibility, entry.PlaceID)
			if e != nil || tag.RowsAffected() != 1 {
				return content.PersonalCollection{}, content.ErrUnavailable
			}
		}
		_, e = tx.Exec(ctx, `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE `+table+` SET visibility='public',status='published',revision=revision+1,owner_confirmed_at=cl.at,published_at=cl.at,updated_at=cl.at FROM cl WHERE id=$1`, id)
	} else {
		if v.Status != "published" || v.Visibility != "public" {
			return content.PersonalCollection{}, content.ErrConflict
		}
		// Retraction is always available to the current owner even after source expiry.
		_, e = tx.Exec(ctx, `UPDATE `+table+` SET visibility='private',status='draft',revision=revision+1,owner_confirmed_at=NULL,published_at=NULL,updated_at=clock_timestamp() WHERE id=$1`, id)
	}
	if e != nil {
		return content.PersonalCollection{}, content.ErrUnavailable
	}
	{
		_, e = tx.Exec(ctx, `INSERT INTO personal_collection_copy_grants(kind,target_id,owner_account_id,publication_revision,grant_version,allow_private_place_copy) SELECT $1,$2,$3,$4,1,$5 WHERE $5::boolean OR EXISTS(SELECT 1 FROM personal_collection_copy_grants WHERE kind=$1 AND target_id=$2) ON CONFLICT(kind,target_id) DO UPDATE SET publication_revision=EXCLUDED.publication_revision,grant_version=least(personal_collection_copy_grants.grant_version+1,8999999999999999),allow_private_place_copy=EXCLUDED.allow_private_place_copy,updated_at=clock_timestamp()`, kind, id, a.ID, revision+1, allowCopy)
		if e != nil {
			return content.PersonalCollection{}, content.ErrUnavailable
		}
	}
	var journey, theme any
	if kind == "journey" {
		journey = id
	} else {
		theme = id
	}
	_, e = tx.Exec(ctx, `INSERT INTO personal_collection_publication_operations(owner_account_id,operation_id,kind,target_id,journey_id,theme_map_id,action,expected_revision,result_revision,input_sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8::bigint,$8::bigint+1::bigint,$9)`, a.ID, op, kind, id, journey, theme, action, revision, digest[:])
	if e != nil {
		return content.PersonalCollection{}, content.ErrUnavailable
	}
	if e = collectionAuditTx(ctx, tx, a.ID, kind, id, strings.ToLower(action)); e != nil {
		return content.PersonalCollection{}, e
	}
	v, e = collectionReadTx(ctx, tx, a.ID, kind, id, false)
	if e != nil {
		return v, e
	}
	v.PublicationOperation = &content.CollectionPublicationOperation{OperationID: op, Action: action, Outcome: "COMMITTED", ExpectedRevision: revision, ResultRevision: revision + 1}
	if e = s.collectionPublicationFinish(ctx, tx, d, a.ID, fence, source); e != nil {
		return content.PersonalCollection{}, e
	}
	s.attachCollectionCopyReadProof(ctx, d, a, &v)
	return v, nil
}
func (s *Store) GetHumanCollectionPublicationOperation(ctx context.Context, d [32]byte, a identity.Actor, op string) (content.PersonalCollection, error) {
	if !content.ValidCollectionID(op) {
		return content.PersonalCollection{}, content.ErrInvalid
	}
	op = strings.ToLower(op)
	ctx, tx, e := s.beginBoundCollectionTx(ctx, d, a)
	if e != nil {
		return content.PersonalCollection{}, e
	}
	defer tx.Rollback(context.Background())
	fence, e := collectionSessionEpoch(ctx, tx, d, a.ID)
	if e != nil {
		return content.PersonalCollection{}, e
	}
	kind, id, meta, _, e := readCollectionPublicationOperation(ctx, tx, a.ID, op)
	if errors.Is(e, pgx.ErrNoRows) {
		return content.PersonalCollection{}, content.ErrNotFound
	}
	if e != nil {
		return content.PersonalCollection{}, content.ErrUnavailable
	}
	v, e := collectionReadTx(ctx, tx, a.ID, kind, id, true)
	if e != nil {
		return v, e
	}
	if v.Revision < meta.ResultRevision {
		return content.PersonalCollection{}, content.ErrUnavailable
	}
	v.PublicationOperation = &meta
	if e = s.collectionPublicationFinish(ctx, tx, d, a.ID, fence, nil); e != nil {
		return content.PersonalCollection{}, e
	}
	s.attachCollectionCopyReadProof(ctx, d, a, &v)
	return v, nil
}

// Close is an explicit human action, serialized with the original publication.
// An absent GET receipt alone never proves that no remote effect can arrive.
func (s *Store) CloseHumanCollectionPublicationOperation(ctx context.Context, d [32]byte, a identity.Actor, op string, in content.CollectionPublicationCloseInput) (content.PersonalCollection, error) {
	if !content.ValidCollectionID(op) {
		return content.PersonalCollection{}, content.ErrInvalid
	}
	in, e := content.NormalizeCollectionPublicationClose(in)
	if e != nil {
		return content.PersonalCollection{}, e
	}
	op = strings.ToLower(op)
	table, _, _, _ := collectionTables(in.Kind)
	ctx, tx, e := s.beginBoundCollectionTx(ctx, d, a)
	if e != nil {
		return content.PersonalCollection{}, e
	}
	defer tx.Rollback(context.Background())
	fence, e := collectionSessionEpoch(ctx, tx, d, a.ID)
	if e != nil {
		return content.PersonalCollection{}, e
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, a.ID+":collection-publication:"+op); e != nil {
		return content.PersonalCollection{}, content.ErrUnavailable
	}
	kind, id, meta, _, e := readCollectionPublicationOperation(ctx, tx, a.ID, op)
	newReceipt := errors.Is(e, pgx.ErrNoRows)
	if e != nil && !newReceipt {
		return content.PersonalCollection{}, content.ErrUnavailable
	}
	if !newReceipt && (kind != in.Kind || id != in.ID || meta.Action != in.Action || meta.ExpectedRevision != in.Revision) {
		return content.PersonalCollection{}, content.ErrConflict
	}
	// The original entity is locked even for recovery. Never fabricate an
	// entity for a physically missing target or replace an existing receipt.
	var locked string
	e = tx.QueryRow(ctx, `SELECT id FROM `+table+` WHERE id=$1 AND owner_account_id=$2 FOR UPDATE`, in.ID, a.ID).Scan(&locked)
	if errors.Is(e, pgx.ErrNoRows) {
		return content.PersonalCollection{}, content.ErrNotFound
	}
	if e != nil {
		return content.PersonalCollection{}, content.ErrUnavailable
	}
	v, e := collectionReadTx(ctx, tx, a.ID, in.Kind, in.ID, false)
	if e != nil {
		return v, e
	}
	if newReceipt {
		var journey, theme any
		if in.Kind == "journey" {
			journey = in.ID
		} else {
			theme = in.ID
		}
		// Expected revision is the original operation selector, not a grant to
		// mutate today's entity. NO_EFFECT stores neither body nor approval hash.
		_, e = tx.Exec(ctx, `INSERT INTO personal_collection_publication_operations(owner_account_id,operation_id,kind,target_id,journey_id,theme_map_id,action,expected_revision,outcome,result_revision,input_sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8::bigint,'NO_EFFECT',NULL,NULL)`, a.ID, op, in.Kind, in.ID, journey, theme, in.Action, in.Revision)
		if e != nil {
			return content.PersonalCollection{}, content.ErrUnavailable
		}
		if e = collectionAuditTx(ctx, tx, a.ID, in.Kind, in.ID, "close_publication_operation"); e != nil {
			return content.PersonalCollection{}, e
		}
		meta = content.CollectionPublicationOperation{OperationID: op, Action: in.Action, Outcome: "NO_EFFECT", ExpectedRevision: in.Revision}
	} else if meta.Outcome == "COMMITTED" && v.Revision < meta.ResultRevision {
		return content.PersonalCollection{}, content.ErrUnavailable
	}
	v.PublicationOperation = &meta
	// Withdrawn/expired Place or City sources cannot authorize publishing but
	// do not block ending uncertainty. Owner and original Session still fence.
	if e = s.collectionPublicationFinish(ctx, tx, d, a.ID, fence, nil); e != nil {
		return content.PersonalCollection{}, e
	}
	s.attachCollectionCopyReadProof(ctx, d, a, &v)
	return v, nil
}

type collectionPublicProof struct {
	store  *Store
	access content.PublicMomentAccess
	source string
	wire   [32]byte
	until  time.Time
}

func collectionPublicWire(v content.PublicCollection) ([32]byte, error) {
	v.ResponseProof = nil
	v.SourceBinding = ""
	b, e := json.Marshal(v)
	return sha256.Sum256(b), e
}
func collectionPublicReadSQL(kind string) string {
	table, entries, key, e := collectionTables(kind)
	if e != nil {
		return ""
	}
	return `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at),
 session_source AS MATERIALIZED(SELECT a.id actor_id,a.xmin::text actor_epoch,ss.id,ss.authorization_generation,ss.created_at,ss.expires_at,ss.idle_expires_at,ss.authentication_method,encode(ss.token_sha256,'hex') digest
 FROM sessions ss JOIN accounts a ON a.id=ss.account_id CROSS JOIN cl
 WHERE $2::uuid IS NOT NULL AND a.id=$2 AND a.account_type='person' AND a.status='active' AND ss.token_sha256=$3 AND ss.revoked_at IS NULL AND ss.expires_at>cl.at AND ss.idle_expires_at>cl.at AND ($4::boolean OR ss.authentication_method<>'dev_phone')),
 target AS MATERIALIZED(SELECT m.*,concat_ws(':',m.xmin::text,a.xmin::text,c.xmin::text,(SELECT copy_grant.xmin::text FROM personal_collection_copy_grants copy_grant WHERE copy_grant.kind='` + kind + `' AND copy_grant.target_id=m.id),` + strings.ReplaceAll(mapPublicAuthorSafetyGenerationSQL, "$6", "$2") + `) token,c.expires_at city_expiry
 FROM ` + table + ` m JOIN accounts a ON a.id=m.owner_account_id JOIN cities c ON c.id=m.city_id CROSS JOIN cl
 WHERE m.id=$1 AND m.status='published' AND m.visibility='public' AND m.owner_confirmed_at IS NOT NULL AND m.published_at IS NOT NULL AND m.owner_confirmed_at<=m.published_at AND m.published_at<=cl.at
 AND a.account_type='person' AND a.status='active' AND c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>cl.at)
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE $2::uuid IS NOT NULL AND((b.blocker_account_id=$2 AND b.blocked_account_id=a.id)OR(b.blocker_account_id=a.id AND b.blocked_account_id=$2)))
 AND($2::uuid IS NULL OR EXISTS(SELECT 1 FROM session_source))),
 sources AS MATERIALIZED(SELECT e.sequence_no,e.note,e.location_visibility,p.id,p.name,p.latitude,p.longitude,p.location_precision,
 least(p.expires_at,` + currentRetainedPlaceDeadlineSQL("p", "cl.at") + `) deadline,
 concat_ws(':',e.xmin::text,p.xmin::text,` + retainedPlaceReadBindingSQL("p") + `) token
 FROM ` + entries + ` e JOIN target m ON m.id=e.` + key + ` JOIN places p ON p.id=e.place_id AND p.city_id=e.city_id AND p.city_id=m.city_id CROSS JOIN cl
 WHERE p.publication_status='published' AND(p.expires_at IS NULL OR p.expires_at>cl.at) AND ` + currentRetainedPlaceVisibilitySQL("p", "cl.at") + ` AND ` + collectionCopyBorrowedSQL(kind, "m.id", "p.id", "m.owner_account_id") + `),
 payload AS MATERIALIZED(SELECT count(*) n,min(deadline) deadline,
 coalesce(jsonb_agg(jsonb_build_object('note',note,'locationVisibility',location_visibility)||CASE WHEN location_visibility='place' THEN jsonb_build_object('placeId',id,'placeName',name)||CASE WHEN location_precision='point' THEN jsonb_build_object('latitude',latitude,'longitude',longitude) ELSE '{}'::jsonb END ELSE '{}'::jsonb END ORDER BY sequence_no),'[]'::jsonb) items,
 coalesce(jsonb_agg(jsonb_build_array(sequence_no,id,token) ORDER BY sequence_no),'[]'::jsonb)::text token FROM sources),
 snapshot AS MATERIALIZED(SELECT m.id,m.city_id,m.title,m.summary,m.revision,payload.items,
 least(coalesce($5::timestamptz,cl.at+interval '60 seconds'),cl.at+interval '60 seconds',m.city_expiry,payload.deadline,(SELECT least(expires_at,idle_expires_at) FROM session_source)) until,
 concat_ws(':',m.token,payload.token,(SELECT jsonb_build_array(actor_id,actor_epoch,id,authorization_generation,created_at,expires_at,authentication_method,digest)::text FROM session_source)) token
 FROM target m CROSS JOIN payload CROSS JOIN cl WHERE payload.n BETWEEN 1 AND 50 AND payload.n=(SELECT count(*) FROM ` + entries + ` e WHERE e.` + key + `=m.id)),
 ready AS MATERIALIZED(SELECT count(*) FROM snapshot),wall AS MATERIALIZED(SELECT clock_timestamp() at FROM ready)
 SELECT snapshot.id,snapshot.city_id,snapshot.title,snapshot.summary,snapshot.revision,snapshot.items,snapshot.until,snapshot.token,` + collectionCopyGrantSQL(kind, "snapshot.id", "snapshot.revision", "snapshot.until", "snapshot.items") + `
 FROM snapshot CROSS JOIN wall WHERE snapshot.until>wall.at AND ($2::uuid IS NULL OR EXISTS(SELECT 1 FROM sessions ss JOIN session_source original ON ss.id=original.id JOIN accounts a ON a.id=ss.account_id WHERE a.id=$2 AND a.account_type='person' AND a.status='active' AND ss.token_sha256=$3 AND ss.authorization_generation=original.authorization_generation AND ss.revoked_at IS NULL AND ss.expires_at>wall.at AND ss.idle_expires_at>wall.at AND ($4::boolean OR ss.authentication_method<>'dev_phone')))`
}
func (s *Store) readPublicCollection(ctx context.Context, access content.PublicMomentAccess, kind, id string, until *time.Time) (content.PublicCollection, error) {
	var v content.PublicCollection
	if !content.ValidCollectionKind(kind) || !content.ValidCollectionID(id) {
		return v, content.ErrInvalid
	}
	id = strings.ToLower(id)
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return v, content.ErrUnavailable
	}
	var viewer any
	if access.Actor.ID != "" {
		if access.Actor.AccountType != "person" || !validHumanMomentID(access.Actor.ID) || access.SessionDigest == ([32]byte{}) {
			return v, identity.ErrUnauthorized
		}
		viewer = access.Actor.ID
	} else if access.SessionDigest != ([32]byte{}) || access.Actor.AccountType != "" {
		return v, identity.ErrUnauthorized
	}
	var raw, grant []byte
	var token string
	e := s.pool.QueryRow(ctx, collectionPublicReadSQL(kind), id, viewer, access.SessionDigest[:], s.devPhoneEnabled, until).Scan(&v.ID, &v.CityID, &v.Title, &v.Summary, &v.Revision, &raw, &v.SourceExpiresAt, &token, &grant)
	if errors.Is(e, pgx.ErrNoRows) {
		if viewer != nil {
			var live bool
			e = s.pool.QueryRow(ctx, humanMomentCurrentSession, access.SessionDigest[:], access.Actor.ID, s.devPhoneEnabled).Scan(&live)
			if e != nil {
				return v, content.ErrUnavailable
			}
			if !live {
				return v, identity.ErrUnauthorized
			}
		}
		return v, content.ErrNotFound
	}
	if e != nil || ctx.Err() != nil || json.Unmarshal(raw, &v.Entries) != nil {
		return content.PublicCollection{}, content.ErrUnavailable
	}
	h := sha256.Sum256([]byte(token))
	v.SourceBinding = hex.EncodeToString(h[:])
	v.Kind = kind
	v.SourceExpiresAt = v.SourceExpiresAt.UTC()
	if e = decodeCollectionCopyGrant(grant, &v); e != nil {
		return content.PublicCollection{}, e
	}
	if content.ValidatePublicCollection(v, kind, id) != nil {
		return content.PublicCollection{}, content.ErrUnavailable
	}
	return v, nil
}
func (s *Store) GetPublicCollection(ctx context.Context, a content.PublicMomentAccess, kind, id string) (content.PublicCollection, error) {
	if a.Actor.ID != "" {
		var e error
		ctx, e = s.collectionBoundContext(ctx, a.SessionDigest, a.Actor)
		if e != nil {
			return content.PublicCollection{}, e
		}
	}
	v, e := s.readPublicCollection(ctx, a, kind, id, nil)
	if e != nil {
		return v, e
	}
	if a.Actor.ID != "" {
		original, valid, _ := content.OriginalCollectionSession(ctx, a.SessionDigest, a.Actor)
		if !valid {
			return content.PublicCollection{}, content.ErrConflict
		}
		if e = s.ValidateCollectionSession(ctx, a.SessionDigest, a.Actor, original); e != nil {
			return content.PublicCollection{}, e
		}
	}
	wire, e := collectionPublicWire(v)
	if e != nil {
		return content.PublicCollection{}, content.ErrUnavailable
	}
	v.ResponseProof = &collectionPublicProof{s, a, v.SourceBinding, wire, v.SourceExpiresAt}
	return v, nil
}
func (s *Store) RevalidatePublicCollection(ctx context.Context, a content.PublicMomentAccess, v content.PublicCollection) error {
	p, ok := v.ResponseProof.(*collectionPublicProof)
	if !ok || p == nil || p.store != s || p.access != a || p.source != v.SourceBinding {
		return content.ErrUnavailable
	}
	wire, e := collectionPublicWire(v)
	if e != nil || wire != p.wire {
		return content.ErrUnavailable
	}
	next, e := s.readPublicCollection(ctx, a, v.Kind, v.ID, &p.until)
	if e != nil {
		return e
	}
	fresh, e := collectionPublicWire(next)
	if e != nil || fresh != p.wire || next.SourceBinding != p.source {
		return content.ErrUnavailable
	}
	return nil
}
