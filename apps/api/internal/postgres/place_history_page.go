package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	ph "github.com/birdtie/birdtie/apps/api/internal/placehistory"
	"github.com/jackc/pgx/v5"
)

var _ ph.PageStore = (*Store)(nil)

type historyPageProof struct {
	store      *Store
	access     ph.Access
	query      ph.PageQuery
	source     string
	wire       [32]byte
	end, until time.Time
}
type historyPageNative struct {
	source     string
	sessionID  string
	generation int64
	end, until time.Time
}

func historyPageSQL() string {
	return historyPageSQLFromBase(placeHistoryPayloadSQL)
}

// Exact, checked adaptation of the original ACL-first CTE. Newline differences
// are harmless; a missing/ambiguous structural marker is unavailable, never an
// unbounded or moving-window fallback. This function only creates SQL text.
func historyPageSQLFromBase(base string) string {
	base = strings.ReplaceAll(base, "\r\n", "\n")
	cut := " arrangements AS MATERIALIZED ("
	if strings.Count(base, cut) != 1 {
		return ""
	}
	prefix, _, ok := strings.Cut(base, cut)
	if !ok {
		return ""
	}
	replace := func(needle, replacement string) bool {
		if strings.Count(prefix, needle) != 1 {
			return false
		}
		prefix = strings.Replace(prefix, needle, replacement, 1)
		return true
	}
	if !replace(" target AS MATERIALIZED (", " bound AS MATERIALIZED(SELECT coalesce($7::timestamptz,clock.at) asof FROM clock), target AS MATERIALIZED (") ||
		!replace("m.published_at FROM moments m", "m.published_at,CASE WHEN $6::boolean THEN "+publicMomentAuthorProfileSQL("a")+" ELSE NULL END author_public_profile,concat_ws(':',m.xmin::text,a.xmin::text,CASE WHEN $6::boolean THEN "+publicMomentAuthorBindingSQL("a")+" ELSE '' END,"+strings.ReplaceAll(mapPublicAuthorSafetyGenerationSQL, "$6", "$2")+") author_binding FROM moments m") ||
		!replace("CROSS JOIN clock\n   WHERE m.status", "CROSS JOIN clock CROSS JOIN bound\n   WHERE m.status") ||
		!replace("m.published_at>=clock.at-interval '30 days' AND m.published_at<=clock.at", "m.published_at>=bound.asof-interval '30 days' AND m.published_at<=bound.asof AND m.published_at<=clock.at") {
		return ""
	}
	return prefix + ` session_source AS MATERIALIZED(SELECT act.id actor_id,act.xmin::text actor_epoch,ss.id,ss.authorization_generation,ss.created_at,ss.expires_at,ss.idle_expires_at,ss.authentication_method,encode(ss.token_sha256,'hex') digest
  FROM sessions ss JOIN accounts act ON act.id=ss.account_id CROSS JOIN clock
  WHERE $2::uuid IS NOT NULL AND act.id=$2 AND act.account_type=$4 AND act.status='active' AND ss.token_sha256=$3 AND ss.revoked_at IS NULL AND ss.expires_at>clock.at AND ss.idle_expires_at>clock.at AND ($5::boolean OR ss.authentication_method<>'dev_phone')),
 page_rows AS MATERIALIZED(SELECT * FROM recent r WHERE $9::timestamptz IS NULL OR (r.published_at,r.id)<($9::timestamptz,$10::uuid) ORDER BY r.published_at DESC,r.id DESC LIMIT 6),
 payload AS MATERIALIZED(SELECT coalesce(jsonb_agg(jsonb_build_object('id',r.id,'title',r.title,'excerpt',r.excerpt,'revision',r.revision,'publishedAt',r.published_at,'authorId',r.author_public_profile->'accountId','authorPublicProfile',r.author_public_profile) ORDER BY r.published_at DESC,r.id DESC),'[]'::jsonb) items FROM page_rows r)
 SELECT clock.at,bound.asof,
 least(coalesce($8::timestamptz,bound.asof+interval '2 minutes'),bound.asof+interval '2 minutes',p.expires_at,c.expires_at,` + currentRetainedPlaceDeadlineSQL("p", "clock.at") + `,
 (SELECT min(location.deadline) FROM recent r CROSS JOIN LATERAL birdtie_moment_locations_public(r.id,clock.at) location
   WHERE location.kind='PLACE' AND location.precision='place' AND location.place_id=t.id),
 (SELECT least(expires_at,idle_expires_at) FROM session_source)),
 t.city_id,(SELECT count(*) FROM recent),payload.items,
 jsonb_build_array(p.id,p.xmin::text,c.id,c.xmin::text,` + retainedPlaceReadBindingSQL("p") + `,
 (SELECT coalesce(jsonb_agg(jsonb_build_array(r.id,r.author_binding) ORDER BY r.id),'[]'::jsonb) FROM recent r),
 (SELECT jsonb_build_array(actor_id,actor_epoch,id,authorization_generation,created_at,expires_at,authentication_method,digest) FROM session_source))::text,
 coalesce((SELECT id::text FROM session_source),''),coalesce((SELECT authorization_generation FROM session_source),0)
 FROM target t JOIN places p ON p.id=t.id JOIN cities c ON c.id=t.city_id CROSS JOIN clock CROSS JOIN bound CROSS JOIN payload
 WHERE ($2::uuid IS NULL OR EXISTS(SELECT 1 FROM session_source))`
}

// The original payload projection is reused in one FINAL native statement.
// Its fixed window and ACL source and the last wall/session/deadline check are
// one PostgreSQL statement snapshot, with wall evaluated after materialization.
// This is a current read fence, not a write grant or an infinite response lock.
func historyPageFinalSQL() string {
	payload := historyPageSQL()
	if payload == "" {
		return ""
	}
	return `WITH page_snapshot(observed_at,window_end,valid_until,city_id,visible_count,items,private_source,session_id,generation) AS MATERIALIZED(` + payload + `),
 ready AS MATERIALIZED(SELECT count(*) n FROM page_snapshot),
 wall AS MATERIALIZED(SELECT clock_timestamp() at FROM ready)
 SELECT wall.at,snapshot.valid_until,snapshot.private_source,
 ($2::uuid IS NULL OR EXISTS(SELECT 1 FROM sessions se JOIN accounts a ON a.id=se.account_id
 WHERE a.id=$2 AND a.account_type=$4 AND a.status='active'
 AND se.id::text=$11 AND se.token_sha256=$3 AND se.authorization_generation=$12
 AND se.revoked_at IS NULL AND se.expires_at>wall.at AND se.idle_expires_at>wall.at
 AND ($5::boolean OR se.authentication_method<>'dev_phone')))
 FROM wall LEFT JOIN page_snapshot snapshot ON true`
}

func historyPageWire(p ph.Page) ([32]byte, error) {
	p.ObservedAt = time.Time{}
	p.ResponseProof = nil
	raw, e := json.Marshal(p)
	if e != nil {
		return [32]byte{}, e
	}
	return sha256.Sum256(raw), nil
}
func (s *Store) ReadPublicPlaceHistoryPage(ctx context.Context, a ph.Access, q ph.PageQuery) (ph.Page, error) {
	out, n, e := s.readHistoryPage(ctx, a, q, nil)
	if e != nil {
		return ph.Page{}, e
	}
	wire, e := historyPageWire(out)
	if e != nil {
		return ph.Page{}, ph.ErrUnavailable
	}
	copied := q
	if q.Cursor != nil {
		c := *q.Cursor
		copied.Cursor = &c
	}
	out.ResponseProof = &historyPageProof{s, a, copied, n.source, wire, n.end, n.until}
	return out, nil
}
func (s *Store) RevalidatePublicPlaceHistoryPage(ctx context.Context, a ph.Access, q ph.PageQuery, out ph.Page) error {
	proof, ok := out.ResponseProof.(*historyPageProof)
	if !ok || proof == nil || proof.store != s || proof.access.Actor.ID != a.Actor.ID || proof.access.Actor.AccountType != a.Actor.AccountType || proof.access.SessionDigest != a.SessionDigest || proof.query.PlaceID != q.PlaceID || proof.query.WithAuthor != q.WithAuthor {
		return ph.ErrUnavailable
	}
	rawQ, _ := json.Marshal(q)
	frozenQ, _ := json.Marshal(proof.query)
	wire, e := historyPageWire(out)
	if e != nil || string(rawQ) != string(frozenQ) || wire != proof.wire {
		return ph.ErrUnavailable
	}
	fresh, n, e := s.readHistoryPage(ctx, a, q, proof)
	if e != nil {
		return e
	}
	next, e := historyPageWire(fresh)
	if e != nil || n.source != proof.source || next != proof.wire {
		return ph.ErrChanged
	}
	return nil
}
func (s *Store) readHistoryPage(ctx context.Context, a ph.Access, q ph.PageQuery, fixed *historyPageProof) (ph.Page, historyPageNative, error) {
	var out ph.Page
	var n historyPageNative
	if s == nil || s.pool == nil || ctx == nil || ctx.Err() != nil {
		return out, n, ph.ErrUnavailable
	}
	if ph.ValidateAccess(a) != nil || !validHumanMomentID(q.PlaceID) || (q.Cursor != nil && ph.ValidatePageCursor(*q.Cursor, q.PlaceID, q.WithAuthor) != nil) {
		return out, n, ph.ErrInvalid
	}
	var viewer, asof, until, before, id any
	if a.Actor.ID != "" {
		viewer = a.Actor.ID
	}
	if q.Cursor != nil {
		end, deadline, last, e := ph.CursorTimes(*q.Cursor)
		if e != nil {
			return out, n, e
		}
		asof, until, before, id = end, deadline, last, q.Cursor.BeforeID
	}
	if fixed != nil {
		asof, until = fixed.end, fixed.until
	}
	var rows []byte
	var native string
	sql := historyPageSQL()
	if sql == "" {
		return out, n, ph.ErrUnavailable
	}
	e := s.pool.QueryRow(ctx, sql, q.PlaceID, viewer, a.SessionDigest[:], a.Actor.AccountType, s.devPhoneEnabled, q.WithAuthor, asof, until, before, id).Scan(&out.ObservedAt, &n.end, &n.until, &out.CityID, &out.Count, &rows, &native, &n.sessionID, &n.generation)
	if errors.Is(e, pgx.ErrNoRows) {
		if viewer != nil {
			var live bool
			err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions se JOIN accounts a ON a.id=se.account_id WHERE a.id=$1 AND a.account_type=$3 AND a.status='active' AND se.token_sha256=$2 AND se.revoked_at IS NULL AND se.expires_at>clock_timestamp() AND se.idle_expires_at>clock_timestamp() AND ($4::boolean OR se.authentication_method<>'dev_phone'))`, viewer, a.SessionDigest[:], a.Actor.AccountType, s.devPhoneEnabled).Scan(&live)
			if err != nil {
				return out, n, ph.ErrUnavailable
			}
			if !live {
				return out, n, identity.ErrUnauthorized
			}
		}
		return out, n, ph.ErrNotFound
	}
	if e != nil || ctx.Err() != nil || native == "" || json.Unmarshal(rows, &out.Items) != nil {
		return out, n, ph.ErrUnavailable
	}
	for i := range out.Items {
		out.Items[i].PublishedAt = out.Items[i].PublishedAt.UTC()
	}
	out.ObservedAt = out.ObservedAt.UTC()
	n.end = n.end.UTC()
	n.until = n.until.UTC()
	if n.end.After(out.ObservedAt) || !out.ObservedAt.Before(n.until) {
		return out, n, ph.ErrChanged
	}
	h := sha256.Sum256([]byte(ph.PageStamp(n.end) + ":" + q.PlaceID + ":" + native))
	n.source = hex.EncodeToString(h[:])
	if q.Cursor != nil && (n.source != q.Cursor.Source || ph.PageStamp(n.until) != q.Cursor.ValidUntil) {
		return out, n, ph.ErrChanged
	}
	out.SchemaVersion, out.PlaceID, out.WindowDays = ph.PageSchemaVersion, q.PlaceID, ph.WindowDays
	out.AuthorMode = "NONE"
	if q.WithAuthor {
		out.AuthorMode = "PUBLIC_PROFILE"
	}
	out.WindowEnd, out.WindowStart, out.ValidUntil = n.end, n.end.Add(-ph.WindowDays*24*time.Hour), n.until
	out.SourceVersion = n.source
	if len(out.Items) > ph.PageSize {
		out.Items = out.Items[:ph.PageSize]
		last := out.Items[len(out.Items)-1]
		raw, e := ph.EncodePageCursor(ph.PageCursor{Version: 1, PlaceID: q.PlaceID, WithAuthor: q.WithAuthor, WindowEnd: ph.PageStamp(n.end), ValidUntil: ph.PageStamp(n.until), BeforeAt: ph.PageStamp(last.PublishedAt), BeforeID: last.ID, Source: n.source})
		if e != nil {
			return out, n, ph.ErrUnavailable
		}
		out.NextCursor = &raw
	}
	if ph.ValidatePage(out, q) != nil {
		return out, n, ph.ErrUnavailable
	}
	// Repeat the ACL-filtered source in one native FINAL statement, using the
	// original captured window/deadline. The final statement also supplies its
	// wall and original Session ID+111 generation. A successful current session
	// cannot substitute for a withdrawn resource or author source.
	finalSQL := historyPageFinalSQL()
	if finalSQL == "" {
		return out, n, ph.ErrUnavailable
	}
	var now time.Time
	var current bool
	var finalUntil *time.Time
	var finalNative *string
	e = s.pool.QueryRow(ctx, finalSQL, q.PlaceID, viewer, a.SessionDigest[:], a.Actor.AccountType, s.devPhoneEnabled, q.WithAuthor, n.end, n.until, before, id, n.sessionID, n.generation).Scan(&now, &finalUntil, &finalNative, &current)
	if e != nil || ctx.Err() != nil {
		return out, n, ph.ErrUnavailable
	}
	if !current {
		return out, n, identity.ErrUnauthorized
	}
	if finalNative == nil || *finalNative == "" || finalUntil == nil || !finalUntil.Equal(n.until) || !now.Before(n.until) {
		return out, n, ph.ErrChanged
	}
	finalHash := sha256.Sum256([]byte(ph.PageStamp(n.end) + ":" + q.PlaceID + ":" + *finalNative))
	if hex.EncodeToString(finalHash[:]) != n.source {
		return out, n, ph.ErrChanged
	}
	return out, n, nil
}
