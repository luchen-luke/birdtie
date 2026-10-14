package identity

import "testing"

func TestHumanProfileVisibilityMatrix(t *testing.T) {
	for _, visibility := range []string{"public", "followers", "connections", "close", "private"} {
		t.Run(visibility, func(t *testing.T) {
			input := ProfileInput{DisplayName: " 阿布丁学生 ", Bio: "中英文 mixed", Visibility: visibility}
			got, err := NormalizeHumanProfileInput(input)
			if err != nil || got.Visibility != visibility || got.DisplayName != "阿布丁学生" {
				t.Fatalf("valid explicit profile audience rejected: %q %v", visibility, err)
			}
		})
	}
	for _, visibility := range []string{"", "PUBLIC", "FRIENDS", "friends", "close ", "community", "unknown"} {
		t.Run("reject_"+visibility, func(t *testing.T) {
			if _, err := NormalizeHumanProfileInput(ProfileInput{DisplayName: "阿布丁学生", Visibility: visibility}); err != ErrInvalidProfile {
				t.Fatalf("invalid audience accepted: %q %v", visibility, err)
			}
		})
	}
}
