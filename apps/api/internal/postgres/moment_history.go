package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

var _ content.MomentHistoryStore = (*Store)(nil)

type momentHistoryCursor struct {
	Version                 int  `json:"version"`
	Own                     bool `json:"own"`
	Query, Reader, Source   string
	Snapshot, Until, Before time.Time
	ID                      string
}

type momentHistoryNative struct {
	observed, snapshot, until, cursorUntil time.Time
	source, reader, session                string
	generation                             int64
	ids                                    []string
	times                                  []time.Time
}

type momentHistoryProof struct {
	store  *Store
	access content.PublicMomentAccess
	query  content.MomentHistoryQuery
	own    bool
	native momentHistoryNative
	wire   [32]byte
}

func historyKey() ([]byte, error) {
	humanWriteSigning.Do(func() { _, humanWriteSigning.err = rand.Read(humanWriteSigning.key[:]) })
	if humanWriteSigning.err != nil {
		return nil, content.ErrUnavailable
	}
	return humanWriteSigning.key[:], nil
}
func historyQueryHash(q content.MomentHistoryQuery, own bool) string {
	q.Cursor = ""
	b, _ := json.Marshal([]any{"moment-history-query-v1", own, q})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func encodeMomentHistoryCursor(c momentHistoryCursor) (string, error) {
	k, e := historyKey()
	if e != nil {
		return "", e
	}
	b, e := json.Marshal(c)
	if e != nil {
		return "", content.ErrUnavailable
	}
	h := hmac.New(sha256.New, k)
	_, _ = h.Write(b)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(h.Sum(nil)), nil
}
func decodeMomentHistoryCursor(raw string, q content.MomentHistoryQuery, own bool) (*momentHistoryCursor, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 4096 || strings.Count(raw, ".") != 1 {
		return nil, content.ErrInvalid
	}
	p := strings.Split(raw, ".")
	b, e := base64.RawURLEncoding.Strict().DecodeString(p[0])
	if e != nil {
		return nil, content.ErrInvalid
	}
	sig, e := base64.RawURLEncoding.Strict().DecodeString(p[1])
	if e != nil {
		return nil, content.ErrInvalid
	}
	k, e := historyKey()
	if e != nil {
		return nil, e
	}
	h := hmac.New(sha256.New, k)
	_, _ = h.Write(b)
	if !hmac.Equal(sig, h.Sum(nil)) {
		return nil, content.ErrMomentHistoryChanged
	}
	var c momentHistoryCursor
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil {
		return nil, content.ErrInvalid
	}
	canonical, _ := json.Marshal(c)
	if !hmac.Equal(b, canonical) || c.Version != 1 || c.Own != own || c.Query != historyQueryHash(q, own) || (c.Before.IsZero() && c.ID != "") || (!c.Before.IsZero() && !validHumanMomentID(c.ID)) || len(c.Reader) != 64 || len(c.Source) != 64 || c.Snapshot.IsZero() || c.Before.After(c.Snapshot) || !c.Until.After(c.Snapshot) || c.Until.Sub(c.Snapshot) > 10*time.Minute {
		return nil, content.ErrInvalid
	}
	if !time.Now().UTC().Before(c.Until) {
		return nil, content.ErrMomentHistoryChanged
	}
	return &c, nil
}

// Session idle_expires_at is checked on every read but is deliberately absent
// from the immutable reader fingerprint: Authenticate's ordinary idle renewal
// must not retire a cursor. Credentials, Session ID and 111 generation are bound.
const historySessionSQL = `SELECT clock_timestamp(),current_database()||':'||(SELECT oid::text FROM pg_database WHERE datname=current_database()),
 a.xmin::text,se.id::text,se.authorization_generation,se.created_at,se.expires_at,se.idle_expires_at,se.authentication_method
 FROM accounts a JOIN sessions se ON se.account_id=a.id WHERE a.id=$1 AND a.account_type='person' AND a.status='active'
 AND se.token_sha256=$2 AND se.revoked_at IS NULL AND se.expires_at>clock_timestamp() AND se.idle_expires_at>clock_timestamp()
 AND($3::boolean OR se.authentication_method<>'dev_phone')`

func (s *Store) momentHistoryReader(ctx context.Context, tx pgx.Tx, a content.PublicMomentAccess) (momentHistoryNative, error) {
	var n momentHistoryNative
	if a.Actor.ID == "" {
		if a.SessionDigest != ([32]byte{}) || a.Actor.AccountType != "" {
			return n, identity.ErrUnauthorized
		}
		var origin string
		if tx.QueryRow(ctx, `SELECT clock_timestamp(),current_database()||':'||(SELECT oid::text FROM pg_database WHERE datname=current_database())`).Scan(&n.observed, &origin) != nil {
			return n, content.ErrUnavailable
		}
		h := sha256.Sum256([]byte("moment-history-anonymous-v1:" + origin))
		n.reader = hex.EncodeToString(h[:])
		n.cursorUntil = n.observed.Add(10 * time.Minute)
		n.until = n.observed.Add(time.Minute)
		return n, nil
	}
	if a.Actor.AccountType != "person" || !validHumanMomentID(a.Actor.ID) || a.SessionDigest == ([32]byte{}) {
		return n, identity.ErrUnauthorized
	}
	var origin, epoch, method string
	var created, expires, idle time.Time
	e := tx.QueryRow(ctx, historySessionSQL, a.Actor.ID, a.SessionDigest[:], s.devPhoneEnabled).Scan(&n.observed, &origin, &epoch, &n.session, &n.generation, &created, &expires, &idle, &method)
	if errors.Is(e, pgx.ErrNoRows) {
		return n, identity.ErrUnauthorized
	}
	if e != nil {
		return n, content.ErrUnavailable
	}
	if first, valid, present := identity.OriginalHumanControlFrame(ctx, a.SessionDigest, a.Actor); present && (!valid || first.ID != n.session || first.Generation != n.generation) {
		return n, identity.ErrControlChanged
	}
	b, _ := json.Marshal([]any{"moment-history-reader-v1", origin, a.Actor.ID, epoch, n.session, n.generation, created, expires, method, hex.EncodeToString(a.SessionDigest[:])})
	h := sha256.Sum256(b)
	n.reader = hex.EncodeToString(h[:])
	n.cursorUntil = n.observed.Add(10 * time.Minute)
	if expires.Before(n.cursorUntil) {
		n.cursorUntil = expires
	}
	if idle.Before(n.cursorUntil) {
		n.cursorUntil = idle
	}
	n.until = n.observed.Add(time.Minute)
	if expires.Before(n.until) {
		n.until = expires
	}
	if idle.Before(n.until) {
		n.until = idle
	}
	return n, nil
}

const ownHistorySQL = `WITH eligible AS MATERIALIZED(SELECT m.id,m.created_at,m.xmin::text token FROM moments m
 WHERE m.author_account_id=$1 AND m.status<>'withdrawn' AND($2='active' OR m.status=$2) AND m.created_at<=$3),
 page AS MATERIALIZED(SELECT id,created_at FROM eligible WHERE $4::timestamptz IS NULL OR(created_at,id)<($4::timestamptz,$5::uuid)
 ORDER BY created_at DESC,id DESC LIMIT 21)
 SELECT encode(sha256(convert_to(coalesce((SELECT string_agg(id::text||':'||token,',' ORDER BY id) FROM eligible),''),'UTF8')),'hex'),
 coalesce((SELECT jsonb_agg(jsonb_build_object('id',id,'at',created_at) ORDER BY created_at DESC,id DESC) FROM page),'[]'::jsonb)`

func publicHistorySQL() string {
	base := publicMomentContextReadSQL(true)
	_, tail, ok := strings.Cut(base, "FROM moments m JOIN accounts a")
	if !ok || strings.Count(tail, "WHERE m.id=$1 AND m.status='published'") != 1 {
		return ""
	}
	tail = "FROM moments m JOIN accounts a" + strings.Replace(tail, "WHERE m.id=$1 AND m.status='published'", "WHERE m.status='published'", 1)
	// Reuse the v4 current City/Place/PUBLIC/Block/Session predicates exactly.
	return `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at),eligible AS MATERIALIZED(
 SELECT m.id,m.published_at,` + momentPublishableMediaDeadlineSQL("m") + ` deadline,
 jsonb_build_array(birdtie_moment_public_binding(m.id,$2,cl.at),` + momentMediaReadBindingSQL("m") + `,m.xmin::text,a.xmin::text,` + publicMomentAuthorBindingSQL("a") + `,
 ''::text)::text token ` + tail + `
 AND($7='' OR EXISTS(SELECT 1 FROM birdtie_moment_locations_public(m.id,cl.at) location WHERE location.city_id=$7)) AND($8::uuid IS NULL OR m.author_account_id=$8)
 AND($9::timestamptz IS NULL OR m.published_at>=$9) AND($10::timestamptz IS NULL OR m.published_at<$10)
 AND m.published_at<=$11
 AND($6 IN('city','author') OR($6='following' AND EXISTS(SELECT 1 FROM follows f WHERE f.follower_account_id=$2 AND f.person_account_id=a.id))
 OR($6='friends' AND EXISTS(SELECT 1 FROM person_ties t JOIN connection_requests request ON request.id=t.request_id
 AND request.scope='friend' AND request.state='accepted' AND LEAST(request.sender_account_id,request.recipient_account_id)=t.person_a_account_id
 AND GREATEST(request.sender_account_id,request.recipient_account_id)=t.person_b_account_id
 WHERE t.status='active' AND LEAST($2::uuid,a.id)=t.person_a_account_id AND GREATEST($2::uuid,a.id)=t.person_b_account_id)))),
 page AS MATERIALIZED(SELECT id,published_at FROM eligible WHERE $12::timestamptz IS NULL OR(published_at,id)<($12::timestamptz,$13::uuid)
 ORDER BY published_at DESC,id DESC LIMIT 21)
 SELECT encode(sha256(convert_to(jsonb_build_array(
 (SELECT coalesce(jsonb_agg(jsonb_build_array(id,token) ORDER BY id),'[]'::jsonb) FROM eligible),
 (SELECT jsonb_build_array(c.id,c.xmin::text) FROM cities c WHERE c.id=nullif($7,'')),
 (SELECT jsonb_build_array(a.id,a.xmin::text,` + publicMomentAuthorBindingSQL("a") + `) FROM accounts a WHERE a.id=$8),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(ep.owner_account_id,ep.target_account_id,ep.version,ep.xmin::text) ORDER BY ep.owner_account_id,ep.target_account_id),'[]'::jsonb)
 FROM person_block_control_epochs ep WHERE $2::uuid IS NOT NULL AND(ep.owner_account_id=$2 OR ep.target_account_id=$2)),
 CASE WHEN $6='following' THEN(SELECT coalesce(jsonb_agg(jsonb_build_array(f.id,f.person_account_id,f.xmin::text) ORDER BY f.id),'[]'::jsonb)
 FROM follows f WHERE f.follower_account_id=$2 AND f.person_account_id IS NOT NULL) ELSE '[]'::jsonb END,
 CASE WHEN $6='friends' THEN(SELECT coalesce(jsonb_agg(jsonb_build_array(t.id,t.status,t.xmin::text,request.id,request.xmin::text) ORDER BY t.id),'[]'::jsonb)
 FROM person_ties t JOIN connection_requests request ON request.id=t.request_id WHERE t.person_a_account_id=$2 OR t.person_b_account_id=$2) ELSE '[]'::jsonb END)::text,'UTF8')),'hex'),
 coalesce((SELECT jsonb_agg(jsonb_build_object('id',id,'at',published_at) ORDER BY published_at DESC,id DESC) FROM page),'[]'::jsonb),
 (SELECT min(deadline) FROM eligible),
 ($7='' OR EXISTS(SELECT 1 FROM cities c CROSS JOIN cl WHERE c.id=$7 AND c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>cl.at))),
 ($6<>'author' OR EXISTS(SELECT 1 FROM accounts a WHERE a.id=$8 AND a.account_type='person' AND a.status='active' AND ` + publicMomentAuthorProfileSQL("a") + ` IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE $2::uuid IS NOT NULL AND((b.blocker_account_id=$2 AND b.blocked_account_id=a.id) OR(b.blocker_account_id=a.id AND b.blocked_account_id=$2)))))
 FROM cl WHERE $1::uuid IS NULL AND $5::boolean`
}

func (s *Store) readMomentHistoryNative(ctx context.Context, tx pgx.Tx, a content.PublicMomentAccess, q content.MomentHistoryQuery, own bool, c *momentHistoryCursor) (momentHistoryNative, error) {
	n, e := s.momentHistoryReader(ctx, tx, a)
	if e != nil {
		return n, e
	}
	n.snapshot = n.observed
	var before, id any
	if c != nil {
		if c.Reader != n.reader || !n.observed.Before(c.Until) {
			return n, content.ErrMomentHistoryChanged
		}
		n.snapshot = c.Snapshot
		n.cursorUntil = c.Until
		if !c.Before.IsZero() {
			before, id = c.Before, c.ID
		}
	}
	var raw []byte
	var source string
	if own {
		e = tx.QueryRow(ctx, ownHistorySQL, a.Actor.ID, q.Status, n.snapshot, before, id).Scan(&source, &raw)
	} else {
		var viewer, author, from, to any
		if a.Actor.ID != "" {
			viewer = a.Actor.ID
		}
		if q.AuthorID != "" {
			author = q.AuthorID
		}
		if q.From != "" {
			from, _ = time.Parse(time.RFC3339Nano, q.From)
		}
		if q.To != "" {
			to, _ = time.Parse(time.RFC3339Nano, q.To)
		}
		var deadline *time.Time
		var cityOK, authorOK bool
		sql := publicHistorySQL()
		if sql == "" {
			return n, content.ErrUnavailable
		}
		e = tx.QueryRow(ctx, sql, nil, viewer, a.SessionDigest[:], s.devPhoneEnabled, true, q.Scope, q.CityID, author, from, to, n.snapshot, before, id).Scan(&source, &raw, &deadline, &cityOK, &authorOK)
		if e == nil && (!cityOK || !authorOK) {
			return n, content.ErrNotFound
		}
		if deadline != nil && deadline.Before(n.until) {
			n.until = *deadline
		}
	}
	if e != nil {
		return n, content.ErrUnavailable
	}
	var items []struct {
		ID string    `json:"id"`
		At time.Time `json:"at"`
	}
	if json.Unmarshal(raw, &items) != nil || len(items) > 21 {
		return n, content.ErrUnavailable
	}
	for _, item := range items {
		n.ids = append(n.ids, item.ID)
		n.times = append(n.times, item.At.UTC())
	}
	h := sha256.Sum256([]byte("moment-history-source-v1:" + historyQueryHash(q, own) + ":" + n.reader + ":" + n.snapshot.UTC().Format(time.RFC3339Nano) + ":" + source))
	n.source = hex.EncodeToString(h[:])
	if c != nil && c.Source != n.source {
		return n, content.ErrMomentHistoryChanged
	}
	n.observed, n.snapshot, n.until, n.cursorUntil = n.observed.UTC(), n.snapshot.UTC(), n.until.UTC(), n.cursorUntil.UTC()
	if !n.until.After(n.observed) || !n.cursorUntil.After(n.observed) {
		return n, content.ErrMomentHistoryChanged
	}
	return n, nil
}

func historyWire(v any) ([32]byte, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return [32]byte{}, e
	}
	return sha256.Sum256(b), nil
}
func historyNext(n momentHistoryNative, q content.MomentHistoryQuery, own bool) (*string, error) {
	if len(n.ids) <= content.MomentHistoryPageSize {
		return nil, nil
	}
	i := content.MomentHistoryPageSize - 1
	raw, e := encodeMomentHistoryCursor(momentHistoryCursor{Version: 1, Own: own, Query: historyQueryHash(q, own), Reader: n.reader, Source: n.source, Snapshot: n.snapshot, Until: n.cursorUntil, Before: n.times[i], ID: n.ids[i]})
	if e != nil {
		return nil, e
	}
	return &raw, nil
}

func historyRefresh(n momentHistoryNative, q content.MomentHistoryQuery, own bool, c *momentHistoryCursor) (string, error) {
	refresh := momentHistoryCursor{Version: 1, Own: own, Query: historyQueryHash(q, own), Reader: n.reader, Source: n.source, Snapshot: n.snapshot, Until: n.cursorUntil}
	if c != nil {
		refresh.Before, refresh.ID = c.Before, c.ID
	}
	return encodeMomentHistoryCursor(refresh)
}

func (s *Store) beginMomentHistory(ctx context.Context, a content.PublicMomentAccess, q content.MomentHistoryQuery, own bool) (pgx.Tx, *momentHistoryCursor, error) {
	if s == nil || s.pool == nil || ctx == nil || ctx.Err() != nil {
		return nil, nil, content.ErrUnavailable
	}
	if content.ValidateMomentHistoryQuery(q, own) != nil {
		return nil, nil, content.ErrInvalid
	}
	if own && a.Actor.ID == "" || (q.Scope == "following" || q.Scope == "friends") && a.Actor.ID == "" {
		return nil, nil, identity.ErrUnauthorized
	}
	c, e := decodeMomentHistoryCursor(q.Cursor, q, own)
	if e != nil {
		return nil, nil, e
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, nil, content.ErrUnavailable
	}
	return tx, c, nil
}

func (s *Store) ReadOwnMomentHistory(ctx context.Context, d [32]byte, a identity.Actor, q content.MomentHistoryQuery) (content.OwnMomentHistoryPage, error) {
	access := content.PublicMomentAccess{Actor: a, SessionDigest: d}
	tx, c, e := s.beginMomentHistory(ctx, access, q, true)
	if e != nil {
		return content.OwnMomentHistoryPage{}, e
	}
	defer tx.Rollback(context.Background())
	n, e := s.readMomentHistoryNative(ctx, tx, access, q, true, c)
	if e != nil {
		return content.OwnMomentHistoryPage{}, e
	}
	p := content.OwnMomentHistoryPage{SchemaVersion: "owner-moment-history-v1", Status: q.Status, Items: []content.Moment{}, ObservedAt: n.observed, SnapshotAt: n.snapshot, ValidUntil: n.until}
	for _, id := range n.ids[:min(len(n.ids), content.MomentHistoryPageSize)] {
		m, e := scanMoment(tx.QueryRow(ctx, `SELECT `+momentColumns+` FROM moments m WHERE m.id=$1 AND m.author_account_id=$2 AND m.status<>'withdrawn'`, id, a.ID))
		if e != nil {
			return content.OwnMomentHistoryPage{}, content.ErrMomentHistoryChanged
		}
		p.Items = append(p.Items, m)
	}
	p.NextCursor, e = historyNext(n, q, true)
	if e != nil {
		return content.OwnMomentHistoryPage{}, e
	}
	p.RefreshCursor, e = historyRefresh(n, q, true, c)
	if e != nil {
		return content.OwnMomentHistoryPage{}, e
	}
	if content.ValidateOwnMomentHistoryPage(p, q, a.ID) != nil {
		return content.OwnMomentHistoryPage{}, content.ErrUnavailable
	}
	wire, e := historyWire(p)
	if e != nil {
		return content.OwnMomentHistoryPage{}, content.ErrUnavailable
	}
	proof := &momentHistoryProof{s, access, q, true, n, wire}
	p.ResponseProof = proof
	if e = s.validateMomentHistoryNative(ctx, tx, proof); e != nil {
		return content.OwnMomentHistoryPage{}, e
	}
	return p, nil
}

func (s *Store) ReadPublicMomentHistory(ctx context.Context, a content.PublicMomentAccess, q content.MomentHistoryQuery) (content.PublicMomentHistoryPage, error) {
	tx, c, e := s.beginMomentHistory(ctx, a, q, false)
	if e != nil {
		return content.PublicMomentHistoryPage{}, e
	}
	defer tx.Rollback(context.Background())
	n, e := s.readMomentHistoryNative(ctx, tx, a, q, false, c)
	if e != nil {
		return content.PublicMomentHistoryPage{}, e
	}
	p := content.PublicMomentHistoryPage{SchemaVersion: "public-moment-history-v1", Scope: q.Scope, CityID: q.CityID, AuthorID: q.AuthorID, From: q.From, To: q.To, Items: []content.PublicMomentWithContext{}, ObservedAt: n.observed, SnapshotAt: n.snapshot, ValidUntil: n.until}
	for _, id := range n.ids[:min(len(n.ids), content.MomentHistoryPageSize)] {
		r, e := readPublicMomentContextTx(ctx, tx, a, id, true, true, s.devPhoneEnabled)
		if e != nil {
			return content.PublicMomentHistoryPage{}, e
		}
		if r.View.ValidUntil.Before(p.ValidUntil) {
			p.ValidUntil = r.View.ValidUntil
		}
		p.Items = append(p.Items, r.View)
	}
	// All emitted rows share the current page deadline; this does not extend an
	// original photograph lease or imply location for a city-only text row.
	for i := range p.Items {
		p.Items[i].ValidUntil = p.ValidUntil
		if p.Items[i].Photo != nil {
			copy := *p.Items[i].Photo
			if copy.ExpiresAt.After(p.ValidUntil) {
				copy.ExpiresAt = p.ValidUntil
			}
			p.Items[i].Photo = &copy
		}
	}
	n.until = p.ValidUntil
	p.NextCursor, e = historyNext(n, q, false)
	if e != nil {
		return content.PublicMomentHistoryPage{}, e
	}
	p.RefreshCursor, e = historyRefresh(n, q, false, c)
	if e != nil {
		return content.PublicMomentHistoryPage{}, e
	}
	if content.ValidatePublicMomentHistoryPage(p, q) != nil {
		return content.PublicMomentHistoryPage{}, content.ErrUnavailable
	}
	wire, e := historyWire(p)
	if e != nil {
		return content.PublicMomentHistoryPage{}, content.ErrUnavailable
	}
	proof := &momentHistoryProof{s, a, q, false, n, wire}
	p.ResponseProof = proof
	if e = s.validateMomentHistoryNative(ctx, tx, proof); e != nil {
		return content.PublicMomentHistoryPage{}, e
	}
	return p, nil
}

func (s *Store) validateMomentHistoryNative(ctx context.Context, tx pgx.Tx, p *momentHistoryProof) error {
	c := &momentHistoryCursor{Reader: p.native.reader, Source: p.native.source, Snapshot: p.native.snapshot, Until: p.native.cursorUntil}
	// Read the original query's page and complete currently eligible source. A
	// source or relationship ABA retires the original response, including empty.
	if old, e := decodeMomentHistoryCursor(p.query.Cursor, p.query, p.own); e != nil {
		return e
	} else if old != nil {
		c.Before, c.ID = old.Before, old.ID
	}
	n, e := s.readMomentHistoryNative(ctx, tx, p.access, p.query, p.own, c)
	if e != nil {
		return e
	}
	if n.source != p.native.source || n.reader != p.native.reader || n.session != p.native.session || n.generation != p.native.generation || !reflect.DeepEqual(n.ids, p.native.ids) || n.until.Before(p.native.until) || !n.observed.Before(p.native.until) {
		return content.ErrMomentHistoryChanged
	}
	// Re-read the initial Session after the source materialization. There are no
	// locks, authentication/idle renewal or other waits after this native fence.
	fresh, e := s.momentHistoryReader(ctx, tx, p.access)
	if e != nil {
		return e
	}
	if fresh.reader != p.native.reader || !fresh.observed.Before(p.native.until) || fresh.until.Before(p.native.until) {
		return content.ErrMomentHistoryChanged
	}
	return nil
}

func (s *Store) revalidateMomentHistory(ctx context.Context, a content.PublicMomentAccess, q content.MomentHistoryQuery, own bool, v any, rawProof any) error {
	p, ok := rawProof.(*momentHistoryProof)
	if !ok || p == nil || p.store != s || p.own != own || p.access.Actor != a.Actor || p.access.SessionDigest != a.SessionDigest || p.query != q {
		return content.ErrUnavailable
	}
	w, e := historyWire(v)
	if e != nil || w != p.wire {
		return content.ErrUnavailable
	}
	tx, _, e := s.beginMomentHistory(ctx, a, q, own)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	return s.validateMomentHistoryNative(ctx, tx, p)
}
func (s *Store) RevalidateOwnMomentHistory(ctx context.Context, d [32]byte, a identity.Actor, q content.MomentHistoryQuery, p content.OwnMomentHistoryPage) error {
	return s.revalidateMomentHistory(ctx, content.PublicMomentAccess{Actor: a, SessionDigest: d}, q, true, p, p.ResponseProof)
}
func (s *Store) RevalidatePublicMomentHistory(ctx context.Context, a content.PublicMomentAccess, q content.MomentHistoryQuery, p content.PublicMomentHistoryPage) error {
	return s.revalidateMomentHistory(ctx, a, q, false, p, p.ResponseProof)
}

// Compile-time diagnostic formatting must never expose a reader's source IDs.
func (p momentHistoryProof) String() string {
	return fmt.Sprintf("current Moment history proof (%t)", p.own)
}
