package devauth

import "testing"

func TestNormalizePhone(t *testing.T) {
	for _, sample := range []string{"13800138000", "8613800138000", "+8613800138000"} {
		phone, ok := NormalizePhone(sample)
		if !ok || phone != "+8613800138000" {
			t.Fatalf("NormalizePhone(%q) = %q, %v", sample, phone, ok)
		}
	}
	for _, sample := range []string{"", "123", "+012345678", "1380013abcd", "++8613800138000"} {
		if _, ok := NormalizePhone(sample); ok {
			t.Fatalf("invalid phone accepted: %q", sample)
		}
	}
}

func TestPhoneDigestIsStableAndDomainSeparated(t *testing.T) {
	first := PhoneDigest("+8613800138000")
	if first != PhoneDigest("+8613800138000") || first == PhoneDigest("+8613800138001") {
		t.Fatal("development phone digest is not stable and distinct")
	}
}
