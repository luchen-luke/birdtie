package devauth

import "testing"

func TestFixedEmailCanonicalIdentityIsSeparateFromPhone(t *testing.T) {
	a, ok := NormalizeEmail("  Owner+Test@Example.TEST ")
	if !ok || a != "owner+test@example.test" {
		t.Fatal("exact canonical email rejected")
	}
	if EmailDigest(a) != EmailDigest("owner+test@example.test") || EmailDigest(a) == PhoneDigest(a) {
		t.Fatal("identity realms mixed")
	}
	for _, v := range []string{"13800138000", "Owner <owner@example.test>", "a@localhost", "a b@example.test", "a@example.test\nInjected: x", "别名@example.test"} {
		if _, ok := NormalizeEmail(v); ok {
			t.Fatal("noncanonical email accepted")
		}
	}
	if Code != "123456" {
		t.Fatal("fixed test code changed")
	}
}
