package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/birdtie/birdtie/apps/api/internal/supplierprofile"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"testing"
	"time"
)

func supplierNativeFixture(t *testing.T) (*businessConsoleFixture, string, supplierprofile.PermissionInput) {
	t.Helper()
	f := newBusinessConsoleFixture(t)
	id := f.newBusinessID(t)
	t.Cleanup(func() {
		for _, table := range []string{"business_public_profile_audit", "business_public_profile_permissions"} {
			f.exec(t, `DELETE FROM `+table+` WHERE business_id=$1`, id)
		}
	})
	f.claim(t, id)
	f.grant(t, id)
	r := businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "独立本地合成审核，非真实经营证明"}
	if _, e := f.store.ReviewBusinessClaim(f.ctx, f.who(1, id), r); e != nil {
		t.Fatal(e)
	}
	var now time.Time
	if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	p := businessProfileNativeInput(now)
	p.RightsNote = "PRIVATE_RIGHTS_CANARY"
	p.SourceURL = "https://example.invalid/PRIVATE_SOURCE_CANARY"
	p.Facts.Description = "明确批准才可公开的合成介绍"
	if _, e := f.store.PutBusinessProfile(f.ctx, f.who(0, id), p); e != nil {
		t.Fatal(e)
	}
	if _, e := f.store.ReviewBusinessProfile(f.ctx, f.who(1, id), r); e != nil {
		t.Fatal(e)
	}
	v, e := f.store.ReadBusinessPublicPermission(f.ctx, f.who(0, id))
	if e != nil || v.Preview == nil {
		t.Fatal(v, e)
	}
	in := supplierprofile.PermissionInput{ExpectedProfileVersion: v.Preview.ProfileVersion, Action: "publish", ValidUntil: v.Preview.ValidUntil.Format(time.RFC3339Nano), SourceSnapshot: v.Preview.SourceSnapshot}
	return f, id, in
}
func TestSupplierNativeNanosecondExactRetry(t *testing.T) {
	f, id, in := supplierNativeFixture(t)
	until, e := time.Parse(time.RFC3339Nano, in.ValidUntil)
	if e != nil {
		t.Fatal(e)
	}
	// A shorter, 9-digit nanosecond intent must be normalized once to the
	// database microsecond precision, not rejected on an exact request retry.
	in.ValidUntil = until.Add(-time.Minute).Truncate(time.Second).Add(123456789 * time.Nanosecond).Format(time.RFC3339Nano)
	first, e := f.store.ChangeBusinessPublicPermission(f.ctx, f.who(0, id), in)
	if e != nil {
		t.Fatal(e)
	}
	again, e := f.store.ChangeBusinessPublicPermission(f.ctx, f.who(0, id), in)
	if e != nil || first.Version != again.Version {
		t.Fatal(first, again, e)
	}
}
func TestSupplierNativeUTCWireAndDatabaseZoneIndependentAuthority(t *testing.T) {
	f, id, in := supplierNativeFixture(t)
	view, e := f.store.ReadBusinessPublicPermission(f.ctx, f.who(0, id))
	if e != nil || view.Preview == nil {
		t.Fatal(view, e)
	}
	raw, e := json.Marshal(view)
	if e != nil {
		t.Fatal(e)
	}
	if view.Preview.ReviewedAt.Location() != time.UTC || view.Preview.ValidUntil.Location() != time.UTC || strings.Contains(string(raw), "+08:00") {
		t.Fatal("non UTC strict client wire", string(raw))
	}
	committed, e := f.store.ChangeBusinessPublicPermission(f.ctx, f.who(0, id), in)
	if e != nil || committed.ValidUntil == nil || committed.ValidUntil.Location() != time.UTC {
		t.Fatal(committed, e)
	}
	cfg := f.pool.Config()
	cfg.ConnConfig.RuntimeParams["timezone"] = "Asia/Tokyo"
	other, e := pgxpool.NewWithConfig(f.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	public, e := New(other, false).ReadPublicBusiness(f.ctx, id, "")
	if e != nil || public.ProfileStatus != "verified" || public.ProfileVersion != view.Preview.ProfileVersion || public.ReviewedAt == nil || public.ReviewedAt.Location() != time.UTC || public.ValidUntil == nil || public.ValidUntil.Location() != time.UTC {
		t.Fatal("authority hash changed with database zone", public, e)
	}
}
func TestSupplierNativeCanonicalOrganizationActivityProjection(t *testing.T) {
	f := organizationKnowledgeNativeFixture(t)
	b := f.p.base
	public := f.activity(t, "真实路径合成公开活动", "public")
	private := f.activity(t, "不可公开的成员活动", "organizer_members")
	b.exec(`UPDATE activities SET organization_id=NULL WHERE id=$1`, public.ID)
	profile, e := b.store.GetPublicProfile(b.ctx, b.orgID, "")
	if e != nil {
		t.Fatal(e)
	}
	if len(profile.UpcomingActivities) != 1 || profile.UpcomingActivities[0].ID != public.ID || profile.UpcomingActivities[0].ID == private.ID {
		t.Fatal("canonical public list", profile, e)
	}
	activity := profile.UpcomingActivities[0]
	if activity.Organizer.Type != "ORGANIZATION" || activity.Organizer.ID != b.orgID || activity.OrganizationID == nil || *activity.OrganizationID != b.orgID || activity.Visibility != "public" || activity.Schedule == "" || activity.Source.Reference == "" {
		t.Fatal("incomplete public activity", activity)
	}
	if _, e = b.store.GetActivity(b.ctx, public.ID, ""); e != nil {
		t.Fatal("stable canonical detail", e)
	}
	b.exec(`UPDATE organizations SET visibility='private' WHERE id=$1`, b.orgID)
	if _, e = b.store.GetPublicProfile(b.ctx, b.orgID, ""); !errors.Is(e, organization.ErrNotFound) {
		t.Fatal("private org exposed", e)
	}
}
func TestSupplierNativeExplicitPublicationAndRevocation(t *testing.T) {
	f, id, in := supplierNativeFixture(t)
	b, e := f.store.ReadPublicBusiness(f.ctx, id, "")
	if e != nil || b.ProfileStatus != "unpublished" || len(b.OfficialLinks) != 0 || b.Description != "" {
		t.Fatal("review auto-published management fields", b, e)
	}
	p, e := f.store.ChangeBusinessPublicPermission(f.ctx, f.who(0, id), in)
	if e != nil || p.Version != 1 {
		t.Fatal(p, e)
	}
	if p, e = f.store.ChangeBusinessPublicPermission(f.ctx, f.who(0, id), in); e != nil || p.Version != 1 {
		t.Fatal("retry duplicated", p, e)
	}
	b, e = f.store.ReadPublicBusiness(f.ctx, id, "")
	if e != nil || b.ProfileStatus != "verified" || len(b.OfficialLinks) != 1 || b.ProfileVersion != 2 {
		t.Fatal("authorized public read", b, e)
	}
	raw, _ := json.Marshal(b)
	for _, secret := range []string{"PRIVATE_RIGHTS_CANARY", "PRIVATE_SOURCE_CANARY", "rightsNote", "reviewedBy", "openingHours", "sourceSnapshot"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("private management disclosed", secret)
		}
	}
	var audits int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM business_public_profile_audit WHERE business_id=$1`, id).Scan(&audits); e != nil || audits != 1 {
		t.Fatal(audits, e)
	}
	if p, e = f.store.ChangeBusinessPublicPermission(f.ctx, f.who(0, id), supplierprofile.PermissionInput{ExpectedVersion: 1, Action: "revoke"}); e != nil || p.Version != 2 {
		t.Fatal(p, e)
	}
	b, e = f.store.ReadPublicBusiness(f.ctx, id, "")
	if e != nil || b.ProfileStatus != "unpublished" || len(b.OfficialLinks) != 0 {
		t.Fatal("revoked content retained", b, e)
	}
}
func TestSupplierNativeOwnerAndSourceVersionBoundaries(t *testing.T) {
	f, id, in := supplierNativeFixture(t)
	for _, who := range []int{1, 2, 3} {
		t.Run("non_owner_"+f.people[who], func(t *testing.T) {
			if _, e := f.store.ChangeBusinessPublicPermission(f.ctx, f.who(who, id), in); !errors.Is(e, businessconsole.ErrForbidden) {
				t.Fatal(e)
			}
		})
	}
	if _, e := f.store.ChangeBusinessMember(f.ctx, f.who(0, id), businessconsole.MemberInput{TargetPersonID: f.people[2], Action: "grant", Role: "admin"}); e != nil {
		t.Fatal(e)
	}
	if _, e := f.store.ChangeBusinessPublicPermission(f.ctx, f.who(2, id), in); !errors.Is(e, businessconsole.ErrForbidden) {
		t.Fatal("admin publish", e)
	}
	// Membership source changes must invalidate the old precise preview, even
	// when the same owner and profile version subsequently remain in place.
	f.exec(t, `UPDATE business_memberships SET updated_at=clock_timestamp() WHERE business_id=$1 AND user_account_id=$2`, id, f.people[0])
	if _, e := f.store.ChangeBusinessPublicPermission(f.ctx, f.who(0, id), in); !errors.Is(e, businessconsole.ErrConflict) {
		t.Fatal("ABA stale preview", e)
	}
	v, e := f.store.ReadBusinessPublicPermission(f.ctx, f.who(0, id))
	if e != nil || v.Preview == nil {
		t.Fatal(v, e)
	}
	in.SourceSnapshot = v.Preview.SourceSnapshot
	if _, e = f.store.ChangeBusinessPublicPermission(f.ctx, f.who(0, id), in); e != nil {
		t.Fatal(e)
	}
	f.exec(t, `UPDATE business_review_grants SET state='revoked' WHERE business_id=$1`, id)
	b, e := f.store.ReadPublicBusiness(f.ctx, id, "")
	if e != nil || b.ProfileStatus != "unpublished" || len(b.OfficialLinks) != 0 {
		t.Fatal("revoked source leaked", b, e)
	}
	f.exec(t, `UPDATE business_review_grants SET state='active' WHERE business_id=$1`, id)
	b, e = f.store.ReadPublicBusiness(f.ctx, id, "")
	if e != nil || b.ProfileStatus != "unpublished" {
		t.Fatal("regrant resurrected old disclosure", b, e)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	if _, e = f.store.ReadPublicBusiness(ctx, id, ""); e == nil {
		t.Fatal("cancelled read accepted")
	}
}
func TestSupplierNativePendingEditInvalidatesPublication(t *testing.T) {
	f, id, in := supplierNativeFixture(t)
	if _, e := f.store.ChangeBusinessPublicPermission(f.ctx, f.who(0, id), in); e != nil {
		t.Fatal(e)
	}
	var now time.Time
	if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	p := businessProfileNativeInput(now)
	p.ExpectedVersion = 2
	p.Facts.Description = "下一版本未批准公开"
	if _, e := f.store.PutBusinessProfile(f.ctx, f.who(0, id), p); e != nil {
		t.Fatal(e)
	}
	b, e := f.store.ReadPublicBusiness(f.ctx, id, "")
	if e != nil || b.ProfileStatus != "unpublished" || b.Description != "" {
		t.Fatal("edit reused old approval", b, e)
	}
	if _, e = f.store.ReviewBusinessProfile(f.ctx, f.who(1, id), businessconsole.ReviewInput{ExpectedVersion: 3, Decision: "approve", Note: "新资料独立审核不是公开批准"}); e != nil {
		t.Fatal(e)
	}
	b, e = f.store.ReadPublicBusiness(f.ctx, id, "")
	if e != nil || b.ProfileStatus != "unpublished" {
		t.Fatal("new review silently published", b, e)
	}
	f.exec(t, `UPDATE businesses SET claim_status='revoked' WHERE id=$1`, id)
	if _, e = f.store.ReadPublicBusiness(f.ctx, id, ""); !errors.Is(e, businessconsole.ErrNotFound) {
		t.Fatal("claim revoked remains visible", e)
	}
}
