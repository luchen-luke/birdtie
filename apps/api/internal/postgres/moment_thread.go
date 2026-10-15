package postgres

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	locations []momentLocationsSnapshot
	externals []string
	edit      *momentThreadEditSnapshot
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
	if in.Action == "EDIT" {
		return s.captureMomentThreadEdit(ctx, f, in)
	}
	if in.Action == "WITHDRAW" {
		var edited bool
		if e := f.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM moment_thread_edit_states WHERE thread_id=$1)`, in.ThreadID).Scan(&edited); e != nil {
			return momentThreadSource{}, identity.ErrControlUnavailable
		}
		if edited {
			return s.captureMomentThreadEdit(ctx, f, in)
		}
	}
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
	e = f.tx.QueryRow(ctx, `SELECT count(*) FROM moment_thread_operations WHERE actor_account_id=$1 AND operation_id<>$2 AND status='SUCCEEDED' AND receipt->>'action' IN('CREATE_PUBLISH','PUBLISH','EDIT') AND committed_at>clock_timestamp()-interval '1 minute'`, f.a.ID, in.OperationID).Scan(&recent)
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
	if in.Action == "PUBLISH" || in.Action == "EDIT" {
		src.members = []content.MomentThreadMember{}
	}
	result := content.MomentThreadPreview{HumanControlPreview: base, Input: in, SourceVersion: src.version, Members: src.members, Materials: src.materials, Contexts: src.contexts, Locations: locations}
	if in.Action == "EDIT" {
		preserved := []*momentmedia.Set{}
		for _, carry := range src.edit.carries {
			preserved = append(preserved, carry.set)
		}
		result.PreservedMedia = &preserved
	}
	return result, nil
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
	if src.edit != nil {
		return s.commitEditedThreadInTx(ctx, f, in, src, digest, until)
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
	var edited bool
	e = tx.QueryRow(ctx, `SELECT t.id,t.author_account_id,t.root_moment_id,t.version,t.status,jsonb_build_array(t.xmin::text,e.xmin::text,e.version)::text,a.xmin::text,e.thread_id IS NOT NULL FROM moment_threads t JOIN accounts a ON a.id=t.author_account_id AND a.status='active' LEFT JOIN moment_thread_edit_states e ON e.thread_id=t.id WHERE `+selector, id).Scan(&view.ThreadID, &owner, &root, &view.Version, &status, &stamp, &ownerStamp, &edited)
	if errors.Is(e, pgx.ErrNoRows) {
		return view, content.ErrNotFound
	}
	if e != nil {
		return view, identity.ErrControlUnavailable
	}
	rows, e := tx.Query(ctx, `SELECT CASE WHEN $3 THEN d.display_position ELSE x.position END,x.moment_id,m.revision,m.status,birdtie_moment_public_binding(m.id,NULLIF($2,'')::uuid,clock_timestamp()),jsonb_build_array(m.xmin::text,d.xmin::text,d.version)::text FROM moment_thread_members x JOIN moments m ON m.id=x.moment_id LEFT JOIN moment_thread_display_members d ON d.thread_id=x.thread_id AND d.moment_id=x.moment_id WHERE x.thread_id=$1 AND(NOT $3 OR d.display_position IS NOT NULL) ORDER BY 1`, view.ThreadID, access.Actor.ID, edited)
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
			if moment == root {
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
	minimum := 2
	if edited {
		minimum = 1
	}
	if e != nil || len(view.Members) < minimum || len(view.Members) > content.MomentThreadMaxEntries {
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
	if edited {
		view.SchemaVersion = "moment-thread-view-v2"
	}
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
	var edited bool
	e := f.tx.QueryRow(ctx, `SELECT t.id,t.root_moment_id,t.audience,t.status,t.version,jsonb_build_array(t.xmin::text,origin.xmin::text,es.xmin::text,es.version)::text,clock_timestamp(),es.thread_id IS NOT NULL FROM moment_threads t LEFT JOIN moment_thread_edit_states es ON es.thread_id=t.id LEFT JOIN legacy_native_context_entities origin ON origin.kind='thread' AND origin.target_id=t.id AND origin.owner_account_id=t.author_account_id WHERE `+selector+` AND t.author_account_id=$2 AND(t.audience='PUBLIC' OR origin.target_id IS NOT NULL)`, id, f.a.ID).Scan(&v.ThreadID, &root, &v.Audience, &v.Status, &v.Version, &stamp, &v.ObservedAt, &edited)
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
	if edited {
		v.SchemaVersion = "owned-moment-thread-v2"
		v.RootMomentID = &root
		retired := []content.MomentThreadPosition{}
		v.RetiredMembers = &retired
	}
	rows, e := f.tx.Query(ctx, `SELECT CASE WHEN $3 THEN d.display_position ELSE x.position END,m.id,m.revision,m.status,m.xmin::text,jsonb_build_array(x.xmin::text,d.xmin::text,d.version)::text FROM moment_thread_members x JOIN moments m ON m.id=x.moment_id AND m.author_account_id=$2 LEFT JOIN moment_thread_display_members d ON d.thread_id=x.thread_id AND d.moment_id=x.moment_id WHERE x.thread_id=$1 AND(NOT $3 OR d.display_position IS NOT NULL) ORDER BY 1 LIMIT 21`, v.ThreadID, f.a.ID, edited)
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
	if edited {
		rows, e = f.tx.Query(ctx, `SELECT x.position,m.id,m.revision,m.xmin::text,d.xmin::text,d.version FROM moment_thread_display_members d JOIN moment_thread_members x ON x.thread_id=d.thread_id AND x.moment_id=d.moment_id JOIN moments m ON m.id=d.moment_id AND m.author_account_id=$2 WHERE d.thread_id=$1 AND(d.display_position IS NULL OR m.status='withdrawn') ORDER BY x.position`, v.ThreadID, f.a.ID)
		if e != nil {
			return v, content.ErrUnavailable
		}
		for rows.Next() {
			var p content.MomentThreadPosition
			var id, ms, ds string
			var version int64
			if e = rows.Scan(&p.Position, &id, &p.Revision, &ms, &ds, &version); e != nil {
				break
			}
			p.MomentID = &id
			p.Status = "RETIRED"
			*v.RetiredMembers = append(*v.RetiredMembers, p)
			proof = append(proof, []any{p, ms, ds, version})
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
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

type momentThreadEditSnapshot struct {
	root         string
	edited       bool
	all          []content.MomentThreadMember // immutable historical order
	current      map[string]content.Moment
	display      map[string]int // -1 is retired
	carries      []momentMediaCarry
	mediaSources []string
}

// All current children are locked and bound, including retired identities. A
// supplied display order cannot silently omit another current child.
func (s *Store) captureMomentThreadEdit(ctx context.Context, f humanWriteFrame, in content.MomentThreadInput) (momentThreadSource, error) {
	x := &momentThreadEditSnapshot{all: []content.MomentThreadMember{}, current: map[string]content.Moment{}, display: map[string]int{}}
	out := momentThreadSource{edit: x, members: []content.MomentThreadMember{}, moments: []content.Moment{}, materials: [][]content.MomentThreadMaterial{}, contexts: []content.MomentThreadContext{}}
	var principal string
	if e := f.tx.QueryRow(ctx, `SELECT xmin::text FROM accounts WHERE id=$1 AND account_type='person' AND status='active'`, f.a.ID).Scan(&principal); e != nil {
		return out, identity.ErrControlChanged
	}
	values := []any{"moment-thread-edit-source-v1", principal}
	if in.ThreadID != "" {
		var version int64
		var status, audience, stamp string
		e := f.tx.QueryRow(ctx, `SELECT t.root_moment_id,t.version,t.status,t.audience,jsonb_build_array(t.xmin::text,e.xmin::text,e.version,e.operation_id)::text,e.thread_id IS NOT NULL FROM moment_threads t LEFT JOIN moment_thread_edit_states e ON e.thread_id=t.id WHERE t.id=$1 AND t.author_account_id=$2 FOR UPDATE OF t`, in.ThreadID, f.a.ID).Scan(&x.root, &version, &status, &audience, &stamp, &x.edited)
		if errors.Is(e, pgx.ErrNoRows) {
			return out, content.ErrNotFound
		}
		if e != nil {
			return out, identity.ErrControlUnavailable
		}
		if version != in.ExpectedVersion || in.Action == "WITHDRAW" && (status != "published" || audience != "PUBLIC") {
			return out, identity.ErrControlChanged
		}
		values = append(values, stamp)
		rows, e := f.tx.Query(ctx, `SELECT m.moment_id,m.position,CASE WHEN e.thread_id IS NULL THEN m.position ELSE coalesce(d.display_position,-1) END,coalesce(jsonb_build_array(d.xmin::text,d.version,d.display_position)::text,'') FROM moment_thread_members m LEFT JOIN moment_thread_edit_states e ON e.thread_id=m.thread_id LEFT JOIN moment_thread_display_members d ON d.thread_id=m.thread_id AND d.moment_id=m.moment_id WHERE m.thread_id=$1 ORDER BY m.position`, in.ThreadID)
		if e != nil {
			return out, identity.ErrControlUnavailable
		}
		for rows.Next() {
			m := content.MomentThreadMember{Media: []momentmedia.StageSelector{}}
			var display int
			var ds string
			if e = rows.Scan(&m.MomentID, &m.Position, &display, &ds); e != nil {
				break
			}
			x.all = append(x.all, m)
			x.display[m.MomentID] = display
			values = append(values, []any{m.MomentID, m.Position, display, ds})
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil || len(x.all) < 1 {
			return out, identity.ErrControlUnavailable
		}
	} else if in.BaseMomentID != nil && *in.BaseMomentID != "" {
		x.root = *in.BaseMomentID
		x.all = append(x.all, content.MomentThreadMember{MomentID: x.root, Position: 0, Media: []momentmedia.StageSelector{}})
		x.display[x.root] = 0
	}
	ids := []string{}
	for _, m := range x.all {
		ids = append(ids, m.MomentID)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if _, e := f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,152))`, "moment-media:"+id); e != nil {
			return out, identity.ErrControlUnavailable
		}
	}
	for _, id := range ids {
		var locked string
		if e := f.tx.QueryRow(ctx, `SELECT id FROM moments WHERE id=$1 AND author_account_id=$2 FOR UPDATE`, id, f.a.ID).Scan(&locked); e != nil {
			return out, identity.ErrControlChanged
		}
	}
	if in.ThreadID == "" && x.root != "" {
		var bound bool
		if e := f.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM moment_thread_members WHERE moment_id=$1)`, x.root).Scan(&bound); e != nil {
			return out, identity.ErrControlUnavailable
		}
		if bound {
			return out, identity.ErrControlChanged
		}
	}
	for i := range x.all {
		m, e := getOwnMomentInTx(ctx, f.tx, f.a.ID, x.all[i].MomentID)
		if e != nil {
			return out, e
		}
		owned, e := momentControlOwnedStamp(ctx, f, m.ID)
		if e != nil {
			return out, e
		}
		var ms string
		if e = f.tx.QueryRow(ctx, `SELECT coalesce((SELECT version FROM public_moment_media_sets WHERE moment_id=$1),0),coalesce((SELECT jsonb_build_array(xmin::text,version,status)::text FROM public_moment_media_sets WHERE moment_id=$1),'')`, m.ID).Scan(&x.all[i].MediaSetVersion, &ms); e != nil {
			return out, identity.ErrControlUnavailable
		}
		x.all[i].Status = m.Status
		x.all[i].AppliedRevision = m.Revision
		x.current[m.ID] = m
		if !x.edited && m.Status == "withdrawn" {
			x.display[m.ID] = -1
		}
		out.moments = append(out.moments, m)
		values = append(values, []any{m.ID, owned, ms})
	}
	if in.Action == "WITHDRAW" {
		for _, m := range x.all {
			if pos := x.display[m.MomentID]; pos >= 0 {
				m.Position = pos
				out.members = append(out.members, m)
			}
		}
		sort.Slice(out.members, func(i, j int) bool { return out.members[i].Position < out.members[j].Position })
		out.version = content.MomentControlHash(values)
		return out, nil
	}
	selected := map[string]bool{}
	removed := map[string]bool{}
	for _, item := range in.Items {
		if item.MomentID == "" {
			if !threadOriginalContexts(content.Moment{}, item.Input) {
				return out, identity.ErrControlChanged
			}
			continue
		}
		m, ok := x.current[item.MomentID]
		if !ok || m.Revision != item.ExpectedRevision || (*item.Restore) != (m.Status == "withdrawn") || item.Media == nil && (m.Status != "published" || m.Visibility != "public") {
			return out, identity.ErrControlChanged
		}
		if m.Status != "published" && m.Status != "draft" && m.Status != "withdrawn" {
			return out, identity.ErrControlChanged
		}
		if !threadOriginalContexts(m, item.Input) {
			return out, identity.ErrControlChanged
		}
		selected[m.ID] = true
	}
	for _, item := range *in.RemovedItems {
		m, ok := x.current[item.MomentID]
		if !ok || m.Revision != item.ExpectedRevision || x.display[m.ID] < 0 {
			return out, identity.ErrControlChanged
		}
		removed[m.ID] = true
	}
	for _, m := range x.all {
		// A separately withdrawn entry is unavailable in the editor. Omitting
		// its display slot retires that slot without rewriting or restoring it.
		if x.display[m.MomentID] >= 0 && x.current[m.MomentID].Status != "withdrawn" && !selected[m.MomentID] && !removed[m.MomentID] {
			return out, identity.ErrControlChanged
		}
	}
	// One quota admission per concrete compound action, including single entry.
	if _, e := f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,153))`, f.a.ID+":MOMENT_THREAD_QUOTA"); e != nil {
		return out, identity.ErrControlUnavailable
	}
	var recent int
	if e := f.tx.QueryRow(ctx, `SELECT count(*) FROM moment_thread_operations WHERE actor_account_id=$1 AND operation_id<>$2 AND status='SUCCEEDED' AND receipt->>'action' IN('CREATE_PUBLISH','PUBLISH','EDIT') AND committed_at>clock_timestamp()-interval '1 minute'`, f.a.ID, in.OperationID).Scan(&recent); e != nil {
		return out, identity.ErrControlUnavailable
	}
	if recent >= 10 {
		return out, identity.ErrControlChanged
	}
	lockItems := append([]content.MomentThreadItem{}, in.Items...)
	for i, item := range lockItems {
		if item.Input.Locations == nil && item.MomentID != "" {
			m := x.current[item.MomentID]
			if item.Input.CityID == m.CityID && item.Input.PlaceID == m.PlaceID && item.Input.LocationPrecision == m.LocationPrecision {
				rows, e := readMomentLocationRows(ctx, f.tx, m.ID)
				if e != nil {
					return out, e
				}
				links := []content.MomentLocationInput{}
				for _, r := range rows {
					if r.status == "ACTIVE" {
						links = append(links, r.input)
					}
				}
				lockItems[i].Input.Locations = &links
			}
		}
	}
	if e := threadLockSelectors(ctx, f, lockItems); e != nil {
		return out, e
	}
	mediaIDs := []string{}
	for _, item := range in.Items {
		for _, m := range item.Media {
			mediaIDs = append(mediaIDs, m.MediaID)
		}
	}
	if len(mediaIDs) > 0 {
		if e := s.LockMomentMediaMaterialsInTx(ctx, f.tx, threadAccess(f), mediaIDs); e != nil {
			return out, e
		}
	}
	for position, item := range in.Items {
		var current *content.Moment
		var existing content.Moment
		if item.MomentID != "" {
			existing = x.current[item.MomentID]
			current = &existing
		}
		mi := content.MomentControlInput{OperationID: in.OperationID, Action: "UPDATE_PUBLIC", MomentID: item.MomentID, ExpectedRevision: item.ExpectedRevision, Input: &item.Input}
		stamp, until, _, e := momentControlExternal(ctx, f, mi, existing, true)
		if e != nil {
			return out, e
		}
		out.externals = append(out.externals, stamp)
		values = append(values, stamp)
		momentControlAddDeadline(&out.deadline, until)
		lf := f
		lf.op = fmt.Sprintf("%s:%d", f.op, position)
		loc, e := captureMomentLocationsInTx(ctx, lf, current, item.Input)
		if e != nil {
			return out, e
		}
		out.locations = append(out.locations, loc)
		values = append(values, loc.SourceVersion)
		momentControlAddDeadline(&out.deadline, loc.ValidUntil)
		c := content.MomentThreadContext{CityID: item.Input.CityID, PlaceID: item.Input.PlaceID}
		if c.CityID != "" {
			if e = f.tx.QueryRow(ctx, `SELECT name FROM cities WHERE id=$1`, c.CityID).Scan(&c.CityName); e != nil {
				return out, identity.ErrControlUnavailable
			}
		}
		if c.PlaceID != "" {
			if e = f.tx.QueryRow(ctx, `SELECT name FROM places WHERE id=$1`, c.PlaceID).Scan(&c.PlaceName); e != nil {
				return out, identity.ErrControlUnavailable
			}
		}
		out.contexts = append(out.contexts, c)
		materials := []content.MomentThreadMaterial{}
		carry := momentMediaCarry{}
		mediaSource := ""
		if item.Media == nil {
			carry, e = s.captureMomentMediaCarry(ctx, f.tx, threadAccess(f), item.MomentID)
			if e != nil {
				return out, e
			}
			var ready bool
			if e = f.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public_moment_media_sets WHERE moment_id=$1 AND status='READY')`, item.MomentID).Scan(&ready); e != nil {
				return out, identity.ErrControlUnavailable
			}
			if ready && carry.set == nil {
				return out, identity.ErrControlChanged
			}
			if carry.set != nil {
				values = append(values, carry.set.SourceVersion)
				momentControlAddDeadline(&out.deadline, &carry.set.ValidUntil)
			}
		}
		if len(item.Media) > 0 {
			snapshot, e := s.CaptureMomentMediaMaterialsInTx(ctx, f.tx, threadAccess(f), item.Media)
			if e != nil {
				return out, e
			}
			if len(snapshot.Materials) != len(item.Media) || len(snapshot.Thumbnails) != len(item.Media) {
				return out, identity.ErrControlUnavailable
			}
			for i, m := range snapshot.Materials {
				if m.LocalScope != in.LocalScope {
					return out, identity.ErrControlChanged
				}
				materials = append(materials, content.MomentThreadMaterial{Material: m, ThumbnailBase64: base64.StdEncoding.EncodeToString(snapshot.Thumbnails[i])})
			}
			mediaSource = snapshot.SourceVersion
			values = append(values, mediaSource)
			momentControlAddDeadline(&out.deadline, &snapshot.ValidUntil)
		}
		if !content.HasMomentBody(item.Input.Body) && len(item.Media) == 0 && len(carry.materials) == 0 {
			return out, identity.ErrControlInvalid
		}
		x.carries = append(x.carries, carry)
		x.mediaSources = append(x.mediaSources, mediaSource)
		out.materials = append(out.materials, materials)
	}
	out.version = content.MomentControlHash(values)
	return out, nil
}

func threadOriginalContexts(m content.Moment, in content.MomentInput) bool {
	value := func(v *string) string {
		if v == nil {
			return ""
		}
		return *v
	}
	activity := ""
	if len(m.ActivityIDs) > 1 {
		return false
	}
	if len(m.ActivityIDs) == 1 {
		activity = m.ActivityIDs[0]
	}
	return activity == value(in.ActivityID) && m.CommunityID == value(in.CommunityID) && m.OrganizationID == value(in.OrganizationID)
}

func applyThreadEditedMoment(ctx context.Context, f humanWriteFrame, old content.Moment, input content.MomentInput, locations momentLocationsSnapshot) (content.Moment, error) {
	city, place, precision := momentLocationLegacyFields(locations.Effective)
	var id string
	e := f.tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE moments m SET title=$4,body=$5,occurred_at=$6,time_precision=$7,city_id=NULLIF($8,''),place_id=NULLIF($9,'')::uuid,location_precision=$10,visibility='public',status='published',author_confirmed_at=CASE WHEN m.status='published' THEN m.author_confirmed_at ELSE stamp.at END,published_at=CASE WHEN m.status='published' THEN m.published_at ELSE stamp.at END,updated_at=stamp.at,revision=revision+1 FROM stamp WHERE m.id=$1 AND m.author_account_id=$2 AND m.revision=$3 AND m.status IN('draft','published','withdrawn') RETURNING m.id`, old.ID, f.a.ID, old.Revision, input.Title, input.Body, input.OccurredAt, input.TimePrecision, city, place, precision).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return content.Moment{}, identity.ErrControlChanged
	}
	if e != nil {
		return content.Moment{}, identity.ErrControlUnavailable
	}
	if e = applyMomentLocationsInTx(ctx, f, id, old.Revision+1, locations); e != nil {
		return content.Moment{}, e
	}
	if _, e = f.tx.Exec(ctx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)VALUES($1,'EDIT','moment',$2,'allowed','explicit_public_content')`, f.a.ID, id); e != nil {
		return content.Moment{}, identity.ErrControlUnavailable
	}
	return getOwnMomentInTx(ctx, f.tx, f.a.ID, id)
}

func threadCurrentMember(ctx context.Context, f humanWriteFrame, id string, position int) (content.MomentThreadMember, error) {
	m, e := getOwnMomentInTx(ctx, f.tx, f.a.ID, id)
	if e != nil {
		return content.MomentThreadMember{}, e
	}
	r := content.MomentThreadMember{MomentID: id, Position: position, Status: m.Status, AppliedRevision: m.Revision, Media: []momentmedia.StageSelector{}}
	if e = f.tx.QueryRow(ctx, `SELECT coalesce((SELECT version FROM public_moment_media_sets WHERE moment_id=$1),0)`, id).Scan(&r.MediaSetVersion); e != nil {
		return r, identity.ErrControlUnavailable
	}
	return r, nil
}

func editedThreadEffectStamp(ctx context.Context, f humanWriteFrame, id string) (string, error) {
	base, e := threadEffectStamp(ctx, f, id)
	if e != nil {
		return "", e
	}
	var raw string
	e = f.tx.QueryRow(ctx, `SELECT jsonb_build_array(e.xmin::text,e.version,e.operation_id,(SELECT jsonb_agg(jsonb_build_array(d.moment_id,d.display_position,d.version,d.xmin::text) ORDER BY d.moment_id) FROM moment_thread_display_members d WHERE d.thread_id=e.thread_id),(SELECT coalesce(jsonb_agg(jsonb_build_array(a.moment_id,a.set_version,a.position,a.media_id,a.media_version,a.xmin::text,a.retention_until) ORDER BY a.moment_id,a.set_version,a.position),'[]'::jsonb) FROM public_moment_media_attachments a JOIN moment_thread_members m ON m.moment_id=a.moment_id WHERE m.thread_id=e.thread_id))::text FROM moment_thread_edit_states e WHERE e.thread_id=$1`, id).Scan(&raw)
	if e != nil {
		return "", identity.ErrControlUnavailable
	}
	return content.MomentControlHash([]any{base, raw}), nil
}

// The original operation lock, session frame and concrete approval stay in the
// same transaction as all child changes, current display and the receipt.
func (s *Store) commitEditedThreadInTx(ctx context.Context, f humanWriteFrame, in content.MomentThreadInput, src momentThreadSource, digest string, until time.Time) (content.MomentThreadReceipt, error) {
	x := src.edit
	removed := []content.MomentThreadMember{}
	r := content.MomentThreadReceipt{SchemaVersion: "moment-thread-operation-v2", OperationID: in.OperationID, Status: "SUCCEEDED", Action: in.Action, ThreadID: in.ThreadID, RootMomentID: x.root, Audience: "PUBLIC", ExpectedVersion: in.ExpectedVersion, AppliedVersion: in.ExpectedVersion + 1, PayloadDigest: digest, Members: []content.MomentThreadMember{}, RemovedMembers: &removed}
	newIDs := []string{}
	active := map[string]int{}
	if in.Action == "EDIT" {
		for position, item := range in.Items {
			m := x.current[item.MomentID]
			var e error
			if item.MomentID == "" {
				m, e = createMomentDraftInTx(ctx, f.tx, f.a.ID, item.Input, src.locations[position])
				if e != nil {
					return r, humanMomentFailure(e)
				}
				newIDs = append(newIDs, m.ID)
			}
			m, e = applyThreadEditedMoment(ctx, f, m, item.Input, src.locations[position])
			if e != nil {
				return r, e
			}
			var version int64
			if e = f.tx.QueryRow(ctx, `SELECT coalesce((SELECT version FROM public_moment_media_sets WHERE moment_id=$1),0)`, m.ID).Scan(&version); e != nil {
				return r, identity.ErrControlUnavailable
			}
			var set *momentmedia.Set
			if len(item.Media) > 0 {
				v, e := s.AttachMomentMediaInTx(ctx, f.tx, threadAccess(f), m.ID, m.Revision, version, item.Media)
				if e != nil {
					return r, e
				}
				set = &v
			} else if item.Media == nil && len(x.carries[position].materials) > 0 {
				v, e := s.applyMomentMediaCarry(ctx, f.tx, threadAccess(f), m.ID, m.Revision, version, x.carries[position])
				if e != nil {
					return r, e
				}
				set = &v
			}
			if e = momentHasPublishableContent(ctx, f.tx, m.ID); e != nil {
				return r, e
			}
			member, e := threadCurrentMember(ctx, f, m.ID, position)
			if e != nil {
				return r, e
			}
			if set != nil {
				for _, a := range set.Attachments {
					member.Media = append(member.Media, momentmedia.StageSelector{MediaID: a.MediaID, Version: a.MediaVersion})
				}
			}
			r.Members = append(r.Members, member)
			active[m.ID] = position
			if r.RootMomentID == "" {
				r.RootMomentID = m.ID
			}
		}
		for _, remove := range *in.RemovedItems {
			m := x.current[remove.MomentID]
			if m.Status != "withdrawn" {
				if e := withdrawMomentInTx(ctx, f.tx, f.a.ID, m.ID, m.Revision); e != nil {
					return r, humanMomentFailure(e)
				}
			}
			position := -1
			for _, old := range x.all {
				if old.MomentID == m.ID {
					position = old.Position
					break
				}
			}
			member, e := threadCurrentMember(ctx, f, m.ID, position)
			if e != nil {
				return r, e
			}
			removed = append(removed, member)
		}
	} else {
		for _, m := range src.moments {
			if m.Status != "withdrawn" {
				if e := withdrawMomentInTx(ctx, f.tx, f.a.ID, m.ID, m.Revision); e != nil {
					return r, humanMomentFailure(e)
				}
			}
		}
		for _, old := range src.members {
			member, e := threadCurrentMember(ctx, f, old.MomentID, old.Position)
			if e != nil {
				return r, e
			}
			r.Members = append(r.Members, member)
			active[member.MomentID] = member.Position
		}
	}
	r.RemovedMembers = &removed
	if in.ThreadID == "" {
		if e := f.tx.QueryRow(ctx, `INSERT INTO moment_threads(author_account_id,root_moment_id,audience,status)VALUES($1,$2,'PUBLIC','published')RETURNING id`, f.a.ID, r.RootMomentID).Scan(&r.ThreadID); e != nil {
			return r, identity.ErrControlUnavailable
		}
	}
	if _, e := f.tx.Exec(ctx, `INSERT INTO moment_thread_edit_states(thread_id,version,operation_id)VALUES($1,$2,$3)ON CONFLICT(thread_id)DO UPDATE SET version=EXCLUDED.version,operation_id=EXCLUDED.operation_id`, r.ThreadID, r.AppliedVersion, in.OperationID); e != nil {
		return r, identity.ErrControlUnavailable
	}
	if in.ThreadID != "" {
		status := "published"
		if in.Action == "WITHDRAW" {
			status = "withdrawn"
		}
		tag, e := f.tx.Exec(ctx, `UPDATE moment_threads SET audience='PUBLIC',status=$4,version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND author_account_id=$2 AND version=$3`, r.ThreadID, f.a.ID, in.ExpectedVersion, status)
		if e != nil || tag.RowsAffected() != 1 {
			return r, identity.ErrControlChanged
		}
	}
	all := append([]content.MomentThreadMember{}, x.all...)
	if in.ThreadID == "" && x.root != "" {
		if _, e := f.tx.Exec(ctx, `INSERT INTO moment_thread_members(thread_id,moment_id,position)VALUES($1,$2,0)`, r.ThreadID, x.root); e != nil {
			return r, identity.ErrControlUnavailable
		}
	}
	for _, id := range newIDs {
		position := len(all)
		if _, e := f.tx.Exec(ctx, `INSERT INTO moment_thread_members(thread_id,moment_id,position)VALUES($1,$2,$3)`, r.ThreadID, id, position); e != nil {
			return r, identity.ErrControlUnavailable
		}
		all = append(all, content.MomentThreadMember{MomentID: id, Position: position})
	}
	for _, member := range all {
		var position *int
		if p, ok := active[member.MomentID]; ok {
			value := p
			position = &value
		}
		if _, e := f.tx.Exec(ctx, `INSERT INTO moment_thread_display_members(thread_id,moment_id,display_position,version)VALUES($1,$2,$3,$4)ON CONFLICT(thread_id,moment_id)DO UPDATE SET display_position=EXCLUDED.display_position,version=EXCLUDED.version`, r.ThreadID, member.MomentID, position, r.AppliedVersion); e != nil {
			return r, identity.ErrControlUnavailable
		}
	}
	var at time.Time
	if e := f.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		return r, identity.ErrControlUnavailable
	}
	at = at.UTC()
	r.CommittedAt = &at
	if !content.ValidMomentThreadReceipt(r, in.OperationID) {
		return r, identity.ErrControlUnavailable
	}
	raw, _ := json.Marshal(r)
	b, _ := hex.DecodeString(digest)
	if _, e := f.tx.Exec(ctx, `INSERT INTO moment_thread_operations(actor_account_id,operation_id,status,thread_id,payload_sha256,receipt,committed_at)VALUES($1,$2,'SUCCEEDED',$3,$4,$5,$6)`, f.a.ID, in.OperationID, r.ThreadID, b, raw, at); e != nil {
		return r, identity.ErrControlUnavailable
	}
	effect, e := editedThreadEffectStamp(ctx, f, r.ThreadID)
	if e != nil {
		return r, e
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, until, func() error {
		fresh, e := editedThreadEffectStamp(ctx, f, r.ThreadID)
		if e != nil {
			return e
		}
		if fresh != effect {
			return identity.ErrControlChanged
		}
		for i, item := range in.Items {
			if e = revalidateMomentLocationsInTx(ctx, f, src.locations[i], until); e != nil {
				return e
			}
			stamp, deadline, _, e := momentControlExternal(ctx, f, content.MomentControlInput{OperationID: in.OperationID, Action: "UPDATE_PUBLIC", MomentID: item.MomentID, ExpectedRevision: item.ExpectedRevision, Input: &item.Input}, x.current[item.MomentID], true)
			if e != nil {
				return e
			}
			if stamp != src.externals[i] || deadline != nil && deadline.Before(until) {
				return identity.ErrControlChanged
			}
			if len(item.Media) > 0 {
				snapshot, e := s.CaptureMomentMediaMaterialsInTx(ctx, f.tx, threadAccess(f), item.Media)
				if e != nil {
					return e
				}
				if snapshot.SourceVersion != x.mediaSources[i] || snapshot.ValidUntil.Before(until) {
					return identity.ErrControlChanged
				}
			}
			if e = momentHasPublishableContent(ctx, f.tx, r.Members[i].MomentID); e != nil {
				return e
			}
			if len(r.Members[i].Media) > 0 {
				set, e := s.mediaSetTx(ctx, f.tx, threadAccess(f), r.Members[i].MomentID, true)
				if e != nil {
					return e
				}
				if set.Status != "READY" || set.ValidUntil.Before(until) || len(set.Attachments) != len(r.Members[i].Media) {
					return identity.ErrControlChanged
				}
			}
		}
		return nil
	})); e != nil {
		return content.MomentThreadReceipt{}, e
	}
	return r, nil
}
