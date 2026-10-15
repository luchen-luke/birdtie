package postgres

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	locations []momentLocationsSnapshot
	externals []string
}

func threadAccess(f humanWriteFrame) momentmedia.Access {
	return momentmedia.Access{AccountID: f.a.ID, AccountType: f.a.AccountType, SessionDigest: f.d}
}

func threadLockSelectors(ctx context.Context, f humanWriteFrame, items []content.MomentThreadItem) error {
	all := []content.MomentLocationInput{}
	for _, item := range items {
		if item.Input.Locations != nil {
			all = append(all, (*item.Input.Locations)...)
		} else {
			all = append(all, legacyMomentLocations(f.op, item.Input, nil)...)
		}
	}
	return lockMomentLocationTargets(ctx, f, all)
}

func threadOwnedMembers(ctx context.Context, f humanWriteFrame, thread string, version int64, mode ...string) ([]content.MomentThreadMember, []content.Moment, string, error) {
	var stamp, status, audience string
	var actual int64
	e := f.tx.QueryRow(ctx, `SELECT xmin::text,version,status,audience FROM moment_threads WHERE id=$1 AND author_account_id=$2 FOR UPDATE`, thread, f.a.ID).Scan(&stamp, &actual, &status, &audience)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil, "", content.ErrNotFound
	}
	if e != nil {
		return nil, nil, "", identity.ErrControlUnavailable
	}
	publish := len(mode) > 0 && mode[0] == "PUBLISH"
	if actual != version || (!publish && (status != "published" || audience != "PUBLIC")) || (publish && (status != "draft" || audience != "PRIVATE")) {
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
	stamps := []any{stamp, actual, status, audience}
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
		if publish && (m.Status != "draft" || m.Visibility != "private") {
			return nil, nil, "", identity.ErrControlChanged
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
	if in.Action == "PUBLISH" {
		var stamp string
		out.members, out.moments, stamp, e = threadOwnedMembers(ctx, f, in.ThreadID, in.ExpectedVersion, "PUBLISH")
		if e != nil {
			return out, e
		}
		if len(out.members) != len(in.Items) {
			return out, identity.ErrControlChanged
		}
		for i, item := range in.Items {
			if item.MomentID != out.members[i].MomentID || item.ExpectedRevision != out.moments[i].Revision {
				return out, identity.ErrControlChanged
			}
		}
		values = append(values, stamp)
	}
	// One owner admission lock spans all children; a thread is one action.
	if _, e = f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,153))`, f.a.ID+":MOMENT_THREAD_QUOTA"); e != nil {
		return out, identity.ErrControlUnavailable
	}
	var recent int
	e = f.tx.QueryRow(ctx, `SELECT count(*) FROM moment_thread_operations WHERE actor_account_id=$1 AND operation_id<>$2 AND status='SUCCEEDED' AND receipt->>'action' IN('CREATE_PUBLISH','PUBLISH') AND committed_at>clock_timestamp()-interval '1 minute'`, f.a.ID, in.OperationID).Scan(&recent)
	if e != nil {
		return out, identity.ErrControlUnavailable
	}
	if recent >= 10 {
		return out, identity.ErrControlChanged
	}
	lockItems := in.Items
	if in.Action == "PUBLISH" {
		lockItems = append([]content.MomentThreadItem{}, in.Items...)
		for i, item := range lockItems {
			if item.Input.Locations == nil && item.Input.CityID == out.moments[i].CityID && item.Input.PlaceID == out.moments[i].PlaceID && item.Input.LocationPrecision == out.moments[i].LocationPrecision {
				rows, e := readMomentLocationRows(ctx, f.tx, item.MomentID)
				if e != nil {
					return out, e
				}
				links := []content.MomentLocationInput{}
				for _, r := range rows {
					links = append(links, r.input)
				}
				lockItems[i].Input.Locations = &links
			}
		}
	}
	if e = threadLockSelectors(ctx, f, lockItems); e != nil {
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
	for position, item := range in.Items {
		mi := content.MomentControlInput{OperationID: in.OperationID, Action: "PUBLISH", Input: &item.Input}
		var current *content.Moment
		existing := content.Moment{}
		if in.Action == "PUBLISH" {
			current = &out.moments[position]
			existing = *current
			mi.MomentID = current.ID
			mi.ExpectedRevision = current.Revision
		}
		stamp, until, _, e := momentControlExternal(ctx, f, mi, existing)
		if e != nil {
			return out, e
		}
		lf := f
		lf.op = fmt.Sprintf("%s:%d", f.op, position)
		loc, e := captureMomentLocationsInTx(ctx, lf, current, item.Input)
		if e != nil {
			return out, e
		}
		out.locations = append(out.locations, loc)
		momentControlAddDeadline(&out.deadline, loc.ValidUntil)
		values = append(values, loc.SourceVersion)
		out.externals = append(out.externals, stamp)
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
	locations := [][]content.MomentLocationResolved{}
	for _, loc := range src.locations {
		locations = append(locations, loc.Resolved)
	}
	if in.Action == "PUBLISH" {
		src.members = []content.MomentThreadMember{}
	}
	return content.MomentThreadPreview{HumanControlPreview: base, Input: in, SourceVersion: src.version, Members: src.members, Materials: src.materials, Contexts: src.contexts, Locations: locations}, nil
}

func threadEffectStamp(ctx context.Context, f humanWriteFrame, thread string) (string, error) {
	var raw string
	e := f.tx.QueryRow(ctx, `SELECT jsonb_build_array(t.xmin::text,t.version,t.status,(SELECT jsonb_agg(jsonb_build_array(x.position,x.moment_id,m.xmin::text,m.revision,m.status,ms.xmin::text,ms.version,ms.status,(SELECT jsonb_build_array(version,xmin::text)FROM moment_location_epochs WHERE moment_id=m.id),(SELECT coalesce(jsonb_agg(jsonb_build_array(l.id,l.kind,l.city_id,l.place_id,l.precision,l.position,l.revision,l.xmin::text)ORDER BY l.id),'[]')FROM moment_location_links l WHERE l.moment_id=m.id AND l.status='ACTIVE')) ORDER BY x.position) FROM moment_thread_members x JOIN moments m ON m.id=x.moment_id LEFT JOIN public_moment_media_sets ms ON ms.moment_id=m.id WHERE x.thread_id=t.id))::text FROM moment_threads t WHERE t.id=$1 AND t.author_account_id=$2`, thread, f.a.ID).Scan(&raw)
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
	if in.Action == "CREATE_PUBLISH" || in.Action == "PUBLISH" {
		for position, item := range in.Items {
			var m content.Moment
			var e error
			if in.Action == "CREATE_PUBLISH" {
				m, e = createMomentDraftInTx(ctx, f.tx, a.ID, item.Input, src.locations[position])
			} else {
				m = src.moments[position]
			}
			if e != nil {
				return content.MomentThreadReceipt{}, humanMomentFailure(e)
			}
			m, e = applyMomentPublicControl(ctx, f, content.MomentControlInput{Action: "PUBLISH", MomentID: m.ID, ExpectedRevision: m.Revision, Input: &item.Input}, src.locations[position])
			if e != nil {
				return content.MomentThreadReceipt{}, e
			}
			member := content.MomentThreadMember{MomentID: m.ID, Status: m.Status, Position: position, AppliedRevision: m.Revision, Media: []momentmedia.StageSelector{}}
			if len(item.Media) > 0 {
				expectedSet := int64(0)
				if in.Action == "PUBLISH" {
					expectedSet = src.members[position].MediaSetVersion
				}
				set, e := s.AttachMomentMediaInTx(ctx, f.tx, threadAccess(f), m.ID, m.Revision, expectedSet, item.Media)
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
		if in.Action == "CREATE_PUBLISH" {
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
			tag, err := f.tx.Exec(ctx, `UPDATE moment_threads SET audience='PUBLIC',status='published',version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND author_account_id=$2 AND version=$3 AND audience='PRIVATE' AND status='draft'`, in.ThreadID, a.ID, in.ExpectedVersion)
			if err != nil || tag.RowsAffected() != 1 {
				return content.MomentThreadReceipt{}, identity.ErrControlChanged
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
		if in.Action == "PUBLISH" {
			for i, item := range in.Items {
				if e := revalidateMomentLocationsInTx(ctx, f, src.locations[i], until); e != nil {
					return e
				}
				stamp, deadline, _, e := momentControlExternal(ctx, f, content.MomentControlInput{OperationID: in.OperationID, Action: "PUBLISH", MomentID: item.MomentID, ExpectedRevision: item.ExpectedRevision, Input: &item.Input}, src.moments[i])
				if e != nil {
					return e
				}
				if stamp != src.externals[i] || deadline != nil && deadline.Before(until) {
					return identity.ErrControlChanged
				}
			}
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
	var expiry *time.Time
	e = tx.QueryRow(ctx, `SELECT min(`+momentPublishableMediaDeadlineSQL("m")+`) FROM moment_thread_members x JOIN moments m ON m.id=x.moment_id WHERE x.thread_id=$1 AND birdtie_moment_public_binding(m.id,NULLIF($2,'')::uuid,clock_timestamp())IS NOT NULL`, view.ThreadID, access.Actor.ID).Scan(&expiry)
	if e != nil {
		return view, identity.ErrControlUnavailable
	}
	if expiry != nil && expiry.Before(view.ValidUntil) {
		view.ValidUntil = *expiry
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

func ownedThreadTx(ctx context.Context, f humanWriteFrame, id string, byMoment bool) (content.OwnedMomentThreadView, error) {
	v := content.OwnedMomentThreadView{SchemaVersion: "owned-moment-thread-v1", Members: []content.MomentThreadPosition{}, EntryContexts: []content.OwnedThreadEntryContext{}, Legs: []content.OwnedThreadLeg{}}
	selector := "t.id=$1"
	if byMoment {
		selector = "EXISTS(SELECT 1 FROM moment_thread_members member WHERE member.thread_id=t.id AND member.moment_id=$1)"
	}
	var root, stamp string
	e := f.tx.QueryRow(ctx, `SELECT t.id,t.root_moment_id,t.audience,t.status,t.version,jsonb_build_array(t.xmin::text,origin.xmin::text)::text,clock_timestamp() FROM moment_threads t LEFT JOIN legacy_native_context_entities origin ON origin.kind='thread' AND origin.target_id=t.id AND origin.owner_account_id=t.author_account_id WHERE `+selector+` AND t.author_account_id=$2 AND(t.audience='PUBLIC' OR origin.target_id IS NOT NULL)`, id, f.a.ID).Scan(&v.ThreadID, &root, &v.Audience, &v.Status, &v.Version, &stamp, &v.ObservedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, content.ErrNotFound
	}
	if e != nil {
		return v, content.ErrUnavailable
	}
	v.ObservedAt = v.ObservedAt.UTC()
	v.ValidUntil = v.ObservedAt.Add(30 * time.Second)
	if e = ownedContextLeaseTx(ctx, f, &v.ValidUntil); e != nil {
		return v, e
	}
	proof := []any{stamp}
	readable := map[string]bool{}
	rows, e := f.tx.Query(ctx, `SELECT x.position,m.id,m.revision,m.status,m.xmin::text,x.xmin::text FROM moment_thread_members x JOIN moments m ON m.id=x.moment_id AND m.author_account_id=$2 WHERE x.thread_id=$1 ORDER BY x.position LIMIT 21`, v.ThreadID, f.a.ID)
	if e != nil {
		return v, content.ErrUnavailable
	}
	for rows.Next() {
		var p content.MomentThreadPosition
		var mid, status, ms, xs string
		if e = rows.Scan(&p.Position, &mid, &p.Revision, &status, &ms, &xs); e != nil {
			break
		}
		proof = append(proof, []any{p.Position, mid, p.Revision, status, ms, xs})
		p.Status = "UNAVAILABLE"
		if status != "withdrawn" {
			p.MomentID = &mid
			p.Status = "AVAILABLE"
			readable[mid] = true
			if mid == root {
				v.RootMomentID = &mid
			}
		} else {
			p.Revision = 0
		}
		v.Members = append(v.Members, p)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil || len(v.Members) < 1 || len(v.Members) > 20 {
		return v, content.ErrUnavailable
	}
	for i, m := range v.Members {
		if m.Position != i {
			return v, content.ErrUnavailable
		}
	}
	rows, e = f.tx.Query(ctx, `SELECT position,moment_id,inbound_origin,inbound_mode,duration_minutes,estimated_duration_minutes,xmin::text FROM moment_thread_entry_contexts WHERE thread_id=$1 ORDER BY position`, v.ThreadID)
	if e != nil {
		return v, content.ErrUnavailable
	}
	for rows.Next() {
		var x content.OwnedThreadEntryContext
		var stamp string
		if e = rows.Scan(&x.Position, &x.MomentID, &x.InboundOrigin, &x.InboundMode, &x.DurationMinutes, &x.EstimatedDurationMinutes, &stamp); e != nil {
			break
		}
		proof = append(proof, []any{stamp, x})
		if readable[x.MomentID] {
			v.EntryContexts = append(v.EntryContexts, x)
		}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return v, content.ErrUnavailable
	}
	rows, e = f.tx.Query(ctx, `SELECT id,from_moment_id,to_moment_id,from_position,to_position,mode,coalesce(from_place_id::text,''),coalesce(to_place_id::text,''),source_created_at,source_updated_at,xmin::text FROM moment_thread_route_legs WHERE thread_id=$1 ORDER BY from_position,to_position,id`, v.ThreadID)
	if e != nil {
		return v, content.ErrUnavailable
	}
	for rows.Next() {
		var x content.OwnedThreadLeg
		var stamp string
		if e = rows.Scan(&x.ID, &x.FromMomentID, &x.ToMomentID, &x.FromPosition, &x.ToPosition, &x.Mode, &x.FromPlaceID, &x.ToPlaceID, &x.SourceCreatedAt, &x.SourceUpdatedAt, &stamp); e != nil {
			break
		}
		proof = append(proof, []any{stamp, x})
		if readable[x.FromMomentID] && readable[x.ToMomentID] {
			v.Legs = append(v.Legs, x)
		}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return v, content.ErrUnavailable
	}
	v.SourceVersion = content.MomentControlHash(proof)
	return v, nil
}

type ownedThreadProof struct {
	s        *Store
	d        [32]byte
	a        identity.Actor
	frame    identity.HumanControlFrame
	id       string
	byMoment bool
	v        content.OwnedMomentThreadView
}

func (p *ownedThreadProof) RevalidateHumanControl(ctx context.Context) error {
	if !p.v.ValidUntil.After(time.Now().UTC()) {
		return identity.ErrControlChanged
	}
	ctx = identity.WithHumanControlFrame(ctx, p.d, p.a, p.frame)
	_, e := p.s.readOwnedThread(ctx, p.d, p.a, p.id, p.byMoment, &p.v)
	return e
}
func (s *Store) readOwnedThread(ctx context.Context, d [32]byte, a identity.Actor, id string, byMoment bool, expected *content.OwnedMomentThreadView) (content.OwnedMomentThreadView, error) {
	if !identity.HumanControlUUID(id) {
		return content.OwnedMomentThreadView{}, content.ErrInvalid
	}
	ctx, frame, e := s.humanWriteBound(ctx, d, a)
	if e != nil {
		return content.OwnedMomentThreadView{}, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return content.OwnedMomentThreadView{}, content.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	f := humanWriteFrame{tx: tx, d: d, a: a, session: frame}
	v, e := ownedThreadTx(ctx, f, id, byMoment)
	if e != nil {
		return v, e
	}
	if expected != nil {
		if expected.SourceVersion != v.SourceVersion || !expected.ValidUntil.After(v.ObservedAt) || v.ValidUntil.Before(expected.ValidUntil) {
			return v, identity.ErrControlChanged
		}
		v.ObservedAt, v.ValidUntil = expected.ObservedAt, expected.ValidUntil
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, v.ValidUntil, func() error {
		fresh, e := ownedThreadTx(ctx, f, id, byMoment)
		if e != nil {
			return e
		}
		if fresh.SourceVersion != v.SourceVersion || fresh.ValidUntil.Before(v.ValidUntil) {
			return identity.ErrControlChanged
		}
		return nil
	})); e != nil {
		return content.OwnedMomentThreadView{}, e
	}
	v.ResponseProof = &ownedThreadProof{s, d, a, frame, id, byMoment, v}
	return v, nil
}
func (s *Store) ReadOwnedMomentThread(ctx context.Context, d [32]byte, a identity.Actor, id string, byMoment bool) (content.OwnedMomentThreadView, error) {
	return s.readOwnedThread(ctx, d, a, id, byMoment, nil)
}
