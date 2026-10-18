package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

var _ content.JourneyDiscoveryStore = (*Store)(nil)

// Stored rows contain only approval material and canonical source references.
// Private details, source/origin labels, transport and custom addresses have no
// representation here and can never be serialized by this reader.
type journeyApprovedStop struct {
	content.JourneyPublicationStopInput
	PlaceID string `json:"placeId"`
}

func (s journeyApprovedStop) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"entryId": s.EntryID, "position": s.Position, "title": s.Title, "note": s.Note, "locationVisibility": s.LocationVisibility, "placeId": s.PlaceID, "momentId": s.MomentID, "momentRevision": s.MomentRevision, "commentMomentId": s.CommentMomentID})
}

type journeyPlaceFact struct {
	ID, Name, Stamp     string
	Latitude, Longitude *float64
	Until               time.Time
}
type journeyMomentFact struct {
	Revision int64
	Stamp    string
	Until    time.Time
}
type journeySourceFacts struct {
	places  map[string]journeyPlaceFact
	moments map[string]journeyMomentFact
}

func journeyMin(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// All selected Places and Moments are read in two bounded queries. The Moment
// predicate is exactly the public-context reader, including current blocks,
// source epochs and media-only eligibility; knowing an ID grants no access.
func (s *Store) journeyFactsTx(ctx context.Context, tx pgx.Tx, a content.PublicMomentAccess, stops []journeyApprovedStop, until time.Time) (journeySourceFacts, error) {
	out := journeySourceFacts{map[string]journeyPlaceFact{}, map[string]journeyMomentFact{}}
	placeIDs, momentIDs := []string{}, []string{}
	ps, ms := map[string]bool{}, map[string]bool{}
	for _, e := range stops {
		if e.PlaceID != "" && !ps[e.PlaceID] {
			ps[e.PlaceID] = true
			placeIDs = append(placeIDs, e.PlaceID)
		}
		if e.MomentID != "" && !ms[e.MomentID] {
			ms[e.MomentID] = true
			momentIDs = append(momentIDs, e.MomentID)
		}
	}
	rows, e := tx.Query(ctx, `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at)
 SELECT p.id,p.name,CASE WHEN p.location_precision='point' AND p.coordinate_system='wgs84' THEN p.latitude END,CASE WHEN p.location_precision='point' AND p.coordinate_system='wgs84' THEN p.longitude END,
 least($2::timestamptz,c.expires_at,p.expires_at,`+currentRetainedPlaceDeadlineSQL("p", "cl.at")+`),jsonb_build_array(p.xmin::text,c.xmin::text,`+retainedPlaceReadBindingSQL("p")+`)::text
 FROM places p JOIN cities c ON c.id=p.city_id CROSS JOIN cl WHERE p.id=ANY($1::uuid[]) AND p.publication_status='published' AND c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>cl.at) AND(p.expires_at IS NULL OR p.expires_at>cl.at) AND `+currentRetainedPlaceVisibilitySQL("p", "cl.at")+` ORDER BY p.id`, placeIDs, until)
	if e != nil {
		return out, content.ErrUnavailable
	}
	for rows.Next() {
		var p journeyPlaceFact
		if rows.Scan(&p.ID, &p.Name, &p.Latitude, &p.Longitude, &p.Until, &p.Stamp) != nil {
			rows.Close()
			return out, content.ErrUnavailable
		}
		out.places[p.ID] = p
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, content.ErrUnavailable
	}
	query := strings.Replace(publicMomentContextReadSQL(false), "WHERE m.id=$1 AND", "WHERE m.id=ANY($1::uuid[]) AND", 1)
	var viewer any
	if a.Actor.ID != "" {
		viewer = a.Actor.ID
	}
	rows, e = tx.Query(ctx, query, momentIDs, viewer, a.SessionDigest[:], s.devPhoneEnabled, false)
	if e != nil {
		return out, content.ErrUnavailable
	}
	for rows.Next() {
		var v content.PublicMomentWithContext
		var rawAuthor, rawPhoto []byte
		var stamp, photoProof string
		if rows.Scan(&v.ID, &v.Title, &v.Body, &v.PlaceID, &v.PlaceName, &v.CityID, &v.CityName, &v.LocationPrecision, &v.Revision, &v.PublishedAt, &stamp, &rawAuthor, &v.ObservedAt, &v.ValidUntil, &rawPhoto, &photoProof) != nil {
			rows.Close()
			return out, content.ErrUnavailable
		}
		out.moments[v.ID] = journeyMomentFact{v.Revision, stamp, journeyMin(until, v.ValidUntil)}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, content.ErrUnavailable
	}
	return out, nil
}

func (s *Store) journeyApprovalTx(ctx context.Context, tx pgx.Tx, a content.PublicMomentAccess, v content.PersonalCollection, in content.JourneyPublicationInput) ([]journeyApprovedStop, string, error) {
	if v.JourneyDetails == nil || v.Kind != "journey" || v.Visibility != "private" || v.Status != "draft" {
		return nil, "", content.ErrConflict
	}
	byID := map[string]content.JourneyEntry{}
	for _, e := range v.JourneyDetails.Itinerary {
		byID[e.ID] = e
	}
	stops := make([]journeyApprovedStop, 0, len(in.Stops))
	for _, choice := range in.Stops {
		entry, ok := byID[choice.EntryID]
		if !ok || entry.EntryType != "STOP" || entry.Position != choice.Position {
			return nil, "", content.ErrConflict
		}
		if choice.LocationVisibility != "hidden" && entry.PlaceID == "" {
			return nil, "", content.ErrConflict
		}
		stops = append(stops, journeyApprovedStop{choice, entry.PlaceID})
	}
	// Lock canonical rows in deterministic order before checking their current
	// read eligibility. The original Journey is already FOR UPDATE by the caller.
	places, moments := []string{}, []string{}
	for _, p := range stops {
		if p.PlaceID != "" {
			places = append(places, p.PlaceID)
		}
		if p.MomentID != "" {
			moments = append(moments, p.MomentID)
		}
	}
	for _, q := range []struct {
		sql string
		ids []string
	}{{`SELECT p.id FROM places p JOIN cities c ON c.id=p.city_id WHERE p.id=ANY($1::uuid[]) ORDER BY p.id FOR SHARE OF p,c`, places}, {`SELECT m.id FROM moments m JOIN accounts a ON a.id=m.author_account_id WHERE m.id=ANY($1::uuid[]) ORDER BY m.id FOR SHARE OF m,a`, moments}} {
		rows, e := tx.Query(ctx, q.sql, q.ids)
		if e != nil {
			return nil, "", content.ErrUnavailable
		}
		for rows.Next() {
			var ignored string
			if rows.Scan(&ignored) != nil {
				rows.Close()
				return nil, "", content.ErrUnavailable
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, "", content.ErrUnavailable
		}
	}
	var until time.Time
	if tx.QueryRow(ctx, `SELECT clock_timestamp()+interval '60 seconds'`).Scan(&until) != nil {
		return nil, "", content.ErrUnavailable
	}
	facts, e := s.journeyFactsTx(ctx, tx, a, stops, until)
	if e != nil {
		return nil, "", e
	}
	stamps := []any{}
	for _, p := range stops {
		if p.PlaceID != "" {
			f, ok := facts.places[p.PlaceID]
			if !ok || !f.Until.After(time.Now().UTC()) {
				return nil, "", content.ErrConflict
			}
			stamps = append(stamps, []any{p.EntryID, f.Stamp})
		}
		if p.MomentID != "" {
			m, ok := facts.moments[p.MomentID]
			if !ok || m.Revision != p.MomentRevision || !m.Until.After(time.Now().UTC()) {
				return nil, "", content.ErrConflict
			}
			stamps = append(stamps, []any{p.EntryID, m.Stamp})
		}
	}
	raw, _ := json.Marshal(stamps)
	return stops, identity.HumanControlTextHash(string(raw)), nil
}

func (s *Store) writeJourneyPublicationTx(ctx context.Context, tx pgx.Tx, a content.PublicMomentAccess, v content.PersonalCollection, op, action string, in *content.JourneyPublicationInput) (string, error) {
	if v.JourneyDetails == nil || v.Status != "draft" || v.Visibility != "private" {
		return "", content.ErrConflict
	}
	stamp := ""
	if action == "PUBLISH" {
		if in == nil {
			return "", content.ErrInvalid
		}
		stops, proof, e := s.journeyApprovalTx(ctx, tx, a, v, *in)
		if e != nil {
			return "", e
		}
		stamp = proof
		raw, e := json.Marshal(stops)
		if e != nil {
			return "", content.ErrInvalid
		}
		_, e = tx.Exec(ctx, `INSERT INTO journey_publication_snapshots(journey_id,publication_revision,owner_account_id,source_revision,operation_id,title,summary,stops)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, v.ID, v.Revision+1, v.OwnerID, v.Revision, op, in.Title, in.Summary, raw)
		if e != nil {
			return "", content.ErrUnavailable
		}
		_, e = tx.Exec(ctx, `INSERT INTO journey_publication_current(journey_id,publication_revision,owner_account_id)VALUES($1,$2,$3)ON CONFLICT(journey_id)DO UPDATE SET publication_revision=EXCLUDED.publication_revision,owner_account_id=EXCLUDED.owner_account_id`, v.ID, v.Revision+1, v.OwnerID)
		if e != nil {
			return "", content.ErrUnavailable
		}
	} else {
		tag, e := tx.Exec(ctx, `DELETE FROM journey_publication_current WHERE journey_id=$1 AND owner_account_id=$2`, v.ID, v.OwnerID)
		if e != nil {
			return "", content.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return "", content.ErrConflict
		}
	}
	_, e := tx.Exec(ctx, `UPDATE journeys SET revision=revision+1,updated_at=clock_timestamp() WHERE id=$1 AND owner_account_id=$2 AND revision=$3 AND detail_schema=$4 AND visibility='private' AND status='draft'`, v.ID, v.OwnerID, v.Revision, content.JourneyDetailsSchema)
	if e != nil {
		return "", content.ErrUnavailable
	}
	return stamp, nil
}

func readJourneyOwnerMetadataTx(ctx context.Context, tx pgx.Tx, v *content.PersonalCollection) error {
	var raw []byte
	if tx.QueryRow(ctx, `SELECT birdtie_journey_lifecycle($1,clock_timestamp())`, v.ID).Scan(&raw) != nil || json.Unmarshal(raw, &v.JourneyLifecycle) != nil {
		return content.ErrUnavailable
	}
	var summary content.JourneyPublishedSummary
	e := tx.QueryRow(ctx, `SELECT s.publication_revision,s.source_revision,s.published_at,s.title,s.summary,jsonb_array_length(s.stops) FROM journey_publication_current p JOIN journey_publication_snapshots s USING(journey_id,publication_revision,owner_account_id)WHERE p.journey_id=$1 AND p.owner_account_id=$2`, v.ID, v.OwnerID).Scan(&summary.PublicationRevision, &summary.SourceRevision, &summary.PublishedAt, &summary.Title, &summary.Summary, &summary.StopCount)
	if e == nil {
		summary.PublishedAt = summary.PublishedAt.UTC()
		v.PublishedSnapshot = &summary
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return content.ErrUnavailable
	}
	return nil
}

func (s *Store) readPublicJourneyTx(ctx context.Context, tx pgx.Tx, a content.PublicMomentAccess, id string, capUntil *time.Time) (content.PublicCollection, error) {
	v := content.PublicCollection{Kind: "journey", Entries: []content.PublicCollectionEntry{}}
	var raw []byte
	var owner, headerStamp string
	var at time.Time
	var sessionUntil *time.Time
	var viewer any
	if a.Actor.ID != "" {
		viewer = a.Actor.ID
	}
	safety := strings.ReplaceAll(mapPublicAuthorSafetyGenerationSQL, "$6", "$2")
	// Scan into explicit variables; no private Journey column participates in
	// the public payload or version. Editing a draft cannot update publication.
	var pubRev, srcRev int64
	var published time.Time
	e := tx.QueryRow(ctx, `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at)SELECT s.journey_id,s.title,s.summary,s.publication_revision,s.source_revision,s.published_at,s.stops,s.owner_account_id,cl.at,
 jsonb_build_array(s.xmin::text,cp.xmin::text,a.xmin::text,`+safety+`,(SELECT jsonb_build_array(ss.id,ss.authorization_generation,ss.created_at,ss.expires_at,ss.authentication_method,encode(ss.token_sha256,'hex'),viewer.xmin::text)FROM sessions ss JOIN accounts viewer ON viewer.id=ss.account_id WHERE viewer.id=$2 AND ss.token_sha256=$3))::text,
 (SELECT least(ss.expires_at,ss.idle_expires_at)FROM sessions ss WHERE ss.account_id=$2 AND ss.token_sha256=$3)
 FROM journey_publication_current cp JOIN journey_publication_snapshots s USING(journey_id,publication_revision,owner_account_id) JOIN journeys j ON j.id=s.journey_id AND j.owner_account_id=s.owner_account_id JOIN accounts a ON a.id=s.owner_account_id CROSS JOIN cl
 WHERE s.journey_id=$1 AND j.detail_schema='birdtie.journey-details.v1' AND j.visibility='private' AND j.status='draft' AND a.account_type='person' AND a.status='active' AND s.published_at<=cl.at
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE $2::uuid IS NOT NULL AND((b.blocker_account_id=$2 AND b.blocked_account_id=a.id)OR(b.blocker_account_id=a.id AND b.blocked_account_id=$2)))
 AND($2::uuid IS NULL OR EXISTS(SELECT 1 FROM sessions ss JOIN accounts viewer ON viewer.id=ss.account_id WHERE viewer.id=$2 AND viewer.account_type='person' AND viewer.status='active' AND ss.token_sha256=$3 AND ss.revoked_at IS NULL AND ss.expires_at>cl.at AND ss.idle_expires_at>cl.at AND($4::boolean OR ss.authentication_method<>'dev_phone')))`, id, viewer, a.SessionDigest[:], s.devPhoneEnabled).Scan(&v.ID, &v.Title, &v.Summary, &pubRev, &srcRev, &published, &raw, &owner, &at, &headerStamp, &sessionUntil)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, content.ErrNotFound
	}
	if e != nil {
		return v, content.ErrUnavailable
	}
	until := at.Add(60 * time.Second)
	if sessionUntil != nil {
		until = journeyMin(until, *sessionUntil)
	}
	if capUntil != nil {
		until = journeyMin(until, *capUntil)
	}
	if !until.After(at) {
		return v, content.ErrConflict
	}
	var approved []journeyApprovedStop
	if json.Unmarshal(raw, &approved) != nil {
		return v, content.ErrUnavailable
	}
	facts, e := s.journeyFactsTx(ctx, tx, a, approved, until)
	if e != nil {
		return v, e
	}
	v.Revision = pubRev
	v.SourceExpiresAt = until.UTC()
	v.JourneySnapshot = &content.PublicJourneySnapshot{SchemaVersion: content.JourneyPublicationSchema, PublicationRevision: pubRev, SourceRevision: srcRev, PublishedAt: published.UTC(), Stops: []content.PublicJourneyStop{}}
	stamps := []any{headerStamp}
	for _, e := range approved {
		p, placeOK := facts.places[e.PlaceID]
		m, momentOK := facts.moments[e.MomentID]
		available := (e.PlaceID == "" || placeOK) && (e.MomentID == "" || momentOK && m.Revision == e.MomentRevision)
		row := content.PublicJourneyStop{EntryID: e.EntryID, Position: e.Position, Available: available}
		if available {
			row.Title, row.Note, row.LocationVisibility = e.Title, e.Note, e.LocationVisibility
			row.MomentID, row.MomentRevision, row.CommentMomentID = e.MomentID, e.MomentRevision, e.CommentMomentID
			if e.PlaceID != "" {
				v.SourceExpiresAt = journeyMin(v.SourceExpiresAt, p.Until)
				stamps = append(stamps, []any{e.EntryID, p.Stamp})
				if e.LocationVisibility == "place" {
					placeID := e.PlaceID
					row.PlaceID = &placeID
					row.PlaceName = p.Name
					row.Latitude, row.Longitude = p.Latitude, p.Longitude
				}
			}
			if e.MomentID != "" {
				v.SourceExpiresAt = journeyMin(v.SourceExpiresAt, m.Until)
				stamps = append(stamps, []any{e.EntryID, m.Stamp})
			}
		} else {
			stamps = append(stamps, []any{e.EntryID, "UNAVAILABLE"})
		}
		v.JourneySnapshot.Stops = append(v.JourneySnapshot.Stops, row)
	}
	raw, _ = json.Marshal(stamps)
	v.SourceBinding = identity.HumanControlTextHash(string(raw))
	if !v.SourceExpiresAt.After(time.Now().UTC()) || content.ValidatePublicCollection(v, "journey", id) != nil {
		return content.PublicCollection{}, content.ErrUnavailable
	}
	return v, nil
}

func (s *Store) readPublicJourney(ctx context.Context, a content.PublicMomentAccess, id string, until *time.Time) (content.PublicCollection, error) {
	if a.Actor.ID != "" {
		if a.Actor.AccountType != "person" || a.SessionDigest == ([32]byte{}) {
			return content.PublicCollection{}, identity.ErrUnauthorized
		}
		if _, e := s.CaptureCollectionSession(ctx, a.SessionDigest, a.Actor); e != nil {
			return content.PublicCollection{}, e
		}
	} else if a.Actor.AccountType != "" || a.SessionDigest != ([32]byte{}) {
		return content.PublicCollection{}, identity.ErrUnauthorized
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return content.PublicCollection{}, content.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	return s.readPublicJourneyTx(ctx, tx, a, id, until)
}

type journeyCursor struct {
	At     time.Time `json:"at"`
	ID     string    `json:"id"`
	Viewer string    `json:"viewer"`
}

func encodeJourneyCursor(prefix string, v journeyCursor) string {
	humanWriteSigning.Do(func() { _, humanWriteSigning.err = rand.Read(humanWriteSigning.key[:]) })
	if humanWriteSigning.err != nil {
		return ""
	}
	raw, _ := json.Marshal(v)
	payload := base64.RawURLEncoding.EncodeToString(raw)
	signature := hmac.New(sha256.New, humanWriteSigning.key[:])
	_, _ = signature.Write([]byte(prefix + payload))
	return prefix + payload + "." + base64.RawURLEncoding.EncodeToString(signature.Sum(nil))
}
func decodeJourneyCursor(prefix, cursor, viewer string) (journeyCursor, error) {
	var c journeyCursor
	if !strings.HasPrefix(cursor, prefix) || len(cursor) > 512 {
		return c, content.ErrInvalid
	}
	parts := strings.Split(strings.TrimPrefix(cursor, prefix), ".")
	if len(parts) != 2 {
		return c, content.ErrInvalid
	}
	humanWriteSigning.Do(func() { _, humanWriteSigning.err = rand.Read(humanWriteSigning.key[:]) })
	if humanWriteSigning.err != nil {
		return c, content.ErrUnavailable
	}
	sig, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return c, content.ErrInvalid
	}
	check := hmac.New(sha256.New, humanWriteSigning.key[:])
	_, _ = check.Write([]byte(prefix + parts[0]))
	if !hmac.Equal(sig, check.Sum(nil)) {
		return c, content.ErrInvalid
	}
	raw, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil || json.Unmarshal(raw, &c) != nil || !content.ValidCollectionID(c.ID) || c.Viewer != viewer || c.At.IsZero() || c.At.After(time.Now().UTC().Add(time.Second)) {
		return c, content.ErrInvalid
	}
	return c, nil
}

type journeyDiscoveryProof struct {
	s               *Store
	d               [32]byte
	a               identity.Actor
	cursor          string
	until, observed time.Time
	wire            [32]byte
}

func journeyDiscoveryWire(v content.JourneyDiscoveryPage) [32]byte {
	v.ResponseProof = nil
	raw, _ := json.Marshal(v)
	return sha256.Sum256(raw)
}
func (s *Store) ListJourneyDiscovery(ctx context.Context, d [32]byte, a identity.Actor, cursor string) (content.JourneyDiscoveryPage, error) {
	return s.listJourneyDiscovery(ctx, d, a, cursor, nil)
}
func (s *Store) listJourneyDiscovery(ctx context.Context, d [32]byte, a identity.Actor, cursor string, prior *journeyDiscoveryProof) (content.JourneyDiscoveryPage, error) {
	ctx, tx, e := s.beginBoundCollectionTx(ctx, d, a)
	if e != nil {
		return content.JourneyDiscoveryPage{}, e
	}
	defer tx.Rollback(context.Background())
	var now, sessionUntil time.Time
	if tx.QueryRow(ctx, `SELECT clock_timestamp(),least(expires_at,idle_expires_at) FROM sessions WHERE token_sha256=$1 AND account_id=$2`, d[:], a.ID).Scan(&now, &sessionUntil) != nil {
		return content.JourneyDiscoveryPage{}, identity.ErrUnauthorized
	}
	observed := now.UTC()
	until := journeyMin(now.Add(60*time.Second), sessionUntil)
	if prior != nil {
		observed = prior.observed
		until = journeyMin(until, prior.until)
	}
	var cutoff *time.Time
	last := ""
	if cursor != "" {
		c, e := decodeJourneyCursor("jd1.", cursor, a.ID)
		if e != nil {
			return content.JourneyDiscoveryPage{}, e
		}
		cutoff = &c.At
		last = c.ID
	}
	// Simple public Journeys remain discoverable. Rich records use only their
	// current immutable snapshot; neither branch exposes private draft titles.
	rows, e := tx.Query(ctx, `WITH candidates AS(SELECT j.id,j.published_at at FROM journeys j JOIN accounts author ON author.id=j.owner_account_id WHERE j.detail_schema IS NULL AND j.visibility='public' AND j.status='published' AND author.status='active' AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE(b.blocker_account_id=$1 AND b.blocked_account_id=author.id)OR(b.blocker_account_id=author.id AND b.blocked_account_id=$1)) UNION ALL SELECT s.journey_id,s.published_at FROM journey_publication_current cp JOIN journey_publication_snapshots s USING(journey_id,publication_revision,owner_account_id)JOIN journeys j ON j.id=s.journey_id JOIN accounts author ON author.id=s.owner_account_id WHERE j.detail_schema='birdtie.journey-details.v1' AND j.visibility='private' AND j.status='draft' AND author.status='active' AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE(b.blocker_account_id=$1 AND b.blocked_account_id=author.id)OR(b.blocker_account_id=author.id AND b.blocked_account_id=$1))) SELECT id,at FROM candidates WHERE at IS NOT NULL AND at<=$4 AND($2::timestamptz IS NULL OR(at,id)<($2,NULLIF($3,'')::uuid))ORDER BY at DESC,id DESC LIMIT 81`, a.ID, cutoff, last, now)
	if e != nil {
		return content.JourneyDiscoveryPage{}, content.ErrUnavailable
	}
	type candidate struct {
		id string
		at time.Time
	}
	candidates := []candidate{}
	for rows.Next() {
		var c candidate
		if rows.Scan(&c.id, &c.at) != nil {
			rows.Close()
			return content.JourneyDiscoveryPage{}, content.ErrUnavailable
		}
		candidates = append(candidates, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return content.JourneyDiscoveryPage{}, content.ErrUnavailable
	}
	page := content.JourneyDiscoveryPage{SchemaVersion: content.JourneyDiscoverySchema, ViewerAccountID: a.ID, Items: []content.PublicCollection{}, ObservedAt: observed, ValidUntil: until.UTC()}
	access := content.PublicMomentAccess{Actor: a, SessionDigest: d}
	stamps := []any{}
	for i, c := range candidates {
		if len(page.Items) == 20 {
			page.NextCursor = encodeJourneyCursor("jd1.", journeyCursor{candidates[i-1].at, candidates[i-1].id, a.ID})
			if page.NextCursor == "" {
				return page, content.ErrUnavailable
			}
			break
		}
		v, e := s.readPublicJourneyTx(ctx, tx, access, c.id, &page.ValidUntil)
		if errors.Is(e, content.ErrNotFound) {
			v, e = s.readPublicCollection(ctx, access, "journey", c.id, &page.ValidUntil)
		}
		if errors.Is(e, content.ErrNotFound) {
			if i == len(candidates)-1 && len(candidates) == 81 {
				page.NextCursor = encodeJourneyCursor("jd1.", journeyCursor{c.at, c.id, a.ID})
				if page.NextCursor == "" {
					return page, content.ErrUnavailable
				}
			}
			continue
		}
		if e != nil {
			return page, e
		}
		page.ValidUntil = journeyMin(page.ValidUntil, v.SourceExpiresAt)
		page.Items = append(page.Items, v)
		stamps = append(stamps, []any{v.ID, v.SourceBinding})
		if i == len(candidates)-1 && len(candidates) == 81 {
			page.NextCursor = encodeJourneyCursor("jd1.", journeyCursor{c.at, c.id, a.ID})
			if page.NextCursor == "" {
				return page, content.ErrUnavailable
			}
		}
	}
	// Earlier entries cannot retain a longer lease than the final page lease.
	for i := range page.Items {
		page.Items[i].SourceExpiresAt = page.ValidUntil
		if page.Items[i].CopyGrant != nil {
			page.Items[i].CopyGrant.ExpiresAt = journeyMin(page.Items[i].CopyGrant.ExpiresAt, page.ValidUntil)
		}
	}
	raw, _ := json.Marshal(stamps)
	page.SourceVersion = identity.HumanControlTextHash(string(raw))
	if content.ValidateJourneyDiscovery(page, a.ID) != nil {
		return page, content.ErrUnavailable
	}
	if e = s.collectionCommit(ctx, tx, d, a.ID); e != nil {
		return page, e
	}
	p := &journeyDiscoveryProof{s, d, a, cursor, page.ValidUntil, page.ObservedAt, journeyDiscoveryWire(page)}
	page.ResponseProof = p
	return page, nil
}
func (s *Store) RevalidateJourneyDiscovery(ctx context.Context, d [32]byte, a identity.Actor, v content.JourneyDiscoveryPage) error {
	p, ok := v.ResponseProof.(*journeyDiscoveryProof)
	if !ok || p == nil || p.s != s || p.d != d || p.a != a || journeyDiscoveryWire(v) != p.wire || !p.until.After(time.Now().UTC()) {
		return content.ErrUnavailable
	}
	fresh, e := s.listJourneyDiscovery(ctx, d, a, p.cursor, p)
	if e != nil {
		return e
	}
	if journeyDiscoveryWire(fresh) != p.wire {
		return content.ErrConflict
	}
	return nil
}

// Map callers pass only their already-loaded canonical Moment IDs. This is a
// bounded batch, never one network query per point, and never infers relations.
type JourneyPublicationEdge struct {
	JourneyID                                        string
	PublicationRevision                              int64
	FromMomentID, ToMomentID, FromPlaceID, ToPlaceID string
	FromPosition, ToPosition                         int
	SourceStamp                                      string
	ValidUntil                                       time.Time
}

func (s *Store) ReadJourneyPublicationEdgesTx(ctx context.Context, tx pgx.Tx, a content.PublicMomentAccess, momentIDs []string, maxEdges int) ([]JourneyPublicationEdge, error) {
	if len(momentIDs) > 500 || maxEdges < 1 || maxEdges > 200 {
		return nil, content.ErrInvalid
	}
	for _, id := range momentIDs {
		if !content.ValidCollectionID(id) {
			return nil, content.ErrInvalid
		}
	}
	rows, e := tx.Query(ctx, `SELECT s.journey_id FROM journey_publication_current cp JOIN journey_publication_snapshots s USING(journey_id,publication_revision,owner_account_id)WHERE EXISTS(SELECT 1 FROM jsonb_array_elements(s.stops) stop WHERE stop->>'momentId'=ANY($1::text[]))ORDER BY s.published_at DESC,s.journey_id DESC LIMIT 20`, momentIDs)
	if e != nil {
		return nil, content.ErrUnavailable
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return nil, content.ErrUnavailable
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, content.ErrUnavailable
	}
	loaded := map[string]bool{}
	for _, id := range momentIDs {
		loaded[id] = true
	}
	out := []JourneyPublicationEdge{}
	for _, id := range ids {
		v, e := s.readPublicJourneyTx(ctx, tx, a, id, nil)
		if errors.Is(e, content.ErrNotFound) {
			continue
		}
		if e != nil {
			return nil, e
		}
		stops := v.JourneySnapshot.Stops
		for i := 1; i < len(stops); i++ {
			from, to := stops[i-1], stops[i]
			if !from.Available || !to.Available || to.Position != from.Position+1 || from.PlaceID == nil || to.PlaceID == nil || from.Latitude == nil || to.Latitude == nil || from.MomentID == "" || to.MomentID == "" || !loaded[from.MomentID] || !loaded[to.MomentID] {
				continue
			}
			out = append(out, JourneyPublicationEdge{id, v.Revision, from.MomentID, to.MomentID, *from.PlaceID, *to.PlaceID, from.Position, to.Position, v.SourceBinding, v.SourceExpiresAt})
			if len(out) == maxEdges {
				return out, nil
			}
		}
	}
	return out, nil
}

// The owner list keeps the server classification instant in its cursor. A
// client's timezone or a midnight between pages cannot reshuffle that batch.
func listJourneyOwnerIDsTx(ctx context.Context, tx pgx.Tx, owner, cursor string) ([]string, string, error) {
	var asOf time.Time
	var anchor string
	if cursor == "" {
		if tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&asOf) != nil {
			return nil, "", content.ErrUnavailable
		}
	} else if content.ValidCollectionID(cursor) {
		anchor = cursor
		if tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&asOf) != nil {
			return nil, "", content.ErrUnavailable
		}
	} else {
		c, e := decodeJourneyCursor("jl1.", cursor, owner)
		if e != nil {
			return nil, "", e
		}
		asOf, anchor = c.At, c.ID
	}
	if anchor != "" {
		var ok bool
		if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM journeys WHERE id=$1 AND owner_account_id=$2)`, anchor, owner).Scan(&ok) != nil {
			return nil, "", content.ErrUnavailable
		}
		if !ok {
			return nil, "", content.ErrConflict
		}
	}
	rows, e := tx.Query(ctx, `WITH ranked AS(SELECT id,created_at,CASE WHEN birdtie_journey_lifecycle(id,$3)->>'state'='NOT_STARTED' THEN 0 ELSE 1 END priority FROM journeys WHERE owner_account_id=$1 AND((visibility='private' AND status='draft')OR(visibility='public' AND status='published')))SELECT r.id FROM ranked r WHERE($2='' OR EXISTS(SELECT 1 FROM ranked anchor WHERE anchor.id=NULLIF($2,'')::uuid AND(r.priority>anchor.priority OR(r.priority=anchor.priority AND(r.created_at,r.id)<(anchor.created_at,anchor.id))))) ORDER BY r.priority,r.created_at DESC,r.id DESC LIMIT 21`, owner, anchor, asOf)
	// Use a direct lexicographic predicate matching the mixed-direction ORDER.
	if e != nil {
		return nil, "", content.ErrUnavailable
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return nil, "", content.ErrUnavailable
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, "", content.ErrUnavailable
	}
	next := ""
	if len(ids) > 20 {
		ids = ids[:20]
		next = encodeJourneyCursor("jl1.", journeyCursor{asOf, ids[19], owner})
		if next == "" {
			return nil, "", content.ErrUnavailable
		}
	}
	return ids, next, nil
}

func (s *Store) listJourneyOwnerCollections(ctx context.Context, d [32]byte, a identity.Actor, cursor string) (content.CollectionPage, error) {
	ctx, tx, e := s.beginBoundCollectionTx(ctx, d, a)
	if e != nil {
		return content.CollectionPage{}, e
	}
	defer tx.Rollback(context.Background())
	ids, next, e := listJourneyOwnerIDsTx(ctx, tx, a.ID, cursor)
	if e != nil {
		return content.CollectionPage{}, e
	}
	page := content.CollectionPage{Items: []content.PersonalCollection{}, NextCursor: next}
	for _, id := range ids {
		v, e := collectionReadTx(ctx, tx, a.ID, "journey", id, true)
		if e != nil {
			return content.CollectionPage{}, e
		}
		s.attachCollectionCopyReadProof(ctx, d, a, &v)
		page.Items = append(page.Items, v)
	}
	if e = s.collectionCommit(ctx, tx, d, a.ID); e != nil {
		return content.CollectionPage{}, e
	}
	return page, nil
}
