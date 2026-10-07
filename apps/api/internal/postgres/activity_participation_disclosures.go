package postgres

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	apd "github.com/birdtie/birdtie/apps/api/internal/activityparticipationdisclosure"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
	"io"
	"reflect"
	"strings"
	"sync"
	"time"
)

var _ apd.Store = (*Store)(nil)
var participationDisclosureProcess struct {
	once sync.Once
	key  [32]byte
	err  error
}

const participationDisclosureAAD = "BIRDTIE_HUMAN_PARTICIPATION_DISCLOSURE_PREVIEW_V1"

type participationDisclosureFrame struct {
	Identity, Row, SourceDigest string
	Epoch                       int64
	Revision, ActivityRevision  int64
	Record                      apd.Record
	Observed, ReadEnd           time.Time
}
type participationDisclosureSealed struct {
	Version, Owner  string
	Input           apd.Input
	Frame           participationDisclosureFrame
	Issued, Expires time.Time
}

func participationDisclosureAEAD() (cipher.AEAD, error) {
	participationDisclosureProcess.once.Do(func() {
		_, participationDisclosureProcess.err = io.ReadFull(rand.Reader, participationDisclosureProcess.key[:])
	})
	if participationDisclosureProcess.err != nil {
		return nil, apd.ErrUnavailable
	}
	b, e := aes.NewCipher(participationDisclosureProcess.key[:])
	if e != nil {
		return nil, apd.ErrUnavailable
	}
	return cipher.NewGCM(b)
}
func sealParticipationDisclosure(p participationDisclosureSealed) (string, error) {
	a, e := participationDisclosureAEAD()
	if e != nil {
		return "", e
	}
	nonce := make([]byte, a.NonceSize())
	if _, e = io.ReadFull(rand.Reader, nonce); e != nil {
		return "", apd.ErrUnavailable
	}
	raw, e := json.Marshal(p)
	if e != nil {
		return "", apd.ErrUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(a.Seal(nonce, nonce, raw, []byte(participationDisclosureAAD))), nil
}
func openParticipationDisclosure(token string) (participationDisclosureSealed, error) {
	var p participationDisclosureSealed
	a, e := participationDisclosureAEAD()
	if e != nil {
		return p, e
	}
	raw, e := base64.RawURLEncoding.DecodeString(token)
	if e != nil || len(raw) < a.NonceSize()+a.Overhead() || len(raw) > 18000 {
		return p, apd.ErrChanged
	}
	raw, e = a.Open(nil, raw[:a.NonceSize()], raw[a.NonceSize():], []byte(participationDisclosureAAD))
	if e != nil || json.Unmarshal(raw, &p) != nil || p.Version != participationDisclosureAAD || apd.ValidateInput(p.Input) != nil {
		return p, apd.ErrChanged
	}
	return p, nil
}
func participationDisclosureError(e error) error {
	if e == nil {
		return nil
	}
	for _, v := range []error{apd.ErrInvalid, apd.ErrDenied, apd.ErrChanged, apd.ErrUnavailable} {
		if errors.Is(e, v) {
			return v
		}
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return apd.ErrDenied
	}
	return apd.ErrUnavailable
}

const participationDisclosureGuardSQL = `SELECT
 EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='activity_participations'::regclass AND tgname='participation_disclosure_version' AND tgtype=23 AND tgenabled IN ('O','A') AND tgfoid=to_regprocedure('birdtie_participation_disclosure_version()'))
 AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='audit_events'::regclass AND tgname='participation_disclosure_audit_guard' AND tgtype=31 AND tgenabled IN ('O','A') AND tgfoid=to_regprocedure('birdtie_participation_disclosure_audit_guard()'))
 AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='audit_events'::regclass AND tgname='participation_disclosure_audit_truncate_guard' AND tgtype=34 AND tgenabled IN ('O','A') AND tgfoid=to_regprocedure('birdtie_participation_disclosure_audit_guard()'))
 AND to_regclass('participation_disclosure_audit_epoch') IS NOT NULL AND to_regprocedure('birdtie_participation_public_source(uuid,uuid,timestamp with time zone)') IS NOT NULL`

func (s *Store) beginParticipationDisclosure(ctx context.Context, a agentprofile.PrivateAccess, write bool) (pgx.Tx, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return nil, apd.ErrUnavailable
	}
	if agentprofile.ValidatePrivateAccess(a) != nil {
		return nil, apd.ErrDenied
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, apd.ErrUnavailable
	}
	fail := func(e error) (pgx.Tx, error) {
		tx.Rollback(context.Background())
		return nil, participationDisclosureError(e)
	}
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC';LOCK TABLE accounts,agents,agent_profiles,sessions,activities,activity_participations,activity_organizers,cities,places,venues,venue_candidates,communities,organizations,businesses,account_blocks,audit_events IN ACCESS SHARE MODE`); e != nil {
		return fail(e)
	}
	if write {
		if _, e = tx.Exec(ctx, `LOCK TABLE activity_participations,audit_events IN ROW EXCLUSIVE MODE`); e != nil {
			return fail(e)
		}
	}
	var guarded bool
	if e = tx.QueryRow(ctx, participationDisclosureGuardSQL).Scan(&guarded); e != nil || !guarded {
		return fail(apd.ErrUnavailable)
	}
	var id string
	if e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, a.WorkspacePrincipal.ID).Scan(&id); e != nil {
		return fail(e)
	}
	if e = tx.QueryRow(ctx, `SELECT id FROM agents WHERE principal_account_id=$1 AND agent_type='personal' AND status='active' FOR SHARE`, id).Scan(&id); e != nil {
		return fail(e)
	}
	if e = tx.QueryRow(ctx, `SELECT agent_id FROM agent_profiles WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON' FOR SHARE`, id, a.WorkspacePrincipal.ID).Scan(&id); e != nil {
		return fail(e)
	}
	return tx, nil
}

const participationDisclosureFrameSQL = `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at)
 SELECT jsonb_build_array(owner.id,owner.xmin::text,ag.id,ag.xmin::text,ap.xmin::text,ss.id,ss.created_at,ss.authentication_method,ss.expires_at,encode(ss.token_sha256,'hex'))::text,
 jsonb_build_array(p.xmin::text,p.id,p.activity_id,p.participant_account_id,p.status,p.cancelled_at,p.created_at,p.updated_at,p.disclosure_revision,p.disclosure_visibility,p.disclosure_approved_at,p.disclosure_expires_at,p.disclosure_source_revision,p.disclosure_activity_revision,p.disclosure_source_digest)::text,
 src.digest,p.disclosure_revision,a.revision,
 coalesce((SELECT max(id) FROM audit_events WHERE actor_account_id=owner.id AND resource_type='activity_participation_disclosure' AND purpose='HUMAN_ACTIVITY_PARTICIPATION_DISCLOSURE' AND resource_id=p.id::text),0),
 p.id,p.activity_id,CASE WHEN src.available AND p.status='going' AND p.cancelled_at IS NULL THEN a.title ELSE '已不可公开展示的活动报名' END,p.status,
 (src.available AND p.status='going' AND p.cancelled_at IS NULL),
 CASE WHEN src.available AND p.status='going' AND p.cancelled_at IS NULL THEN a.starts_at END,CASE WHEN src.available AND p.status='going' AND p.cancelled_at IS NULL THEN a.ends_at END,CASE WHEN src.available AND p.status='going' AND p.cancelled_at IS NULL THEN src.expires_at END,
 upper(p.disclosure_visibility),p.disclosure_expires_at,
 (src.available AND p.status='going' AND p.cancelled_at IS NULL AND p.disclosure_visibility='public' AND p.disclosure_source_revision=p.disclosure_revision AND p.disclosure_activity_revision=a.revision AND p.disclosure_source_digest=src.digest AND p.disclosure_expires_at>cl.at),
 cl.at,least(ss.expires_at,ss.idle_expires_at,CASE WHEN src.available AND p.status='going' AND p.cancelled_at IS NULL THEN src.expires_at END)
 FROM activity_participations p JOIN activities a ON a.id=p.activity_id JOIN accounts owner ON owner.id=p.participant_account_id
 JOIN agents ag ON ag.principal_account_id=owner.id AND ag.agent_type='personal' AND ag.status='active'
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=owner.id AND ap.owner_type='PERSON'
 JOIN sessions ss ON ss.account_id=owner.id AND ss.token_sha256=$2 AND ss.revoked_at IS NULL CROSS JOIN cl
 CROSS JOIN LATERAL birdtie_participation_public_source(a.id,owner.id,cl.at) src
 WHERE p.id=$3 AND owner.id=$1 AND owner.account_type='person' AND owner.status='active'
 AND ss.expires_at>cl.at AND ss.idle_expires_at>cl.at AND ($4::boolean OR ss.authentication_method<>'dev_phone')`

func (s *Store) participationDisclosureCapture(ctx context.Context, tx pgx.Tx, a agentprofile.PrivateAccess, id string, locks, write bool) (participationDisclosureFrame, error) {
	var f participationDisclosureFrame
	if locks {
		var act string
		if e := tx.QueryRow(ctx, `SELECT a.id FROM activities a JOIN activity_participations p ON p.activity_id=a.id WHERE p.id=$1 AND p.participant_account_id=$2 FOR SHARE OF a`, id, a.WorkspacePrincipal.ID).Scan(&act); e != nil {
			return f, participationDisclosureError(e)
		}
		lock := " FOR SHARE"
		if write {
			lock = " FOR UPDATE"
		}
		if e := tx.QueryRow(ctx, `SELECT id FROM activity_participations WHERE id=$1 AND participant_account_id=$2`+lock, id, a.WorkspacePrincipal.ID).Scan(&act); e != nil {
			return f, participationDisclosureError(e)
		}
		// Lock only actual referenced sources. No private row is projected or sealed.
		queries := []string{
			`SELECT c.id::text FROM cities c JOIN activities a ON a.city_id=c.id JOIN activity_participations p ON p.activity_id=a.id WHERE p.id=$1 FOR SHARE OF c`,
			`SELECT l.id::text FROM places l JOIN activities a ON a.place_id=l.id JOIN activity_participations p ON p.activity_id=a.id WHERE p.id=$1 FOR SHARE OF l`,
			`SELECT v.place_id::text FROM venues v JOIN activities a ON a.venue_place_id=v.place_id JOIN activity_participations p ON p.activity_id=a.id WHERE p.id=$1 FOR SHARE OF v`,
			`SELECT vc.id::text FROM venue_candidates vc JOIN venues v ON v.source_candidate_id=vc.id JOIN activities a ON a.venue_place_id=v.place_id JOIN activity_participations p ON p.activity_id=a.id WHERE p.id=$1 FOR SHARE OF vc`,
			`SELECT ao.activity_id::text FROM activity_organizers ao JOIN activity_participations p ON p.activity_id=ao.activity_id WHERE p.id=$1 FOR SHARE OF ao`,
			`SELECT h.id::text FROM accounts h JOIN activities a ON a.host_account_id=h.id JOIN activity_participations p ON p.activity_id=a.id WHERE p.id=$1 FOR SHARE OF h`,
			`SELECT g.id::text FROM communities g JOIN activity_organizers ao ON ao.community_id=g.id JOIN activity_participations p ON p.activity_id=ao.activity_id WHERE p.id=$1 FOR SHARE OF g`,
			`SELECT c.id::text FROM cities c JOIN communities g ON g.city_id=c.id JOIN activity_organizers ao ON ao.community_id=g.id JOIN activity_participations p ON p.activity_id=ao.activity_id WHERE p.id=$1 FOR SHARE OF c`,
			`SELECT o.id::text FROM organizations o JOIN activity_organizers ao ON ao.organization_id=o.id JOIN activity_participations p ON p.activity_id=ao.activity_id WHERE p.id=$1 FOR SHARE OF o`,
			`SELECT b.id::text FROM businesses b JOIN activity_organizers ao ON ao.business_id=b.id JOIN activity_participations p ON p.activity_id=ao.activity_id WHERE p.id=$1 FOR SHARE OF b`,
		}
		for _, q := range queries {
			rows, e := tx.Query(ctx, q, id)
			if e != nil {
				return f, participationDisclosureError(e)
			}
			for rows.Next() {
				var value string
				if e = rows.Scan(&value); e != nil {
					rows.Close()
					return f, participationDisclosureError(e)
				}
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return f, participationDisclosureError(e)
			}
		}
		if e := tx.QueryRow(ctx, `SELECT id FROM sessions WHERE account_id=$1 AND token_sha256=$2 FOR SHARE`, a.WorkspacePrincipal.ID, a.SessionDigest[:]).Scan(&act); e != nil {
			return f, participationDisclosureError(e)
		}
	}
	e := scanParticipationDisclosureFrame(tx.QueryRow(ctx, participationDisclosureFrameSQL, a.WorkspacePrincipal.ID, a.SessionDigest[:], id, s.devPhoneEnabled), &f)
	f.Record.Attendance = "UNKNOWN"
	return f, participationDisclosureError(e)
}
func scanParticipationDisclosureFrame(row scanner, f *participationDisclosureFrame) error {
	e := row.Scan(&f.Identity, &f.Row, &f.SourceDigest, &f.Revision, &f.ActivityRevision, &f.Epoch, &f.Record.ParticipationID, &f.Record.ActivityID, &f.Record.Title, &f.Record.Status, &f.Record.SourceAvailable, &f.Record.StartsAt, &f.Record.EndsAt, &f.Record.SourceExpiresAt, &f.Record.Visibility, &f.Record.DisclosureExpiresAt, &f.Record.EffectivePublic, &f.Observed, &f.ReadEnd)
	// pgx may scan UTC SQL timestamps in the process local zone. Bind instants,
	// not Go Location pointers, across native capture/AEAD JSON/HTTP roundtrips.
	for _, at := range []*time.Time{f.Record.StartsAt, f.Record.EndsAt, f.Record.SourceExpiresAt, f.Record.DisclosureExpiresAt} {
		if at != nil {
			*at = at.UTC()
		}
	}
	f.Observed = f.Observed.UTC()
	f.ReadEnd = f.ReadEnd.UTC()
	return e
}
func participationDisclosureSame(a, b participationDisclosureFrame) bool {
	return a.Identity == b.Identity && a.Row == b.Row && a.SourceDigest == b.SourceDigest && a.Epoch == b.Epoch && reflect.DeepEqual(a.Record, b.Record)
}
func participationDisclosureAllowed(f participationDisclosureFrame, in apd.Input) bool {
	if in.Operation == "PRIVATE" {
		return f.Record.Visibility != "PRIVATE"
	}
	return f.Record.SourceAvailable && in.DisclosureExpiresAt != nil && in.DisclosureExpiresAt.After(f.Observed) && !in.DisclosureExpiresAt.After(f.Observed.Add(24*time.Hour)) && f.Record.SourceExpiresAt != nil && !in.DisclosureExpiresAt.After(*f.Record.SourceExpiresAt)
}
func participationDisclosureView(a agentprofile.PrivateAccess, identity string, now time.Time) apd.View {
	return apd.View{SchemaVersion: apd.Schema, OwnerID: a.WorkspacePrincipal.ID, AgentID: interestAgentFromAuthority(identity), ObservedAt: now, Records: []apd.Record{}, Limit: 100}
}
func (s *Store) PreviewOwnParticipationDisclosure(ctx context.Context, a agentprofile.PrivateAccess, in apd.Input) (apd.Preview, error) {
	var out apd.Preview
	if apd.ValidateInput(in) != nil {
		return out, apd.ErrInvalid
	}
	in.ParticipationID = strings.ToLower(in.ParticipationID)
	if in.DisclosureExpiresAt != nil {
		v := in.DisclosureExpiresAt.UTC().Truncate(time.Microsecond)
		in.DisclosureExpiresAt = &v
	}
	tx, e := s.beginParticipationDisclosure(ctx, a, false)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	f, e := s.participationDisclosureCapture(ctx, tx, a, in.ParticipationID, true, false)
	if e != nil {
		return out, e
	}
	if !participationDisclosureAllowed(f, in) {
		return out, apd.ErrChanged
	}
	expiry := f.Observed.Add(90 * time.Second)
	if f.ReadEnd.Before(expiry) {
		expiry = f.ReadEnd
	}
	if in.DisclosureExpiresAt != nil && in.DisclosureExpiresAt.Before(expiry) {
		expiry = *in.DisclosureExpiresAt
	}
	p := participationDisclosureSealed{Version: participationDisclosureAAD, Owner: a.WorkspacePrincipal.ID, Input: in, Frame: f, Issued: f.Observed, Expires: expiry}
	token, e := sealParticipationDisclosure(p)
	if e != nil {
		return out, e
	}
	last, e := s.participationDisclosureCapture(ctx, tx, a, in.ParticipationID, false, false)
	if e != nil {
		return out, e
	}
	if !participationDisclosureSame(f, last) || !last.Observed.Before(expiry) || !participationDisclosureAllowed(last, in) {
		return out, apd.ErrChanged
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return out, apd.ErrUnavailable
	}
	consequence := "仅隐藏我的公开报名声明，不取消报名；不发送消息或修改成员身份。"
	if in.Operation == "PUBLIC" {
		consequence = "在所示有限期限内公开我的报名，可作为共同公开报名依据；不代表到场或成员资格，不发送消息，不开启模型权限。"
	}
	return apd.Preview{SchemaVersion: apd.Schema, OwnerID: p.Owner, AgentID: interestAgentFromAuthority(f.Identity), Record: f.Record, Operation: in.Operation, TargetVisibility: in.Operation, TargetExpiresAt: in.DisclosureExpiresAt, Preview: token, ObservedAt: f.Observed, ExpiresAt: expiry, Consequence: consequence}, nil
}
func (s *Store) ApproveOwnParticipationDisclosure(ctx context.Context, a agentprofile.PrivateAccess, token string) (apd.View, error) {
	var out apd.View
	p, e := openParticipationDisclosure(token)
	if e != nil {
		return out, e
	}
	if p.Owner != a.WorkspacePrincipal.ID {
		return out, apd.ErrDenied
	}
	tx, e := s.beginParticipationDisclosure(ctx, a, true)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	f, e := s.participationDisclosureCapture(ctx, tx, a, p.Input.ParticipationID, true, true)
	if e != nil {
		return out, e
	}
	if !participationDisclosureSame(p.Frame, f) || !f.Observed.Before(p.Expires) || f.Observed.Before(p.Issued) || !participationDisclosureAllowed(f, p.Input) {
		return out, apd.ErrChanged
	}
	if p.Input.Operation == "PUBLIC" {
		_, e = tx.Exec(ctx, `UPDATE activity_participations SET disclosure_visibility='public',disclosure_approved_at=$2,disclosure_expires_at=$3,disclosure_activity_revision=$4,disclosure_source_digest=$5 WHERE id=$1`, p.Input.ParticipationID, f.Observed, p.Input.DisclosureExpiresAt, f.ActivityRevision, f.SourceDigest)
	} else {
		_, e = tx.Exec(ctx, `UPDATE activity_participations SET disclosure_visibility='private',disclosure_approved_at=NULL,disclosure_expires_at=NULL,disclosure_source_revision=NULL,disclosure_activity_revision=NULL,disclosure_source_digest=NULL WHERE id=$1`, p.Input.ParticipationID)
	}
	if e != nil {
		return out, participationDisclosureError(e)
	}
	var audit int64
	if e = auditQueryRow(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose) VALUES($1,$2,'activity_participation_disclosure',$3,'allowed','HUMAN_ACTIVITY_PARTICIPATION_DISCLOSURE') RETURNING id`, p.Owner, p.Input.Operation, p.Input.ParticipationID).Scan(&audit); e != nil {
		return out, participationDisclosureError(e)
	}
	last, e := s.participationDisclosureCapture(ctx, tx, a, p.Input.ParticipationID, false, true)
	if e != nil {
		return out, e
	}
	if last.Identity != p.Frame.Identity || last.SourceDigest != p.Frame.SourceDigest || last.Epoch != audit || last.Revision != f.Revision+1 || last.Record.Visibility != p.Input.Operation || !last.Observed.Before(p.Expires) || (p.Input.Operation == "PUBLIC" && (!last.Record.EffectivePublic || !reflect.DeepEqual(last.Record.DisclosureExpiresAt, p.Input.DisclosureExpiresAt))) {
		return out, apd.ErrChanged
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return out, apd.ErrUnavailable
	}
	out = participationDisclosureView(a, last.Identity, last.Observed)
	out.Records = append(out.Records, last.Record)
	return out, nil
}
func (s *Store) ReadOwnParticipationDisclosures(ctx context.Context, a agentprofile.PrivateAccess, options bool) (apd.View, error) {
	var out apd.View
	tx, e := s.beginParticipationDisclosure(ctx, a, false)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	rows, e := tx.Query(ctx, `SELECT id FROM activity_participations WHERE participant_account_id=$1 ORDER BY id LIMIT 101`, a.WorkspacePrincipal.ID)
	if e != nil {
		return out, participationDisclosureError(e)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return out, participationDisclosureError(e)
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, participationDisclosureError(e)
	}
	var identity string
	var observed, end time.Time
	first := []participationDisclosureFrame{}
	for _, id := range ids {
		f, e := s.participationDisclosureCapture(ctx, tx, a, id, true, false)
		if e != nil {
			return out, e
		}
		first = append(first, f)
	}

	final := []participationDisclosureFrame{}
	if len(first) > 0 {
		prior := map[string]participationDisclosureFrame{}
		for _, f := range first {
			prior[f.Record.ParticipationID] = f
			if end.IsZero() || f.ReadEnd.Before(end) {
				end = f.ReadEnd
			}
			if f.Record.EffectivePublic && f.Record.DisclosureExpiresAt.Before(end) {
				end = *f.Record.DisclosureExpiresAt
			}
		}
		// One final snapshot and PG clock for every loaded source and the Session.
		// All required table/row locks were acquired above, before this statement.
		q := strings.Replace(participationDisclosureFrameSQL, "p.id=$3", "p.id=ANY($3::uuid[])", 1) + " ORDER BY p.id"
		rows, e := tx.Query(ctx, q, a.WorkspacePrincipal.ID, a.SessionDigest[:], ids, s.devPhoneEnabled)
		if e != nil {
			return out, participationDisclosureError(e)
		}
		for rows.Next() {
			var f participationDisclosureFrame
			if e = scanParticipationDisclosureFrame(rows, &f); e != nil {
				rows.Close()
				return out, participationDisclosureError(e)
			}
			f.Record.Attendance = "UNKNOWN"
			old, ok := prior[f.Record.ParticipationID]
			if !ok || !participationDisclosureSame(old, f) || !f.Observed.Before(end) {
				rows.Close()
				return out, apd.ErrChanged
			}
			delete(prior, f.Record.ParticipationID)
			identity = f.Identity
			observed = f.Observed
			final = append(final, f)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, participationDisclosureError(e)
		}
		if len(prior) > 0 {
			return out, apd.ErrDenied
		}
	} else {
		var agent string
		// Empty lists still lock the original Session BEFORE the current statement.
		if e = tx.QueryRow(ctx, `SELECT id FROM sessions WHERE account_id=$1 AND token_sha256=$2 FOR SHARE`, a.WorkspacePrincipal.ID, a.SessionDigest[:]).Scan(&agent); e != nil {
			return out, participationDisclosureError(e)
		}
		e = tx.QueryRow(ctx, `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at) SELECT ag.id,cl.at FROM accounts a JOIN agents ag ON ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active' JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON' JOIN sessions ss ON ss.account_id=a.id CROSS JOIN cl WHERE a.id=$1 AND a.account_type='person' AND a.status='active' AND ss.token_sha256=$2 AND ss.revoked_at IS NULL AND ss.expires_at>cl.at AND ss.idle_expires_at>cl.at AND ($3::boolean OR ss.authentication_method<>'dev_phone')`, a.WorkspacePrincipal.ID, a.SessionDigest[:], s.devPhoneEnabled).Scan(&agent, &observed)
		if e != nil {
			return out, participationDisclosureError(e)
		}
		out = participationDisclosureView(a, "", observed)
		out.AgentID = agent
	}
	if len(final) > 0 {
		out = participationDisclosureView(a, identity, observed)
	}
	for _, f := range final {
		if options && !f.Record.SourceAvailable {
			continue
		}
		if len(out.Records) == 100 {
			out.Truncated = true
			break
		}
		out.Records = append(out.Records, f.Record)
	}
	// Scanned maximum is explicit even if filtering yielded fewer options.
	if len(ids) > 100 {
		out.Truncated = true
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return apd.View{}, apd.ErrUnavailable
	}
	return out, nil
}
