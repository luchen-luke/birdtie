package postgres

import (
	"errors"
	"reflect"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentseed"
)

// These are current human owner edits through the original atomic Seed action.
// They do not implement Agent inference, calibrated confidence or a purpose grant.
func TestProfileCompletionNativeSelectedGroupAndPrivateCanaries(t *testing.T) {
	f, actor := seedNative(t)
	b := f.base
	savePrivateCanaries(t, f)
	source, e := b.store.ReadOwnAgentSeed(b.ctx, f.owner.SessionDigest, actor)
	if e != nil {
		t.Fatal(e)
	}
	source, e = b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, actor, seedDraft(t, source))
	if e != nil {
		t.Fatal(e)
	}
	for _, group := range []string{"name", "language", "interests"} {
		t.Run(group, func(t *testing.T) {
			before, err := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
			if err != nil {
				t.Fatal(err)
			}
			input := agentseed.Input{ExpectedSnapshot: source.Snapshot, Action: "SAVE", DisplayName: source.DisplayName,
				CurrentCityID: source.CurrentCity.ID, CurrentCitySnapshot: source.CurrentCity.Snapshot,
				LanguagePreferences: source.LanguagePreferences, BasicIntent: source.Intent.BasicIntent,
				InterestChoice: "SKIP", Interests: []string{}}
			switch group {
			case "name":
				input.DisplayName = "渐进修改昵称"
			case "language":
				input.LanguagePreferences = []string{"zh-CN", "en"}
			case "interests":
				input.InterestChoice = "SET"
				input.Interests = []string{"羽毛球", "摄影"}
			}
			next, err := b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, actor, input)
			if err != nil {
				t.Fatal(err)
			}
			after, err := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
			if err != nil {
				t.Fatal(err)
			}
			expected := before.Fields
			if group == "language" {
				expected.LanguagePreferences = input.LanguagePreferences
			}
			if group == "interests" {
				expected.PersonalPreferences = input.Interests
			}
			if !reflect.DeepEqual(expected, after.Fields) {
				t.Fatal("current other private fields not preserved")
			}
			if next.DisplayName != input.DisplayName || next.Intent.BasicIntent != source.Intent.BasicIntent || next.CurrentCity.ID != source.CurrentCity.ID {
				t.Fatal("other groups changed", next)
			}
			var visibility string
			if err := b.pool.QueryRow(b.ctx, `SELECT pc.visibility FROM person_contexts pc JOIN contexts c ON c.id=pc.context_id WHERE pc.person_account_id=$1 AND pc.relation='current' AND c.context_type='CITY'`, actor.ID).Scan(&visibility); err != nil || visibility != "private" {
				t.Fatal("original private City consequence", visibility, err)
			}
			effects := seedEffects(t, f)
			if _, err = b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, actor, input); !errors.Is(err, agentseed.ErrConflict) {
				t.Fatal("stale reviewed source", err)
			}
			if seedEffects(t, f) != effects {
				t.Fatal("stale retry effects")
			}
			source, err = b.store.ReadOwnAgentSeed(b.ctx, f.owner.SessionDigest, actor)
			if err != nil || source.Snapshot != next.Snapshot {
				t.Fatal("durable current result", err)
			}
		})
	}
	t.Log("LOCAL_SYNTHETIC_ONLY current human groups; no automatic Agent enrichment permission")
}

func TestProfileCompletionNativeMissingAndChangedSourcesDoNotGuess(t *testing.T) {
	f, actor := seedNative(t)
	b := f.base
	source, e := b.store.ReadOwnAgentSeed(b.ctx, f.owner.SessionDigest, actor)
	if e != nil {
		t.Fatal(e)
	}
	effects := seedEffects(t, f)
	partial := agentseed.Input{ExpectedSnapshot: source.Snapshot, Action: "SAVE", DisplayName: source.DisplayName, InterestChoice: "SET", Interests: []string{"摄影"}}
	if _, e = b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, actor, partial); !errors.Is(e, agentseed.ErrInvalid) {
		t.Fatal("no invented City/language/intent", e)
	}
	if seedEffects(t, f) != effects {
		t.Fatal("partial draft mutated sources")
	}
	source, e = b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, actor, seedDraft(t, source))
	if e != nil {
		t.Fatal(e)
	}
	private, e := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
	if e != nil {
		t.Fatal(e)
	}
	private.Fields.SocialPreferences = []string{"其他窗口更新"}
	if _, e = b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.owner, agentprofile.ReplacePrivateInput{ExpectedVersion: private.Profile.ProfileVersion, Fields: private.Fields}); e != nil {
		t.Fatal(e)
	}
	effects = seedEffects(t, f)
	if _, e = b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, actor, seedDraft(t, source)); !errors.Is(e, agentseed.ErrConflict) {
		t.Fatal("metadata source change", e)
	}
	if seedEffects(t, f) != effects {
		t.Fatal("source conflict wrote stale choices")
	}
	fresh, e := b.store.ReadOwnAgentSeed(b.ctx, f.owner.SessionDigest, actor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, actor, seedDraft(t, fresh)); e != nil {
		t.Fatal(e)
	}
	retained, e := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
	if e != nil || !reflect.DeepEqual(retained.Fields.SocialPreferences, private.Fields.SocialPreferences) {
		t.Fatal("new current social field lost", e)
	}
}
