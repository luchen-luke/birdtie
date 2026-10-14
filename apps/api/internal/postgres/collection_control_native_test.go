package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/content"
)

func collectionControlNative(t *testing.T) *collectionPublicationNativeFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_COLLECTION_CONTROL_NATIVE_TEST") != "1" {
		t.Skip("NOT_RUN: explicit owned collection control native activation missing")
	}
	return collectionPublicationFixture(t)
}
func collectionControlNativeInput(f *collectionPublicationNativeFixture, v content.PersonalCollection, action string) content.CollectionControlInput {
	p := content.CollectionControlInput{SchemaVersion: content.CollectionControlSchemaVersion, OperationID: f.uuid(), Action: action, Kind: v.Kind, CollectionID: v.ID, ExpectedRevision: v.Revision}
	if action == "UPDATE" {
		p.Input = &content.CollectionControlBody{Kind: v.Kind, CityID: v.CityID, Title: "本人具体修改", Summary: "本人摘要", Entries: []content.CollectionControlEntry{}}
		for _, entry := range v.Entries {
			p.Input.Entries = append(p.Input.Entries, content.CollectionControlEntry{PlaceID: entry.PlaceID, Note: "仅本人修改后说明", LocationVisibility: "hidden"})
		}
	}
	return p
}
func collectionControlNativeApprove(t *testing.T, f *collectionPublicationNativeFixture, p content.CollectionControlInput) content.CollectionControlSubmission {
	t.Helper()
	v, e := f.s.PreviewHumanCollectionControl(f.ctx, f.digest, f.owner, p)
	if e != nil || content.ValidateCollectionControlPreview(v, p, v.ObservedAt) != nil || v.ResponseProof == nil {
		t.Fatal("actual concrete native preview", e)
	}
	if e = v.ResponseProof.RevalidateHumanControl(f.ctx); e != nil {
		t.Fatal("actual preview encoding proof", e)
	}
	return content.CollectionControlSubmission{CollectionControlInput: p, Approval: v.Approval}
}
func TestCollectionControlNativeAtomicFactReplayAndIndependentLaterState(t *testing.T) {
	for _, kind := range []string{"journey", "theme_map"} {
		t.Run(kind, func(t *testing.T) {
			f := collectionControlNative(t)
			v, _ := f.draft(kind)
			var agents int
			if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agents WHERE principal_account_id=$1`, f.owner.ID).Scan(&agents); e != nil || agents != 0 {
				t.Fatal("ordinary PERSON unexpectedly needs Agent")
			}
			p := collectionControlNativeInput(f, v, "UPDATE")
			in := collectionControlNativeApprove(t, f, p)
			first, e := f.s.ApplyHumanCollectionControl(f.ctx, f.digest, f.owner, in)
			if e != nil || first.Status != "SUCCEEDED" || first.CollectionID != v.ID || first.AppliedRevision != 2 || first.ExpectedRevision == nil || *first.ExpectedRevision != 1 {
				t.Fatal("atomic original update", e)
			}
			if e = f.s.FinalizeHumanCollectionControlReceipt(f.ctx, f.digest, f.owner, first); e != nil {
				t.Fatal("actual final fact proof", e)
			}
			again, e := f.s.ApplyHumanCollectionControl(f.ctx, f.digest, f.owner, in)
			if e != nil || again.CommittedAt == nil || !again.CommittedAt.Equal(*first.CommittedAt) {
				t.Fatal("same op produced another fact", e)
			}
			latest, e := f.s.GetHumanCollection(f.ctx, f.digest, f.owner, kind, v.ID)
			if e != nil || latest.Revision != 2 || latest.Title != p.Input.Title || latest.Entries[0].Note != p.Input.Entries[0].Note {
				t.Fatal("entry/audit/resource not committed with fact", e)
			}
			changed := p.Input.CollectionInput()
			changed.Title = "另一次明确修改"
			if _, e = f.s.UpdateHumanCollection(f.ctx, f.digest, f.owner, v.ID, 2, changed); e != nil {
				t.Fatal("independent later native update", e)
			}
			// Retired public source must not erase this actor's immutable tiny history.
			f.exec(`UPDATE places SET publication_status='hidden' WHERE id=ANY($1::uuid[])`, f.places)
			history, e := f.s.ReadHumanCollectionControl(f.ctx, f.digest, f.owner, p.OperationID)
			if e != nil || history.AppliedRevision != 2 || history.CommittedAt == nil || !history.CommittedAt.Equal(*first.CommittedAt) {
				t.Fatal("current object state replaced original fact", e)
			}
			if e = f.s.FinalizeHumanCollectionControlReceipt(f.ctx, f.digest, f.owner, history); e != nil {
				t.Fatal("history after source loss", e)
			}
			history, e = f.s.StopHumanCollectionControl(f.ctx, f.digest, f.owner, p.OperationID)
			if e != nil || history.Status != "SUCCEEDED" {
				t.Fatal("stop erased successful fact", e)
			}
			var count int
			if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE resource_id=$1 AND action='update_draft'`, v.ID).Scan(&count); e != nil || count != 2 {
				t.Fatal("replayed effect/audit", e)
			}
			foreign, e := f.s.ReadHumanCollectionControl(f.ctx, f.peerDigest, f.peer, p.OperationID)
			if !errors.Is(e, content.ErrNotFound) || foreign.Status != "" {
				t.Fatal("foreign owner received original fact", e)
			}
			if _, e = f.pool.Exec(f.ctx, `UPDATE personal_collection_control_operations SET payload_digest=repeat('a',64) WHERE owner_account_id=$1 AND operation_id=$2`, f.owner.ID, p.OperationID); e == nil {
				t.Fatal("immutable original fact changed")
			}
		})
	}
}
func TestCollectionControlNativeStopBeforeLateProducerAndSaturatedTermination(t *testing.T) {
	f := collectionControlNative(t)
	v, _ := f.draft("journey")
	p := collectionControlNativeInput(f, v, "WITHDRAW")
	late := collectionControlNativeApprove(t, f, p)
	stopped, e := f.s.StopHumanCollectionControl(f.ctx, f.digest, f.owner, p.OperationID)
	if e != nil || stopped.Status != "STOPPED" || stopped.CollectionID != "" || stopped.ExpectedRevision != nil {
		t.Fatal("zero-effect original tombstone", e)
	}
	lateFact, e := f.s.ApplyHumanCollectionControl(f.ctx, f.digest, f.owner, late)
	if e != nil || lateFact.Status != "STOPPED" {
		t.Fatal("late producer survived explicit stop", e)
	}
	table, _, _, _ := collectionTables(v.Kind)
	var state string
	var rev int64
	if e = f.pool.QueryRow(f.ctx, `SELECT status,revision FROM `+table+` WHERE id=$1`, v.ID).Scan(&state, &rev); e != nil || state != "draft" || rev != 1 {
		t.Fatal("stopped producer mutated resource", e)
	}
	f.exec(`UPDATE `+table+` SET revision=$2 WHERE id=$1`, v.ID, content.MaxCollectionControlRevision)
	v.Revision = content.MaxCollectionControlRevision
	saturated := collectionControlNativeInput(f, v, "WITHDRAW")
	out, e := f.s.ApplyHumanCollectionControl(f.ctx, f.digest, f.owner, collectionControlNativeApprove(t, f, saturated))
	if e != nil || out.Status != "SUCCEEDED" || out.AppliedRevision != content.MaxCollectionControlRevision {
		t.Fatal("terminal revision overflow or forbidden termination", e)
	}
	if e = f.pool.QueryRow(f.ctx, `SELECT status,revision FROM `+table+` WHERE id=$1`, v.ID).Scan(&state, &rev); e != nil || state != "withdrawn" || rev != content.MaxCollectionControlRevision {
		t.Fatal("saturated terminal effect", e)
	}
	missing := f.uuid()
	fact, e := f.s.ReadHumanCollectionControl(f.ctx, f.digest, f.owner, missing)
	if !errors.Is(e, content.ErrNotFound) || fact.Status != "" {
		t.Fatal("different op inferred success from withdrawn current object", e)
	}
}
func TestCollectionControlNativeRejectedConcreteVersionAndAtomicFailure(t *testing.T) {
	f := collectionControlNative(t)
	v, _ := f.draft("theme_map")
	p := collectionControlNativeInput(f, v, "UPDATE")
	in := collectionControlNativeApprove(t, f, p)
	changed := in
	body := *p.Input
	body.Title = "未展示的另一稿"
	changed.Input = &body
	if _, e := f.s.ApplyHumanCollectionControl(f.ctx, f.digest, f.owner, changed); e == nil {
		t.Fatal("changed input reused old concrete approval")
	}
	// Failure after resource/entry/audit effects must roll back the whole action.
	f.exec(`CREATE FUNCTION birdtie_owned_control_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'owned ledger fault'; END $$; CREATE TRIGGER birdtie_owned_control_fault BEFORE INSERT ON personal_collection_control_operations FOR EACH ROW EXECUTE FUNCTION birdtie_owned_control_fault()`)
	if _, e := f.s.ApplyHumanCollectionControl(f.ctx, f.digest, f.owner, in); e == nil {
		t.Fatal("owned ledger failure was accepted")
	}
	current, e := f.s.GetHumanCollection(f.ctx, f.digest, f.owner, v.Kind, v.ID)
	if e != nil || current.Revision != v.Revision || current.Title != v.Title || current.Entries[0].Note != v.Entries[0].Note {
		t.Fatal("resource/entry partial commit", e)
	}
	var facts, audits int
	if e = f.pool.QueryRow(f.ctx, `SELECT(SELECT count(*) FROM personal_collection_control_operations WHERE owner_account_id=$1),(SELECT count(*) FROM audit_events WHERE resource_id=$2 AND action='update_draft')`, f.owner.ID, v.ID).Scan(&facts, &audits); e != nil || facts != 0 || audits != 0 {
		t.Fatal("partial fact or audit", e)
	}
	f.exec(`DROP TRIGGER birdtie_owned_control_fault ON personal_collection_control_operations;DROP FUNCTION birdtie_owned_control_fault()`)
	// Returning to the same public state changes the retained source epoch.
	f.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.places[0])
	f.exec(`UPDATE places SET publication_status='published' WHERE id=$1`, f.places[0])
	if _, e = f.s.ApplyHumanCollectionControl(f.ctx, f.digest, f.owner, in); e == nil {
		t.Fatal("source ABA revived old approval")
	}
}
func TestCollectionControlNativeAccountABAAndSessionExpiryAcrossWait(t *testing.T) {
	for _, mode := range []string{"account-aba", "expired-session"} {
		t.Run(mode, func(t *testing.T) {
			f := collectionControlNative(t)
			v, _ := f.draft("journey")
			p := collectionControlNativeInput(f, v, "UPDATE")
			in := collectionControlNativeApprove(t, f, p)
			tx, e := f.pool.Begin(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
			var pid int
			if e = tx.QueryRow(f.ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
				t.Fatal(e)
			}
			if mode == "account-aba" {
				_, e = tx.Exec(f.ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, f.owner.ID)
			} else {
				_, e = tx.Exec(f.ctx, `SELECT id FROM journeys WHERE id=$1 FOR UPDATE`, v.ID)
			}
			if e != nil {
				t.Fatal(e)
			}
			type result struct {
				r content.CollectionControlReceipt
				e error
			}
			done := make(chan result, 1)
			go func() { r, e := f.s.ApplyHumanCollectionControl(f.ctx, f.digest, f.owner, in); done <- result{r, e} }()
			hcNativeWaitPID(t, &savedPageNativeFixture{ctx: f.ctx, pool: f.pool}, pid)
			if mode == "account-aba" {
				if _, e = tx.Exec(f.ctx, `UPDATE accounts SET status='suspended' WHERE id=$1`, f.owner.ID); e != nil {
					t.Fatal(e)
				}
				if _, e = tx.Exec(f.ctx, `UPDATE accounts SET status='active' WHERE id=$1`, f.owner.ID); e != nil {
					t.Fatal(e)
				}
			} else {
				f.exec(`UPDATE sessions SET created_at=clock_timestamp()-interval '1 hour',expires_at=clock_timestamp()-interval '1 second',idle_expires_at=clock_timestamp()-interval '2 seconds' WHERE id=$1`, f.session)
			}
			if e = tx.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			out := <-done
			if out.e == nil || out.r.Status != "" {
				t.Fatal("retired authority crossed wait", out.e)
			}
			var revision int64
			var facts, audits int
			if e = f.pool.QueryRow(f.ctx, `SELECT revision,(SELECT count(*) FROM personal_collection_control_operations WHERE owner_account_id=$2),(SELECT count(*) FROM audit_events WHERE resource_id=$1::text AND action='update_draft') FROM journeys WHERE id=$1::uuid`, v.ID, f.owner.ID).Scan(&revision, &facts, &audits); e != nil || revision != 1 || facts != 0 || audits != 0 {
				t.Fatal("retired wait left effect/fact/audit", e)
			}
		})
	}
}

func TestCollectionControlNativeStoppedApprovalCannotRebindOperation(t *testing.T) {
	f := collectionControlNative(t)
	v, _ := f.draft("journey")
	original := collectionControlNativeInput(f, v, "UPDATE")
	late := collectionControlNativeApprove(t, f, original)
	stopped, e := f.s.StopHumanCollectionControl(f.ctx, f.digest, f.owner, original.OperationID)
	if e != nil || stopped.Status != "STOPPED" {
		t.Fatal("original stop failed", e)
	}
	// The same payload digest and target are intentional: the approval must
	// independently bind the original operation, not just its body/version.
	late.OperationID = f.uuid()
	if out, e := f.s.ApplyHumanCollectionControl(f.ctx, f.digest, f.owner, late); e == nil || out.Status != "" {
		t.Fatal("stopped approval was rebound to a new operation", e)
	}
	var revision int64
	var title string
	var facts, audits int
	if e = f.pool.QueryRow(f.ctx, `SELECT revision,title,(SELECT count(*) FROM personal_collection_control_operations WHERE owner_account_id=$2 AND operation_id=$3),(SELECT count(*) FROM audit_events WHERE resource_id=$1::text AND action='update_draft') FROM journeys WHERE id=$1::uuid`, v.ID, f.owner.ID, late.OperationID).Scan(&revision, &title, &facts, &audits); e != nil || revision != 1 || title != v.Title || facts != 0 || audits != 0 {
		t.Fatal("rebound approval left an effect, fact or audit", e)
	}
}
