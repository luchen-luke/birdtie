package postgres

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/saved"
	"github.com/jackc/pgx/v5"
)

var _ saved.HumanSearchPageStore = (*Store)(nil)
var savedSearchCipherOnce sync.Once
var savedSearchCipher cipher.AEAD
var savedSearchCipherErr error

func savedSearchAEAD() (cipher.AEAD, error) {
	savedSearchCipherOnce.Do(func() {
		key := make([]byte, 32)
		if _, savedSearchCipherErr = rand.Read(key); savedSearchCipherErr != nil {
			return
		}
		block, e := aes.NewCipher(key)
		if e != nil {
			savedSearchCipherErr = e
			return
		}
		savedSearchCipher, savedSearchCipherErr = cipher.NewGCM(block)
	})
	return savedSearchCipher, savedSearchCipherErr
}

const savedSearchDomain = "saved-human-search-v1"

type savedSearchCursor struct {
	Version                int `json:"v"`
	Owner, Session, Source string
	Generation             int64
	Query                  [32]byte
	Snapshot, At           time.Time
	ID                     string
}
type savedSearchProof struct {
	store        *Store
	digest       [32]byte
	actor        identity.Actor
	query        saved.Query
	cursor       string
	binding      savedSearchCursor
	observed     time.Time
	wire, source [32]byte
}

func savedSearchSeal(c savedSearchCursor) (string, error) {
	a, e := savedSearchAEAD()
	if e != nil {
		return "", saved.ErrPageUnavailable
	}
	b, e := json.Marshal(c)
	if e != nil {
		return "", saved.ErrPageUnavailable
	}
	n := make([]byte, a.NonceSize())
	if _, e = rand.Read(n); e != nil {
		return "", saved.ErrPageUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(append(n, a.Seal(nil, n, b, []byte(savedSearchDomain))...)), nil
}
func savedSearchOpen(raw string) (savedSearchCursor, error) {
	var c savedSearchCursor
	if !saved.ValidSearchCursor(raw) || raw == "" {
		return c, saved.ErrInvalidPage
	}
	a, e := savedSearchAEAD()
	if e != nil {
		return c, saved.ErrPageUnavailable
	}
	b, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil || len(b) < a.NonceSize()+a.Overhead() {
		return c, saved.ErrInvalidPage
	}
	b, e = a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], []byte(savedSearchDomain))
	if e != nil {
		return c, saved.ErrPageChanged
	}
	if json.Unmarshal(b, &c) != nil || c.Version != 1 || !saved.ValidPageCursor(c.Owner) || c.Owner == "" || !saved.ValidPageCursor(c.Session) || c.Session == "" || !saved.ValidPageCursor(c.ID) || c.ID == "" || c.Generation < 1 || c.Source == "" || c.Snapshot.IsZero() || c.At.IsZero() || c.At.After(c.Snapshot) {
		return c, saved.ErrInvalidPage
	}
	canonical, _ := json.Marshal(c)
	if string(canonical) != string(b) {
		return c, saved.ErrInvalidPage
	}
	return c, nil
}
func savedSearchBinding(ctx context.Context, tx pgx.Tx, d [32]byte, a identity.Actor) (savedSearchCursor, time.Time, error) {
	c := savedSearchCursor{Version: 1, Owner: a.ID}
	var now time.Time
	e := tx.QueryRow(ctx, `SELECT se.id,se.authorization_generation,clock_timestamp(),current_database()||':'||(SELECT oid::text FROM pg_database WHERE datname=current_database()) FROM sessions se WHERE se.account_id=$1 AND se.token_sha256=$2`, a.ID, d[:]).Scan(&c.Session, &c.Generation, &now, &c.Source)
	if e != nil {
		return c, now, savedPageNativeFailure(e)
	}
	return c, now.UTC(), nil
}
func savedSearchSQL() string {
	base := strings.TrimSuffix(humanSavedRowsSQL, "ORDER BY s.created_at DESC,s.id DESC LIMIT $3")
	base = strings.Replace(base, "clock_timestamp()+interval '60 seconds'", "$4::timestamptz+interval '60 seconds'", 1)
	base = strings.Replace(base, "birdtie_entity_cover_metadata('activity',a.id,$1::uuid,clock_timestamp())", "birdtie_entity_cover_metadata('activity',a.id,$1::uuid,$4::timestamptz)", 1)
	base = strings.Replace(base, "birdtie_place_photo_metadata(p.id,clock_timestamp())", "birdtie_place_photo_metadata(p.id,$4::timestamptz)", 1)
	base = strings.ReplaceAll(base, "'observedAt',legacy.observed_at", "'observedAt',$4::timestamptz")
	base = strings.ReplaceAll(base, "legacy.observed_at+interval '30 seconds'", "$4::timestamptz+interval '30 seconds'")
	return `WITH projected(id,kind,target_id,title,summary,city_id,available,saved_at,note,note_revision,city_name,community_avatar,latitude,longitude,source_expires_at,private_source,cover,place_photo,legacy_reference) AS MATERIALIZED (` + base + `)
 SELECT * FROM projected WHERE saved_at<=$8::timestamptz
 AND ($10::uuid IS NULL OR(saved_at,id)<($9::timestamptz,$10::uuid))
 AND (($5='' AND $6='' AND $7='') OR available OR legacy_reference IS NOT NULL)
 AND($5='' OR strpos(lower(title||E'\n'||summary||E'\n'||city_name),lower($5))>0)
 AND($6='' OR kind=$6) AND($7='' OR city_id=$7)
 ORDER BY saved_at DESC,id DESC LIMIT $3`
}
func readSavedSearch(ctx context.Context, tx pgx.Tx, q saved.Query, cursor string, b savedSearchCursor, observed time.Time) (saved.Page, [32]byte, error) {
	out := saved.Page{Items: []saved.Item{}}
	var anchor, last any
	var lastAt any
	if q.Filtered() {
		if b.ID != "" {
			last, lastAt = b.ID, b.At
		}
	} else if cursor != "" {
		anchor = cursor
	}
	rows, e := tx.Query(ctx, savedSearchSQL(), b.Owner, anchor, saved.PageSize+1, observed, q.Text, q.Kind, q.CityID, b.Snapshot, lastAt, last)
	if e != nil {
		return out, [32]byte{}, savedPageNativeFailure(e)
	}
	sources := []string{}
	for rows.Next() {
		var i saved.Item
		var epoch string
		var legacyRaw []byte
		e = rows.Scan(&i.ID, &i.Kind, &i.TargetID, &i.Title, &i.Summary, &i.CityID, &i.Available, &i.SavedAt, &i.Note, &i.NoteRevision, &i.CityName, &i.CommunityAvatar, &i.Latitude, &i.Longitude, &i.SourceExpiresAt, &epoch, &i.Cover, &i.PlacePhoto, &legacyRaw)
		if e == nil && len(legacyRaw) > 0 {
			e = json.Unmarshal(legacyRaw, &i.LegacyReference)
		}
		if e != nil {
			rows.Close()
			return out, [32]byte{}, savedPageNativeFailure(e)
		}
		i.SavedAt = i.SavedAt.UTC()
		if i.SourceExpiresAt != nil {
			at := i.SourceExpiresAt.UTC()
			i.SourceExpiresAt = &at
		}
		out.Items = append(out.Items, i)
		sources = append(sources, epoch)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, [32]byte{}, savedPageNativeFailure(e)
	}
	if len(out.Items) > saved.PageSize {
		out.Items = out.Items[:saved.PageSize]
		sources = sources[:saved.PageSize]
		last := out.Items[len(out.Items)-1]
		if q.Filtered() {
			next := b
			next.ID, next.At = last.ID, last.SavedAt
			raw, e := savedSearchSeal(next)
			if e != nil {
				return saved.Page{}, [32]byte{}, e
			}
			out.NextCursor = &raw
		} else {
			id := last.ID
			out.NextCursor = &id
		}
	}
	// A fresh random seal is not data. Validation compares emitted rows and
	// the canonical cursor boundary, rather than re-encrypting to compare bytes.
	data, _ := json.Marshal(sources)
	return out, sha256.Sum256(data), nil
}
func savedSearchWire(p saved.Page) ([32]byte, error) {
	p.NextCursor = nil
	b, e := json.Marshal(p)
	return sha256.Sum256(b), e
}

const savedSearchRelations = `LOCK TABLE saved_items,private_saved_notes,places,activities,communities,cities,community_avatars,accounts,sessions,account_blocks,place_sources,place_field_evidence,place_source_permissions,place_candidates IN ACCESS SHARE MODE;` + entityCoverRelations + `;` + placePhotoRelations

func (s *Store) SearchHumanSavedPage(ctx context.Context, d [32]byte, a identity.Actor, q saved.Query, cursor string) (saved.Page, error) {
	empty := saved.Page{Items: []saved.Item{}}
	n, e := saved.NormalizeQuery(q)
	if e != nil || n != q {
		return empty, saved.ErrInvalidPage
	}
	if !q.Filtered() && !saved.ValidPageCursor(cursor) || q.Filtered() && !saved.ValidSearchCursor(cursor) {
		return empty, saved.ErrInvalidPage
	}
	tx, e := s.beginHumanMomentTx(ctx, d, a)
	if e != nil {
		return empty, savedPageNativeFailure(e)
	}
	defer tx.Rollback(context.Background())
	initial, _, e := savedSearchBinding(ctx, tx, d, a)
	if e != nil {
		return empty, e
	}
	if !q.Filtered() && cursor != "" {
		var id string
		e = tx.QueryRow(ctx, `SELECT id FROM saved_items WHERE owner_account_id=$1 AND id=$2 FOR SHARE`, a.ID, cursor).Scan(&id)
		if errors.Is(e, pgx.ErrNoRows) {
			return empty, saved.ErrPageChanged
		}
		if e != nil {
			return empty, savedPageNativeFailure(e)
		}
	}
	if _, e = tx.Exec(ctx, savedSearchRelations); e != nil {
		return empty, savedPageNativeFailure(e)
	}
	if e = s.lockHumanMomentSession(ctx, tx, d, a.ID); e != nil {
		return empty, savedPageNativeFailure(e)
	}
	b, observed, e := savedSearchBinding(ctx, tx, d, a)
	if e != nil {
		return empty, e
	}
	if b.Session != initial.Session || b.Generation != initial.Generation || b.Source != initial.Source {
		return empty, saved.ErrPageChanged
	}
	b.Query = q.Digest()
	b.Snapshot = observed
	if q.Filtered() && cursor != "" {
		prior, e := savedSearchOpen(cursor)
		if e != nil {
			return empty, e
		}
		if prior.Owner != b.Owner || prior.Session != b.Session || prior.Generation != b.Generation || prior.Source != b.Source || prior.Query != b.Query || prior.Snapshot.After(observed) {
			return empty, saved.ErrPageChanged
		}
		b = prior
	}
	out, source, e := readSavedSearch(ctx, tx, q, cursor, b, observed)
	if e != nil {
		return empty, e
	}
	if q.Filtered() {
		e = saved.ValidateSearchPage(out, q, cursor)
	} else {
		e = saved.ValidatePage(out, cursor)
	}
	if e != nil {
		return empty, saved.ErrPageUnavailable
	}
	wire, e := savedSearchWire(out)
	if e != nil {
		return empty, saved.ErrPageUnavailable
	}
	if e = s.finishHumanMomentTx(ctx, tx, d, a.ID); e != nil {
		return empty, savedPageNativeFailure(e)
	}
	out.ResponseProof = &savedSearchProof{s, d, a, q, cursor, b, observed, wire, source}
	return out, nil
}
func (s *Store) ValidateHumanSavedSearchPage(ctx context.Context, d [32]byte, a identity.Actor, q saved.Query, cursor string, out saved.Page) error {
	p, ok := out.ResponseProof.(*savedSearchProof)
	wire, e := savedSearchWire(out)
	if !ok || p == nil || p.store != s || p.digest != d || p.actor != a || p.query != q || p.cursor != cursor || e != nil || wire != p.wire {
		return saved.ErrPageUnavailable
	}
	tx, e := s.beginHumanMomentTx(ctx, d, a)
	if e != nil {
		return savedPageNativeFailure(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, savedSearchRelations); e != nil {
		return savedPageNativeFailure(e)
	}
	if e = s.lockHumanMomentSession(ctx, tx, d, a.ID); e != nil {
		return savedPageNativeFailure(e)
	}
	b, now, e := savedSearchBinding(ctx, tx, d, a)
	if e != nil {
		return e
	}
	if b.Owner != p.binding.Owner || b.Session != p.binding.Session || b.Generation != p.binding.Generation || b.Source != p.binding.Source || !now.Before(p.observed.Add(60*time.Second)) {
		return saved.ErrPageChanged
	}
	var privateUntil *time.Time
	for _, item := range out.Items {
		if ref := item.LegacyReference; ref != nil {
			if !ref.Valid(now) {
				return saved.ErrPageChanged
			}
			if privateUntil == nil || ref.ValidUntil.Before(*privateUntil) {
				until := ref.ValidUntil
				privateUntil = &until
			}
		}
	}
	current, source, e := readSavedSearch(ctx, tx, q, cursor, p.binding, p.observed)
	if e != nil {
		return e
	}
	actual, e := savedSearchWire(current)
	if e != nil || actual != p.wire || source != p.source || (out.NextCursor == nil) != (current.NextCursor == nil) {
		return saved.ErrPageChanged
	}
	if out.NextCursor != nil && q.Filtered() {
		c, e := savedSearchOpen(*out.NextCursor)
		if e != nil || c.Owner != p.binding.Owner || c.Query != p.binding.Query || c.Session != p.binding.Session || c.Generation != p.binding.Generation || c.Source != p.binding.Source || !c.Snapshot.Equal(p.binding.Snapshot) || len(out.Items) == 0 || c.ID != out.Items[len(out.Items)-1].ID || !c.At.Equal(out.Items[len(out.Items)-1].SavedAt) {
			return saved.ErrPageChanged
		}
	} else if out.NextCursor != nil && *out.NextCursor != *current.NextCursor {
		return saved.ErrPageChanged
	}
	if e = checkHumanMomentSession(ctx, tx, d, a.ID, s.devPhoneEnabled); e != nil {
		return savedPageNativeFailure(e)
	}
	if privateUntil != nil {
		if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			return saved.ErrPageUnavailable
		}
		if !now.Before(*privateUntil) {
			return saved.ErrPageChanged
		}
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return saved.ErrPageUnavailable
	}
	return nil
}
