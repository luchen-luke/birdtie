package main

import (
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"testing"
)

func TestPreferenceInvalidationCommandRegisteredHandler(t *testing.T) {
	args := append(commandArgs(), "--handler=preference-invalidation-v1")
	v, ok := parseSettings(args)
	if !ok || v.handler != agentoutbox.PreferenceHandler || v.subject.ID != testPerson {
		t.Fatal("real command cannot select closed preference consumer")
	}
	if _, ok = parseSettings(append(commandArgs(), "--handler=PreferenceUpdated")); ok {
		t.Fatal("event label is not a trusted maintenance handler")
	}
}
