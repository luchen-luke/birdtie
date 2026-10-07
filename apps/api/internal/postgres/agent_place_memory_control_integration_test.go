package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentplacememory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"strings"
	"testing"
	"time"
)

func TestPlaceMemoryHumanControlNativeExpiryHiddenRenewalAndCAS(t *testing.T) {
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	r := f.declare(t, agentplacememory.Visited)
	initial, e := b.store.ReadOwnPlaceDeclarationControls(b.ctx, f.private.owner, f.place)
	if e != nil || len(initial.Declarations) != 1 || initial.Declarations[0].Status != agentmemory.StatusActive {
		t.Fatal("current native declaration control", e)
	}
	in := placeDeclarationInput(f.place, agentplacememory.Visited)
	in.ExpectedVersion = r.Version
	in.ValidUntil = time.Now().UTC().Add(400 * time.Millisecond)
	expired, e := b.store.PutOwnPlaceDeclaration(b.ctx, f.private.owner, r.ID, in)
	if e != nil {
		t.Fatal(e)
	}
	time.Sleep(500 * time.Millisecond)
	current, e := b.store.ReadOwnPlaceDeclarationControls(b.ctx, f.private.owner, f.place)
	if e != nil || len(current.Declarations) != 1 || current.Declarations[0].MemoryID != r.ID || current.Declarations[0].Version != expired.Version || current.Declarations[0].Status != agentmemory.StatusExpired {
		t.Fatal("exact expired ID/version not recoverable", e)
	}
	if active := f.read(t); len(active.Signals) != 0 {
		t.Fatal("expired control promoted to current signal")
	}
	if e = b.store.RevalidateOwnPlaceDeclarationControls(b.ctx, f.private.owner, initial); !errors.Is(e, agentplacememory.ErrForbidden) && !errors.Is(e, agentplacememory.ErrExpired) {
		t.Fatal("old control accepted source revision/expiry", e)
	}
	renew := placeDeclarationInput(f.place, agentplacememory.Visited)
	renew.ExpectedVersion = expired.Version
	if r, e = b.store.PutOwnPlaceDeclaration(b.ctx, f.private.owner, r.ID, renew); e != nil || r.Version != expired.Version+1 {
		t.Fatal("native same ID finite renewal", e)
	}
	b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place)
	hidden, e := b.store.ReadOwnPlaceDeclarationControls(b.ctx, f.private.owner, f.place)
	if e != nil || len(hidden.Declarations) != 1 {
		t.Fatal("hidden target blocks own control", e)
	}
	deleted, e := b.store.DeleteOwnPlaceDeclaration(b.ctx, f.private.owner, r.ID, r.Version)
	if e != nil || deleted.Status != agentmemory.StatusDeleted {
		t.Fatal("hidden owner withdrawal", e)
	}
	final, e := b.store.ReadOwnPlaceDeclarationControls(b.ctx, f.private.owner, f.place)
	if e != nil || len(final.Declarations) != 0 {
		t.Fatal("deleted contents reappeared", e)
	}
}
func TestPlaceMemoryHumanControlNativeSourceIdentityAndABABoundary(t *testing.T) {
	for _, event := range []string{"crossOwner", "organization", "newSession", "metadataRebuild", "agentRestore", "recordUpdated", "unknownPlace", "idleRefresh"} {
		t.Run(event, func(t *testing.T) {
			f := placeMemoryNativeFixture(t)
			b := f.private.base
			r := f.declare(t, agentplacememory.Liked)
			old, e := b.store.ReadOwnPlaceDeclarationControls(b.ctx, f.private.owner, f.place)
			if e != nil {
				t.Fatal(e)
			}
			a := f.private.owner
			want := agentplacememory.ErrForbidden
			switch event {
			case "crossOwner":
				a = f.private.peer
			case "organization":
				a.WorkspacePrincipal = actorref.PrincipalRef{Type: actorref.Organization, ID: b.other.ID}
			case "newSession":
				_, digest, err := identity.NewToken()
				if err != nil {
					t.Fatal(err)
				}
				b.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '2 hours',clock_timestamp()+interval '1 hour')`, b.person.ID, digest[:])
				a.SessionDigest = digest
			case "metadataRebuild":
				b.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, b.personID)
				if _, e = b.store.EnsureAgentProfile(b.ctx, b.personID, b.person); e != nil {
					t.Fatal(e)
				}
			case "agentRestore":
				b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, b.personID)
				b.exec(`UPDATE agents SET status='active' WHERE id=$1`, b.personID)
			case "recordUpdated":
				in := placeDeclarationInput(f.place, agentplacememory.Liked)
				in.ExpectedVersion = r.Version
				in.Visibility = agentmemory.VisibilityAgentOnly
				if _, e = b.store.PutOwnPlaceDeclaration(b.ctx, a, r.ID, in); e != nil {
					t.Fatal(e)
				}
			case "unknownPlace":
				p, e := b.store.ReadOwnPlaceDeclarationControls(b.ctx, a, agentMemoryID(t, f.private))
				if e != nil || len(p.Declarations) != 0 {
					t.Fatal("private empty address invented metadata", e)
				}
				return
			case "idleRefresh":
				if _, e = b.store.Authenticate(b.ctx, a.SessionDigest); e != nil {
					t.Fatal(e)
				}
				want = nil
			}
			e = b.store.RevalidateOwnPlaceDeclarationControls(b.ctx, a, old)
			if !errors.Is(e, want) {
				t.Fatal("native control boundary", event, e, want)
			}
		})
	}
}
func TestPlaceMemoryHumanControlNativeOnlyExactTypedNamespace(t *testing.T) {
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	in := agentMemoryInput("other.place.private")
	in.MemoryType = agentmemory.TypePlace
	in.StructuredValue = []byte(`{"placeId":"` + f.place + `","body":"unrelated private value"}`)
	mustPutAgentMemory(t, f.private, agentMemoryID(t, f.private), in)
	f.declare(t, agentplacememory.Visited)
	p, e := b.store.ReadOwnPlaceDeclarationControls(b.ctx, f.private.owner, f.otherPlace)
	if e != nil || len(p.Declarations) != 0 {
		t.Fatal("unselected target loaded typed contents", e)
	}
	p, e = b.store.ReadOwnPlaceDeclarationControls(b.ctx, f.private.owner, f.place)
	if e != nil || len(p.Declarations) != 1 {
		t.Fatal("generic PLACE or private Memory copied", e)
	}
	raw, _ := json.Marshal(p)
	text := string(raw)
	if strings.Contains(text, "unrelated") {
		t.Fatal("private body in control")
	}
	var wrong agentprofile.PrivateAccess
	if _, e = b.store.ReadOwnPlaceDeclarationControls(context.Background(), wrong, f.place); e == nil {
		t.Fatal("anonymous native control accepted")
	}
}
func TestPlaceMemoryHumanControlNativeStoredExpiredFutureNeverReactivates(t *testing.T) {
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	r := f.declare(t, agentplacememory.Liked)
	b.exec(`UPDATE agent_memories SET status='EXPIRED',version=version+1,updated_at=clock_timestamp() WHERE id=$1`, r.ID)
	if _, e := b.store.ReadOwnPlaceDeclarationControls(b.ctx, f.private.owner, f.place); e == nil {
		t.Fatal("stored expired future record reactivated")
	}
}
func TestPlaceMemoryHumanControlNativeExpectedAgentBeforeAnyWrite(t *testing.T) {
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	old := b.personID
	b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, old)
	b.exec(`DELETE FROM agents WHERE id=$1`, old)
	var replacement string
	if e := b.pool.QueryRow(b.ctx, `INSERT INTO agents(agent_type,principal_account_id,status) VALUES('personal',$1,'active') RETURNING id`, b.person.ID).Scan(&replacement); e != nil {
		t.Fatal(e)
	}
	if _, e := b.store.EnsureAgentProfile(b.ctx, replacement, b.person); e != nil {
		t.Fatal(e)
	}
	id := agentMemoryID(t, f.private)
	input := placeDeclarationInput(f.place, agentplacememory.Liked)
	if r, e := b.store.PutOwnPlaceDeclarationBound(b.ctx, f.private.owner, old, id, input); !errors.Is(e, agentplacememory.ErrForbidden) || r.ID != "" {
		t.Fatal("old preview migrated to replacement Agent", e)
	}
	var count int
	if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memories WHERE id=$1`, id).Scan(&count); e != nil || count != 0 {
		t.Fatal("rejected target wrote original ledger", count, e)
	}
	r, e := b.store.PutOwnPlaceDeclarationBound(b.ctx, f.private.owner, replacement, id, input)
	if e != nil || r.AgentID != replacement {
		t.Fatal("same current target rejected", e)
	}
}
