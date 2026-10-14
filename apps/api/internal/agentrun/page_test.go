package agentrun

import (
	"bytes"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"strings"
	"testing"
	"time"
)

func TestAgentRunPageCipherRejectsTamperAndRestart(t *testing.T) {
	c, e := NewCursorCodec(bytes.Repeat([]byte{1}, 32))
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	f := CursorFrame{Schema: PageSchema, Owner: "11111111-1111-4111-8111-111111111111", Agent: "22222222-2222-4222-8222-222222222222", Session: "33333333-3333-4333-8333-333333333333", Generation: 1, Authority: strings.Repeat("a", 64), Ceiling: now, Until: now.Add(PageLease)}
	v, e := c.Seal(f)
	if e != nil || strings.Contains(v, f.Owner) {
		t.Fatal("cursor leaked or invalid", e)
	}
	got, e := c.Open(v)
	if e != nil || got != f {
		t.Fatal(got, e)
	}
	other, _ := NewCursorCodec(bytes.Repeat([]byte{2}, 32))
	if _, e = other.Open(v); e == nil {
		t.Fatal("restart accepted old key")
	}
	bad := []byte(v)
	bad[9] = '!'
	if _, e = c.Open(string(bad)); e == nil {
		t.Fatal("tamper accepted")
	}
	f.Until = f.Ceiling.Add(PageLease + time.Second)
	if _, e = c.Seal(f); e == nil {
		t.Fatal("extended lease accepted")
	}
}
func TestAgentRunPageShapeNeverGrantsModelOrExtendsLease(t *testing.T) {
	now := time.Now().UTC()
	p := Page{Schema: PageSchema, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: "11111111-1111-4111-8111-111111111111"}, AgentID: "22222222-2222-4222-8222-222222222222", Items: []Record{}, ObservedAt: now, ExpiresAt: now.Add(PageLease)}
	if ValidatePage(p, p.Owner.ID, now) != nil {
		t.Fatal("empty bounded page rejected")
	}
	p.ModelAccess = true
	if ValidatePage(p, p.Owner.ID, now) == nil {
		t.Fatal("page granted model")
	}
	p.ModelAccess = false
	p.ExpiresAt = now.Add(PageLease + time.Second)
	if ValidatePage(p, p.Owner.ID, now) == nil {
		t.Fatal("lease extended")
	}
	p.ExpiresAt = now.Add(PageLease)
	v := "cursor"
	p.NextCursor = &v
	if ValidatePage(p, p.Owner.ID, now) == nil {
		t.Fatal("empty continuation accepted")
	}
}
