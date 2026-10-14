package organization

import (
	"strings"
	"testing"
)

const controlTestID = "11111111-1111-4111-8111-111111111111"

func TestOrganizationHumanControlNormalizeAndExactDigest(t *testing.T) {
	base := ControlInput{Action: "INVITE", OperationID: controlTestID, ExpectedAuthorityVersion: 1, UserAccountID: controlTestID, Role: "member"}
	if _, e := NormalizeControl(base); e != nil {
		t.Fatal(e)
	}
	cases := map[string]func(*ControlInput){"unknown": func(i *ControlInput) { i.Action = "DELETE" }, "unsafeVersion": func(i *ControlInput) { i.ExpectedAuthorityVersion = MaxControlVersion + 1 }, "noVersion": func(i *ControlInput) { i.ExpectedAuthorityVersion = 0 }, "nonPersonID": func(i *ControlInput) { i.UserAccountID = "opaque" }, "crossAction": func(i *ControlInput) { i.CityID = "aberdeen-gb" }, "roleOwnerInvite": func(i *ControlInput) { i.Role = "owner" }}
	for n, f := range cases {
		t.Run(n, func(t *testing.T) {
			i := base
			f(&i)
			if _, e := NormalizeControl(i); e != ErrControlInvalid {
				t.Fatal("invalid control accepted", e)
			}
		})
	}
	original := ControlDigest(controlTestID, base)
	for _, f := range []func(*ControlInput){func(i *ControlInput) { i.ExpectedMemberVersion = 1 }, func(i *ControlInput) { i.Role = "admin" }, func(i *ControlInput) { i.OperationID = "22222222-2222-4222-8222-222222222222" }} {
		i := base
		f(&i)
		if ControlDigest(controlTestID, i) == original {
			t.Fatal("changed input revived original approval")
		}
	}
	if len(original) != 64 || strings.ToLower(original) != original {
		t.Fatal("digest shape")
	}
	if NormalizeMemberDisplayName("  合成姓名  ") != "合成姓名" || NormalizeMemberDisplayName(strings.Repeat("a", 81)) != "" || NormalizeMemberDisplayName("A") != "" {
		t.Fatal("unsafe optional name blocked human roster instead of honest fallback")
	}
}
