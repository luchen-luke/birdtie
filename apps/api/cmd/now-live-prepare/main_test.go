package main

import (
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"testing"
)

func TestTrialCredentialUsesOriginalHTTPBearerContract(t *testing.T) {
	token, digest, err := prepareTrialCredential()
	if err != nil {
		t.Fatal("credential preparation failed")
	}
	parsed, err := identity.ParseBearer("Bearer " + token)
	if err != nil || parsed != digest {
		t.Fatal("trial credential does not match original HTTP bearer contract")
	}
	second, other, err := prepareTrialCredential()
	if err != nil || second == token || other == digest {
		t.Fatal("credential must be independently generated")
	}
}
