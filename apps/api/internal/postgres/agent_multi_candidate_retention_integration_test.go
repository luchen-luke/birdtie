package postgres

import (
	"context"
	"encoding/json"
	"errors"
	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	amc "github.com/birdtie/birdtie/apps/api/internal/agentmulticandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"reflect"
	"strings"
	"testing"
	"time"
)

type multiNativeFixture struct {
	f         *enrichmentPurposeFixture
	grants    []aep.Grant
	selection amc.Selection
}

func multiNative(t *testing.T, bodies ...string) *multiNativeFixture {
	t.Helper()
	ownedMigrationDatabase(t)
	f := enrichmentPurposeNative(t)
	b := f.f.place.private.base
	a := f.f.place.private.owner
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// Original owned-profile cleanup removes Memory/Evidence through their
		// authorized parent cascade. Direct DELETE would bypass 056/057 tombstones.
		for _, q := range []string{`DELETE FROM agent_effect_ledger WHERE subject_id=ANY($1::uuid[])`, `DELETE FROM agent_memory_candidates WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM agent_multi_candidate_bindings WHERE grant_id IN(SELECT id FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]))`, `DELETE FROM agent_multi_candidate_previews WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM agent_candidate_retention_bindings WHERE grant_id IN(SELECT id FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]))`, `DELETE FROM agent_candidate_retention_previews WHERE owner_id=ANY($1::uuid[])`} {
			if _, e := b.pool.Exec(ctx, q, b.accounts); e != nil {
				t.Error("multi fixture cleanup", e)
			}
		}
	})
	if len(bodies) != 0 && len(bodies) != 2 {
		t.Fatal("fixture needs exactly two private texts")
	}
	secondBody := "另一条独立羽毛球记录_MULTI_PRIVATE_BODY_CANARY"
	if len(bodies) == 2 {
		m, e := b.store.UpdateMomentDraft(b.ctx, b.person.ID, f.moment.ID, f.moment.Revision, content.MomentInput{CityID: f.f.place.city, Title: f.moment.Title, Body: bodies[0], TimePrecision: "unknown", LocationPrecision: "city"})
		if e != nil {
			t.Fatal(e)
		}
		f.moment = m
		f.selection.MomentRevision = m.Revision
		secondBody = bodies[1]
	}
	_, g := f.approve(t)
	m, e := b.store.CreateMomentDraft(b.ctx, b.person.ID, content.MomentInput{CityID: f.f.place.city, Title: "PRIVATE_MULTI_TITLE_CANARY", Body: secondBody, TimePrecision: "unknown", LocationPrecision: "city"})
	if e != nil {
		t.Fatal(e)
	}
	sel := f.selection
	sel.MomentID = m.ID
	sel.MomentRevision = m.Revision
	p, e := b.store.PreviewOwnEnrichmentPurpose(b.ctx, a, sel)
	if e != nil {
		t.Fatal(e)
	}
	g2, e := b.store.ApproveOwnEnrichmentPurpose(b.ctx, a, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	return &multiNativeFixture{f: f, grants: []aep.Grant{g, g2}, selection: amc.Selection{AnalysisGrantIDs: []string{g.ID, g2.ID}, RetainUntil: time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)}}
}
func (f *multiNativeFixture) approve(t *testing.T) (amc.Preview, amc.Grant) {
	t.Helper()
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	p, e := b.store.PreviewOwnMultiCandidate(b.ctx, a, f.selection)
	if e != nil {
		t.Fatal("multi preview", e)
	}
	g, e := b.store.ApproveOwnMultiCandidate(b.ctx, a, p.ID)
	if e != nil {
		t.Fatal("multi approve", e)
	}
	return p, g
}
func TestMultiCandidateNativeConcreteSelectionIndependentAuthority(t *testing.T) {
	f := multiNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	p, g := f.approve(t)
	if amc.ValidatePreview(p) != nil || amc.ValidateGrant(g) != nil || p.Review.Clusters != 2 || len(p.Review.Sources) != 2 || g.ExpiresAt.After(p.ExpiresAt) {
		t.Fatal("closed DTO", p, g)
	}
	retry, e := b.store.ApproveOwnMultiCandidate(b.ctx, a, p.ID)
	if e != nil || retry.ID != g.ID || !retry.ExpiresAt.Equal(g.ExpiresAt) {
		t.Fatal("approve renewal", e)
	}
	read, e := b.store.ReadOwnMultiCandidatePreview(b.ctx, a, p.ID)
	if e != nil || read.State != "RECEIPT_ONLY" || read.Review != nil {
		t.Fatal("metadata", e)
	}
	var raw string
	if e = b.pool.QueryRow(b.ctx, `SELECT to_jsonb(p)::text FROM agent_multi_candidate_previews p WHERE id=$1`, p.ID).Scan(&raw); e != nil || strings.Contains(raw, "PRIVATE_BODY_CANARY") || strings.Contains(raw, "PRIVATE_MULTI_TITLE_CANARY") {
		t.Fatal("persisted body", e)
	}
	reversed := f.selection
	reversed.AnalysisGrantIDs = []string{f.selection.AnalysisGrantIDs[1], f.selection.AnalysisGrantIDs[0]}
	p2, e := b.store.PreviewOwnMultiCandidate(b.ctx, a, reversed)
	if e != nil || !reflect.DeepEqual(p.Review.Sources, p2.Review.Sources) || p.Review.AnchorEventID != p2.Review.AnchorEventID {
		t.Fatal("unstable anchor", e)
	}
	if _, e = b.store.ApproveOwnMultiCandidate(b.ctx, a, f.grants[0].PreviewID); e == nil {
		t.Fatal("analysis preview became retention")
	}
	for _, id := range []string{f.grants[0].ID, f.grants[1].ID} {
		if _, e = NewMultiCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMultiCandidate(b.ctx, a, id); e == nil {
			t.Fatal("analysis became producer grant")
		}
	}
	rev, e := b.store.RevokeOwnMultiCandidate(b.ctx, a, g.ID, 1)
	if e != nil || rev.RevokedAt == nil {
		t.Fatal(e)
	}
	if _, e = b.store.ApproveOwnMultiCandidate(b.ctx, a, p.ID); !errors.Is(e, amc.ErrExpired) {
		t.Fatal("revived grant", e)
	}
}
func multiZero(t *testing.T, r amc.Receipt, e error) {
	t.Helper()
	if e == nil || !reflect.DeepEqual(r, amc.Receipt{}) {
		t.Fatal("payload on denial", r, e)
	}
}
func TestMultiCandidateNativeMemberMutationRejectsSameCategory(t *testing.T) {
	f := multiNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	_, g := f.approve(t)
	_, e := b.pool.Exec(b.ctx, `UPDATE moments SET body=body||' 羽毛球',revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, f.grants[1].Selection.MomentID)
	if e != nil {
		t.Fatal(e)
	}
	r, e := NewMultiCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMultiCandidate(b.ctx, a, g.ID)
	multiZero(t, r, e)
}
func TestMultiCandidateNativeRejectDuplicateSourceAndAmbiguousClass(t *testing.T) {
	f := multiNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	p, e := b.store.PreviewOwnEnrichmentPurpose(b.ctx, a, f.f.selection)
	if e != nil {
		t.Fatal(e)
	}
	g, e := b.store.ApproveOwnEnrichmentPurpose(b.ctx, a, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	s := f.selection
	s.AnalysisGrantIDs = []string{f.grants[0].ID, g.ID}
	if _, e = b.store.PreviewOwnMultiCandidate(b.ctx, a, s); !errors.Is(e, amc.ErrDenied) {
		t.Fatal("same Moment duplicate", e)
	}
	m, e := b.store.UpdateMomentDraft(b.ctx, b.person.ID, f.grants[1].Selection.MomentID, f.grants[1].Selection.MomentRevision, content.MomentInput{CityID: f.f.f.place.city, Title: "多义标题", Body: "羽毛球与篮球", TimePrecision: "unknown", LocationPrecision: "city"})
	if e != nil {
		t.Fatal(e)
	}
	selected := f.f.selection
	selected.MomentID = m.ID
	selected.MomentRevision = m.Revision
	ap, e := b.store.PreviewOwnEnrichmentPurpose(b.ctx, a, selected)
	if e != nil {
		t.Fatal(e)
	}
	ag, e := b.store.ApproveOwnEnrichmentPurpose(b.ctx, a, ap.ID)
	if e != nil {
		t.Fatal(e)
	}
	s.AnalysisGrantIDs = []string{f.grants[0].ID, ag.ID}
	denied, e := b.store.PreviewOwnMultiCandidate(b.ctx, a, s)
	if !errors.Is(e, amc.ErrUnavailable) || !reflect.DeepEqual(denied, amc.Preview{}) {
		t.Fatal("ambiguous lexical source must be unavailable with zero review", e)
	}
}

func TestMultiCandidateNativeFiveSourcesExactFieldsAndNoProbability(t *testing.T) {
	f := multiNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	for i := 0; i < 3; i++ {
		m, e := b.store.CreateMomentDraft(b.ctx, b.person.ID, content.MomentInput{CityID: f.f.f.place.city, Title: "未选标题", Body: "羽毛球", TimePrecision: "unknown", LocationPrecision: "city"})
		if e != nil {
			t.Fatal(e)
		}
		s := f.f.selection
		s.MomentID = m.ID
		s.MomentRevision = m.Revision
		p, e := b.store.PreviewOwnEnrichmentPurpose(b.ctx, a, s)
		if e != nil {
			t.Fatal(e)
		}
		g, e := b.store.ApproveOwnEnrichmentPurpose(b.ctx, a, p.ID)
		if e != nil {
			t.Fatal(e)
		}
		f.selection.AnalysisGrantIDs = append(f.selection.AnalysisGrantIDs, g.ID)
	}
	p, g := f.approve(t)
	if p.Review.Clusters != 5 || p.Review.Proposal.Assessment.Value != nil || p.Review.Proposal.Assessment.Level != "LOW" || len(p.Review.Sources) != 5 {
		t.Fatal("native cluster ordinal", p.Review)
	}
	r, e := NewMultiCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMultiCandidate(b.ctx, a, g.ID)
	if e != nil || len(r.Candidate.Sources) != 5 || r.Candidate.Assessment.Value != nil {
		t.Fatal("five selected current sources", e)
	}
	raw, _ := json.Marshal(p)
	if strings.Contains(string(raw), "未选标题") {
		t.Fatal("unselected title leaked into review")
	}
}

var _ = agentprofile.PrivateAccess{}

func TestMultiPreviewReceiptNativeOriginalHistoryIsNotSourcePermission(t *testing.T) {
	f := multiNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	p, e := b.store.PreviewOwnMultiCandidate(b.ctx, a, f.selection)
	if e != nil {
		t.Fatal(e)
	}
	before := receiptPublicXmin(t, b.pool, b.ctx)
	r, e := b.store.ReadOwnMultiCandidatePreviewReceipt(b.ctx, a, p.ID)
	if e != nil || r.State != "OPEN_UNCONSUMED" || amc.ValidatePreviewReceipt(r) != nil {
		t.Fatal(r, e)
	}
	if receiptPublicXmin(t, b.pool, b.ctx) != before {
		t.Fatal("multi metadata GET writes")
	}
	g, e := b.store.ApproveOwnMultiCandidate(b.ctx, a, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.RevokeOwnMultiCandidate(b.ctx, a, g.ID, g.Revision); e != nil {
		t.Fatal(e)
	}
	b.exec(`UPDATE moments SET body='RECEIPT_HIDDEN_BODY_CANARY',revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, f.f.moment.ID)
	before = receiptPublicXmin(t, b.pool, b.ctx)
	r, e = b.store.ReadOwnMultiCandidatePreviewReceipt(b.ctx, a, p.ID)
	if e != nil || r.State != "APPROVAL_RECORDED" || r.ConsumedGrantID != g.ID {
		t.Fatal(r, e)
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "source") || strings.Contains(string(raw), `"review"`) || strings.Contains(string(raw), "CANARY") || strings.Contains(string(raw), "selection") {
		t.Fatal("multi history leaked projection")
	}
	if receiptPublicXmin(t, b.pool, b.ctx) != before {
		t.Fatal("multi history GET writes")
	}
	if _, e = b.store.ReadOwnMultiCandidatePreviewReceipt(b.ctx, f.f.f.place.private.peer, p.ID); e == nil {
		t.Fatal("cross owner metadata")
	}
}

func TestMultiPreviewReceiptNativeIndependentSourceLocksAndNewSession(t *testing.T) {
	f := multiNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	p, g := f.approve(t)
	var token [32]byte
	for i := range token {
		token[i] = byte(i + 91)
	}
	b.exec(`WITH n AS MATERIALIZED(SELECT clock_timestamp() at) INSERT INTO sessions(id,account_id,token_sha256,expires_at,idle_expires_at,authentication_method) SELECT gen_random_uuid(),$1,$2,n.at+interval '1 hour',n.at+interval '30 minutes','test' FROM n`, b.person.ID, token[:])
	a.SessionDigest = token
	before := receiptPublicXmin(t, b.pool, b.ctx)
	held, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer held.Rollback(context.Background())
	if _, e = held.Exec(b.ctx, `LOCK TABLE moments,agent_tasks,contexts,city_contexts,cities,agent_domain_outbox,moment_activity_links,moment_community_links,moment_organization_links IN ACCESS EXCLUSIVE MODE`); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(b.ctx, 3*time.Second)
	defer cancel()
	r, e := NewMultiCandidatePipeline(b.store, pipelineFlags(t, false)).ReadOwnMultiCandidatePreviewReceipt(ctx, a, p.ID)
	if e != nil || r.State != "APPROVAL_RECORDED" || r.ConsumedGrantID != g.ID {
		t.Fatal("new Session bodyless metadata through actual executor", r, e)
	}
	if e = held.Rollback(b.ctx); e != nil {
		t.Fatal(e)
	}
	if receiptPublicXmin(t, b.pool, b.ctx) != before {
		t.Fatal("multi receipt changed rows/xmin")
	}
}

func TestMultiPreviewReceiptNativeClosedUnconsumedAndMissingGuard(t *testing.T) {
	f := multiNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()+interval '2 seconds'`).Scan(&f.selection.RetainUntil); e != nil {
		t.Fatal(e)
	}
	p, e := b.store.PreviewOwnMultiCandidate(b.ctx, a, f.selection)
	if e != nil {
		t.Fatal(e)
	}
	b.exec(`ALTER TABLE agent_multi_candidate_previews DISABLE TRIGGER agent_multi_candidate_preview_guard`)
	before := receiptPublicXmin(t, b.pool, b.ctx)
	r, e := b.store.ReadOwnMultiCandidatePreviewReceipt(b.ctx, a, p.ID)
	if !errors.Is(e, amc.ErrUnavailable) || !reflect.DeepEqual(r, amc.PreviewReceipt{}) {
		t.Fatal("disabled approval guard must not issue history metadata", r, e)
	}
	b.exec(`ALTER TABLE agent_multi_candidate_previews ENABLE TRIGGER agent_multi_candidate_preview_guard`)
	for {
		var expired bool
		if e = b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()>=$1`, p.ExpiresAt).Scan(&expired); e != nil {
			t.Fatal(e)
		}
		if expired {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	r, e = b.store.ReadOwnMultiCandidatePreviewReceipt(b.ctx, a, p.ID)
	if e != nil || r.State != "CLOSED_UNCONSUMED" || r.ConsumedGrantID != "" || !r.PreviewExpiresAt.Equal(p.ExpiresAt) {
		t.Fatal("fixed past preview has no current approval record", r, e)
	}
	if receiptPublicXmin(t, b.pool, b.ctx) != before {
		t.Fatal("closed metadata changed rows/xmin")
	}
	b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:])
	r, e = b.store.ReadOwnMultiCandidatePreviewReceipt(b.ctx, a, p.ID)
	if !errors.Is(e, amc.ErrDenied) || !reflect.DeepEqual(r, amc.PreviewReceipt{}) {
		t.Fatal("current Session required even for closed history", r, e)
	}
}
