package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"strconv"
	"strings"
	"time"
)

var _ content.HumanMomentPublicationStore = (*Store)(nil)

const momentPublicationTTL = 90 * time.Second

// Only short-lived previews use a process-only key. Restart invalidates those
// previews; it does not alter the persisted native publication ledger.
var momentPublicationSigningKey, momentPublicationSigningError = newMomentPublicationSigningKey()

func newMomentPublicationSigningKey() ([32]byte, error) {
	var key [32]byte
	_, e := rand.Read(key[:])
	return key, e
}

type publicationFrame struct {
	moment                                                            content.Moment
	placeName, cityEpoch, placeEpoch, accountEpoch, session, material string
}

func (s *Store) lockPublicationFrame(ctx context.Context, tx pgx.Tx, digest [32]byte, actor identity.Actor, id string) (publicationFrame, error) {
	var f publicationFrame
	m, e := scanMoment(tx.QueryRow(ctx, `SELECT `+momentColumns+` FROM moments m WHERE m.id=$1 AND m.author_account_id=$2 FOR UPDATE OF m`, id, actor.ID))
	if errors.Is(e, pgx.ErrNoRows) {
		return f, content.ErrNotFound
	}
	if e != nil {
		return f, content.ErrUnavailable
	}
	if m.Status != "draft" || m.Visibility != "private" || m.PlaceID == "" || m.LocationPrecision != "place" {
		return f, content.ErrConflict
	}
	f.moment = m
	e = tx.QueryRow(ctx, `SELECT p.name,c.xmin::text,p.xmin::text FROM cities c JOIN places p ON p.city_id=c.id WHERE c.id=$1 AND p.id=$2 AND c.publication_status='published' AND p.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp()) AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp()) FOR SHARE OF c,p`, m.CityID, m.PlaceID).Scan(&f.placeName, &f.cityEpoch, &f.placeEpoch)
	if errors.Is(e, pgx.ErrNoRows) {
		return f, content.ErrConflict
	}
	if e != nil {
		return f, content.ErrUnavailable
	}
	if e = s.lockHumanMomentSession(ctx, tx, digest, actor.ID); e != nil {
		return f, e
	}
	e = tx.QueryRow(ctx, `SELECT a.xmin::text,ss.id FROM accounts a JOIN sessions ss ON ss.account_id=a.id WHERE a.id=$1 AND ss.token_sha256=$2`, actor.ID, digest[:]).Scan(&f.accountEpoch, &f.session)
	if e != nil {
		return f, content.ErrUnavailable
	}
	// Full source metadata stays within the current native transaction. The
	// preview publishes no private contexts and the token exposes no payload.
	var raw string
	e = tx.QueryRow(ctx, `SELECT to_jsonb(m)::text FROM moments m WHERE m.id=$1`, id).Scan(&raw)
	if e != nil {
		return f, content.ErrUnavailable
	}
	b, e := json.Marshal([]any{"SELF_MOMENT_PUBLICATION_V1", actor.ID, f.accountEpoch, f.session, raw, f.cityEpoch, f.placeEpoch})
	if e != nil {
		return f, content.ErrUnavailable
	}
	f.material = string(b)
	return f, nil
}
func publicationSnapshot(digest [32]byte, material string, issued, expiry int64) string {
	if momentPublicationSigningError != nil {
		return ""
	}
	h := hmac.New(sha256.New, momentPublicationSigningKey[:])
	fmt.Fprintf(h, "%x:%d:%d:%s", digest, issued, expiry, material)
	return fmt.Sprintf("mp1.%d.%d.%s", issued, expiry, hex.EncodeToString(h.Sum(nil)))
}
func (s *Store) finishPublication(ctx context.Context, tx pgx.Tx, digest [32]byte, actor identity.Actor, f publicationFrame, expiry int64) error {
	var alive bool
	e := tx.QueryRow(ctx, `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at) SELECT EXISTS(SELECT 1 FROM accounts a JOIN sessions ss ON ss.account_id=a.id JOIN cities c ON c.id=$4 JOIN places p ON p.id=$5 AND p.city_id=c.id CROSS JOIN cl WHERE a.id=$1 AND a.account_type='person' AND a.status='active' AND a.xmin::text=$6 AND ss.id=$7 AND ss.token_sha256=$2 AND ss.revoked_at IS NULL AND ss.expires_at>cl.at AND ss.idle_expires_at>cl.at AND ($3::boolean OR ss.authentication_method<>'dev_phone') AND c.publication_status='published' AND p.publication_status='published' AND c.xmin::text=$8 AND p.xmin::text=$9 AND (c.expires_at IS NULL OR c.expires_at>cl.at) AND (p.expires_at IS NULL OR p.expires_at>cl.at) AND cl.at<$10::timestamptz)`, actor.ID, digest[:], s.devPhoneEnabled, f.moment.CityID, f.moment.PlaceID, f.accountEpoch, f.session, f.cityEpoch, f.placeEpoch, time.Unix(0, expiry).UTC()).Scan(&alive)
	if e != nil || ctx.Err() != nil {
		return content.ErrUnavailable
	}
	if !alive {
		if e = checkHumanMomentSession(ctx, tx, digest, actor.ID, s.devPhoneEnabled); e != nil {
			return e
		}
		return content.ErrConflict
	}
	if e = tx.Commit(ctx); e != nil {
		return content.ErrUnavailable
	}
	return nil
}
func (s *Store) PreviewHumanMomentPublication(ctx context.Context, digest [32]byte, actor identity.Actor, id string) (content.MomentPublicationPreview, error) {
	var out content.MomentPublicationPreview
	if !validHumanMomentID(id) {
		return out, content.ErrInvalid
	}
	tx, e := s.beginHumanMomentTx(ctx, digest, actor)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	f, e := s.lockPublicationFrame(ctx, tx, digest, actor, id)
	if e != nil {
		return out, e
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return out, content.ErrUnavailable
	}
	expiry := now.Add(momentPublicationTTL)
	out = content.MomentPublicationPreview{MomentID: id, PlaceID: f.moment.PlaceID, CityID: f.moment.CityID, PlaceName: f.placeName, Title: f.moment.Title, Body: f.moment.Body, Revision: f.moment.Revision, Snapshot: publicationSnapshot(digest, f.material, now.UnixNano(), expiry.UnixNano()), ExpiresAt: expiry.UTC()}
	if out.Snapshot == "" {
		return content.MomentPublicationPreview{}, content.ErrUnavailable
	}
	if e = s.finishPublication(ctx, tx, digest, actor, f, expiry.UnixNano()); e != nil {
		return content.MomentPublicationPreview{}, e
	}
	return out, nil
}
func (s *Store) PublishHumanMoment(ctx context.Context, digest [32]byte, actor identity.Actor, id string, in content.MomentPublicationInput) (content.MomentPublicationReceipt, error) {
	var out content.MomentPublicationReceipt
	if !validHumanMomentID(id) || in.Revision < 1 || !in.ConfirmPublic || len(in.Snapshot) > 200 || in.Snapshot == "" {
		return out, content.ErrInvalid
	}
	tx, e := s.beginHumanMomentTx(ctx, digest, actor)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	f, e := s.lockPublicationFrame(ctx, tx, digest, actor, id)
	if e != nil {
		return out, e
	}
	if in.Revision != f.moment.Revision {
		return out, content.ErrConflict
	}
	parts := strings.Split(in.Snapshot, ".")
	if len(parts) != 4 || parts[0] != "mp1" {
		return out, content.ErrConflict
	}
	issued, e1 := strconv.ParseInt(parts[1], 10, 64)
	expiry, e2 := strconv.ParseInt(parts[2], 10, 64)
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return out, content.ErrUnavailable
	}
	if e1 != nil || e2 != nil || issued <= 0 || expiry <= issued || expiry-issued != int64(momentPublicationTTL) || issued > now.UnixNano() || expiry <= now.UnixNano() || !hmac.Equal([]byte(in.Snapshot), []byte(publicationSnapshot(digest, f.material, issued, expiry))) {
		return out, content.ErrConflict
	}
	e = tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE moments m SET visibility='public',status='published',author_confirmed_at=stamp.at,published_at=stamp.at,updated_at=stamp.at,revision=revision+1 FROM stamp WHERE m.id=$1 AND m.author_account_id=$2 AND m.revision=$3 AND m.status='draft' AND m.visibility='private' RETURNING m.id,m.place_id,m.revision,m.status,m.published_at`, id, actor.ID, in.Revision).Scan(&out.MomentID, &out.PlaceID, &out.Revision, &out.Status, &out.PublishedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, content.ErrConflict
	}
	if e != nil {
		return content.MomentPublicationReceipt{}, content.ErrUnavailable
	}
	_, e = tx.Exec(ctx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose) VALUES($1,'publish_public','moment',$2,'allowed','explicit_place_publication')`, actor.ID, id)
	if e != nil {
		return content.MomentPublicationReceipt{}, content.ErrUnavailable
	}
	// Existing outbox capture is explicitly private draft/withdrawn only. Do
	// not invent a Published event or grant cognitive processing to this text.
	if e = s.finishPublication(ctx, tx, digest, actor, f, expiry); e != nil {
		return content.MomentPublicationReceipt{}, e
	}
	out.PublishedAt = out.PublishedAt.UTC()
	return out, nil
}
