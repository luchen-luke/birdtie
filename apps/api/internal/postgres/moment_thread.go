package postgres

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/momentmedia"
	"github.com/jackc/pgx/v5"
)

var _ content.MomentThreadStore = (*Store)(nil)

func (s *Store) beginMomentThread(ctx context.Context, d [32]byte, a identity.Actor, op string, resources bool) (context.Context, humanWriteFrame, error) {
	ctx, frame, e := s.humanWriteBound(ctx, d, a)
	f := humanWriteFrame{d: d, a: a, session: frame, family: "MOMENT_THREAD", op: op}
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
	if _, e = f.tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e == nil {
		_, e = f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,153))`, a.ID+":MOMENT_THREAD:"+op)
	}
	if e == nil && resources {
		e = s.humanMomentResources(ctx, f.tx, d, a)
	}
	if e != nil {
		_ = f.tx.Rollback(context.Background())
		return ctx, f, e
	}
	return ctx, f, nil
}

func threadReceiptTx(ctx context.Context, f humanWriteFrame) (content.MomentThreadReceipt, error) {
	var r content.MomentThreadReceipt
	var raw []byte
	e := f.tx.QueryRow(ctx, `SELECT receipt FROM moment_thread_operations WHERE actor_account_id=$1 AND operation_id=$2`, f.a.ID, f.op).Scan(&raw)
	if e != nil {
		return r, e
	}
	if json.Unmarshal(raw, &r) != nil || !content.ValidMomentThreadReceipt(r, f.op) {
		return r, identity.ErrControlUnavailable
	}
	return r, nil
}

func (s *Store) ReadMomentThreadOperation(ctx context.Context, d [32]byte, a identity.Actor, op string, stop bool) (content.MomentThreadReceipt, error) {
	ctx, f, e := s.beginMomentThread(ctx, d, a, op, false)
	if e != nil {
		return content.MomentThreadReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	r, e := threadReceiptTx(ctx, f)
	if errors.Is(e, pgx.ErrNoRows) {
		if !stop {
			if e = s.humanWriteFinish(ctx, f); e != nil {
				return r, e
			}
			return r, content.ErrNotFound
		}
		r = content.MomentThreadReceipt{SchemaVersion: "moment-thread-operation-v1", OperationID: op, Status: "STOPPED", Members: []content.MomentThreadMember{}}
		raw, _ := json.Marshal(r)
		_, e = f.tx.Exec(ctx, `INSERT INTO moment_thread_operations(actor_account_id,operation_id,status,receipt)VALUES($1,$2,'STOPPED',$3)`, a.ID, op, raw)
	}
	if e != nil {
		return content.MomentThreadReceipt{}, identity.ErrControlUnavailable
	}
	if e = s.humanWriteFinish(ctx, f); e != nil {
		return content.MomentThreadReceipt{}, e
	}
	return r, nil
}

type momentThreadSource struct {
	version   string
	deadline  *time.Time
	members   []content.MomentThreadMember
	moments   []content.Moment
	materials [][]content.MomentThreadMaterial
	contexts  []content.MomentThreadContext
}

func threadAccess(f humanWriteFrame) momentmedia.Access {
	return momentmedia.Access{AccountID: f.a.ID, AccountType: f.a.AccountType, SessionDigest: f.d}
}

func threadLockSelectors(ctx context.Context, f humanWriteFrame, items []content.MomentThreadItem) error {
	cities, places := map[string]bool{}, map[string]bool{}
	for _, item := range items {
		cities[item.Input.CityID] = true
		if item.Input.PlaceID != "" {
			places[item.Input.PlaceID] = true
		}
	}
	lock := func(table string, values map[string]bool) error {
		ids := []string{}
		for id := range values {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			var actual string
			if e := f.tx.QueryRow(ctx, `SELECT id FROM `+table+` WHERE id=$1 FOR SHARE`, id).Scan(&actual); errors.Is(e, pgx.ErrNoRows) {
				return identity.ErrControlChanged
			} else if e != nil {
				return identity.ErrControlUnavailable
			}
		}
		return nil
	}
	if e := lock("cities", cities); e != nil {
		return e
	}
	return lock("places", places)
}

func threadOwnedMembers(ctx context.Context, f humanWriteFrame, thread string, version int64) ([]content.MomentThreadMember, []content.Moment, string, error) {
	var stamp, status string
	var actual int64
	e := f.tx.QueryRow(ctx, `SELECT xmin::text,version,status FROM moment_threads WHERE id=$1 AND author_account_id=$2 FOR UPDATE`, thread, f.a.ID).Scan(&stamp, &actual, &status)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil, "", content.ErrNotFound
	}
	if e != nil {
		return nil, nil, "", identity.ErrControlUnavailable
	}
	if actual != version || status != "published" {
		return nil, nil, "", identity.ErrControlChanged
	}
	// Close the lock cursor before querying child state on this connection.
	rows, e := f.tx.Query(ctx, `SELECT m.id FROM moments m JOIN moment_thread_members x ON x.moment_id=m.id WHERE x.thread_id=$1 ORDER BY m.id FOR UPDATE OF m`, thread)
	if e != nil {
		return nil, nil, "", identity.ErrControlUnavailable
	}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			break
		}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return nil, nil, "", identity.ErrControlUnavailable
	}
	rows, e = f.tx.Query(ctx, `SELECT moment_id,position FROM moment_thread_members WHERE thread_id=$1 ORDER BY position`, thread)
	if e != nil {
		return nil, nil, "", identity.ErrControlUnavailable
	}
	members := []content.MomentThreadMember{}
	for rows.Next() {
		member := content.MomentThreadMember{Media: []momentmedia.StageSelector{}}
		if e = rows.Scan(&member.MomentID, &member.Position); e != nil {
			break
		}
		members = append(members, member)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil || len(members) < 2 || len(members) > content.MomentThreadMaxEntries {
		return nil, nil, "", identity.ErrControlUnavailable
	}
	moments := []content.Moment{}
	stamps := []any{stamp, actual, status}
	for i := range members {
		m, e := getOwnMomentInTx(ctx, f.tx, f.a.ID, members[i].MomentID)
		if e != nil {
			return nil, nil, "", e
		}
		owned, e := momentControlOwnedStamp(ctx, f, m.ID)
		if e != nil {
			return nil, nil, "", e
		}
		var mediaStamp string
		var setVersion int64
		e = f.tx.QueryRow(ctx, `SELECT coalesce((SELECT version FROM public_moment_media_sets WHERE moment_id=$1),0),coalesce((SELECT jsonb_build_array(s.xmin::text,s.version,s.status)::text FROM public_moment_media_sets s WHERE s.moment_id=$1),'')`, m.ID).Scan(&setVersion, &mediaStamp)
		if e != nil {
			return nil, nil, "", identity.ErrControlUnavailable
		}
		members[i].AppliedRevision, members[i].MediaSetVersion, members[i].Status = m.Revision, setVersion, m.Status
		moments = append(moments, m)
		stamps = append(stamps, []any{members[i].Position, m.ID, m.Revision, m.Status, owned, mediaStamp})
	}
	return members, moments, content.MomentControlHash(stamps), nil
}

func (s *Store) momentThreadSource(ctx context.Context, f humanWriteFrame, in content.MomentThreadInput) (momentThreadSource, error) {
	out := momentThreadSource{members: []content.MomentThreadMember{}, moments: []content.Moment{}, materials: [][]content.MomentThreadMaterial{}, contexts: []content.MomentThreadContext{}}
	var principal string
	e := f.tx.QueryRow(ctx, `SELECT xmin::text FROM accounts WHERE id=$1 AND account_type='person' AND status='active'`, f.a.ID).Scan(&principal)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, identity.ErrControlChanged
	}
	if e != nil {
		return out, identity.ErrControlUnavailable
	}
	values := []any{"moment-thread-source-v1", principal}
	if in.Action == "WITHDRAW" {
		var stamp string
		out.members, out.moments, stamp, e = threadOwnedMembers(ctx, f, in.ThreadID, in.ExpectedVersion)
		if e != nil {
			return out, e
		}
		out.version = content.MomentControlHash(append(values, stamp))
		return out, nil
	}
	// One owner admission lock spans all children; a thread is one action.
	if _, e = f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,153))`, f.a.ID+":MOMENT_THREAD_QUOTA"); e != nil {
		return out, identity.ErrControlUnavailable
	}
	var recent int
	e = f.tx.QueryRow(ctx, `SELECT count(*) FROM moment_thread_operations WHERE actor_account_id=$1 AND operation_id<>$2 AND status='SUCCEEDED' AND receipt->>'action'='CREATE_PUBLISH' AND committed_at>clock_timestamp()-interval '1 minute'`, f.a.ID, in.OperationID).Scan(&recent)
	if e != nil {
		return out, identity.ErrControlUnavailable
	}
	if recent >= 10 {
		return out, identity.ErrControlChanged
	}
	if e = threadLockSelectors(ctx, f, in.Items); e != nil {
		return out, e
	}
	mediaIDs := []string{}
	for _, item := range in.Items {
		for _, image := range item.Media {
			mediaIDs = append(mediaIDs, image.MediaID)
		}
	}
	if len(mediaIDs) > 0 {
		if e = s.LockMomentMediaMaterialsInTx(ctx, f.tx, threadAccess(f), mediaIDs); e != nil {
			return out, e
		}
	}
	for _, item := range in.Items {
		mi := content.MomentControlInput{OperationID: in.OperationID, Action: "PUBLISH", Input: &item.Input}
		stamp, until, _, e := momentControlExternal(ctx, f, mi, content.Moment{})
		if e != nil {
			return out, e
		}
		values = append(values, stamp)
		momentControlAddDeadline(&out.deadline, until)
		contextValue := content.MomentThreadContext{CityID: item.Input.CityID, PlaceID: item.Input.PlaceID}
		if item.Input.CityID != "" {
			if e = f.tx.QueryRow(ctx, `SELECT name FROM cities WHERE id=$1`, item.Input.CityID).Scan(&contextValue.CityName); e != nil {
				return out, identity.ErrControlUnavailable
			}
		}
		if item.Input.PlaceID != "" {
			if e = f.tx.QueryRow(ctx, `SELECT name FROM places WHERE id=$1`, item.Input.PlaceID).Scan(&contextValue.PlaceName); e != nil {
				return out, identity.ErrControlUnavailable
			}
		}
		out.contexts = append(out.contexts, contextValue)
		materials := []content.MomentThreadMaterial{}
		if len(item.Media) > 0 {
			snapshot, e := s.CaptureMomentMediaMaterialsInTx(ctx, f.tx, threadAccess(f), item.Media)
			if e != nil {
				return out, e
			}
			if len(snapshot.Materials) != len(item.Media) || len(snapshot.Thumbnails) != len(item.Media) {
				return out, identity.ErrControlUnavailable
			}
			for i, material := range snapshot.Materials {
				if material.LocalScope != in.LocalScope {
					return out, identity.ErrControlChanged
				}
				materials = append(materials, content.MomentThreadMaterial{Material: material, ThumbnailBase64: base64.StdEncoding.EncodeToString(snapshot.Thumbnails[i])})
			}
			values = append(values, snapshot.SourceVersion)
			momentControlAddDeadline(&out.deadline, &snapshot.ValidUntil)
		}
		out.materials = append(out.materials, materials)
	}
	out.version = content.MomentControlHash(values)
	return out, nil
}

type momentThreadPreviewProof struct {
	s      *Store
	f      humanWriteFrame
	in     content.MomentThreadInput
	source string
	until  time.Time
}

func (p *momentThreadPreviewProof) RevalidateHumanControl(ctx context.Context) error {
	ctx = identity.WithHumanControlFrame(ctx, p.f.d, p.f.a, p.f.session)
	ctx, f, e := p.s.beginMomentThread(ctx, p.f.d, p.f.a, p.in.OperationID, true)
	if e != nil {
		return e
	}
	defer f.tx.Rollback(context.Background())
	return p.s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, p.until, func() error {
		current, e := p.s.momentThreadSource(ctx, f, p.in)
		if e != nil {
			return e
		}
		if current.version != p.source || current.deadline != nil && current.deadline.Before(p.until) {
			return identity.ErrControlChanged
		}
		return nil
	}))
}

func (s *Store) PreviewMomentThread(ctx context.Context, d [32]byte, a identity.Actor, in content.MomentThreadInput) (content.MomentThreadPreview, error) {
	in, e := content.NormalizeMomentThread(in, false)
	if e != nil {
		return content.MomentThreadPreview{}, e
	}
	ctx, f, e := s.beginMomentThread(ctx, d, a, in.OperationID, true)
	if e != nil {
		return content.MomentThreadPreview{}, e
	}
	defer f.tx.Rollback(context.Background())
	if _, e = threadReceiptTx(ctx, f); e == nil {
		return content.MomentThreadPreview{}, identity.ErrControlChanged
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return content.MomentThreadPreview{}, identity.ErrControlUnavailable
	}
	src, e := s.momentThreadSource(ctx, f, in)
	if e != nil {
		return content.MomentThreadPreview{}, e
	}
	base, e := s.humanWritePreview(ctx, f, in.ThreadID, in.Action, content.MomentThreadDigest(in), src.version, src.deadline)
	if e != nil {
		return content.MomentThreadPreview{}, e
	}
	proof := &momentThreadPreviewProof{s: s, f: f, in: in, source: src.version, until: base.ExpiresAt}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, base.ExpiresAt, func() error {
		fresh, e := s.momentThreadSource(ctx, f, in)
		if e != nil {
			return e
		}
		if fresh.version != src.version {
			return identity.ErrControlChanged
		}
		return nil
	})); e != nil {
		return content.MomentThreadPreview{}, e
	}
	base.ResponseProof = proof
	return content.MomentThreadPreview{HumanControlPreview: base, Input: in, SourceVersion: src.version, Members: src.members, Materials: src.materials, Contexts: src.contexts}, nil
}

func threadEffectStamp(ctx context.Context, f humanWriteFrame, thread string) (string, error) {
	var raw string
	e := f.tx.QueryRow(ctx, `SELECT jsonb_build_array(t.xmin::text,t.version,t.status,(SELECT jsonb_agg(jsonb_build_array(x.position,x.moment_id,m.xmin::text,m.revision,m.status,ms.xmin::text,ms.version,ms.status) ORDER BY x.position) FROM moment_thread_members x JOIN moments m ON m.id=x.moment_id LEFT JOIN public_moment_media_sets ms ON ms.moment_id=m.id WHERE x.thread_id=t.id))::text FROM moment_threads t WHERE t.id=$1 AND t.author_account_id=$2`, thread, f.a.ID).Scan(&raw)
	if e != nil {
		return "", identity.ErrControlUnavailable
	}
	return content.MomentControlHash(raw), nil
}

func (s *Store) CommitMomentThread(ctx context.Context, d [32]byte, a identity.Actor, in content.MomentThreadInput) (content.MomentThreadReceipt, error) {
	in, e := content.NormalizeMomentThread(in, true)
	if e != nil {
		return content.MomentThreadReceipt{}, e
	}
	ctx, f, e := s.beginMomentThread(ctx, d, a, in.OperationID, true)
	if e != nil {
		return content.MomentThreadReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	digest := content.MomentThreadDigest(in)
	if old, e := threadReceiptTx(ctx, f); e == nil {
		if old.Status == "SUCCEEDED" && old.PayloadDigest != digest {
			return content.MomentThreadReceipt{}, identity.ErrControlChanged
		}
		if e = s.humanWriteFinish(ctx, f); e != nil {
			return content.MomentThreadReceipt{}, e
		}
		return old, nil
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return content.MomentThreadReceipt{}, identity.ErrControlUnavailable
	}
	src, e := s.momentThreadSource(ctx, f, in)
	if e != nil {
		return content.MomentThreadReceipt{}, e
	}
	if src.version != in.SourceVersion {
		return content.MomentThreadReceipt{}, identity.ErrControlChanged
	}
	until, e := s.humanWriteApprove(ctx, f, in.ThreadID, in.Action, digest, src.version, in.Approval, src.deadline)
	if e != nil {
		return content.MomentThreadReceipt{}, e
	}
	r := content.MomentThreadReceipt{SchemaVersion: "moment-thread-operation-v1", OperationID: in.OperationID, Status: "SUCCEEDED", Action: in.Action, ThreadID: in.ThreadID, Audience: "PUBLIC", ExpectedVersion: in.ExpectedVersion, AppliedVersion: in.ExpectedVersion + 1, PayloadDigest: digest, Members: []content.MomentThreadMember{}}
	if in.Action == "CREATE_PUBLISH" {
		for position, item := range in.Items {
			m, e := createMomentDraftInTx(ctx, f.tx, a.ID, item.Input)
			if e != nil {
				return content.MomentThreadReceipt{}, humanMomentFailure(e)
			}
			m, e = applyMomentPublicControl(ctx, f, content.MomentControlInput{Action: "PUBLISH", MomentID: m.ID, ExpectedRevision: 1, Input: &item.Input})
			if e != nil {
				return content.MomentThreadReceipt{}, e
			}
			member := content.MomentThreadMember{MomentID: m.ID, Status: m.Status, Position: position, AppliedRevision: m.Revision, Media: []momentmedia.StageSelector{}}
			if len(item.Media) > 0 {
				set, e := s.AttachMomentMediaInTx(ctx, f.tx, threadAccess(f), m.ID, m.Revision, 0, item.Media)
				if e != nil {
					return content.MomentThreadReceipt{}, e
				}
				member.MediaSetVersion = set.SetVersion
				for _, attachment := range item.Media {
					member.Media = append(member.Media, momentmedia.StageSelector{MediaID: attachment.MediaID, Version: attachment.MediaVersion})
				}
			}
			r.Members = append(r.Members, member)
		}
		r.RootMomentID = r.Members[0].MomentID
		e = f.tx.QueryRow(ctx, `INSERT INTO moment_threads(author_account_id,root_moment_id,audience)VALUES($1,$2,'PUBLIC')RETURNING id`, a.ID, r.RootMomentID).Scan(&r.ThreadID)
		if e != nil {
			return content.MomentThreadReceipt{}, identity.ErrControlUnavailable
		}
		for _, member := range r.Members {
			if _, e = f.tx.Exec(ctx, `INSERT INTO moment_thread_members(thread_id,moment_id,position)VALUES($1,$2,$3)`, r.ThreadID, member.MomentID, member.Position); e != nil {
				return content.MomentThreadReceipt{}, identity.ErrControlUnavailable
			}
		}
	} else {
		for i, m := range src.moments {
			if m.Status != "withdrawn" {
				if e = withdrawMomentInTx(ctx, f.tx, a.ID, m.ID, m.Revision); e != nil {
					return content.MomentThreadReceipt{}, humanMomentFailure(e)
				}
				m, e = getOwnMomentInTx(ctx, f.tx, a.ID, m.ID)
				if e != nil {
					return content.MomentThreadReceipt{}, e
				}
			}
			member := src.members[i]
			member.AppliedRevision = m.Revision
			member.Status = m.Status
			if e = f.tx.QueryRow(ctx, `SELECT coalesce((SELECT version FROM public_moment_media_sets WHERE moment_id=$1),0)`, m.ID).Scan(&member.MediaSetVersion); e != nil {
				return content.MomentThreadReceipt{}, identity.ErrControlUnavailable
			}
			member.Media = []momentmedia.StageSelector{}
			r.Members = append(r.Members, member)
		}
		r.RootMomentID = r.Members[0].MomentID
		if _, e = f.tx.Exec(ctx, `UPDATE moment_threads SET status='withdrawn',version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND author_account_id=$2 AND version=$3`, in.ThreadID, a.ID, in.ExpectedVersion); e != nil {
			return content.MomentThreadReceipt{}, identity.ErrControlUnavailable
		}
	}
	var at time.Time
	if e = f.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		return content.MomentThreadReceipt{}, identity.ErrControlUnavailable
	}
	at = at.UTC()
	r.CommittedAt = &at
	if !content.ValidMomentThreadReceipt(r, in.OperationID) {
		return content.MomentThreadReceipt{}, identity.ErrControlUnavailable
	}
	raw, _ := json.Marshal(r)
	b, _ := hex.DecodeString(digest)
	if _, e = f.tx.Exec(ctx, `INSERT INTO moment_thread_operations(actor_account_id,operation_id,status,thread_id,payload_sha256,receipt,committed_at)VALUES($1,$2,'SUCCEEDED',$3,$4,$5,$6)`, a.ID, in.OperationID, r.ThreadID, b, raw, at); e != nil {
		return content.MomentThreadReceipt{}, identity.ErrControlUnavailable
	}
	effect, e := threadEffectStamp(ctx, f, r.ThreadID)
	if e != nil {
		return content.MomentThreadReceipt{}, e
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, until, func() error {
		fresh, e := threadEffectStamp(ctx, f, r.ThreadID)
		if e != nil {
			return e
		}
		if fresh != effect {
			return identity.ErrControlChanged
		}
		if in.Action == "CREATE_PUBLISH" {
			external, e := s.momentThreadSource(ctx, f, in)
			if e != nil {
				return e
			}
			if external.version != src.version || external.deadline != nil && external.deadline.Before(until) {
				return identity.ErrControlChanged
			}
		}
		return nil
	})); e != nil {
		return content.MomentThreadReceipt{}, e
	}
	return r, nil
}

type momentThreadReadProof struct {
	s        *Store
	access   content.PublicMomentAccess
	frame    *identity.HumanControlFrame
	id       string
	byMoment bool
	source   string
	until    time.Time
}

func (p *momentThreadReadProof) RevalidateHumanControl(ctx context.Context) error {
	if p.frame != nil {
		ctx = identity.WithHumanControlFrame(ctx, p.access.SessionDigest, p.access.Actor, *p.frame)
	}
	view, e := p.s.ReadMomentThread(ctx, p.access, p.id, p.byMoment)
	if e != nil {
		return e
	}
	if view.SourceVersion != p.source || !p.until.After(view.ObservedAt) || view.ValidUntil.Before(p.until) {
		return identity.ErrControlChanged
	}
	return nil
}

func (s *Store) ReadMomentThread(ctx context.Context, access content.PublicMomentAccess, id string, byMoment bool) (content.MomentThreadView, error) {
	var view content.MomentThreadView
	if !identity.HumanControlUUID(id) {
		return view, identity.ErrControlInvalid
	}
	var frame *identity.HumanControlFrame
	if access.Actor.ID != "" || access.SessionDigest != ([32]byte{}) {
		var f identity.HumanControlFrame
		var e error
		ctx, f, e = s.humanWriteBound(ctx, access.SessionDigest, access.Actor)
		if e != nil {
			return view, e
		}
		frame = &f
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return view, identity.ErrControlUnavailable
	}
	defer tx.Rollback(context.Background())
	selector := `t.id=$1`
	if byMoment {
		selector = `EXISTS(SELECT 1 FROM moment_thread_members x WHERE x.thread_id=t.id AND x.moment_id=$1)`
	}
	var owner, root, status, stamp, ownerStamp string
	e = tx.QueryRow(ctx, `SELECT t.id,t.author_account_id,t.root_moment_id,t.version,t.status,t.xmin::text,a.xmin::text FROM moment_threads t JOIN accounts a ON a.id=t.author_account_id AND a.status='active' WHERE `+selector, id).Scan(&view.ThreadID, &owner, &root, &view.Version, &status, &stamp, &ownerStamp)
	if errors.Is(e, pgx.ErrNoRows) {
		return view, content.ErrNotFound
	}
	if e != nil {
		return view, identity.ErrControlUnavailable
	}
	rows, e := tx.Query(ctx, `SELECT x.position,x.moment_id,m.revision,m.status,birdtie_moment_public_binding(m.id,NULLIF($2,'')::uuid,clock_timestamp()),m.xmin::text FROM moment_thread_members x JOIN moments m ON m.id=x.moment_id WHERE x.thread_id=$1 ORDER BY x.position`, view.ThreadID, access.Actor.ID)
	if e != nil {
		return view, identity.ErrControlUnavailable
	}
	view.Members = []content.MomentThreadPosition{}
	values := []any{stamp, owner, status, ownerStamp}
	available := 0
	for rows.Next() {
		var position int
		var moment, mstatus, childStamp string
		var revision int64
		var binding *string
		if e = rows.Scan(&position, &moment, &revision, &mstatus, &binding, &childStamp); e != nil {
			break
		}
		entry := content.MomentThreadPosition{Position: position, Status: "UNAVAILABLE"}
		if binding != nil {
			entry.MomentID, entry.Revision, entry.Status = &moment, revision, "AVAILABLE"
			available++
			if position == 0 {
				view.RootMomentID = &moment
			}
		}
		view.Members = append(view.Members, entry)
		values = append(values, []any{position, moment, revision, mstatus, childStamp, binding})
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil || len(view.Members) < 2 {
		return view, identity.ErrControlUnavailable
	}
	if available == 0 && access.Actor.ID != owner {
		return view, content.ErrNotFound
	}
	// A by-Moment read may not use a hidden child ID to discover its siblings.
	if byMoment {
		found := false
		for _, entry := range view.Members {
			if entry.MomentID != nil && *entry.MomentID == id {
				found = true
			}
		}
		if !found {
			return view, content.ErrNotFound
		}
	}
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&view.ObservedAt); e != nil {
		return view, identity.ErrControlUnavailable
	}
	view.ValidUntil = view.ObservedAt.Add(30 * time.Second)
	// Bound the lease to every currently disclosed city's and Place's expiry.
	var expiry *time.Time
	e = tx.QueryRow(ctx, `SELECT min(least(c.expires_at,pc.expires_at,`+momentPublishableMediaDeadlineSQL("m")+`,CASE WHEN p.id IS NOT NULL THEN `+currentRetainedPlaceDeadlineSQL("p", "clock_timestamp()")+` END)) FROM moment_thread_members x JOIN moments m ON m.id=x.moment_id LEFT JOIN cities c ON c.id=m.city_id LEFT JOIN places p ON p.id=m.place_id AND(m.city_id IS NULL OR p.city_id=m.city_id) LEFT JOIN cities pc ON pc.id=p.city_id WHERE x.thread_id=$1 AND birdtie_moment_public_binding(m.id,NULLIF($2,'')::uuid,clock_timestamp()) IS NOT NULL`, view.ThreadID, access.Actor.ID).Scan(&expiry)
	if e != nil {
		return view, identity.ErrControlUnavailable
	}
	if expiry != nil && expiry.Before(view.ValidUntil) {
		view.ValidUntil = *expiry
	}
	for _, entry := range view.Members {
		if entry.MomentID == nil {
			continue
		}
		var placeExpiry *time.Time
		e = tx.QueryRow(ctx, `SELECT `+currentRetainedPlaceDeadlineSQL("p", "clock_timestamp()")+` FROM moments m JOIN places p ON p.id=m.place_id WHERE m.id=$1`, *entry.MomentID).Scan(&placeExpiry)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return view, identity.ErrControlUnavailable
		}
		if placeExpiry != nil && placeExpiry.Before(view.ValidUntil) {
			view.ValidUntil = *placeExpiry
		}
	}
	view.SchemaVersion, view.Audience = "moment-thread-view-v1", "PUBLIC"
	view.SourceVersion = content.MomentControlHash(values)
	view.CanWithdraw = access.Actor.ID == owner && status == "published"
	if frame != nil {
		f := humanWriteFrame{tx: tx, d: access.SessionDigest, a: access.Actor, session: *frame}
		if e = s.humanWriteFinish(ctx, f); e != nil {
			return view, e
		}
	} else if e = tx.Commit(ctx); e != nil {
		return view, identity.ErrControlUnavailable
	}
	view.ResponseProof = &momentThreadReadProof{s: s, access: access, frame: frame, id: id, byMoment: byMoment, source: view.SourceVersion, until: view.ValidUntil}
	return view, nil
}
