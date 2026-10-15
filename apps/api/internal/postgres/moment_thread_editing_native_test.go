package postgres

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/momentmedia"
	"image"
	"image/jpeg"
	"testing"
	"time"
)

func TestMomentThreadEditingNativeSingleUpgradeOrderedRetirementAndRecovery(t *testing.T) {
	f := savedPageNative(t)
	restore := false
	empty := ""
	removed := []content.MomentThreadRemoval{}
	scope := f.id(t)
	none := []content.MomentLocationInput{}
	text := func(body string) content.MomentInput {
		return content.MomentInput{Body: body, TimePrecision: "unknown", LocationPrecision: "none", Locations: &none}
	}
	_, place := f.bookmark(t)
	mc144RetainedPlace(t, f, place, 0)
	city := "synthetic168-" + f.id(t)
	f.exec(t, `INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label)VALUES($1,'合成跨城168','合成地区','GB','Europe/London','published','LOCAL_SYNTHETIC_FIXTURE','disposable://editing168','合成')`, city)
	links := []content.MomentLocationInput{{ID: f.id(t), Kind: "PLACE", PlaceID: place, Precision: "place"}, {ID: f.id(t), Kind: "CITY", CityID: city, Precision: "city"}}
	value := text("")
	value.Locations = &links
	private := text("同地点私有条目不因公开串文而扩权")
	private.Locations = &[]content.MomentLocationInput{{ID: f.id(t), Kind: "PLACE", PlaceID: place, Precision: "place"}}
	privateFact, _ := mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "CREATE", Input: &private})
	var pixels bytes.Buffer
	if e := jpeg.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 32, 32)), nil); e != nil {
		t.Fatal(e)
	}
	access := momentmedia.Access{AccountID: f.actor.ID, AccountType: "person", SessionDigest: f.digest}
	prepare := func() []momentmedia.AttachmentInput {
		t.Helper()
		stage, e := f.store.PrepareMomentMedia(f.ctx, access, momentmedia.StageInput{OperationID: f.id(t), LocalScope: scope, ContentType: "image/jpeg", Content: pixels.Bytes()})
		if e != nil || stage.Material == nil {
			t.Fatal("ready stage", e)
		}
		return []momentmedia.AttachmentInput{{MediaID: stage.Material.MediaID, MediaVersion: stage.Material.Version, Rights: momentmedia.Rights{Basis: "OWNED", CanRetain: true, CanPublish: true, RetentionUntil: time.Now().UTC().Add(time.Hour)}}}
	}
	attachments := prepare()
	draft, _ := mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "CREATE", Input: &value})
	first, _ := mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "PUBLISH", MomentID: draft.MomentID, ExpectedRevision: 1, Input: &value, Media: &attachments, LocalScope: scope})
	root := first.MomentID
	other := interactionNativeActor(t, f)
	interactionNativeWrite(t, other, content.MomentInteractionInput{OperationID: f.id(t), Action: "LIKE", MomentID: root, ExpectedMomentRevision: 2})
	comment, _ := interactionNativeWrite(t, other, content.MomentInteractionInput{OperationID: f.id(t), Action: "COMMENT", MomentID: root, ExpectedMomentRevision: 2, ExpectedLikeVersion: 1, Body: "original identity"})
	base := root
	in := content.MomentThreadInput{OperationID: f.id(t), Action: "EDIT", BaseMomentID: &base, LocalScope: scope, Audience: "PUBLIC", RemovedItems: &removed, Items: []content.MomentThreadItem{{MomentID: root, ExpectedRevision: 2, Restore: &restore, Input: value, Media: nil}}}
	for i := 1; i < 20; i++ {
		in.Items = append(in.Items, content.MomentThreadItem{Restore: &restore, Input: text(fmt.Sprintf("entry %d", i)), Media: []momentmedia.AttachmentInput{}})
	}
	failed := in
	failed.OperationID = f.id(t)
	failed.Items = append([]content.MomentThreadItem{}, in.Items...)
	failed.Items[1].Input = text("EDIT_ATOMIC_SECOND_FAILURE")
	approvedFailure := threadNativeApprove(t, f, failed)
	f.exec(t, `CREATE FUNCTION edit_test_second_fail() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.body='EDIT_ATOMIC_SECOND_FAILURE' THEN RAISE EXCEPTION 'isolated editing rollback';END IF;RETURN NEW;END$$`)
	f.exec(t, `CREATE TRIGGER edit_test_second BEFORE INSERT ON moments FOR EACH ROW EXECUTE FUNCTION edit_test_second_fail()`)
	if _, e := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, approvedFailure); e == nil {
		t.Fatal("compound child failure committed")
	}
	f.exec(t, `DROP TRIGGER edit_test_second ON moments`)
	f.exec(t, `DROP FUNCTION edit_test_second_fail()`)
	if m, e := f.store.GetOwnMoment(f.ctx, f.actor.ID, root); e != nil || m.Revision != 2 || m.Status != "published" {
		t.Fatal("failed adoption changed original", e)
	}
	if _, e := f.store.ReadMomentThreadOperation(f.ctx, f.digest, f.actor, failed.OperationID, false); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("failed operation gained fact", e)
	}
	write := func(in content.MomentThreadInput) (content.MomentThreadReceipt, content.MomentThreadInput) {
		t.Helper()
		approved := threadNativeApprove(t, f, in)
		r, e := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, approved)
		if e != nil {
			t.Fatal("native EDIT commit", in.Action, e)
		}
		if !content.ValidMomentThreadReceipt(r, in.OperationID) {
			t.Fatal("invalid receipt")
		}
		return r, approved
	}
	r, original := write(in)
	firstReceipt := r
	if len(r.Members) != 20 || r.RootMomentID != root || r.Members[0].MomentID != root || len(r.Members[0].Media) != 1 {
		t.Fatal("standalone identity or media carry lost")
	}
	interactions, e := f.store.ReadMomentInteractions(f.ctx, content.PublicMomentAccess{Actor: f.actor, SessionDigest: f.digest}, root, "", comment.CommentID)
	if e != nil || interactions.LikeCount != 1 || interactions.CommentCount != 1 || interactions.Comments[0].ID != comment.CommentID {
		t.Fatal("original interactions detached", e)
	}
	next := func(r content.MomentThreadReceipt, items []content.MomentThreadItem, rem []content.MomentThreadRemoval) content.MomentThreadInput {
		return content.MomentThreadInput{OperationID: f.id(t), Action: "EDIT", ThreadID: r.ThreadID, BaseMomentID: &empty, LocalScope: scope, Audience: "PUBLIC", ExpectedVersion: r.AppliedVersion, Items: items, RemovedItems: &rem}
	}
	currentItems := func(r content.MomentThreadReceipt) []content.MomentThreadItem {
		t.Helper()
		items := []content.MomentThreadItem{}
		for _, member := range r.Members {
			m, e := f.store.GetOwnMoment(f.ctx, f.actor.ID, member.MomentID)
			if e != nil {
				t.Fatal(e)
			}
			input := text(m.Body)
			input.Title = m.Title
			if m.ID == root && m.Status == "published" && len(member.Media) > 0 {
				input = value
			}
			items = append(items, content.MomentThreadItem{MomentID: m.ID, ExpectedRevision: m.Revision, Restore: &restore, Input: input, Media: nil})
		}
		return items
	}
	ordered := currentItems(r)
	ordered = append(ordered[1:], ordered[0])
	reorder := next(r, ordered, []content.MomentThreadRemoval{})
	stale := threadNativeApprove(t, f, reorder)
	change := ordered[0].Input
	change.Body = "independent original child edit"
	changed, _ := mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "UPDATE_PUBLIC", MomentID: ordered[0].MomentID, ExpectedRevision: ordered[0].ExpectedRevision, Input: &change})
	if _, e := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, stale); !errors.Is(e, identity.ErrControlChanged) {
		t.Fatal("stale compound CAS crossed child edit", e)
	}
	ordered[0].ExpectedRevision = changed.AppliedRevision
	ordered[0].Input = change
	reorder.OperationID = f.id(t)
	reorder.Items = ordered
	r, _ = write(reorder)
	if r.RootMomentID != root || r.Members[19].MomentID != root {
		t.Fatal("published display sort replaced root identity")
	}
	view, e := f.store.ReadMomentThread(f.ctx, content.PublicMomentAccess{}, r.ThreadID, false)
	if e != nil || *view.Members[19].MomentID != root {
		t.Fatal("current public sort", e)
	}
	keep := currentItems(r)[:1]
	rem := []content.MomentThreadRemoval{}
	for _, m := range r.Members[1:] {
		rem = append(rem, content.MomentThreadRemoval{MomentID: m.MomentID, ExpectedRevision: m.AppliedRevision})
	}
	r, _ = write(next(r, keep, rem))
	view, e = f.store.ReadMomentThread(f.ctx, content.PublicMomentAccess{}, r.ThreadID, false)
	if e != nil || view.SchemaVersion != "moment-thread-view-v2" || len(view.Members) != 1 || view.RootMomentID != nil {
		t.Fatal("one active ordinary projection or retired root leaked", e)
	}
	if e = view.ResponseProof.RevalidateHumanControl(f.ctx); e != nil {
		t.Fatal("single current response proof", e)
	}
	if _, e = f.store.ReadMomentThread(f.ctx, content.PublicMomentAccess{}, root, true); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("retired root discovered siblings", e)
	}
	if _, e = f.store.CapturePublicMomentWithContext(f.ctx, content.PublicMomentAccess{}, root, true, true); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("withdrawn root body public", e)
	}
	if _, e = f.store.ReadPublicMomentMedia(f.ctx, momentmedia.Access{}, root); !errors.Is(e, momentmedia.ErrNotFound) {
		t.Fatal("withdrawn root media public", e)
	}
	if _, e = f.store.CaptureMomentLocations(f.ctx, content.PublicMomentAccess{}, root, false); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("withdrawn root geography public", e)
	}
	owned, e := f.store.ReadOwnedMomentThread(f.ctx, f.digest, f.actor, r.ThreadID, false)
	if e != nil || owned.RetiredMembers == nil || len(*owned.RetiredMembers) != 19 || owned.RootMomentID == nil || *owned.RootMomentID != root {
		t.Fatal("retired owner history", e)
	}
	items := currentItems(r)
	items = append(items, content.MomentThreadItem{Restore: &restore, Input: text("append after twenty historical positions"), Media: []momentmedia.AttachmentInput{}})
	r, _ = write(next(r, items, []content.MomentThreadRemoval{}))
	var position int
	if e = f.pool.QueryRow(f.ctx, `SELECT position FROM moment_thread_members WHERE moment_id=$1`, r.Members[1].MomentID).Scan(&position); e != nil || position != 20 {
		t.Fatal("history incorrectly capped at twenty", position, e)
	}
	beforeIndependent := r
	independentTarget := r.Members[1]
	withdrawn, _ := mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "WITHDRAW", MomentID: independentTarget.MomentID, ExpectedRevision: independentTarget.AppliedRevision})
	items = currentItems(r)[:1]
	items = append(items, content.MomentThreadItem{Restore: &restore, Input: text("append after independently withdrawn entry"), Media: []momentmedia.AttachmentInput{}})
	r, _ = write(next(r, items, []content.MomentThreadRemoval{}))
	if m, e := f.store.GetOwnMoment(f.ctx, f.actor.ID, independentTarget.MomentID); e != nil || m.Status != "withdrawn" || m.Revision != withdrawn.AppliedRevision {
		t.Fatal("omitted withdrawn slot rewritten or restored", e)
	}
	var retired bool
	if e = f.pool.QueryRow(f.ctx, `SELECT display_position IS NULL FROM moment_thread_display_members WHERE moment_id=$1`, independentTarget.MomentID).Scan(&retired); e != nil || !retired {
		t.Fatal("independently withdrawn display slot not retired", e)
	}
	if _, e = f.store.CapturePublicMomentWithContext(f.ctx, content.PublicMomentAccess{}, independentTarget.MomentID, true, true); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("independently withdrawn body became public", e)
	}
	if _, e = f.store.ReadMomentThread(f.ctx, content.PublicMomentAccess{}, independentTarget.MomentID, true); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("independently withdrawn entry discovered siblings", e)
	}
	oldOperation, e := f.store.ReadMomentThreadOperation(f.ctx, f.digest, f.actor, beforeIndependent.OperationID, false)
	oldBytes, _ := json.Marshal(beforeIndependent)
	currentBytes, _ := json.Marshal(oldOperation)
	if e != nil || !bytes.Equal(oldBytes, currentBytes) {
		t.Fatal("independent withdrawal changed original thread receipt", e)
	}
	rootNow, e := f.store.GetOwnMoment(f.ctx, f.actor.ID, root)
	if e != nil {
		t.Fatal(e)
	}
	items = currentItems(r)
	items = append(items, content.MomentThreadItem{MomentID: root, ExpectedRevision: rootNow.Revision, Restore: &restore, Input: text("explicitly restore original identity"), Media: []momentmedia.AttachmentInput{}})
	invalid := next(r, items, []content.MomentThreadRemoval{})
	if _, e = f.store.PreviewMomentThread(f.ctx, f.digest, f.actor, invalid); !errors.Is(e, identity.ErrControlChanged) {
		t.Fatal("implicit restoration allowed", e)
	}
	yes := true
	items[len(items)-1].Restore = &yes
	r, _ = write(next(r, items, []content.MomentThreadRemoval{}))
	if r.RootMomentID != root || r.Members[2].MomentID != root || len(r.Members[2].Media) != 0 {
		t.Fatal("explicit restoration inferred old public media")
	}
	if m, e := f.store.GetOwnMoment(f.ctx, f.actor.ID, privateFact.MomentID); e != nil || m.Status != "draft" || m.Visibility != "private" {
		t.Fatal("related private entry expanded visibility", e)
	}
	whole := content.MomentThreadInput{OperationID: f.id(t), Action: "WITHDRAW", ThreadID: r.ThreadID, Audience: "PUBLIC", ExpectedVersion: r.AppliedVersion, Items: []content.MomentThreadItem{}}
	r, _ = write(whole)
	if r.SchemaVersion != "moment-thread-operation-v2" || len(r.Members) != 3 || r.RootMomentID != root {
		t.Fatal("edited whole-withdraw result")
	}
	replay, e := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, original)
	before, _ := json.Marshal(firstReceipt)
	after, _ := json.Marshal(replay)
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("original operation changed after edits/withdraw", e)
	}
	single := content.MomentThreadInput{OperationID: f.id(t), Action: "EDIT", BaseMomentID: &empty, LocalScope: scope, Audience: "PUBLIC", RemovedItems: &removed, Items: []content.MomentThreadItem{{Restore: &restore, Input: text(""), Media: prepare()}}}
	r, _ = write(single)
	if len(r.Members) != 1 || len(r.Members[0].Media) != 1 {
		t.Fatal("new single pure-media EDIT")
	}
	bad := single
	bad.OperationID = f.id(t)
	bad.Items = append([]content.MomentThreadItem{}, single.Items...)
	bad.Items[0].Media = append([]momentmedia.AttachmentInput{}, single.Items[0].Media...)
	bad.Items[0].Media[0].MediaID = f.id(t)
	if _, e = f.store.PreviewMomentThread(f.ctx, f.digest, f.actor, bad); e == nil {
		t.Fatal("missing upload silently dropped")
	}
	if _, e = f.store.ReadMomentThreadOperation(f.ctx, f.digest, f.actor, bad.OperationID, false); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("failed upload created receipt", e)
	}
	principal, org := f.id(t), f.id(t)
	f.exec(t, `INSERT INTO accounts(id,account_type)VALUES($1,'organization')`, principal)
	f.exec(t, `INSERT INTO organizations(id,account_id,organization_type,name)VALUES($1,$2,'club','仅隔离零城市原关联168')`, org, principal)
	f.exec(t, `INSERT INTO organization_memberships(organization_id,user_account_id,role,status)VALUES($1,$2,'owner','active')`, org, f.actor.ID)
	linked := text("retain original association without requiring a city")
	linked.CityID, linked.Locations, linked.OrganizationID = "aberdeen-gb", nil, &org
	linkedDraft, _ := mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "CREATE", Input: &linked})
	linked.CityID, linked.Locations = "", &none
	linkedEdit := content.MomentThreadInput{OperationID: f.id(t), Action: "EDIT", BaseMomentID: &linkedDraft.MomentID, LocalScope: scope, Audience: "PUBLIC", RemovedItems: &removed, Items: []content.MomentThreadItem{{MomentID: linkedDraft.MomentID, ExpectedRevision: linkedDraft.AppliedRevision, Restore: &restore, Input: linked, Media: []momentmedia.AttachmentInput{}}}}
	approvedLinked := threadNativeApprove(t, f, linkedEdit)
	f.exec(t, `UPDATE organization_memberships SET status='removed' WHERE organization_id=$1 AND user_account_id=$2`, org, f.actor.ID)
	if _, e = f.store.CommitMomentThread(f.ctx, f.digest, f.actor, approvedLinked); !errors.Is(e, identity.ErrControlChanged) {
		t.Fatal("optional city bypassed original association authority", e)
	}
	if m, e := f.store.GetOwnMoment(f.ctx, f.actor.ID, linkedDraft.MomentID); e != nil || m.Status != "draft" || m.Visibility != "private" || m.Revision != linkedDraft.AppliedRevision {
		t.Fatal("revoked original association partially published", e)
	}
	f.exec(t, `UPDATE organization_memberships SET status='active' WHERE organization_id=$1 AND user_account_id=$2`, org, f.actor.ID)
	linkedEdit.OperationID = f.id(t)
	r, _ = write(linkedEdit)
	if len(r.Members) != 1 || r.Members[0].MomentID != linkedDraft.MomentID {
		t.Fatal("zero-city original association replaced identity")
	}
	if m, e := f.store.GetOwnMoment(f.ctx, f.actor.ID, linkedDraft.MomentID); e != nil || m.OrganizationID != org || m.CityID != "" || m.Status != "published" || m.Visibility != "public" {
		t.Fatal("zero-city original association not committed", e)
	}
}
