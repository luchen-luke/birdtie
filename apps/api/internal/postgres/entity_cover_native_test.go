package postgres

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	ec "github.com/birdtie/birdtie/apps/api/internal/entitycover"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/birdtie/birdtie/apps/api/internal/saved"
)

func entityCoverNativeInput(t *testing.T, f *savedPageNativeFixture, v int64) ec.Input {
	t.Helper()
	var b bytes.Buffer
	if png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 400, 240))) != nil {
		t.Fatal("fixture image")
	}
	return ec.Input{OperationID: f.id(t), ExpectedVersion: v, Action: "SAVE", ContentType: "image/png", Content: b.Bytes(), Rights: &ec.Rights{Basis: "OWNED", Attribution: "合成隔离测试", RetentionUntil: time.Now().UTC().Add(time.Hour), CanRetain: true, CanPublish: true}}
}
func entityCoverNativeAccess(f *savedPageNativeFixture) ec.Access {
	return ec.Access{AccountID: f.actor.ID, AccountType: f.actor.AccountType, SessionDigest: f.digest}
}
func entityCoverNativeOrg(t *testing.T, f *savedPageNativeFixture) ec.Ref {
	t.Helper()
	o, e := f.store.CreateOrganization(f.ctx, f.actor.ID, organization.CreateInput{OrganizationType: "club", Name: "合成封面组织"})
	if e != nil {
		t.Fatal(e)
	}
	f.exec(t, `DELETE FROM agents WHERE principal_account_id=$1`, o.AccountID)
	f.exec(t, `UPDATE organizations SET visibility='public' WHERE id=$1`, o.ID)
	return ec.Ref{Type: "organization", ID: o.ID}
}
func entityCoverNativeActivity(t *testing.T, f *savedPageNativeFixture) ec.Ref {
	t.Helper()
	a, e := f.store.CreateSocialDraft(f.ctx, f.actor.ID, activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: f.actor.ID}, CityID: "aberdeen-gb", Title: "合成线上活动", Summary: "合成隔离验证", Modality: "online", PhysicalPlaceStatus: "not_applicable", StartsAt: time.Now().UTC().Add(time.Hour), EndsAt: time.Now().UTC().Add(2 * time.Hour), TimeZone: "Europe/London", Visibility: "public"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.PublishSocialActivity(f.ctx, f.actor.ID, a.ID); e != nil {
		t.Fatal(e)
	}
	return ec.Ref{Type: "activity", ID: a.ID}
}
func entityCoverNativeWrite(t *testing.T, f *savedPageNativeFixture, ref ec.Ref, in ec.Input) (ec.Input, ec.Receipt) {
	t.Helper()
	a := entityCoverNativeAccess(f)
	preview, e := f.store.PreviewEntityCover(f.ctx, a, ref, in)
	if e != nil {
		t.Fatal("actual approved derivative", e)
	}
	if in.Action == "SAVE" {
		raw, e := base64.StdEncoding.Strict().DecodeString(preview.PreviewDerivative.ContentBase64)
		conf, format, e2 := image.DecodeConfig(bytes.NewReader(raw))
		if e != nil || e2 != nil || format != "jpeg" || conf.Width != 320 || conf.Height != 180 {
			t.Fatal("preview was not canonical JPEG", e, e2)
		}
	}
	in.Approval = preview.Token
	r, e := f.store.ChangeEntityCover(f.ctx, a, ref, in)
	if e != nil {
		t.Fatal("actual cover write", e)
	}
	return in, r
}
func TestBatchB1EntityCoverResultCurrentTTLAndSource(t *testing.T) {
	f := savedPageNative(t)
	ref := entityCoverNativeActivity(t, f)
	in, _ := entityCoverNativeWrite(t, f, ref, entityCoverNativeInput(t, f, 0))
	f.exec(t, `INSERT INTO agents(agent_type,principal_account_id)VALUES('personal',$1)`, f.actor.ID)
	f.exec(t, `INSERT INTO city_contexts(city_id)VALUES('aberdeen-gb')ON CONFLICT(city_id) DO NOTHING`)
	task, e := f.store.SaveTask(f.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: f.actor.ID, ActingUserID: f.actor.ID, CityID: "aberdeen-gb", Query: "合成封面活动", Intent: agentworkspace.FindActivity, Status: agentworkspace.TaskActive, Filters: map[string]string{"currentQuery": "合成封面活动", "timePreference": "anytime"}, Conversation: []agentworkspace.Message{{Role: "user", Text: "合成封面活动"}}})
	if e != nil {
		t.Fatal(e)
	}
	expected, _ := json.Marshal(agentworkspace.SanitizeTaskForResponse(task))
	a := arp.Access{Actor: f.actor, SessionDigest: f.digest, TaskID: task.ID, ExpectedTask: expected}
	q := arp.Query{CityID: task.CityID, Kind: "activity", TimePreference: "anytime", CompareIDs: []string{}}
	r, e := f.store.ReadAgentResultProjection(f.ctx, a, q)
	if e != nil || arp.ValidateItems(r.Items) != nil {
		t.Fatal("current original result projection", e)
	}
	found, foundLegacy := 0, 0
	for _, item := range r.Items {
		if item.Entity == (arp.Ref{Type: ref.Type, ID: ref.ID}) {
			found++
			if item.Cover == nil || item.Cover.Ref != ref || item.Cover.Version != 1 || item.Detail == nil || *item.Detail != item.Entity {
				t.Fatal("canonical current result cover", item.Entity)
			}
		} else if item.Cover != nil && (item.Cover.Ref != (ec.Ref{Type: item.Entity.Type, ID: item.Entity.ID})) {
			t.Fatal("another actual source borrowed cover")
		}
	}
	machine := []foundation.Activity{}
	for _, activity := range r.Activities {
		if activity.ID == ref.ID {
			foundLegacy++
			if activity.Cover == nil || activity.Cover.Ref != ref || activity.Cover.Version != 1 {
				t.Fatal("canonical human compatibility activity lost cover")
			}
		}
		machine = append(machine, coverFreeToolActivity(activity))
	}
	if found != 1 || foundLegacy != 1 {
		t.Fatal("canonical activity missing/duplicated among current sources", found, foundLegacy)
	}
	toolJSON, _ := json.Marshal(agenttool.Result{Activities: machine})
	if strings.Contains(string(toolJSON), `"cover"`) || strings.Contains(string(toolJSON), `"CoverSourceProof"`) {
		t.Fatal("human image purpose entered machine tool")
	}
	if e = f.store.RevalidateAgentResultProjection(f.ctx, a, q, r); e != nil {
		t.Fatal("new clock renewed proof", e)
	}
	freshProjection, e := f.store.ReadAgentResultProjection(f.ctx, a, q)
	if e != nil || !unchangedOutputProjection(r, freshProjection) {
		t.Fatal("current search/reply/tool comparator rejected same cover", e)
	}
	f.exec(t, `UPDATE entity_covers SET thumbnail=thumbnail WHERE entity_type=$1 AND entity_id=$2`, ref.Type, ref.ID)
	if e = f.store.RevalidateAgentResultProjection(f.ctx, a, q, r); !errors.Is(e, arp.ErrChanged) {
		t.Fatal("cover xmin ABA revived result", e)
	}
	aCover := entityCoverNativeAccess(f)
	before, e := f.store.ReadEntityCover(f.ctx, aCover, ref)
	if e != nil {
		t.Fatal(e)
	}
	f.exec(t, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '35 minutes' WHERE id=$1`, f.session)
	if e = f.store.ValidateEntityCoverProjection(f.ctx, aCover, ref, before.Cover, before.SourceProof); e != nil {
		t.Fatal("idle refresh retired media", e)
	}
	candidate := entityCoverNativeInput(t, f, 1)
	preview, e := f.store.PreviewEntityCover(f.ctx, aCover, ref, candidate)
	if e != nil {
		t.Fatal(e)
	}
	f.exec(t, `UPDATE activities SET summary=summary WHERE id=$1`, ref.ID)
	candidate.Approval = preview.Token
	if _, e = f.store.ChangeEntityCover(f.ctx, aCover, ref, candidate); !errors.Is(e, ec.ErrChanged) {
		t.Fatal("source ABA inherited approval", e)
	}
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
	if _, e = f.store.ChangeEntityCover(f.ctx, aCover, ref, in); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("revoked Session replay", e)
	}
	f.exec(t, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.session)
	fresh, e := f.store.ReadEntityCover(f.ctx, aCover, ref)
	if e != nil {
		t.Fatal(e)
	}
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
	f.exec(t, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.session)
	if e = f.store.ValidateEntityCoverProjection(f.ctx, aCover, ref, fresh.Cover, fresh.SourceProof); !errors.Is(e, ec.ErrChanged) {
		t.Fatal("Session ABA revived response image", e)
	}
}
func TestBatchB1EntityCoverWaitSessionABARollsBack(t *testing.T) {
	f := savedPageNative(t)
	ref := entityCoverNativeOrg(t, f)
	a := entityCoverNativeAccess(f)
	in := entityCoverNativeInput(t, f, 0)
	preview, e := f.store.PreviewEntityCover(f.ctx, a, ref, in)
	if e != nil {
		t.Fatal(e)
	}
	in.Approval = preview.Token
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(f.ctx, `SELECT id FROM organizations WHERE id=$1 FOR UPDATE`, ref.ID); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, err := f.store.ChangeEntityCover(f.ctx, a, ref, in); done <- err }()
	deadline := time.Now().Add(4 * time.Second)
	waited := false
	for time.Now().Before(deadline) {
		var n int
		e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM organizations WHERE id=$1 FOR UPDATE%'`).Scan(&n)
		if e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			waited = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waited {
		t.Fatal("actual cover resource wait absent")
	}
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
	f.exec(t, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.session)
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, ec.ErrChanged) {
			t.Fatal("ABA accepted approval", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cover write did not retire")
	}
	var state, history int
	if e = f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM entity_covers WHERE entity_type=$1 AND entity_id=$2),(SELECT count(*) FROM entity_cover_operations WHERE actor_account_id=$3 AND operation_id=$4)`, ref.Type, ref.ID, f.actor.ID, in.OperationID).Scan(&state, &history); e != nil || state != 0 || history != 0 {
		t.Fatal("late Session wrote image or receipt", state, history, e)
	}
}
func TestBatchB1EntityCoverPermissionsReceiptsAndPublicProjection(t *testing.T) {
	f := savedPageNative(t)
	for _, file := range []string{"137_entity_covers.down.sql", "137_entity_covers.sql"} {
		raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", file))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.pool.Exec(f.ctx, string(raw)); e != nil {
			t.Fatal("empty owned137 reversible", e)
		}
	}
	for _, kind := range []string{"activity", "organization"} {
		t.Run(kind, func(t *testing.T) {
			ref := entityCoverNativeOrg(t, f)
			if kind == "activity" {
				ref = entityCoverNativeActivity(t, f)
			}
			a := entityCoverNativeAccess(f)
			in, r := entityCoverNativeWrite(t, f, ref, entityCoverNativeInput(t, f, 0))
			if r.Status != "SUCCEEDED" || r.AppliedVersion != 1 {
				t.Fatal("original success")
			}
			replay, e := f.store.ChangeEntityCover(f.ctx, a, ref, in)
			if e != nil || replay != r {
				t.Fatal("replay changed effect", e)
			}
			state, e := f.store.ReadEntityCover(f.ctx, ec.Access{}, ref)
			if e != nil || state.Cover == nil || state.Cover.Version != 1 {
				t.Fatal("anonymous canonical source", e)
			}
			if e = f.store.ValidateEntityCoverProjection(f.ctx, ec.Access{}, ref, state.Cover, state.SourceProof); e != nil {
				t.Fatal("lease renewed during validation", e)
			}
			m, raw, e := f.store.ReadEntityCoverContent(f.ctx, ec.Access{}, ref, 1)
			if e != nil || m.Width != 320 || m.Height != 180 || len(raw) != m.ByteCount {
				t.Fatal("actual content", e)
			}
			if kind == "activity" {
				if _, e = f.store.Save(f.ctx, f.actor.ID, "activity", ref.ID); e != nil {
					t.Fatal(e)
				}
				page, e := f.store.ListHumanSavedPage(f.ctx, f.digest, f.actor, "")
				if e != nil || len(page.Items) != 1 || page.Items[0].Cover == nil || page.Items[0].Cover.Ref != ref {
					t.Fatal("original Saved projection", e)
				}
				if e = f.store.ValidateHumanSavedSearchPage(f.ctx, f.digest, f.actor, saved.Query{}, "", page); e != nil {
					t.Fatal("Saved fixed lease proof", e)
				}
				detail, e := f.store.GetActivity(f.ctx, ref.ID, "")
				if e != nil || detail.Cover == nil || detail.Cover.Ref != ref {
					t.Fatal("canonical activity DTO", e)
				}
			} else {
				profile, e := f.store.GetPublicProfile(f.ctx, ref.ID, "")
				if e != nil || profile.AgentAvailable || profile.Cover == nil || profile.Cover.Ref != ref {
					t.Fatal("organization cover borrowed Agent", e)
				}
				peer := f.id(t)
				f.exec(t, `INSERT INTO accounts(id,account_type)VALUES($1,'person')`, peer)
				_, peerDigest, err := identity.NewToken()
				if err != nil {
					t.Fatal(err)
				}
				f.exec(t, `INSERT INTO sessions(id,account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,$3,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, f.id(t), peer, peerDigest[:])
				f.exec(t, `INSERT INTO organization_memberships(organization_id,user_account_id,role,status)VALUES($1,$2,'member','active')`, ref.ID, peer)
				peerInput := entityCoverNativeInput(t, f, 1)
				peerAccess := ec.Access{AccountID: peer, AccountType: "person", SessionDigest: peerDigest}
				if _, err = f.store.PreviewEntityCover(f.ctx, peerAccess, ref, peerInput); !errors.Is(err, ec.ErrForbidden) {
					t.Fatal("ordinary member became cover manager", err)
				}
				beforeRole, e := f.store.ReadEntityCover(f.ctx, ec.Access{}, ref)
				if e != nil {
					t.Fatal(e)
				}
				f.exec(t, `UPDATE organization_memberships SET status='removed' WHERE organization_id=$1 AND user_account_id=$2`, ref.ID, f.actor.ID)
				if _, _, e = f.store.ReadEntityCoverContent(f.ctx, ec.Access{}, ref, 1); !errors.Is(e, ec.ErrNotFound) {
					t.Fatal("retired uploader image remained public", e)
				}
				f.exec(t, `UPDATE organization_memberships SET status='active' WHERE organization_id=$1 AND user_account_id=$2`, ref.ID, f.actor.ID)
				if e = f.store.ValidateEntityCoverProjection(f.ctx, ec.Access{}, ref, beforeRole.Cover, beforeRole.SourceProof); !errors.Is(e, ec.ErrChanged) {
					t.Fatal("membership ABA revived old image", e)
				}
			}
			unknown := f.id(t)
			if _, e = f.store.ReadEntityCoverOperation(f.ctx, a, ref, unknown); !errors.Is(e, ec.ErrNotFound) {
				t.Fatal("404 is not no effect", e)
			}
			stopped, e := f.store.StopEntityCoverOperation(f.ctx, a, ref, unknown, 1)
			if e != nil || stopped.Status != "STOPPED" {
				t.Fatal("stop original", e)
			}
			late := entityCoverNativeInput(t, f, 1)
			late.OperationID = unknown
			if later, e := f.store.ChangeEntityCover(f.ctx, a, ref, late); e != nil || later.Status != "STOPPED" {
				t.Fatal("late request passed tombstone", e)
			}
			if success, e := f.store.StopEntityCoverOperation(f.ctx, a, ref, in.OperationID, 0); e != nil || success != r {
				t.Fatal("stop success rewrote history", e)
			}
			clear := ec.Input{OperationID: f.id(t), ExpectedVersion: 1, Action: "DELETE"}
			_, _ = entityCoverNativeWrite(t, f, ref, clear)
			if _, _, e = f.store.ReadEntityCoverContent(f.ctx, ec.Access{}, ref, 1); !errors.Is(e, ec.ErrNotFound) {
				t.Fatal("old image remained after delete", e)
			}
			if history, e := f.store.ReadEntityCoverOperation(f.ctx, a, ref, in.OperationID); e != nil || history != r {
				t.Fatal("later cover changed original receipt", e)
			}
		})
	}
	conn, e := f.pool.Acquire(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", "137_entity_covers.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = conn.Exec(f.ctx, string(raw)); e == nil {
		t.Fatal("used cover history downgraded")
	}
	_, _ = conn.Exec(context.Background(), "ROLLBACK")
}
