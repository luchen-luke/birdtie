package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

var _ content.JourneyHistoryStore = (*Store)(nil)

type journeyHistoryCursor struct {
	Version                              int
	Owner, Reader, Query, Source, Before string
	Until                                time.Time
}

func historyReader(f humanWriteFrame) string {
	return content.MomentControlHash([]any{hex.EncodeToString(f.d[:]), f.session.ID, f.session.Generation, f.a.ID})
}
func journeyHistoryQueryHash(q content.JourneyHistoryQuery) string {
	q.Cursor = ""
	return content.MomentControlHash(q)
}
func encodeJourneyHistoryCursor(v journeyHistoryCursor) (string, error) {
	k, e := historyKey()
	if e != nil {
		return "", e
	}
	b, _ := json.Marshal(v)
	h := hmac.New(sha256.New, k)
	h.Write(b)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(h.Sum(nil)), nil
}
func decodeJourneyHistoryCursor(raw string) (*journeyHistoryCursor, error) {
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
	h.Write(b)
	if !hmac.Equal(sig, h.Sum(nil)) {
		return nil, identity.ErrControlChanged
	}
	var v journeyHistoryCursor
	if json.Unmarshal(b, &v) != nil || v.Version != 1 || !identity.HumanControlUUID(v.Owner) || !identity.HumanControlUUID(v.Before) || v.Until.IsZero() {
		return nil, content.ErrInvalid
	}
	canonical, e := encodeJourneyHistoryCursor(v)
	if e != nil || canonical != raw {
		return nil, content.ErrInvalid
	}
	return &v, nil
}

const journeyHistorySourceGate = `EXISTS(SELECT 1 FROM legacy_social_history_facts f JOIN legacy_social_history_runs r ON r.run_id=f.run_id JOIN legacy_entity_mappings u ON u.run_id=r.run_id AND u.entity_kind='user' AND u.target_id=f.owner_account_id AND u.owner_account_id=f.owner_account_id WHERE f.kind='thread' AND f.target_id=t.id AND f.owner_account_id=t.owner_account_id)`

func readJourneyHistoryTx(ctx context.Context, f humanWriteFrame, q content.JourneyHistoryQuery) (content.JourneyHistoryPage, error) {
	v := content.JourneyHistoryPage{SchemaVersion: content.JourneyHistorySchema, Threads: []content.JourneyHistoryThread{}, Messages: []content.JourneyHistoryMessage{}}
	if e := f.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&v.ObservedAt); e != nil {
		return v, content.ErrUnavailable
	}
	v.ObservedAt = v.ObservedAt.UTC()
	v.ValidUntil = v.ObservedAt.Add(30 * time.Second)
	if e := ownedContextLeaseTx(ctx, f, &v.ValidUntil); e != nil {
		return v, e
	}
	if q.Preferences {
		var p content.OriginalNotificationPreferences
		var raw string
		p.Settings = map[string]bool{}
		var chat, request, comment, follow, praise, journey, system, preview bool
		e := f.tx.QueryRow(ctx, `SELECT p.source_version,p.source_updated_at,p.chat_push,p.request_push,p.comment_push,p.follow_push,p.praise_save_push,p.journey_push,p.system_push,p.preview_enabled,p.collaboration_push,p.time_zone,p.quiet_start,p.quiet_end,jsonb_build_array(p.xmin::text,f.xmin::text,r.xmin::text)::text FROM personal_notification_preferences p JOIN legacy_social_history_facts f ON f.kind='preferences' AND f.target_id=p.id AND f.owner_account_id=p.owner_account_id JOIN legacy_social_history_runs r ON r.run_id=f.run_id WHERE p.owner_account_id=$1`, f.a.ID).Scan(&p.SourceVersion, &p.UpdatedAt, &chat, &request, &comment, &follow, &praise, &journey, &system, &preview, &p.CollaborationPush, &p.TimeZone, &p.QuietStart, &p.QuietEnd, &raw)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return v, content.ErrUnavailable
		}
		if e == nil {
			p.Settings = map[string]bool{"chatPush": chat, "requestPush": request, "commentPush": comment, "followPush": follow, "praiseSavePush": praise, "journeyPush": journey, "systemPush": system, "previewEnabled": preview}
			v.Preferences = &p
		}
		v.SourceVersion = content.MomentControlHash([]any{"original-preferences", f.a.ID, raw})
		return v, nil
	}
	cursor, e := decodeJourneyHistoryCursor(q.Cursor)
	if e != nil {
		return v, e
	}
	before := "ffffffff-ffff-ffff-ffff-ffffffffffff"
	var source string
	e = f.tx.QueryRow(ctx, `SELECT coalesce(jsonb_agg(jsonb_build_array(t.id,t.xmin::text,f.xmin::text,r.xmin::text,j.xmin::text,j.status)ORDER BY t.id),'[]')::text FROM personal_journey_history_threads t JOIN legacy_social_history_facts f ON f.kind='thread' AND f.target_id=t.id AND f.owner_account_id=t.owner_account_id JOIN legacy_social_history_runs r ON r.run_id=f.run_id LEFT JOIN journeys j ON j.id=t.journey_id AND j.owner_account_id=t.owner_account_id WHERE t.owner_account_id=$1 AND($2='' OR t.journey_id=NULLIF($2,'')::uuid)`, f.a.ID, q.JourneyID).Scan(&source)
	if e != nil {
		return v, content.ErrUnavailable
	}
	v.SourceVersion = content.MomentControlHash([]any{source, journeyHistoryQueryHash(q)})
	if cursor != nil {
		if cursor.Owner != f.a.ID || cursor.Reader != historyReader(f) || cursor.Query != journeyHistoryQueryHash(q) || cursor.Source != v.SourceVersion || !cursor.Until.After(v.ObservedAt) {
			return v, identity.ErrControlChanged
		}
		before = cursor.Before
		if cursor.Until.Before(v.ValidUntil) {
			v.ValidUntil = cursor.Until
		}
	}
	rows, e := f.tx.Query(ctx, `SELECT t.id,coalesce(j.id::text,''),CASE WHEN t.source_draft_context_id IS NOT NULL THEN 'DRAFT_CONTEXT' WHEN j.id IS NULL THEN 'JOURNEY_UNAVAILABLE' ELSE 'JOURNEY' END,t.summary,t.source_status,t.source_context_revision,t.source_created_at,t.source_updated_at FROM personal_journey_history_threads t LEFT JOIN journeys j ON j.id=t.journey_id AND j.owner_account_id=t.owner_account_id AND j.status<>'withdrawn' WHERE t.owner_account_id=$1 AND `+journeyHistorySourceGate+` AND($2='' OR t.journey_id=NULLIF($2,'')::uuid)AND($3='' OR t.id=NULLIF($3,'')::uuid)AND t.id<$4::uuid ORDER BY t.id DESC LIMIT 21`, f.a.ID, q.JourneyID, q.ThreadID, before)
	if e != nil {
		return v, content.ErrUnavailable
	}
	for rows.Next() {
		var t content.JourneyHistoryThread
		if e = rows.Scan(&t.ID, &t.JourneyID, &t.ContextKind, &t.Summary, &t.SourceStatus, &t.SourceContextRevision, &t.CreatedAt, &t.UpdatedAt); e != nil {
			break
		}
		v.Threads = append(v.Threads, t)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return v, content.ErrUnavailable
	}
	if q.ThreadID != "" {
		if len(v.Threads) != 1 {
			return v, content.ErrNotFound
		}
		rows, e = f.tx.Query(ctx, `SELECT m.id,m.role,m.body,m.source_created_at FROM personal_journey_history_messages m JOIN personal_journey_history_threads t ON t.id=m.history_thread_id JOIN legacy_social_history_facts f ON f.kind='message' AND f.target_id=m.id AND f.owner_account_id=t.owner_account_id WHERE m.history_thread_id=$1 AND t.owner_account_id=$2 ORDER BY m.source_created_at,m.id LIMIT 1001`, q.ThreadID, f.a.ID)
		if e != nil {
			return v, content.ErrUnavailable
		}
		size := 0
		for rows.Next() {
			var m content.JourneyHistoryMessage
			if e = rows.Scan(&m.ID, &m.Role, &m.Body, &m.CreatedAt); e != nil {
				break
			}
			size += len(m.Body)
			v.Messages = append(v.Messages, m)
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil || len(v.Messages) > 1000 || size > 2<<20 {
			return v, content.ErrUnavailable
		}
		v.SourceVersion = content.MomentControlHash([]any{v.SourceVersion, v.Threads, v.Messages})
	}
	if len(v.Threads) > 20 {
		v.Threads = v.Threads[:20]
		c, e := encodeJourneyHistoryCursor(journeyHistoryCursor{Version: 1, Owner: f.a.ID, Reader: historyReader(f), Query: journeyHistoryQueryHash(q), Source: v.SourceVersion, Before: v.Threads[19].ID, Until: v.ValidUntil})
		if e != nil {
			return v, e
		}
		v.NextCursor = &c
	}
	return v, nil
}

type journeyHistoryProof struct {
	s     *Store
	d     [32]byte
	a     identity.Actor
	frame identity.HumanControlFrame
	q     content.JourneyHistoryQuery
	v     content.JourneyHistoryPage
}

func (p *journeyHistoryProof) RevalidateHumanControl(ctx context.Context) error {
	if !p.v.ValidUntil.After(time.Now().UTC()) {
		return identity.ErrControlChanged
	}
	ctx = identity.WithHumanControlFrame(ctx, p.d, p.a, p.frame)
	_, e := p.s.readJourneyHistory(ctx, p.d, p.a, p.q, &p.v)
	return e
}
func (s *Store) readJourneyHistory(ctx context.Context, d [32]byte, a identity.Actor, q content.JourneyHistoryQuery, expected *content.JourneyHistoryPage) (content.JourneyHistoryPage, error) {
	empty := content.JourneyHistoryPage{}
	if q.JourneyID != "" && !identity.HumanControlUUID(q.JourneyID) || q.ThreadID != "" && !identity.HumanControlUUID(q.ThreadID) || len(q.Cursor) > 4096 || q.Preferences && (q.Cursor != "" || q.JourneyID != "" || q.ThreadID != "") {
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
	v, e := readJourneyHistoryTx(ctx, f, q)
	if e != nil {
		return empty, e
	}
	if expected != nil {
		if v.SourceVersion != expected.SourceVersion || !expected.ValidUntil.After(v.ObservedAt) || v.ValidUntil.Before(expected.ValidUntil) {
			return empty, identity.ErrControlChanged
		}
		v.ObservedAt, v.ValidUntil = expected.ObservedAt, expected.ValidUntil
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, v.ValidUntil, func() error {
		fresh, e := readJourneyHistoryTx(ctx, f, q)
		if e != nil {
			return e
		}
		if fresh.SourceVersion != v.SourceVersion || fresh.ValidUntil.Before(v.ValidUntil) {
			return identity.ErrControlChanged
		}
		return nil
	})); e != nil {
		return empty, e
	}
	v.ResponseProof = &journeyHistoryProof{s, d, a, frame, q, v}
	return v, nil
}
func (s *Store) ReadJourneyHistory(ctx context.Context, d [32]byte, a identity.Actor, q content.JourneyHistoryQuery) (content.JourneyHistoryPage, error) {
	return s.readJourneyHistory(ctx, d, a, q, nil)
}
