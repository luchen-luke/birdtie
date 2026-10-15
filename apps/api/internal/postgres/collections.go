package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	lpr "github.com/birdtie/birdtie/apps/api/internal/legacyplacereference"
	"github.com/jackc/pgx/v5"
	"time"
)

var _ content.HumanCollectionStore = (*Store)(nil)

func collectionTables(kind string) (string, string, string, error) {
	switch kind {
	case "journey":
		return "journeys", "journey_stops", "journey_id", nil
	case "theme_map":
		return "theme_maps", "theme_map_places", "theme_map_id", nil
	default:
		return "", "", "", content.ErrInvalid
	}
}
func collectionReadTx(ctx context.Context, tx pgx.Tx, owner, kind, id string, lock bool) (content.PersonalCollection, error) {
	table, entries, key, e := collectionTables(kind)
	if e != nil || !content.ValidCollectionID(id) {
		return content.PersonalCollection{}, content.ErrInvalid
	}
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	v := content.PersonalCollection{}
	v.Kind = kind
	e = tx.QueryRow(ctx, `SELECT id,owner_account_id,coalesce(city_id,''),title,summary,visibility,status,revision,created_at,updated_at,EXISTS(SELECT 1 FROM personal_collection_copy_borrowed_places borrowed WHERE borrowed.destination_kind='`+kind+`' AND borrowed.destination_id=$1) FROM `+table+` WHERE id=$1 AND owner_account_id=$2`+suffix, id, owner).Scan(&v.ID, &v.OwnerID, &v.CityID, &v.Title, &v.Summary, &v.Visibility, &v.Status, &v.Revision, &v.CreatedAt, &v.UpdatedAt, &v.PrivatePlaceCopyRestricted)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, content.ErrNotFound
	}
	if e != nil {
		return v, content.ErrUnavailable
	}
	if kind == "journey" {
		v.JourneyDetails, e = readJourneyDetailsTx(ctx, tx, owner, id)
		if e != nil {
			return v, e
		}
		if v.JourneyDetails != nil {
			v.Entries = []content.CollectionEntry{}
			return v, nil
		}
	}
	locked, e := tx.Query(ctx, `SELECT p.id FROM places p JOIN `+entries+` e ON e.place_id=p.id AND e.city_id=p.city_id WHERE e.`+key+`=$1 ORDER BY p.id FOR SHARE OF p`, id)
	if e != nil {
		return v, content.ErrUnavailable
	}
	for locked.Next() {
		var sourceID string
		if locked.Scan(&sourceID) != nil {
			locked.Close()
			return v, content.ErrUnavailable
		}
	}
	e = locked.Err()
	locked.Close()
	if e != nil {
		return v, content.ErrUnavailable
	}
	// Preserve references and private notes; a withdrawn source is no longer
	// labelled as a currently available Place and exposes no stale source name.
	eligible := `p.publication_status='published' AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp()) AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp()) AND ` + currentRetainedPlaceVisibilitySQL("p", "clock_timestamp()")
	eligible += ` AND ` + collectionCopyBorrowedSQL(kind, "e."+key, "p.id", "$2")
	frame, hasFrame := content.CollectionSessionOwnerFrame(ctx, owner)
	var sessionID any
	var generation int64
	if hasFrame {
		sessionID = frame.ID
		generation = frame.Generation
	}
	rows, e := tx.Query(ctx, `SELECT COALESCE(e.place_id::text,''),e.note,e.location_visibility,COALESCE(CASE WHEN `+eligible+` THEN p.name END,''),COALESCE(`+eligible+`,false),CASE WHEN `+eligible+` AND p.location_precision='point' AND p.coordinate_system='wgs84' THEN p.latitude END,CASE WHEN `+eligible+` AND p.location_precision='point' AND p.coordinate_system='wgs84' THEN p.longitude END, CASE WHEN `+eligible+` THEN least(clock_timestamp()+interval '60 seconds',c.expires_at,p.expires_at,`+currentRetainedPlaceDeadlineSQL("p", "clock_timestamp()")+`,`+collectionCopyBorrowedDeadlineSQL(kind, "e."+key, "p.id")+`,(SELECT least(expires_at,idle_expires_at) FROM sessions WHERE id=$3::uuid AND account_id=$2 AND authorization_generation=$4)) END FROM `+entries+` e LEFT JOIN places p ON p.id=e.place_id AND p.city_id=e.city_id LEFT JOIN cities c ON c.id=e.city_id WHERE e.`+key+`=$1 ORDER BY e.sequence_no`, id, owner, sessionID, generation)
	if e != nil {
		return v, content.ErrUnavailable
	}
	defer rows.Close()
	v.Entries = []content.CollectionEntry{}
	for rows.Next() {
		var entry content.CollectionEntry
		if e = rows.Scan(&entry.PlaceID, &entry.Note, &entry.LocationVisibility, &entry.PlaceName, &entry.Available, &entry.Latitude, &entry.Longitude, &entry.SourceExpiresAt); e != nil {
			return v, content.ErrUnavailable
		}
		v.Entries = append(v.Entries, entry)
	}
	if rows.Err() != nil {
		return v, content.ErrUnavailable
	}
	rows.Close()
	if kind == "theme_map" && v.Status == "draft" && v.Visibility == "private" {
		for i := range v.Entries {
			entry := &v.Entries[i]
			if entry.Available {
				continue
			}
			private, e := legacyPlaceReferenceTx(ctx, tx, owner, lpr.Reference{Kind: kind, RecordID: id, PlaceID: entry.PlaceID}, time.Time{})
			if errors.Is(e, lpr.ErrNotFound) {
				continue
			}
			if e != nil {
				return v, e
			}
			entry.PlaceName = private.Name
			entry.LegacyReference = &private.Reference
		}
	}
	return v, nil
}
func collectionSourcesTx(ctx context.Context, tx pgx.Tx, p content.CollectionInput) error {
	var city string
	e := tx.QueryRow(ctx, `SELECT id FROM cities WHERE id=$1 AND publication_status='published' AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE`, p.CityID).Scan(&city)
	if errors.Is(e, pgx.ErrNoRows) {
		return content.ErrConflict
	}
	if e != nil {
		return content.ErrUnavailable
	}
	// Fixed sorting prevents distinct collections from taking Place locks in
	// opposite order. IDs never carry ownership or visibility permission.
	ids := make([]string, 0, len(p.Entries))
	for _, entry := range p.Entries {
		ids = append(ids, entry.PlaceID)
	}
	rows, e := tx.Query(ctx, `SELECT p.id FROM places p WHERE p.id=ANY($1::uuid[]) AND p.city_id=$2 AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp()) AND `+currentRetainedPlaceVisibilitySQL("p", "clock_timestamp()")+` ORDER BY p.id FOR SHARE`, ids, p.CityID)
	if e != nil {
		return content.ErrUnavailable
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			return content.ErrUnavailable
		}
		count++
	}
	if rows.Err() != nil {
		return content.ErrUnavailable
	}
	if count != len(ids) {
		return content.ErrConflict
	}
	return nil
}
func collectionEntriesTx(ctx context.Context, tx pgx.Tx, id string, p content.CollectionInput) error {
	_, table, key, e := collectionTables(p.Kind)
	if e != nil {
		return e
	}
	// A complete ordinary PUT must preserve origins of retained Place IDs.
	// Only removing the ID detaches it; a later explicit canonical append is
	// the person's own new selection. Entry-row replacement never detaches it.
	ids := make([]string, 0, len(p.Entries))
	for _, entry := range p.Entries {
		ids = append(ids, entry.PlaceID)
	}
	if _, e = tx.Exec(ctx, `DELETE FROM personal_collection_copy_borrowed_places WHERE destination_kind=$1 AND destination_id=$2 AND NOT(place_id=ANY($3::uuid[]))`, p.Kind, id, ids); e != nil {
		return content.ErrUnavailable
	}
	if _, e = tx.Exec(ctx, `DELETE FROM `+table+` WHERE `+key+`=$1`, id); e != nil {
		return content.ErrUnavailable
	}
	for i, entry := range p.Entries {
		if _, e = tx.Exec(ctx, `INSERT INTO `+table+` (`+key+`,city_id,sequence_no,place_id,note,location_visibility) VALUES($1,$2,$3,$4,$5,$6)`, id, p.CityID, i+1, entry.PlaceID, entry.Note, entry.LocationVisibility); e != nil {
			return content.ErrUnavailable
		}
	}
	return nil
}
func collectionAuditTx(ctx context.Context, tx pgx.Tx, owner, kind, id, action string) error {
	_, e := tx.Exec(ctx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose) VALUES($1,$2,$3,$4,'allowed','personal_content')`, owner, action, kind, id)
	if e != nil {
		return content.ErrUnavailable
	}
	return nil
}
func (s *Store) collectionCommit(ctx context.Context, tx pgx.Tx, d [32]byte, owner string) error {
	if e := s.lockHumanMomentSession(ctx, tx, d, owner); e != nil {
		return e
	}
	if original, valid, present := content.OriginalCollectionSession(ctx, d, identity.Actor{ID: owner, AccountType: "person"}); present {
		if !valid {
			return content.ErrConflict
		}
		current, e := collectionSessionEpoch(ctx, tx, d, owner)
		if e != nil {
			return e
		}
		if current.id != original.ID || current.generation != original.Generation {
			return content.ErrConflict
		}
	}
	return s.finishHumanMomentTx(ctx, tx, d, owner)
}
func (s *Store) CreateHumanCollection(ctx context.Context, d [32]byte, a identity.Actor, p content.CollectionCreate) (content.PersonalCollection, error) {
	if !content.ValidCollectionID(p.OperationID) {
		return content.PersonalCollection{}, content.ErrInvalid
	}
	in, e := content.NormalizeCollectionInput(p.CollectionInput)
	if e != nil {
		return content.PersonalCollection{}, e
	}
	p.CollectionInput = in
	ctx, tx, e := s.beginBoundCollectionTx(ctx, d, a)
	if e != nil {
		return content.PersonalCollection{}, e
	}
	defer tx.Rollback(context.Background())
	// One durable operation per owner even when clients race or lose receipts.
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, a.ID+":"+p.OperationID); e != nil {
		return content.PersonalCollection{}, content.ErrUnavailable
	}
	digest := content.CollectionInputDigest(in)
	var oldKind, id string
	var previous []byte
	e = tx.QueryRow(ctx, `SELECT kind,target_id,input_sha256 FROM personal_collection_operations WHERE owner_account_id=$1 AND operation_id=$2`, a.ID, p.OperationID).Scan(&oldKind, &id, &previous)
	if e == nil {
		if oldKind != in.Kind || !bytes.Equal(previous, digest[:]) {
			return content.PersonalCollection{}, content.ErrConflict
		}
		v, e := collectionReadTx(ctx, tx, a.ID, oldKind, id, true)
		if e != nil {
			return v, e
		}
		if e = s.collectionCommit(ctx, tx, d, a.ID); e != nil {
			return content.PersonalCollection{}, e
		}
		s.attachCollectionCopyReadProof(ctx, d, a, &v)
		return v, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return content.PersonalCollection{}, content.ErrUnavailable
	}
	if handled, _, _, err := journeyDetailsAuthorityTx(ctx, tx, a.ID, "", in); err != nil {
		return content.PersonalCollection{}, err
	} else if !handled {
		if e = collectionSourcesTx(ctx, tx, in); e != nil {
			return content.PersonalCollection{}, e
		}
	}
	table, _, _, _ := collectionTables(in.Kind)
	if in.JourneyDetails != nil {
		e = tx.QueryRow(ctx, `INSERT INTO journeys(owner_account_id,city_id,title,summary,detail_schema)VALUES($1,NULLIF($2,''),$3,$4,$5)RETURNING id`, a.ID, in.CityID, in.Title, in.Summary, content.JourneyDetailsSchema).Scan(&id)
	} else {
		e = tx.QueryRow(ctx, `INSERT INTO `+table+`(owner_account_id,city_id,title,summary) VALUES($1,$2,$3,$4) RETURNING id`, a.ID, in.CityID, in.Title, in.Summary).Scan(&id)
	}
	if e != nil {
		return content.PersonalCollection{}, content.ErrUnavailable
	}
	if in.JourneyDetails != nil {
		e = writeJourneyDetailsTx(ctx, tx, id, *in.JourneyDetails)
	} else {
		e = collectionEntriesTx(ctx, tx, id, in)
	}
	if e != nil {
		return content.PersonalCollection{}, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO personal_collection_operations(owner_account_id,operation_id,kind,target_id,input_sha256) VALUES($1,$2,$3,$4,$5)`, a.ID, p.OperationID, in.Kind, id, digest[:]); e != nil {
		return content.PersonalCollection{}, content.ErrUnavailable
	}
	if e = collectionAuditTx(ctx, tx, a.ID, in.Kind, id, "create_draft"); e != nil {
		return content.PersonalCollection{}, e
	}
	v, e := collectionReadTx(ctx, tx, a.ID, in.Kind, id, false)
	if e != nil {
		return v, e
	}
	if e = s.collectionCommit(ctx, tx, d, a.ID); e != nil {
		return content.PersonalCollection{}, e
	}
	s.attachCollectionCopyReadProof(ctx, d, a, &v)
	return v, nil
}
func (s *Store) GetHumanCollection(ctx context.Context, d [32]byte, a identity.Actor, kind, id string) (content.PersonalCollection, error) {
	ctx, tx, e := s.beginBoundCollectionTx(ctx, d, a)
	if e != nil {
		return content.PersonalCollection{}, e
	}
	defer tx.Rollback(context.Background())
	v, e := collectionReadTx(ctx, tx, a.ID, kind, id, true)
	if e != nil {
		return v, e
	}
	if e = s.collectionCommit(ctx, tx, d, a.ID); e != nil {
		return content.PersonalCollection{}, e
	}
	s.attachCollectionCopyReadProof(ctx, d, a, &v)
	return v, nil
}
func (s *Store) GetHumanCollectionOperation(ctx context.Context, d [32]byte, a identity.Actor, op string) (content.PersonalCollection, error) {
	if !content.ValidCollectionID(op) {
		return content.PersonalCollection{}, content.ErrInvalid
	}
	ctx, tx, e := s.beginBoundCollectionTx(ctx, d, a)
	if e != nil {
		return content.PersonalCollection{}, e
	}
	defer tx.Rollback(context.Background())
	var kind, id string
	e = tx.QueryRow(ctx, `SELECT kind,target_id FROM personal_collection_operations WHERE owner_account_id=$1 AND operation_id=$2`, a.ID, op).Scan(&kind, &id)
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
	if e = s.collectionCommit(ctx, tx, d, a.ID); e != nil {
		return content.PersonalCollection{}, e
	}
	s.attachCollectionCopyReadProof(ctx, d, a, &v)
	return v, nil
}
func (s *Store) ListHumanCollections(ctx context.Context, d [32]byte, a identity.Actor, kind, cursor string) (content.CollectionPage, error) {
	table, _, _, e := collectionTables(kind)
	if e != nil || (cursor != "" && !content.ValidCollectionID(cursor)) {
		return content.CollectionPage{}, content.ErrInvalid
	}
	ctx, tx, e := s.beginBoundCollectionTx(ctx, d, a)
	if e != nil {
		return content.CollectionPage{}, e
	}
	defer tx.Rollback(context.Background())
	if cursor != "" {
		var id string
		e = tx.QueryRow(ctx, `SELECT id FROM `+table+` WHERE id=$1 AND owner_account_id=$2`, cursor, a.ID).Scan(&id)
		if errors.Is(e, pgx.ErrNoRows) {
			return content.CollectionPage{}, content.ErrConflict
		}
		if e != nil {
			return content.CollectionPage{}, content.ErrUnavailable
		}
	}
	query := fmt.Sprintf(`SELECT id FROM %s WHERE owner_account_id=$1 AND ((visibility='private' AND status='draft') OR(visibility='public' AND status='published')) AND ($2='' OR (created_at,id)<(SELECT created_at,id FROM %s WHERE id=NULLIF($2,'')::uuid AND owner_account_id=$1)) ORDER BY created_at DESC,id DESC LIMIT 21`, table, table)
	rows, e := tx.Query(ctx, query, a.ID, cursor)
	if e != nil {
		return content.CollectionPage{}, content.ErrUnavailable
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return content.CollectionPage{}, content.ErrUnavailable
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return content.CollectionPage{}, content.ErrUnavailable
	}
	page := content.CollectionPage{Items: []content.PersonalCollection{}}
	if len(ids) > content.CollectionPageSize {
		ids = ids[:content.CollectionPageSize]
		page.NextCursor = ids[len(ids)-1]
	}
	for _, id := range ids {
		v, e := collectionReadTx(ctx, tx, a.ID, kind, id, true)
		if e != nil {
			return content.CollectionPage{}, e
		}
		if v.Status == "draft" && v.Visibility == "private" || v.Status == "published" && v.Visibility == "public" {
			s.attachCollectionCopyReadProof(ctx, d, a, &v)
			page.Items = append(page.Items, v)
		}
	}
	if e = s.collectionCommit(ctx, tx, d, a.ID); e != nil {
		return content.CollectionPage{}, e
	}
	return page, nil
}
func (s *Store) UpdateHumanCollection(ctx context.Context, d [32]byte, a identity.Actor, id string, revision int64, in content.CollectionInput) (content.PersonalCollection, error) {
	if !content.ValidCollectionID(id) || revision < 1 {
		return content.PersonalCollection{}, content.ErrInvalid
	}
	in, e := content.NormalizeCollectionInput(in)
	if e != nil {
		return content.PersonalCollection{}, e
	}
	ctx, tx, e := s.beginBoundCollectionTx(ctx, d, a)
	if e != nil {
		return content.PersonalCollection{}, e
	}
	defer tx.Rollback(context.Background())
	if _, e = updateHumanCollectionTx(ctx, tx, a.ID, id, revision, in); e != nil {
		return content.PersonalCollection{}, e
	}
	v, e := collectionReadTx(ctx, tx, a.ID, in.Kind, id, false)
	if e != nil {
		return v, e
	}
	if e = s.collectionCommit(ctx, tx, d, a.ID); e != nil {
		return content.PersonalCollection{}, e
	}
	s.attachCollectionCopyReadProof(ctx, d, a, &v)
	return v, nil
}

// The original effect is shared by the ordinary compatibility method and the
// controlled operation's own transaction. It never opens or commits a pool
// transaction, so entries, borrowed origins, audit and receipt stay atomic.
func updateHumanCollectionTx(ctx context.Context, tx pgx.Tx, owner, id string, revision int64, in content.CollectionInput) (int64, error) {
	if !content.ValidCollectionID(id) || revision < 1 || revision >= content.MaxCollectionAppendRevision {
		return 0, content.ErrInvalid
	}
	var e error
	in, e = content.NormalizeCollectionInput(in)
	if e != nil {
		return 0, e
	}
	table, _, _, _ := collectionTables(in.Kind)
	var oldCity string
	e = tx.QueryRow(ctx, `SELECT coalesce(city_id,'') FROM `+table+` WHERE id=$1 AND owner_account_id=$2 AND revision=$3 AND status='draft' AND visibility='private' FOR UPDATE`, id, owner, revision).Scan(&oldCity)
	if errors.Is(e, pgx.ErrNoRows) {
		return 0, content.ErrConflict
	}
	if e != nil {
		return 0, content.ErrUnavailable
	}
	if handled, _, _, err := journeyDetailsAuthorityTx(ctx, tx, owner, id, in); handled || err != nil {
		if err != nil {
			return 0, err
		}
		if in.JourneyDetails == nil {
			return 0, content.ErrConflict
		}
		if e = writeJourneyDetailsTx(ctx, tx, id, *in.JourneyDetails); e != nil {
			return 0, e
		}
		if _, e = tx.Exec(ctx, `UPDATE journeys SET city_id=NULLIF($2,''),title=$3,summary=$4,revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, id, in.CityID, in.Title, in.Summary); e != nil {
			return 0, content.ErrUnavailable
		}
		if e = collectionAuditTx(ctx, tx, owner, in.Kind, id, "update_draft"); e != nil {
			return 0, e
		}
		return revision + 1, nil
	}
	// A saved collection has a stable city; changing exploration scope creates
	// a separate draft instead of silently moving an approved resource.
	if oldCity != in.CityID {
		return 0, content.ErrConflict
	}
	if retained, _, _, err := legacyCollectionRetainedAuthorityTx(ctx, tx, owner, id, in); err != nil {
		return 0, err
	} else if !retained {
		if e = collectionSourcesTx(ctx, tx, in); e != nil {
			return 0, e
		}
	}
	if e = collectionEntriesTx(ctx, tx, id, in); e != nil {
		return 0, e
	}
	if _, e = tx.Exec(ctx, `UPDATE `+table+` SET title=$2,summary=$3,revision=revision+1,updated_at=now() WHERE id=$1`, id, in.Title, in.Summary); e != nil {
		return 0, content.ErrUnavailable
	}
	if e = collectionAuditTx(ctx, tx, owner, in.Kind, id, "update_draft"); e != nil {
		return 0, e
	}
	return revision + 1, nil
}
func (s *Store) WithdrawHumanCollection(ctx context.Context, d [32]byte, a identity.Actor, kind, id string, revision int64) error {
	_, _, _, e := collectionTables(kind)
	if e != nil || !content.ValidCollectionID(id) || revision < 1 {
		return content.ErrInvalid
	}
	ctx, tx, e := s.beginBoundCollectionTx(ctx, d, a)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	if _, e = withdrawHumanCollectionTx(ctx, tx, a.ID, kind, id, revision); e != nil {
		return e
	}
	return s.collectionCommit(ctx, tx, d, a.ID)
}

func withdrawHumanCollectionTx(ctx context.Context, tx pgx.Tx, owner, kind, id string, revision int64) (int64, error) {
	table, _, _, e := collectionTables(kind)
	if e != nil || !content.ValidCollectionID(id) || revision < 1 || revision > content.MaxCollectionAppendRevision {
		return 0, content.ErrInvalid
	}
	var applied int64
	e = tx.QueryRow(ctx, `UPDATE `+table+` SET status='withdrawn',revision=CASE WHEN revision<$4 THEN revision+1 ELSE revision END,updated_at=now() WHERE id=$1 AND owner_account_id=$2 AND revision=$3 AND status='draft' AND visibility='private' RETURNING revision`, id, owner, revision, content.MaxCollectionAppendRevision).Scan(&applied)
	if errors.Is(e, pgx.ErrNoRows) {
		return 0, content.ErrConflict
	}
	if e != nil {
		return 0, content.ErrUnavailable
	}
	if e = collectionAuditTx(ctx, tx, owner, kind, id, "withdraw"); e != nil {
		return 0, e
	}
	return applied, nil
}
