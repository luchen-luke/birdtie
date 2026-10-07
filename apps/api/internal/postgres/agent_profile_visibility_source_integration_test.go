package postgres

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func TestAgentProfileVisibilityOrdinarySourceEditIntegration(t *testing.T) {
	f := agentVisibilityTestFixture(t)
	b := f.private.base
	rules := agentprofile.DefaultFieldRules()
	rules[agentprofile.FieldDisplayName] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPrivate}
	rules[agentprofile.FieldBio] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityAgentOnly}
	policy := replaceVisibilityRules(t, f, rules)
	var wait sync.WaitGroup
	errorsFound := make(chan error, 2)
	wait.Add(2)
	go func() {
		defer wait.Done()
		for i := 0; i < 8; i++ {
			_, err := b.store.UpdateOwnProfile(b.ctx, b.person.ID, identity.ProfileInput{
				DisplayName: fmt.Sprintf("合成资料改动%d", i), Bio: "仍受原逐字段限制", Visibility: "public",
			})
			if err != nil {
				errorsFound <- errors.New("ordinary source edit failed or deadlocked")
				return
			}
		}
	}()
	go func() {
		defer wait.Done()
		for i := 0; i < 8; i++ {
			own, err := b.store.ReadAgentProfileFields(b.ctx, f.private.owner.SessionDigest, b.person.ID)
			if err != nil || agentprofile.ValidateProjectedRecord(own) != nil {
				errorsFound <- errors.New("self source projection failed or deadlocked")
				return
			}
			other, err := b.store.ReadAgentProfileFields(b.ctx, f.private.peer.SessionDigest, b.person.ID)
			if !errors.Is(err, agentprofile.ErrNotFound) || len(other.Fields) != 0 {
				errorsFound <- errors.New("source edit disclosed restricted content")
				return
			}
		}
	}()
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
	after, err := b.store.ReadOwnAgentProfileVisibility(b.ctx, f.private.owner)
	if err != nil || after.Profile.ProfileVersion != policy.Profile.ProfileVersion ||
		after.Rules[agentprofile.FieldDisplayName].Visibility != agentprofile.VisibilityPrivate ||
		after.Rules[agentprofile.FieldBio].Visibility != agentprofile.VisibilityAgentOnly {
		t.Fatal("ordinary source edit changed separate policy/version")
	}
	// This metadata revision deliberately does not impersonate UserProfile's
	// updated_at/source revision or a concrete future approval revision.
	profile, err := b.store.ReadProfile(b.ctx, b.other.ID, b.person.ID)
	if err != nil || profile.DisplayName != "" || profile.Bio != "" {
		t.Fatal("ordinary reader bypassed current field restriction")
	}
}
