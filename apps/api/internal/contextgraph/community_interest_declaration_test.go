package contextgraph

import (
	"encoding/json"
	"testing"
)

func TestCommunityInterestBodyClosedAuthority(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	for _, op := range []string{"PRIVATE", "PUBLIC", "DELETE"} {
		raw, _ := json.Marshal(CommunityInterestInput{id, op})
		if _, _, e := DecodeCommunityInterestBody(raw, false); e != nil {
			t.Fatal(e)
		}
	}
	for _, raw := range []string{`{"communityId":"` + id + `","operation":"PUBLIC","confirmed":true}`, `{"communityId":"` + id + `","operation":"PUBLIC","operation":"PUBLIC"}`, `{"communityId":"` + id + `","operation":"MEMBER"}`, `{"communityId":null,"operation":"PUBLIC"}`, `{} {}`, `{"preview":"token","owner":"` + id + `"}`} {
		if _, _, e := DecodeCommunityInterestBody([]byte(raw), false); e == nil {
			t.Fatal("accepted authority", raw)
		}
	}
	for _, raw := range []string{`{"preview":"opaque"}`, `{"preview":"x","preview":"y"}`, `{"preview":"x","confirmed":true}`} {
		_, token, e := DecodeCommunityInterestBody([]byte(raw), true)
		if raw == `{"preview":"opaque"}` {
			if e != nil || token != "opaque" {
				t.Fatal(e)
			}
		} else if e == nil {
			t.Fatal(raw)
		}
	}
}
