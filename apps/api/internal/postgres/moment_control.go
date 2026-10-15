package postgres

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mm "github.com/birdtie/birdtie/apps/api/internal/momentmedia"
	"github.com/jackc/pgx/v5"
	"time"
)

var _ content.MomentControlStore = (*Store)(nil)

func (s *Store) beginMomentControl(ctx context.Context, d [32]byte, a identity.Actor, op string, resources bool) (context.Context, humanWriteFrame, error) {
	ctx, first, e := s.humanWriteBound(ctx, d, a)
	f := humanWriteFrame{d: d, a: a, session: first, family: "MOMENT", op: op}
	if e != nil {
		return ctx, f, e
	}
	if !identity.HumanControlUUID(op) {
		return ctx, f, identity.ErrControlInvalid
	}
	f.tx, e = s.pool.Begin(ctx)
	if e != nil {
		return ctx, f, identity.ErrControlUnavailable
	}
	fail := func(e error) (context.Context, humanWriteFrame, error) {
		_ = f.tx.Rollback(context.Background())
		return ctx, f, e
	}
	if _, e = f.tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return fail(identity.ErrControlUnavailable)
	}
	if _, e = f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,144))`, a.ID+":MOMENT:"+op); e != nil {
		return fail(identity.ErrControlUnavailable)
	}
	// Optional enrichment metadata is locked in the original canonical order,
	// solely for the original outbox. It is not an authority or approval field.
	if resources {
		if e = s.humanMomentResources(ctx, f.tx, d, a); e != nil {
			return fail(e)
		}
	}
	return ctx, f, nil
}
func momentControlReceiptTx(ctx context.Context, f humanWriteFrame) (content.MomentControlReceipt, error) {
	r := content.MomentControlReceipt{OperationID: f.op}
	var action, id *string
	var digest []byte
	var at time.Time
	e := f.tx.QueryRow(ctx, `SELECT status,action,moment_id,expected_revision,applied_revision,payload_sha256,committed_at FROM human_moment_control_operations WHERE actor_account_id=$1 AND operation_id=$2`, f.a.ID, f.op).Scan(&r.Status, &action, &id, &r.ExpectedRevision, &r.AppliedRevision, &digest, &at)
	if e != nil {
		return r, e
	}
	if r.Status == "STOPPED" {
		return r, nil
	}
	if action == nil || id == nil || len(digest) != 32 {
		return r, identity.ErrControlUnavailable
	}
	r.Action = *action
	r.MomentID = *id
	r.PayloadDigest = hex.EncodeToString(digest)
	at = at.UTC()
	r.CommittedAt = &at
	if !content.ValidMomentControlReceipt(r, f.op) {
		return r, identity.ErrControlUnavailable
	}
	return r, nil
}
func (s *Store) momentControlHistory(ctx context.Context, d [32]byte, a identity.Actor, op string, stop bool) (content.MomentControlReceipt, error) {
	ctx, f, e := s.beginMomentControl(ctx, d, a, op, false)
	if e != nil {
		return content.MomentControlReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	r, e := momentControlReceiptTx(ctx, f)
	if errors.Is(e, pgx.ErrNoRows) {
		if !stop {
			if e = s.humanWriteFinish(ctx, f); e != nil {
				return r, e
			}
			return r, content.ErrNotFound
		}
		if _, e = f.tx.Exec(ctx, `INSERT INTO human_moment_control_operations(actor_account_id,operation_id,status,applied_revision)VALUES($1,$2,'STOPPED',0)`, a.ID, op); e != nil {
			return r, identity.ErrControlUnavailable
		}
		r.Status = "STOPPED"
	} else if e != nil {
		return r, identity.ErrControlUnavailable
	}
	if e = s.humanWriteFinish(ctx, f); e != nil {
		return content.MomentControlReceipt{}, e
	}
	return r, nil
}
func (s *Store) ReadMomentControlOperation(ctx context.Context, d [32]byte, a identity.Actor, op string) (content.MomentControlReceipt, error) {
	return s.momentControlHistory(ctx, d, a, op, false)
}
func (s *Store) StopMomentControlOperation(ctx context.Context, d [32]byte, a identity.Actor, op string) (content.MomentControlReceipt, error) {
	return s.momentControlHistory(ctx, d, a, op, true)
}

type momentControlSource struct {
	target                         content.Moment
	targetStamp, external, version string
	deadline                       *time.Time
	preserved                      bool
	materials                      MomentMediaMaterialSnapshot
	carry                          momentMediaCarry
	locations                      momentLocationsSnapshot
}

func momentControlOwnedStamp(ctx context.Context, f humanWriteFrame, id string) (string, error) {
	var raw string
	e := f.tx.QueryRow(ctx, `SELECT jsonb_build_array(m.xmin::text,(SELECT jsonb_agg(jsonb_build_array(x.activity_id,x.xmin::text) ORDER BY x.activity_id) FROM moment_activity_links x WHERE x.moment_id=m.id),(SELECT jsonb_agg(jsonb_build_array(x.community_id,x.xmin::text)) FROM moment_community_links x WHERE x.moment_id=m.id),(SELECT jsonb_agg(jsonb_build_array(x.organization_id,x.xmin::text)) FROM moment_organization_links x WHERE x.moment_id=m.id),(SELECT jsonb_build_array(image.photo_id,image.version,image.status,image.sha256,image.xmin::text) FROM public_moment_photos image WHERE image.moment_id=m.id),(SELECT jsonb_build_array(version,xmin::text)FROM moment_location_epochs WHERE moment_id=m.id),(SELECT coalesce(jsonb_agg(jsonb_build_array(l.id,l.revision,l.status,l.position,l.xmin::text)ORDER BY l.id),'[]')FROM moment_location_links l WHERE l.moment_id=m.id AND l.status='ACTIVE'))::text FROM moments m WHERE m.id=$1 AND m.author_account_id=$2`, id, f.a.ID).Scan(&raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", content.ErrNotFound
	}
	if e != nil {
		return "", identity.ErrControlUnavailable
	}
	return identity.HumanControlTextHash(raw), nil
}
func momentControlAddDeadline(out **time.Time, v *time.Time) {
	if v != nil && (*out == nil || v.Before(**out)) {
		x := v.UTC()
		*out = &x
	}
}

// Only newly selected links use current external reader permission. Omitted
// links are the person's existing selectors, not renewed external metadata.
func momentControlLink(ctx context.Context, f humanWriteFrame, kind, id, city string, independentContextCity ...bool) (string, *time.Time, error) {
	// EDIT checks exact original associations before using this option. Their
	// current authority remains mandatory when geography is independently edited.
	independentCity := len(independentContextCity) > 0 && independentContextCity[0]
	var q string
	switch kind {
	case "activity":
		q = `SELECT jsonb_build_array(a.xmin::text,host.xmin::text,ao.xmin::text,
 (SELECT jsonb_agg(jsonb_build_array(m.id,m.xmin::text) ORDER BY m.id) FROM organization_memberships m WHERE m.organization_id=ao.organization_id),
 (SELECT jsonb_agg(jsonb_build_array(m.id,m.xmin::text) ORDER BY m.id) FROM community_memberships m WHERE m.community_id=ao.community_id),
 (SELECT jsonb_agg(jsonb_build_array(m.id,m.xmin::text) ORDER BY m.id) FROM business_memberships m WHERE m.business_id=ao.business_id),
 (SELECT jsonb_agg(jsonb_build_array(i.id,i.xmin::text) ORDER BY i.id) FROM activity_invitations i WHERE i.activity_id=a.id),
 (SELECT jsonb_agg(jsonb_build_array(o.xmin::text,ac.xmin::text)) FROM organizations o JOIN accounts ac ON ac.id=o.account_id WHERE o.id=ao.organization_id),
 (SELECT jsonb_agg(jsonb_build_array(b.xmin::text,ac.xmin::text)) FROM businesses b JOIN accounts ac ON ac.id=b.account_id WHERE b.id=ao.business_id),
 (SELECT jsonb_agg(jsonb_build_array(c.xmin::text,ac.xmin::text)) FROM communities c JOIN accounts ac ON ac.id=c.owner_account_id WHERE c.id=ao.community_id),
 (SELECT jsonb_agg(jsonb_build_array(v.xmin::text,c.xmin::text)) FROM venues v JOIN venue_candidates c ON c.id=v.source_candidate_id WHERE v.place_id=a.place_id),
 (SELECT jsonb_agg(jsonb_build_array(r.business_id,r.xmin::text) ORDER BY r.business_id) FROM business_venue_relations r WHERE r.place_id=a.place_id))::text,
 least(a.expires_at,(SELECT min(v.expires_at) FROM venues v WHERE v.place_id IN(a.place_id,a.venue_place_id)),(SELECT min(c.expires_at) FROM communities c WHERE c.id=ao.community_id))
 FROM activities a JOIN activity_organizers ao ON ao.activity_id=a.id JOIN accounts host ON host.id=a.host_account_id AND host.status='active'
 WHERE a.id=$1 AND ($4 OR a.city_id=$3) AND a.publication_status='published' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND birdtie_activity_visible_to(a.id,$2)
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE (b.blocker_account_id=$2 AND b.blocked_account_id=a.host_account_id) OR(b.blocker_account_id=a.host_account_id AND b.blocked_account_id=$2))`
	case "community":
		q = `SELECT jsonb_build_array(c.xmin::text,m.xmin::text,ac.xmin::text)::text,c.expires_at FROM communities c JOIN community_memberships m ON m.community_id=c.id AND m.user_account_id=$2 AND m.status='active' JOIN accounts ac ON ac.id=c.owner_account_id AND ac.status='active' WHERE c.id=$1 AND(c.city_id IS NULL OR c.city_id=$3 OR $4) AND c.lifecycle_status='active' AND c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>clock_timestamp())`
	case "organization":
		q = `SELECT jsonb_build_array(o.xmin::text,m.xmin::text,ac.xmin::text)::text,NULL::timestamptz FROM organizations o JOIN organization_memberships m ON m.organization_id=o.id AND m.user_account_id=$2 AND m.status='active' JOIN accounts ac ON ac.id=o.account_id AND ac.status='active' WHERE o.id=$1 AND o.status='active' AND ($3::text<>'' OR $4)`
	default:
		return "", nil, identity.ErrControlInvalid
	}
	var raw string
	var until *time.Time
	e := f.tx.QueryRow(ctx, q, id, f.a.ID, city, independentCity).Scan(&raw, &until)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", nil, identity.ErrControlChanged
	}
	if e != nil {
		return "", nil, identity.ErrControlUnavailable
	}
	return identity.HumanControlTextHash(raw), until, nil
}
func momentControlExternal(ctx context.Context, f humanWriteFrame, in content.MomentControlInput, old content.Moment, independentContextCity ...bool) (string, *time.Time, bool, error) {
	// Bind the concrete approval to the original eligible person row. An
	// account status ABA changes xmin even when the Session generation stays
	// unchanged. Session idle renewal and optional Agent metadata are not source
	// versions for this human action.
	var principalStamp string
	e := f.tx.QueryRow(ctx, `SELECT concat_ws(':',id::text,account_type,status,xmin::text) FROM accounts WHERE id=$1 AND account_type='person' AND status='active'`, f.a.ID).Scan(&principalStamp)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", nil, false, identity.ErrControlChanged
	}
	if e != nil {
		return "", nil, false, identity.ErrControlUnavailable
	}
	if in.Action == "WITHDRAW" {
		return content.MomentControlHash([]any{"owned-withdraw", old.ID, principalStamp}), nil, false, nil
	}
	v := in.Input
	var deadline *time.Time
	values := []any{principalStamp}
	preserved := false
	for _, slot := range []struct {
		kind string
		id   *string
	}{{"activity", v.ActivityID}, {"community", v.CommunityID}, {"organization", v.OrganizationID}} {
		if slot.id != nil && *slot.id != "" {
			stamp, until, e := momentControlLink(ctx, f, slot.kind, *slot.id, v.CityID, independentContextCity...)
			if e != nil {
				return "", nil, false, e
			}
			values = append(values, slot.kind, *slot.id, stamp)
			momentControlAddDeadline(&deadline, until)
		}
	}
	// Persistent pair tombstones detect block->unblock, including an otherwise
	// identical current author-visible Activity link.
	if v.ActivityID != nil && *v.ActivityID != "" {
		var blocks string
		e = f.tx.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_array(owner_account_id,target_account_id,version,xmin::text) ORDER BY owner_account_id,target_account_id),'[]'::jsonb)::text FROM person_block_control_epochs WHERE owner_account_id=$1 OR target_account_id=$1`, f.a.ID).Scan(&blocks)
		if e != nil {
			return "", nil, false, identity.ErrControlUnavailable
		}
		values = append(values, blocks)
	}
	var now time.Time
	if e = f.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return "", nil, false, identity.ErrControlUnavailable
	}
	if v.OccurredAt != nil && v.OccurredAt.After(now.Add(24*time.Hour)) {
		return "", nil, false, identity.ErrControlInvalid
	}
	if deadline != nil && !deadline.After(now) {
		return "", nil, false, identity.ErrControlChanged
	}
	return content.MomentControlHash(values), deadline, preserved, nil
}
func (s *Store) momentControlReadSource(ctx context.Context, f humanWriteFrame, in content.MomentControlInput) (momentControlSource, error) {
	out := momentControlSource{locations: momentLocationsSnapshot{Resolved: []content.MomentLocationResolved{}, SourceVersion: content.MomentControlHash([]any{"no-location-input-v1"})}}
	if in.Action != "CREATE" {
		// Match attachment writers before locking the parent, including legacy
		// photo writers, so a public edit cannot deadlock a concurrent replacement.
		if _, e := f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,152))`, "moment-media:"+in.MomentID); e != nil {
			return out, identity.ErrControlUnavailable
		}
		if _, e := f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,145))`, "moment-photo-slot:"+in.MomentID); e != nil {
			return out, identity.ErrControlUnavailable
		}
		var id string
		e := f.tx.QueryRow(ctx, `SELECT id FROM moments WHERE id=$1 AND author_account_id=$2 FOR UPDATE`, in.MomentID, f.a.ID).Scan(&id)
		if errors.Is(e, pgx.ErrNoRows) {
			return out, content.ErrNotFound
		}
		if e != nil {
			return out, identity.ErrControlUnavailable
		}
		out.target, e = getOwnMomentInTx(ctx, f.tx, f.a.ID, id)
		if e != nil {
			return out, e
		}
		if out.target.Revision != in.ExpectedRevision || out.target.Status == "withdrawn" || in.Action == "UPDATE" && (out.target.Status != "draft" || out.target.Visibility != "private") {
			return out, identity.ErrControlChanged
		}
		if in.Action == "PUBLISH" && (out.target.Status != "draft" || out.target.Visibility != "private") || in.Action == "UPDATE_PUBLIC" && (out.target.Status != "published" || out.target.Visibility != "public") {
			return out, identity.ErrControlChanged
		}
		if (in.Action == "PUBLISH" || in.Action == "UPDATE_PUBLIC") && !momentPublicInputMatches(out.target, in) {
			return out, identity.ErrControlChanged
		}
		out.targetStamp, e = momentControlOwnedStamp(ctx, f, id)
		if e != nil {
			return out, e
		}
	}
	var e error
	out.external, out.deadline, out.preserved, e = momentControlExternal(ctx, f, in, out.target)
	if e != nil {
		return out, e
	}
	if in.Action != "WITHDRAW" {
		var current *content.Moment
		if out.target.ID != "" {
			current = &out.target
		}
		out.locations, e = captureMomentLocationsInTx(ctx, f, current, *in.Input)
		if e != nil {
			return out, e
		}
		momentControlAddDeadline(&out.deadline, out.locations.ValidUntil)
		for _, r := range out.locations.Resolved {
			if !r.Available {
				out.preserved = true
			}
		}
	}
	mediaSource := ""
	access := mm.Access{AccountID: f.a.ID, AccountType: f.a.AccountType, SessionDigest: f.d}
	if in.Media != nil && len(*in.Media) > 0 {
		out.materials, e = s.CaptureMomentMediaMaterialsInTx(ctx, f.tx, access, *in.Media)
		if e != nil {
			return out, e
		}
		for _, material := range out.materials.Materials {
			if material.LocalScope != in.LocalScope {
				return out, identity.ErrControlChanged
			}
		}
		mediaSource = out.materials.SourceVersion
		momentControlAddDeadline(&out.deadline, &out.materials.ValidUntil)
	} else if in.Action == "UPDATE_PUBLIC" && in.Media == nil {
		out.carry, e = s.captureMomentMediaCarry(ctx, f.tx, access, in.MomentID)
		if e != nil {
			return out, e
		}
		if out.carry.set != nil {
			mediaSource = out.carry.set.SourceVersion
			momentControlAddDeadline(&out.deadline, &out.carry.set.ValidUntil)
		}
		if len(out.carry.materials) == 0 {
			var legacyCurrent bool
			if e = f.tx.QueryRow(ctx, `SELECT birdtie_public_moment_photo_current($1,$2,clock_timestamp())`, in.MomentID, f.a.ID).Scan(&legacyCurrent); e != nil {
				return out, identity.ErrControlUnavailable
			}
			// 145's Place-only thumbnail is a separate approval contract. A
			// caller must explicitly replace/remove it, never lose it as a side
			// effect of an unrelated title, text or geography edit.
			if legacyCurrent {
				return out, content.ErrMomentLegacyPhotoReselection
			}
		}
		if !content.HasMomentBody(in.Input.Body) && len(out.carry.materials) == 0 {
			return out, identity.ErrControlChanged
		}
	}

	out.version = content.MomentControlHash([]any{in.Action, in.MomentID, in.ExpectedRevision, out.targetStamp, out.external, out.preserved, mediaSource, out.locations.SourceVersion})
	return out, nil
}

type momentControlProof struct {
	s      *Store
	d      [32]byte
	a      identity.Actor
	frame  identity.HumanControlFrame
	in     content.MomentControlInput
	source string
	until  time.Time
}

func (p *momentControlProof) RevalidateHumanControl(ctx context.Context) error {
	ctx = identity.WithHumanControlFrame(ctx, p.d, p.a, p.frame)
	ctx, f, e := p.s.beginMomentControl(ctx, p.d, p.a, p.in.OperationID, true)
	if e != nil {
		return e
	}
	defer f.tx.Rollback(context.Background())
	before, e := p.s.momentControlReadSource(ctx, f, p.in)
	if e != nil {
		return e
	}
	if before.version != p.source || before.deadline != nil && before.deadline.Before(p.until) {
		return identity.ErrControlChanged
	}
	return p.s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, p.until, func() error {
		current, e := p.s.momentControlReadSource(ctx, f, p.in)
		if e != nil {
			return e
		}
		if current.version != p.source || current.deadline != nil && current.deadline.Before(p.until) {
			return identity.ErrControlChanged
		}
		return nil
	}))
}
func (s *Store) PreviewMomentControl(ctx context.Context, d [32]byte, a identity.Actor, in content.MomentControlInput) (content.MomentControlPreview, error) {
	in, e := content.NormalizeMomentControl(in, false)
	if e != nil {
		return content.MomentControlPreview{}, e
	}
	ctx, f, e := s.beginMomentControl(ctx, d, a, in.OperationID, true)
	if e != nil {
		return content.MomentControlPreview{}, e
	}
	defer f.tx.Rollback(context.Background())
	if _, e = momentControlReceiptTx(ctx, f); e == nil {
		return content.MomentControlPreview{}, identity.ErrControlChanged
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return content.MomentControlPreview{}, identity.ErrControlUnavailable
	}
	src, e := s.momentControlReadSource(ctx, f, in)
	if e != nil {
		return content.MomentControlPreview{}, e
	}
	base, e := s.humanWritePreview(ctx, f, in.MomentID, in.Action, content.MomentControlDigest(in), src.version, src.deadline)
	if e != nil {
		return content.MomentControlPreview{}, e
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, base.ExpiresAt, func() error {
		fresh, e := s.momentControlReadSource(ctx, f, in)
		if e != nil {
			return e
		}
		if fresh.version != src.version || fresh.deadline != nil && fresh.deadline.Before(base.ExpiresAt) {
			return identity.ErrControlChanged
		}
		return nil
	})); e != nil {
		return content.MomentControlPreview{}, e
	}
	base.ResponseProof = &momentControlProof{s, d, a, f.session, in, src.version, base.ExpiresAt}
	result := content.MomentControlPreview{HumanControlPreview: base, MomentID: in.MomentID, ExpectedRevision: in.ExpectedRevision, Input: in.Input, InputSHA256: content.MomentControlInputHash(in.Input), SourceVersion: src.version, PreservedPlaceUnavailable: src.preserved, PreservedMedia: src.carry.set, Locations: src.locations.Resolved, LocationsSourceVersion: src.locations.SourceVersion}
	for i, material := range src.materials.Materials {
		result.Materials = append(result.Materials, content.MomentThreadMaterial{Material: material, ThumbnailBase64: base64.StdEncoding.EncodeToString(src.materials.Thumbnails[i])})
	}
	return result, nil
}
func (s *Store) CommitMomentControl(ctx context.Context, d [32]byte, a identity.Actor, in content.MomentControlInput) (content.MomentControlReceipt, error) {
	in, e := content.NormalizeMomentControl(in, true)
	if e != nil {
		return content.MomentControlReceipt{}, e
	}
	ctx, f, e := s.beginMomentControl(ctx, d, a, in.OperationID, true)
	if e != nil {
		return content.MomentControlReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	digest := content.MomentControlDigest(in)
	if old, e := momentControlReceiptTx(ctx, f); e == nil {
		if old.Status == "SUCCEEDED" && old.PayloadDigest != digest {
			return content.MomentControlReceipt{}, identity.ErrControlChanged
		}
		if e = s.humanWriteFinish(ctx, f); e != nil {
			return content.MomentControlReceipt{}, e
		}
		return old, nil
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return content.MomentControlReceipt{}, identity.ErrControlUnavailable
	}
	src, e := s.momentControlReadSource(ctx, f, in)
	if e != nil {
		return content.MomentControlReceipt{}, e
	}
	if src.version != in.SourceVersion {
		return content.MomentControlReceipt{}, identity.ErrControlChanged
	}
	until, e := s.humanWriteApprove(ctx, f, in.MomentID, in.Action, digest, src.version, in.Approval, src.deadline)
	if e != nil {
		return content.MomentControlReceipt{}, e
	}
	var m content.Moment
	switch in.Action {
	case "CREATE":
		m, e = createMomentDraftInTx(ctx, f.tx, a.ID, *in.Input, src.locations)
	case "UPDATE":
		m, e = updateMomentDraftInTx(ctx, f.tx, a.ID, in.MomentID, in.ExpectedRevision, *in.Input, src.locations)
	case "PUBLISH", "UPDATE_PUBLIC":
		m, e = applyMomentPublicControl(ctx, f, in, src.locations)
	case "WITHDRAW":
		e = withdrawMomentInTx(ctx, f.tx, a.ID, in.MomentID, in.ExpectedRevision)
		if e == nil {
			m, e = getOwnMomentInTx(ctx, f.tx, a.ID, in.MomentID)
		}
	}
	if e != nil {
		return content.MomentControlReceipt{}, humanMomentFailure(e)
	}
	var appliedMedia *mm.Set
	if in.Action == "PUBLISH" || in.Action == "UPDATE_PUBLIC" {
		access := mm.Access{AccountID: a.ID, AccountType: a.AccountType, SessionDigest: d}
		if in.Media != nil && len(*in.Media) > 0 || len(src.carry.materials) > 0 {
			var version int64
			if e = f.tx.QueryRow(ctx, `SELECT coalesce((SELECT version FROM public_moment_media_sets WHERE moment_id=$1),0)`, m.ID).Scan(&version); e != nil {
				return content.MomentControlReceipt{}, identity.ErrControlUnavailable
			}
			var set mm.Set
			if in.Media != nil {
				set, e = s.AttachMomentMediaInTx(ctx, f.tx, access, m.ID, m.Revision, version, *in.Media)
			} else {
				set, e = s.applyMomentMediaCarry(ctx, f.tx, access, m.ID, m.Revision, version, src.carry)
			}
			if e != nil {
				return content.MomentControlReceipt{}, e
			}
			appliedMedia = &set
		}
		if e = momentHasPublishableContent(ctx, f.tx, m.ID); e != nil {
			return content.MomentControlReceipt{}, e
		}
	}
	stamp, e := momentControlOwnedStamp(ctx, f, m.ID)
	if e != nil {
		return content.MomentControlReceipt{}, e
	}
	var at time.Time
	b, _ := hex.DecodeString(digest)
	e = f.tx.QueryRow(ctx, `INSERT INTO human_moment_control_operations(actor_account_id,operation_id,status,action,moment_id,expected_revision,applied_revision,payload_sha256)VALUES($1,$2,'SUCCEEDED',$3,$4,$5,$6,$7)RETURNING committed_at`, a.ID, in.OperationID, in.Action, m.ID, in.ExpectedRevision, m.Revision, b).Scan(&at)
	if e != nil {
		return content.MomentControlReceipt{}, identity.ErrControlUnavailable
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, until, func() error {
		latest, e := momentControlOwnedStamp(ctx, f, m.ID)
		if e != nil {
			return e
		}
		if latest != stamp {
			return identity.ErrControlChanged
		}
		external, deadline, preserved, e := momentControlExternal(ctx, f, in, src.target)
		if e != nil {
			return e
		}
		if external != src.external || preserved || deadline != nil && deadline.Before(until) {
			return identity.ErrControlChanged
		}
		if in.Action != "WITHDRAW" {
			if e = revalidateMomentLocationsInTx(ctx, f, src.locations, until); e != nil {
				return e
			}
		}
		if in.Action == "PUBLISH" || in.Action == "UPDATE_PUBLIC" {
			if e = momentHasPublishableContent(ctx, f.tx, m.ID); e != nil {
				return e
			}
			if in.Media != nil && len(*in.Media) > 0 {
				fresh, e := s.CaptureMomentMediaMaterialsInTx(ctx, f.tx, mm.Access{AccountID: a.ID, AccountType: a.AccountType, SessionDigest: d}, *in.Media)
				if e != nil {
					return e
				}
				if fresh.SourceVersion != src.materials.SourceVersion || fresh.ValidUntil.Before(until) {
					return identity.ErrControlChanged
				}
			}
			if appliedMedia != nil {
				fresh, e := s.mediaSetTx(ctx, f.tx, mm.Access{AccountID: a.ID, AccountType: a.AccountType, SessionDigest: d}, m.ID, true)
				if e != nil {
					return e
				}
				if e = compareMediaSets(*appliedMedia, fresh); e != nil {
					return e
				}
			}
		}
		return nil
	})); e != nil {
		return content.MomentControlReceipt{}, e
	}
	expected := in.ExpectedRevision
	at = at.UTC()
	return content.MomentControlReceipt{OperationID: in.OperationID, Status: "SUCCEEDED", Action: in.Action, MomentID: m.ID, ExpectedRevision: &expected, AppliedRevision: m.Revision, PayloadDigest: digest, CommittedAt: &at}, nil
}
