package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	ns "github.com/birdtie/birdtie/apps/api/internal/agentnotificationschedule"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/supplierprofile"
)

func TestBusinessPublicUpdateRegisteredNativePublish(t *testing.T) {
	f := businessConsoleHTTPNew(t)
	t.Cleanup(func() {
		for _, table := range []string{"native_notification_decisions", "business_public_profile_audit", "business_public_profile_permissions"} {
			query := "DELETE FROM " + table + " WHERE business_id=$1"
			if table == "native_notification_decisions" {
				query = "DELETE FROM native_notification_decisions WHERE recipient_id=ANY($1::uuid[]) OR actor_id=ANY($1::uuid[])"
				f.exec(t, query, f.ids)
			} else {
				f.exec(t, query, f.business)
			}
		}
	})
	f.claim(t)
	p := businessconsole.ProfileInput{Facts: businessconsole.ProfileFacts{Name: "本地合成公开商家", Description: "明确公开批准的资料", TimeZone: "Europe/London", OpeningHours: []businessconsole.HoursDay{}, OfficialLinks: []string{"https://example.invalid/public"}}, SourceURL: "https://example.invalid/PRIVATE_PROOF", RightsNote: "PRIVATE_RIGHTS", ValidUntil: time.Now().UTC().Add(time.Hour)}
	f.request(t, "PUT", f.path("/profile"), f.tokens[0], businessConsoleHTTPJSON(t, p), 200)
	f.request(t, "POST", f.path("/profile/review"), f.tokens[1], businessConsoleHTTPJSON(t, businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "具体合成资料版本"}), 200)
	f.request(t, "POST", "/v1/me/follows", f.tokens[2], businessConsoleHTTPJSON(t, map[string]string{"targetType": "BUSINESS", "targetId": f.business}), 201)
	var view struct {
		Data supplierprofile.PermissionView `json:"data"`
	}
	raw := f.request(t, "GET", f.path("/public-profile/permission"), f.tokens[0], "", 200)
	if e := json.Unmarshal(raw.Body.Bytes(), &view); e != nil || view.Data.Preview == nil {
		t.Fatal(e)
	}
	preview := view.Data.Preview
	input := supplierprofile.PermissionInput{Action: "publish", ExpectedProfileVersion: preview.ProfileVersion, SourceSnapshot: preview.SourceSnapshot, ValidUntil: preview.ValidUntil.Format(time.RFC3339Nano)}
	body := businessConsoleHTTPJSON(t, input)
	f.request(t, "PUT", f.path("/public-profile/permission"), f.tokens[0], body, 200)
	f.request(t, "PUT", f.path("/public-profile/permission"), f.tokens[0], body, 200)
	list := f.request(t, "GET", "/v1/me/inbox", f.tokens[2], "", 200)
	if strings.Count(list.Body.String(), `"resourceType":"business_update"`) != 1 || !strings.Contains(list.Body.String(), `"targetBusinessId":"`+f.business+`"`) {
		t.Fatal("real explicit publish must yield exactly one public business destination", list.Body.String())
	}
	for _, private := range []string{"PRIVATE_PROOF", "PRIVATE_RIGHTS", "明确公开批准的资料", "sourceSnapshot"} {
		if strings.Contains(list.Body.String(), private) {
			t.Fatal("notification copied source content", private)
		}
	}
	t.Log("ACTUAL_PUBLIC_UPDATE_INBOX_JSON " + list.Body.String())
	var inbox struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if e := json.Unmarshal(list.Body.Bytes(), &inbox); e != nil || len(inbox.Data) != 1 {
		t.Fatal(e)
	}
	read := f.request(t, "POST", "/v1/me/inbox/"+inbox.Data[0].ID+"/read", f.tokens[2], "", 200)
	if !strings.Contains(read.Body.String(), `"targetBusinessId":"`+f.business+`"`) || !strings.Contains(read.Body.String(), `"readAt":`) {
		t.Fatal("actual read destination", read.Body.String())
	}
	t.Log("ACTUAL_PUBLIC_UPDATE_READ_JSON " + read.Body.String())
	public := f.request(t, "GET", "/v1/businesses/"+f.business, f.tokens[2], "", 200)
	t.Log("ACTUAL_PUBLIC_UPDATE_PUBLIC_JSON " + public.Body.String())
	f.request(t, "PUT", f.path("/public-profile/permission"), f.tokens[0], businessConsoleHTTPJSON(t, supplierprofile.PermissionInput{ExpectedVersion: 1, Action: "revoke"}), 200)
	list = f.request(t, "GET", "/v1/me/inbox", f.tokens[2], "", 200)
	if strings.Contains(list.Body.String(), `"resourceType":"business_update"`) {
		t.Fatal("revoked public source remains visible", list.Body.String())
	}
	f.request(t, "POST", "/v1/me/inbox/"+inbox.Data[0].ID+"/read", f.tokens[2], "", 404)
}

// Actual registered handlers, PostgreSQL clock and original scheduler. Fixtures
// are owned local people/businesses; no IdP, provider, push or real merchant.
func TestBusinessPublicUpdateRegisteredNativeDigest(t *testing.T) {
	f := businessConsoleHTTPNew(t)
	t.Cleanup(func() {
		f.exec(t, `DELETE FROM native_notification_decisions WHERE recipient_id=ANY($1::uuid[]) OR actor_id=ANY($1::uuid[])`, f.ids)
		f.exec(t, `DELETE FROM business_public_profile_audit WHERE business_id=$1`, f.business)
		f.exec(t, `DELETE FROM business_public_profile_permissions WHERE business_id=$1`, f.business)
	})
	f.claim(t)
	p := businessconsole.ProfileInput{Facts: businessconsole.ProfileFacts{Name: "本地合成摘要商家", Description: "明确公开批准的摘要资料", TimeZone: "Europe/London", OpeningHours: []businessconsole.HoursDay{}, OfficialLinks: []string{"https://example.invalid/digest-public"}}, SourceURL: "https://example.invalid/DIGEST_PRIVATE_PROOF", RightsNote: "DIGEST_PRIVATE_RIGHTS", ValidUntil: time.Now().UTC().Add(time.Hour)}
	f.request(t, "PUT", f.path("/profile"), f.tokens[0], businessConsoleHTTPJSON(t, p), 200)
	f.request(t, "POST", f.path("/profile/review"), f.tokens[1], businessConsoleHTTPJSON(t, businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "本地摘要测试独立审核具体资料版本"}), 200)
	f.request(t, "POST", "/v1/me/follows", f.tokens[2], businessConsoleHTTPJSON(t, map[string]string{"targetType": "BUSINESS", "targetId": f.business}), 201)

	var agentID string
	if e := f.pool.QueryRow(f.ctx, `INSERT INTO agents(principal_account_id,agent_type,status) VALUES($1,'personal','active') RETURNING id`, f.ids[2]).Scan(&agentID); e != nil {
		t.Fatal(e)
	}
	principal, e := actorref.ParsePrincipal("person", f.ids[2])
	if e != nil {
		t.Fatal(e)
	}
	store := postgres.New(f.pool, false)
	if _, e = store.EnsureAgentProfile(f.ctx, agentID, principal); e != nil {
		t.Fatal(e)
	}
	f.request(t, "GET", "/v1/me/notification-policy", f.tokens[2], "", 200)
	f.request(t, "PUT", "/v1/me/notification-policy", f.tokens[2], businessConsoleHTTPJSON(t, agentnotification.PutInput{Enabled: true, DefaultRoute: agentnotification.Digest, Rules: []agentnotification.Rule{}, ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}), 200)
	f.request(t, "GET", "/v1/me/notification-schedule", f.tokens[2], "", 200)
	var now time.Time
	if e = f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	due := now.UTC().Truncate(time.Minute).Add(time.Minute)
	if due.Sub(now) < 10*time.Second {
		due = due.Add(time.Minute)
	}
	f.request(t, "PUT", "/v1/me/notification-schedule", f.tokens[2], businessConsoleHTTPJSON(t, ns.PutInput{Settings: ns.Settings{Enabled: true, TimeZone: "UTC", LocalMinute: due.Hour()*60 + due.Minute(), GapPolicy: ns.GapSkip, FoldPolicy: ns.FoldEarlierOnce, MaxContactsPerDay: 1, Categories: []agentnotification.Category{agentnotification.CategoryBusiness}}, ExpiresAt: time.Now().UTC().Add(10 * time.Minute).Truncate(time.Microsecond)}), 200)

	var view struct {
		Data supplierprofile.PermissionView `json:"data"`
	}
	raw := f.request(t, "GET", f.path("/public-profile/permission"), f.tokens[0], "", 200)
	if e = json.Unmarshal(raw.Body.Bytes(), &view); e != nil || view.Data.Preview == nil {
		t.Fatal("real owner publication preview", e)
	}
	preview := view.Data.Preview
	publication := businessConsoleHTTPJSON(t, supplierprofile.PermissionInput{Action: "publish", ExpectedProfileVersion: preview.ProfileVersion, SourceSnapshot: preview.SourceSnapshot, ValidUntil: preview.ValidUntil.Format(time.RFC3339Nano)})
	f.request(t, "PUT", f.path("/public-profile/permission"), f.tokens[0], publication, 200)
	f.request(t, "PUT", f.path("/public-profile/permission"), f.tokens[0], publication, 200)
	readList := func() ([]inbox.Item, string) {
		t.Helper()
		response := f.request(t, "GET", "/v1/me/inbox", f.tokens[2], "", 200)
		var result struct {
			Data []inbox.Item `json:"data"`
		}
		if e := json.Unmarshal(response.Body.Bytes(), &result); e != nil {
			t.Fatal(e)
		}
		return result.Data, response.Body.String()
	}
	if rows, _ := readList(); len(rows) != 0 {
		t.Fatal("pending Digest must not be exposed by registered Inbox", rows)
	}

	ctx, cancel := context.WithTimeout(f.ctx, 2*time.Minute)
	defer cancel()
	waitStart := time.Now()
	for {
		if e = f.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			t.Fatal(e)
		}
		if !now.Before(due) {
			t.Logf("ACTUAL_REGISTERED_DIGEST_PG_DUE due=%s database_clock=%s elapsed=%s", due.Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano), time.Since(waitStart))
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("bounded original PostgreSQL due wait", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	type cycle struct {
		result ns.RunResult
		err    error
	}
	cycles := make(chan cycle, 2)
	for range 2 {
		go func() {
			result, e := store.ProcessNotificationSchedules(ctx, 10)
			cycles <- cycle{result, e}
		}()
	}
	delivered, slots := 0, 0
	for range 2 {
		result := <-cycles
		if result.err != nil {
			t.Fatal("two original scheduler workers", result.err)
		}
		delivered += result.result.Delivered
		slots += result.result.Slots
	}
	if delivered != 1 || slots != 1 {
		t.Fatal("one logical delivery despite two workers", delivered, slots)
	}
	rows, wire := readList()
	if len(rows) != 1 {
		t.Fatal("actual delivered Digest Inbox count", len(rows))
	}
	item := rows[0]
	if item.ResourceType != "business_update" || item.Category != "updates" || item.SemanticCategory != "BUSINESS" || item.NotificationRoute != "DIGEST" || item.NotificationPriority != 10 || item.TargetBusinessID == nil || *item.TargetBusinessID != f.business || item.ResourceID == f.business || item.ReadAt != nil {
		t.Fatal("current registered native Digest target/metadata", item)
	}
	var auditID, auditedBusiness string
	if e = f.pool.QueryRow(ctx, `SELECT d.source_id,audit.business_id FROM native_notification_decisions d JOIN business_public_profile_audit audit ON audit.id=d.business_public_update_id WHERE d.id=$1 AND d.kind='business_update' AND d.recipient_id=$2`, item.ResourceID, f.ids[2]).Scan(&auditID, &auditedBusiness); e != nil || auditID == item.ResourceID || auditedBusiness != f.business {
		t.Fatal("Inbox resource is exact routing decision, with separate native publication audit", e)
	}
	for _, private := range []string{"DIGEST_PRIVATE_PROOF", "DIGEST_PRIVATE_RIGHTS", "明确公开批准的摘要资料", "sourceSnapshot"} {
		if strings.Contains(wire, private) {
			t.Fatal("Digest copied private or public source content", private)
		}
	}
	t.Log("ACTUAL_REGISTERED_DIGEST_INBOX_JSON " + wire)
	f.request(t, "POST", "/v1/me/inbox/"+item.ID+"/read", "", "", 401)
	f.request(t, "POST", "/v1/me/inbox/"+item.ID+"/read", f.tokens[3], "", 404)
	read := f.request(t, "POST", "/v1/me/inbox/"+item.ID+"/read", f.tokens[2], "", 200)
	var readResult struct {
		Data inbox.Item `json:"data"`
	}
	if e = json.Unmarshal(read.Body.Bytes(), &readResult); e != nil || readResult.Data.ID != item.ID || readResult.Data.ReadAt == nil || readResult.Data.ResourceID != item.ResourceID || readResult.Data.TargetBusinessID == nil || *readResult.Data.TargetBusinessID != f.business || readResult.Data.NotificationRoute != "DIGEST" {
		t.Fatal("registered Digest same Inbox read receipt and destination", e)
	}
	t.Log("ACTUAL_REGISTERED_DIGEST_READ_JSON " + read.Body.String())
	public := f.request(t, "GET", "/v1/businesses/"+f.business, f.tokens[2], "", 200)
	var publicResult struct {
		Data supplierprofile.Business `json:"data"`
	}
	if e = json.Unmarshal(public.Body.Bytes(), &publicResult); e != nil || publicResult.Data.ID != f.business || publicResult.Data.ProfileStatus != "verified" || publicResult.Data.Description != p.Facts.Description {
		t.Fatal("current original public business destination", e)
	}
	t.Log("ACTUAL_REGISTERED_DIGEST_PUBLIC_JSON " + public.Body.String())
	if result, e := store.ProcessNotificationSchedules(ctx, 10); e != nil || result.Delivered != 0 || result.Slots != 0 {
		t.Fatal("original scheduler retry cannot duplicate slot or delivery", result, e)
	}
	var count, receipts int
	if e = f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM inbox_items WHERE recipient_account_id=$1),(SELECT count(*) FROM native_notification_schedule_deliveries WHERE owner_id=$1 AND delivered)`, f.ids[2]).Scan(&count, &receipts); e != nil || count != 1 || receipts != 1 {
		t.Fatal("durable Inbox and rolling receipt remain unique", count, receipts, e)
	}
	f.request(t, "PUT", f.path("/public-profile/permission"), f.tokens[0], businessConsoleHTTPJSON(t, supplierprofile.PermissionInput{ExpectedVersion: 1, Action: "revoke"}), 200)
	if rows, _ := readList(); len(rows) != 0 {
		t.Fatal("revoked native Digest source still visible", rows)
	}
	f.request(t, "POST", "/v1/me/inbox/"+item.ID+"/read", f.tokens[2], "", 404)
}
