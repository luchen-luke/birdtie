package agentruntime

import "testing"

func TestCandidateAttributeReusesSAF004(t *testing.T) {
	for _, v := range []string{"badminton", "basketball", "football", "sports", "culture", "hiking"} {
		t.Run(v, func(t *testing.T) {
			if ValidateOrdinaryCandidateAttribute(SocialPreferenceActivityCategory, v) != nil {
				t.Fatal("existing category rejected")
			}
		})
	}
	for _, v := range []string{"health", "medical conditions", "mental health", "religion", "ethnicity", "race", "political ideology", "sexual orientation", "sex life", "trade union membership", "criminal history", "nightlife", "personality", "friendship", "precise_location", "unknown"} {
		t.Run(v, func(t *testing.T) {
			if ValidateOrdinaryCandidateAttribute(SocialPreferenceActivityCategory, v) == nil || ValidateOrdinaryCandidateAttribute(v, "sports") == nil {
				t.Fatal("sensitive/unknown accepted")
			}
		})
	}
}

func TestCandidateHikingDoesNotExpandSocialInferenceReviewOrPublish(t *testing.T) {
	if socialPreferenceCategory("hiking") || ValidateOrdinaryCandidateAttribute(SocialPreferenceActivityCategory, "hiking") != nil {
		t.Fatal("candidate vocabulary and private/public inference are separate")
	}
	for _, provenance := range []string{SocialPreferenceExplicit, SocialPreferenceInferred} {
		for _, public := range []bool{false, true} {
			now, req, facts := socialInferenceFixture(provenance)
			facts.Claim.Value = "hiking"
			for i := range facts.Claim.Sources {
				facts.Claim.Sources[i].Category = "hiking"
			}
			if public {
				req.Scope = Public
				req.Action = SocialActionPublish
				req.Purpose = SocialPreferencePublishPurpose
			}
			if DecideSocialInference(now, req, facts).Allowed {
				t.Fatal("candidate-only vocabulary granted old inference permission", provenance, public)
			}
		}
	}
}
