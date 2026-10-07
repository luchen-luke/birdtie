package postgres

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

// These are calls to the incumbent native owner domain, not a new Store or
// purpose resolver. All identities and contents are disposable synthetic data.
func TestSocialPreferenceSeedNativeEightFieldsAndCAS(t *testing.T) {
	f := agentPrivateTestFixture(t)
	b := f.base
	saved := savePrivateCanaries(t, f)
	expected := saved.Fields
	fields := saved.Fields
	fields.SocialPreferences = []string{"小群体", "大型活动", "一对一", "同一大学", "同一城市", "共同兴趣", "国际社群"}
	var beforeOther []byte
	if e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('profile',(SELECT to_jsonb(p) FROM user_profiles p WHERE account_id=$1),'intent',(SELECT to_jsonb(i) FROM agent_seed_user_intents i WHERE owner_id=$1),'memory',(SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY m.id),'[]') FROM agent_memories m WHERE owner_id=$1),'policy',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.family),'[]') FROM agent_policy_settings p WHERE owner_id=$1))`, f.owner.WorkspacePrincipal.ID).Scan(&beforeOther); e != nil {
		t.Fatal(e)
	}
	current, e := b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.owner, agentprofile.ReplacePrivateInput{ExpectedVersion: saved.Profile.ProfileVersion, Fields: fields})
	if e != nil || current.Profile.ProfileVersion != saved.Profile.ProfileVersion+1 || !reflect.DeepEqual(current.Fields.SocialPreferences, fields.SocialPreferences) {
		t.Fatal("native preference save", e)
	}
	actual := current.Fields
	actual.SocialPreferences = expected.SocialPreferences
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("other eight private fields changed")
	}
	read, e := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
	if e != nil || !reflect.DeepEqual(read.Fields, current.Fields) {
		t.Fatal("durable native preference", e)
	}
	if _, e = b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.owner, agentprofile.ReplacePrivateInput{ExpectedVersion: saved.Profile.ProfileVersion, Fields: fields}); !errors.Is(e, agentprofile.ErrConflict) {
		t.Fatal("old review", e)
	}
	var afterOther []byte
	if e = b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('profile',(SELECT to_jsonb(p) FROM user_profiles p WHERE account_id=$1),'intent',(SELECT to_jsonb(i) FROM agent_seed_user_intents i WHERE owner_id=$1),'memory',(SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY m.id),'[]') FROM agent_memories m WHERE owner_id=$1),'policy',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.family),'[]') FROM agent_policy_settings p WHERE owner_id=$1))`, f.owner.WorkspacePrincipal.ID).Scan(&afterOther); e != nil || string(afterOther) != string(beforeOther) {
		t.Fatal("unrelated native rows changed", e)
	}
	t.Log("Native private descriptions only: Same university is not verified education or matching permission")
}
func TestSocialPreferenceSeedNativeConcurrentVersionAndExplicitClear(t *testing.T) {
	f := agentPrivateTestFixture(t)
	b := f.base
	saved := savePrivateCanaries(t, f)
	start := make(chan struct{})
	done := make(chan error, 2)
	var wg sync.WaitGroup
	for _, choice := range []string{"小群体", "大型活动"} {
		wg.Add(1)
		go func(choice string) {
			defer wg.Done()
			<-start
			fields := saved.Fields
			fields.SocialPreferences = []string{choice}
			_, e := b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.owner, agentprofile.ReplacePrivateInput{ExpectedVersion: saved.Profile.ProfileVersion, Fields: fields})
			done <- e
		}(choice)
	}
	close(start)
	wg.Wait()
	close(done)
	success, conflicts := 0, 0
	for e := range done {
		if e == nil {
			success++
		} else if errors.Is(e, agentprofile.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal(success, conflicts)
	}
	read, e := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
	if e != nil {
		t.Fatal(e)
	}
	expected := read.Fields
	fields := read.Fields
	fields.SocialPreferences = []string{}
	cleared, e := b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.owner, agentprofile.ReplacePrivateInput{ExpectedVersion: read.Profile.ProfileVersion, Fields: fields})
	if e != nil || len(cleared.Fields.SocialPreferences) != 0 {
		t.Fatal(e)
	}
	actual := cleared.Fields
	actual.SocialPreferences = expected.SocialPreferences
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("explicit clear removed other private contents")
	}
}
