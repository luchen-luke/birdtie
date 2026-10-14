package postgres

import (
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func TestPeopleSearchNativePublicLiteralAndCurrentSession(t *testing.T) {
	ownedMigrationDatabase(t)
	f := agentPrivateTestFixture(t)
	b := f.base
	a := identity.ProfileAccess{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.owner.SessionDigest}
	marker := agentMemoryID(t, f)
	publicID, privateID, wildcardID := b.other.ID, agentMemoryID(t, f), agentMemoryID(t, f)
	for _, id := range []string{privateID, wildcardID} {
		b.exec(`INSERT INTO accounts(id,account_type,status) VALUES($1,'person','active')`, id)
		b.accounts = append(b.accounts, id)
	}
	b.exec(`UPDATE user_profiles SET display_name=$2,visibility='public' WHERE account_id=$1`, publicID, marker+"公开联系人")
	b.exec(`INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,$2,'private'),($3,$4,'public')`, privateID, marker+"私密联系人", wildcardID, marker+"100%_测试")
	t.Run("public-name-and-exact-id", func(t *testing.T) {
		ids, e := b.store.SearchPublicPersonIDs(b.ctx, a, marker)
		if e != nil || len(ids) != 2 {
			t.Fatal("public search", len(ids), e)
		}
		for _, id := range ids {
			if id == privateID || id == b.person.ID {
				t.Fatal("private or own account exposed")
			}
		}
		ids, e = b.store.SearchPublicPersonIDs(b.ctx, a, publicID)
		if e != nil || len(ids) != 1 || ids[0] != publicID {
			t.Fatal("exact public id", e)
		}
		ids, e = b.store.SearchPublicPersonIDs(b.ctx, a, privateID)
		if e != nil || len(ids) != 0 {
			t.Fatal("private id exposed", e)
		}
	})
	t.Run("wildcards-are-literal-name-text", func(t *testing.T) {
		for _, q := range []string{"%", "_", "%_"} {
			ids, e := b.store.SearchPublicPersonIDs(b.ctx, a, q)
			if e != nil || len(ids) != 1 || ids[0] != wildcardID {
				t.Fatal("wildcard directory expansion", q, len(ids), e)
			}
		}
	})
	t.Run("both-block-directions-hidden", func(t *testing.T) {
		for _, reverse := range []bool{false, true} {
			owner, target := b.person.ID, publicID
			if reverse {
				owner, target = target, owner
			}
			b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, owner, target)
			ids, e := b.store.SearchPublicPersonIDs(b.ctx, a, publicID)
			if e != nil || len(ids) != 0 {
				t.Fatal("blocked person exposed", reverse, e)
			}
			b.exec(`DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, owner, target)
		}
	})
	t.Run("revoked-session-has-no-private-directory-results", func(t *testing.T) {
		b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, f.owner.SessionDigest[:])
		ids, e := b.store.SearchPublicPersonIDs(b.ctx, a, marker)
		if e != nil || len(ids) != 0 {
			t.Fatal("revoked search exposed directory", len(ids), e)
		}
	})
}
