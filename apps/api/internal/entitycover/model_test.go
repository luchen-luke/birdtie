package entitycover_test

import (
	"context"
	"encoding/json"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	. "github.com/birdtie/birdtie/apps/api/internal/entitycover"
	"github.com/birdtie/birdtie/apps/api/internal/saved"
	"strings"
	"testing"
	"time"
)

func TestEntityCoverOriginalSessionContextCannotChangeIdentityOrPublicProof(t *testing.T) {
	a := Access{AccountID: coverTestID, AccountType: "person"}
	a.SessionDigest[0] = 5
	frame := SessionFrame{ID: coverTestID, Generation: 7}
	ctx := WithOriginalSession(context.Background(), a, frame)
	if got, ok := OriginalSession(ctx, a); !ok || got != frame {
		t.Fatal("original frame missing")
	}
	changed := a
	changed.SessionDigest[0] = 6
	if _, ok := OriginalSession(ctx, changed); ok {
		t.Fatal("borrowed Session from new credential")
	}
	if _, ok := OriginalSession(ctx, Access{}); ok {
		t.Fatal("authenticated context downgraded to anonymous")
	}
	if !HasOriginalSession(ctx) {
		t.Fatal("mismatched frame treated as unbound")
	}
	newFrame := frame
	newFrame.Generation++
	rebound := WithOriginalSession(ctx, a, newFrame)
	if _, ok := OriginalSession(rebound, a); ok {
		t.Fatal("fresh capture overwrote immutable original frame")
	}
	if got, ok := OriginalSession(WithOriginalSession(ctx, a, frame), a); !ok || got != frame {
		t.Fatal("same original frame should be reusable")
	}
	b, _ := json.Marshal(State{Version: 0, SourceProof: "SQL_RESOURCE_ONLY"})
	if strings.Contains(string(b), "generation") || strings.Contains(string(b), coverTestID) || strings.Contains(string(b), "SQL_RESOURCE_ONLY") {
		t.Fatal("server-only Session frame escaped wire")
	}
	private, _ := json.Marshal([]any{frame, OriginalContent{Bytes: []byte("PIXELS"), SourceProof: "FIRST_PRIVATE_PROOF"}})
	if strings.Contains(string(private), coverTestID) || strings.Contains(string(private), "FIRST_PRIVATE_PROOF") || strings.Contains(string(private), "PIXELS") {
		t.Fatal("internal frame/envelope became public DTO")
	}
}

const coverTestID = "01234567-89ab-4cde-8fab-0123456789ab"

func TestEntityCoverTargetLeaseRightsAndOriginalReceipt(t *testing.T) {
	now := time.Now().UTC()
	r := Ref{Type: "activity", ID: coverTestID}
	m := Metadata{Ref: r, Version: 1, ThumbnailPath: Path(r, 1), Width: Width, Height: Height, ByteCount: 12, SHA256: strings.Repeat("a", 64), ExpiresAt: now.Add(45 * time.Second)}
	if !m.Valid(now) {
		t.Fatal("canonical metadata")
	}
	for _, change := range []func(*Metadata){func(m *Metadata) { m.Type = "place" }, func(m *Metadata) { m.ThumbnailPath = "https://external.invalid/photo" }, func(m *Metadata) { m.Width = 128 }, func(m *Metadata) { m.ExpiresAt = now.Add(62 * time.Second) }, func(m *Metadata) { m.ExpiresAt = now }} {
		bad := m
		change(&bad)
		if bad.Valid(now) {
			t.Fatal("invalid/crossentity media accepted")
		}
	}
	current := m
	current.ExpiresAt = now.Add(60 * time.Second)
	if !PreserveLease(&m, &current, now) || current.ExpiresAt != m.ExpiresAt {
		t.Fatal("renewed emitted lease")
	}
	short := m
	short.ExpiresAt = now.Add(20 * time.Second)
	if PreserveLease(&m, &short, now) {
		t.Fatal("shortened source deadline accepted")
	}
	rights := Rights{Basis: "OWNED", RetentionUntil: now.Add(time.Hour), CanRetain: true, CanPublish: true}
	if !rights.Valid(now) {
		t.Fatal("explicit own source")
	}
	rights.CanPublish = false
	if rights.Valid(now) {
		t.Fatal("missing publication right")
	}
	rights.CanPublish = true
	rights.Basis = "LICENSED"
	if rights.Valid(now) {
		t.Fatal("license evidence missing")
	}
	for _, status := range []Receipt{{Ref: r, OperationID: coverTestID, Action: "SAVE", AppliedVersion: 2, Status: "SUCCEEDED"}, {Ref: r, OperationID: coverTestID, Action: "STOP", Status: "STOPPED"}} {
		if !status.Valid(r, coverTestID) {
			t.Fatal("original causal receipt")
		}
	}
	stopped := Receipt{Ref: r, OperationID: coverTestID, Action: "STOP", AppliedVersion: 1, Status: "STOPPED"}
	if stopped.Valid(r, coverTestID) {
		t.Fatal("stop claimed effect")
	}
	b, _ := json.Marshal(State{Version: 1, Cover: &m, SourceProof: "PRIVATE_AUTHORITY"})
	if strings.Contains(string(b), "PRIVATE_AUTHORITY") {
		t.Fatal("internal proof escaped")
	}
}

func TestEntityCoverHumanProjectionCannotBorrowAnotherEntity(t *testing.T) {
	now := time.Now().UTC()
	ref := Ref{Type: "activity", ID: coverTestID}
	m := Metadata{Ref: ref, Version: 1, ThumbnailPath: Path(ref, 1), Width: Width, Height: Height, ByteCount: 20, SHA256: strings.Repeat("a", 64), ExpiresAt: now.Add(40 * time.Second)}
	item := arp.Item{Entity: arp.Ref{Type: "activity", ID: coverTestID}, Title: "合成活动", Scope: arp.AuthorizedView, Cover: &m}
	if arp.ValidateItem(item) != nil {
		t.Fatal("human original cover")
	}
	for _, kind := range []string{"place", "person", "community", "organization"} {
		bad := item
		bad.Entity.Type = kind
		if arp.ValidateItem(bad) == nil {
			t.Fatal("borrowed entity", kind)
		}
	}
	bookmark := saved.Item{Kind: "activity", TargetID: coverTestID, Available: true, Cover: &m}
	if !bookmark.ValidCover(now) {
		t.Fatal("original Saved cover")
	}
	bookmark.Kind = "place"
	if bookmark.ValidCover(now) {
		t.Fatal("place borrowed Activity cover")
	}
	bookmark.Kind = "activity"
	bookmark.Available = false
	if bookmark.ValidCover(now) {
		t.Fatal("retired source retained picture")
	}
}
