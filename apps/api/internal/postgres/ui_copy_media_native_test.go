package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	ca "github.com/birdtie/birdtie/apps/api/internal/communityavatar"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	"github.com/birdtie/birdtie/apps/api/internal/intent"
	av "github.com/birdtie/birdtie/apps/api/internal/profileavatar"
	"github.com/jackc/pgx/v5/pgxpool"
)

func copyMediaNativeAvatar(t *testing.T, ctx context.Context, pool *pgxpool.Pool, owner string) {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 16, 16))
	picture.Set(4, 4, color.RGBA{R: 255, A: 255})
	var buffer bytes.Buffer
	if e := png.Encode(&buffer, picture); e != nil {
		t.Fatal(e)
	}
	thumb, e := av.Prepare(buffer.Bytes(), "image/png")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, owner); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO person_avatars(account_id,version,thumbnail,sha256) VALUES($1,1,$2,$3)`, owner, thumb.Bytes, thumb.SHA256); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, e := pool.Exec(cleanup, `DELETE FROM person_avatars WHERE account_id=$1`, owner); e != nil {
			t.Error(e)
		}
	})
}

func TestCopyMediaNativeCommunityResultSavedAvatars(t *testing.T) {
	ownedMigrationDatabase(t)
	f := contextBuilderNative(t)
	b := f.place.private.base
	c, e := b.store.CreateSocialCommunity(b.ctx, b.other.ID, community.SocialInput{Name: "合成受控头像社群", Summary: "隔离测试，不是真实社群", CityID: f.place.city, Visibility: "public", JoinPolicy: "open", AvatarURL: "https://legacy.invalid/ungranted.jpg"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		for _, sql := range []string{`DELETE FROM saved_items WHERE community_id=$1`, `DELETE FROM audit_events WHERE resource_type='community' AND resource_id=$1`, `DELETE FROM communities WHERE id=$1`} {
			if _, err := b.pool.Exec(context.Background(), sql, c.ID); err != nil {
				t.Error("owned community projection cleanup", err)
			}
		}
	})
	if _, e = b.store.Save(b.ctx, b.person.ID, "group", c.ID); e != nil {
		t.Fatal(e)
	}
	a, q := resultNativeRequest(t, f, "community")
	before, e := b.store.ReadAgentResultProjection(b.ctx, a, q)
	if e != nil || len(before.Items) != 1 || before.Items[0].CommunityAvatar != nil {
		t.Fatal("legacy URL must not become controlled image", e)
	}
	avatarAccess := ca.Access{AccountID: b.other.ID, AccountType: "person", SessionDigest: f.place.private.peer.SessionDigest}
	picture := image.NewRGBA(image.Rect(0, 0, 16, 16))
	picture.Set(3, 3, color.RGBA{G: 255, A: 255})
	var raw bytes.Buffer
	if e = png.Encode(&raw, picture); e != nil {
		t.Fatal(e)
	}
	write := func(version int64, clear bool) {
		t.Helper()
		var op string
		if e = b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()`).Scan(&op); e != nil {
			t.Fatal(e)
		}
		in := ca.Input{OperationID: op, ExpectedVersion: version, ContentType: "image/png", Content: raw.Bytes()}
		if clear {
			in.Delete = true
			in.Content = nil
			in.ContentType = ""
		}
		approval, err := b.store.PreviewCommunityAvatar(b.ctx, avatarAccess, c.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		in.Approval = approval.Token
		if _, err = b.store.ChangeCommunityAvatar(b.ctx, avatarAccess, c.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	readSaved := func(wantVersion int64, wantAvailable bool) {
		t.Helper()
		page, err := b.store.ListHumanSavedPage(b.ctx, a.SessionDigest, a.Actor, "")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range page.Items {
			if item.Kind == "group" && item.TargetID == c.ID {
				found = true
				if item.Available != wantAvailable || !item.ValidCommunityMedia() {
					t.Fatal("Saved original visible source pairing")
				}
				if wantVersion == 0 {
					if item.CommunityAvatar != nil {
						t.Fatal("invisible/deleted/legacy source image")
					}
				} else if item.CommunityAvatar == nil || item.CommunityAvatar.CommunityID != c.ID || item.CommunityAvatar.Version != wantVersion {
					t.Fatal("Saved current community metadata")
				}
			}
		}
		if !found {
			t.Fatal("original bookmark lost")
		}
		target, err := b.store.GetHumanSavedTarget(b.ctx, a.SessionDigest, a.Actor, "group", c.ID)
		if err != nil || target == nil || !target.ValidCommunityMedia() {
			t.Fatal("owned target lookup", err)
		}
		if wantVersion > 0 && (target.CommunityAvatar == nil || target.CommunityAvatar.Version != wantVersion) {
			t.Fatal("target lookup lost metadata")
		}
	}
	readSaved(0, true)
	write(0, false)
	if e = b.store.RevalidateAgentResultProjection(b.ctx, a, q, before); !errors.Is(e, arp.ErrChanged) {
		t.Fatal("avatar addition did not retire original receipt", e)
	}
	r, e := b.store.ReadAgentResultProjection(b.ctx, a, q)
	if e != nil || len(r.Items) != 1 || r.Items[0].CommunityAvatar == nil || r.Items[0].CommunityAvatar.CommunityID != c.ID || r.Items[0].Avatar != nil {
		t.Fatal("original Community media projection", e)
	}
	if e = b.store.RevalidateAgentResultProjection(b.ctx, a, q, r); e != nil {
		t.Fatal("current exact receipt", e)
	}
	readSaved(1, true)
	viewer := ca.Access{AccountID: a.Actor.ID, AccountType: "person", SessionDigest: a.SessionDigest}
	meta, pixels, e := b.store.ReadCommunityAvatarContent(b.ctx, viewer, c.ID, 1)
	if e != nil || meta.CommunityID != c.ID || meta.ByteCount != len(pixels) {
		t.Fatal("projected same-origin content", e)
	}
	write(1, false)
	if e = b.store.RevalidateAgentResultProjection(b.ctx, a, q, r); !errors.Is(e, arp.ErrChanged) {
		t.Fatal("old community image receipt survived version", e)
	}
	next, e := b.store.ReadAgentResultProjection(b.ctx, a, q)
	if e != nil || next.Proof == r.Proof || next.Items[0].CommunityAvatar.Version != 2 {
		t.Fatal("media version excluded from MATERIALIZED proof", e)
	}
	readSaved(2, true)
	if _, _, e = b.store.ReadCommunityAvatarContent(b.ctx, viewer, c.ID, 1); !errors.Is(e, ca.ErrNotFound) {
		t.Fatal("old content version reusable", e)
	}
	b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, b.person.ID, b.other.ID)
	if e = b.store.RevalidateAgentResultProjection(b.ctx, a, q, next); !errors.Is(e, arp.ErrChanged) {
		t.Fatal("blocked source receipt survived", e)
	}
	readSaved(0, false)
	if _, _, e = b.store.ReadCommunityAvatarContent(b.ctx, viewer, c.ID, 2); !errors.Is(e, ca.ErrNotFound) {
		t.Fatal("blocked media content available", e)
	}
	b.exec(`DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, b.person.ID, b.other.ID)
	write(2, true)
	plain, e := b.store.ReadAgentResultProjection(b.ctx, a, q)
	if e != nil || len(plain.Items) != 1 || plain.Items[0].CommunityAvatar != nil {
		t.Fatal("cleared image removed valid original content", e)
	}
	if e = b.store.RevalidateAgentResultProjection(b.ctx, a, q, next); !errors.Is(e, arp.ErrChanged) {
		t.Fatal("cleared media reused sealed receipt", e)
	}
	readSaved(0, true)
	b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:])
	if _, e = b.store.ReadAgentResultProjection(b.ctx, a, q); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("revoked Session fetched result avatar", e)
	}
	if _, e = b.store.ListHumanSavedPage(b.ctx, a.SessionDigest, a.Actor, ""); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("revoked Session fetched bookmark avatar", e)
	}
}

func TestCopyMediaNativePersonProjectionReceipts(t *testing.T) {
	ownedMigrationDatabase(t)
	f := contextBuilderNative(t)
	b := f.place.private.base
	copyMediaNativeAvatar(t, b.ctx, b.pool, b.other.ID)
	public, e := b.store.SubmitIntent(b.ctx, b.other.ID, f.place.city, intent.Input{Confirmed: true, Topic: "合成公开头像人物", Details: "未请求的私人详情不得进入头像", AvailableFrom: time.Now().Add(-time.Minute), AvailableUntil: time.Now().Add(time.Hour), TimeZone: "Europe/London", CoarseAreaLabel: "本人公开范围", ExpiresAt: time.Now().Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := b.pool.Exec(context.Background(), `DELETE FROM intents WHERE id=$1`, public.ID); e != nil {
			t.Error(e)
		}
	})
	a, q := resultNativeRequest(t, f, "person")
	r, e := b.store.ReadAgentResultProjection(b.ctx, a, q)
	if e != nil || len(r.Items) != 1 || r.Items[0].Entity.ID != b.other.ID || r.Items[0].Avatar == nil || !r.Items[0].Avatar.Valid() || r.Items[0].Avatar.AccountID != b.other.ID {
		t.Fatal("current permitted original Person avatar projection", e)
	}
	if e = b.store.RevalidateAgentResultProjection(b.ctx, a, q, r); e != nil {
		t.Fatal("unchanged sealed receipt", e)
	}
	// The picture changes without changing the Person/Intent row. Its concrete
	// metadata must independently retire the old proof and sealed receipt.
	b.exec(`UPDATE person_avatars SET version=2,updated_at=clock_timestamp() WHERE account_id=$1`, b.other.ID)
	if e = b.store.RevalidateAgentResultProjection(b.ctx, a, q, r); !errors.Is(e, arp.ErrChanged) {
		t.Fatal("old projection survived avatar replacement", e)
	}
	next, e := b.store.ReadAgentResultProjection(b.ctx, a, q)
	if e != nil || len(next.Items) != 1 || next.Items[0].Avatar == nil || next.Items[0].Avatar.Version != 2 || next.Proof == r.Proof {
		t.Fatal("avatar version did not enter proof", e)
	}
	_, _, e = b.store.ReadAvatarContent(b.ctx, av.Access{AccountID: a.Actor.ID, AccountType: "person", SessionDigest: a.SessionDigest}, b.other.ID, 1)
	if !errors.Is(e, av.ErrNotFound) {
		t.Fatal("old avatar content reused", e)
	}
	b.exec(`UPDATE person_avatars SET version=3,thumbnail=NULL,sha256=NULL,updated_at=clock_timestamp() WHERE account_id=$1`, b.other.ID)
	if e = b.store.RevalidateAgentResultProjection(b.ctx, a, q, next); !errors.Is(e, arp.ErrChanged) {
		t.Fatal("delete retained old media receipt", e)
	}
	plain, e := b.store.ReadAgentResultProjection(b.ctx, a, q)
	if e != nil || len(plain.Items) != 1 || plain.Items[0].Avatar != nil {
		t.Fatal("missing image removed valid Person content", e)
	}
	// Current source/Session checks remain the original authorization boundary.
	b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:])
	if _, e = b.store.ReadAgentResultProjection(b.ctx, a, q); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("revoked actor read media projection", e)
	}
}

func TestCopyMediaNativeInboxActorsAndRetirement(t *testing.T) {
	f := inboxPageFixture(t)
	b := f.private.base
	copyMediaNativeAvatar(t, b.ctx, b.pool, b.person.ID)
	actor := identity.Actor{ID: b.other.ID, AccountType: "person"}
	digest := f.private.peer.SessionDigest
	req, e := b.store.CreateFriendRequest(b.ctx, b.person.ID, b.other.ID, "合成本人明确联系")
	if e != nil {
		t.Fatal(e)
	}
	request := f.delivered(t, agentnotification.KindConnectionRequest, req.ID, b.other.ID, agentnotification.Normal, 1)
	item, e := b.store.ReadHumanInboxItem(b.ctx, digest, actor, request.InboxID)
	if e != nil || item.ActorAvatar == nil || item.ActorID == nil || *item.ActorID != b.person.ID || !item.ValidActorMedia() {
		t.Fatal("request decision lost original requester avatar", e)
	}
	pageIn := inbox.PageRequest{View: inbox.NotificationsView, Limit: 20}
	page, e := b.store.ListHumanInboxPage(b.ctx, digest, actor, pageIn)
	if e != nil || len(page.Items) != 1 || page.Items[0].ActorAvatar == nil {
		t.Fatal("real request page avatar", e)
	}
	if e = b.store.ValidateHumanInboxPageResponse(b.ctx, digest, actor, pageIn, page); e != nil {
		t.Fatal("unchanged page receipt", e)
	}
	b.exec(`UPDATE person_avatars SET version=2,updated_at=clock_timestamp() WHERE account_id=$1`, b.person.ID)
	if e = b.store.ValidateHumanInboxPageResponse(b.ctx, digest, actor, pageIn, page); !errors.Is(e, inbox.ErrSourceChanged) {
		t.Fatal("avatar replacement escaped exact page comparison", e)
	}
	if _, e = b.store.DecideRequest(b.ctx, b.other.ID, req.ID, "accept"); e != nil {
		t.Fatal(e)
	}
	ties, e := b.store.ListTies(b.ctx, b.person.ID)
	if e != nil || len(ties) != 1 {
		t.Fatal("accepted original relationship", e)
	}
	cv, e := b.store.StartFriendConversation(b.ctx, b.person.ID, ties[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	message, e := b.store.SendMessage(b.ctx, b.person.ID, cv.ID, "PRIVATE_BODY_NOT_MEDIA_PROJECTION")
	if e != nil {
		t.Fatal(e)
	}
	delivered := f.delivered(t, agentnotification.KindDirectMessage, message.ID, b.other.ID, agentnotification.Normal, 1)
	item, e = b.store.ReadHumanInboxItem(b.ctx, digest, actor, delivered.InboxID)
	if e != nil || item.ActorAvatar == nil || item.ActorID == nil || *item.ActorID != message.SenderID || item.ActorAvatar.Version != 2 {
		t.Fatal("message decision lost original sender avatar", e)
	}
	wire, _ := json.Marshal(item)
	if bytes.Contains(wire, []byte("PRIVATE_BODY_NOT_MEDIA_PROJECTION")) {
		t.Fatal("private message body became media caption")
	}
	b.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, b.person.ID)
	item, e = b.store.ReadHumanInboxItem(b.ctx, digest, actor, delivered.InboxID)
	if e != nil || item.ActorAvatar != nil || item.ActorID != nil {
		t.Fatal("private profile image leaked through retained notification", e)
	}
	b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, b.other.ID, b.person.ID)
	if _, e = b.store.ReadHumanInboxItem(b.ctx, digest, actor, delivered.InboxID); !errors.Is(e, inbox.ErrNotFound) {
		t.Fatal("blocked notification reused authorized thumbnail", e)
	}
}

func TestCopyMediaNativeSavedCityNamesRespectTargetVisibility(t *testing.T) {
	f := privateSavedNotesNativeFixture(t)
	page, e := f.store.ListHumanSavedPage(f.ctx, f.digest, f.owner, "")
	if e != nil || len(page.Items) != 1 || page.Items[0].CityName == "" || page.Items[0].CityName == page.Items[0].CityID {
		t.Fatal("canonical city name missing", e)
	}
	f.exec(t, `UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place)
	page, e = f.store.ListHumanSavedPage(f.ctx, f.digest, f.owner, "")
	if e != nil || page.Items[0].Available || page.Items[0].CityName != "" || page.Items[0].CityID != "" {
		t.Fatal("withdrawn target retained city projection", e)
	}
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
	if _, e = f.store.ListHumanSavedPage(f.ctx, f.digest, f.owner, ""); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("revoked owner read city names", e)
	}
}
