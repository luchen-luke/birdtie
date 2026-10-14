package main

import (
	"context"
	"errors"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"testing"
)

func TestAgentActivityJoinStartupDefaultOffAndIndependentSelection(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		present     bool
	}{
		{name: "unset"}, {name: "empty", present: true},
		{name: "explicit-off", value: "false", present: true},
		{name: "selected-without-native-authority", value: "true", present: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keys := []string{}
			g, e := loadAgentActivityJoinStartup(func(key string) (string, bool) { keys = append(keys, key); return tc.value, tc.present }, nil, nil)
			if e != nil || g == nil {
				t.Fatal("startup unavailable", e)
			}
			if len(keys) != 1 || keys[0] != agentActivityJoinEnabledKey {
				t.Fatal("unexpected configuration lookup")
			}
			_, e = g.PreviewOwn(context.Background(), agentprofile.PrivateAccess{}, aa.ActivityJoinInput{})
			if !errors.Is(e, aa.ErrUnavailable) {
				t.Fatal("selection granted action authority", e)
			}
		})
	}
}

func TestAgentActivityJoinStartupRejectsMalformedConfiguration(t *testing.T) {
	for _, value := range []string{"1", "0", "TRUE", " false", "true ", "{\"confirmed\":true}", "Bearer private-synthetic"} {
		g, e := loadAgentActivityJoinStartup(func(string) (string, bool) { return value, true }, nil, nil)
		if e == nil || g != nil {
			t.Fatal("noncanonical configuration accepted")
		}
		if e.Error() != "invalid Agent Activity JOIN configuration" {
			t.Fatal("configuration detail leaked")
		}
	}
	if g, e := loadAgentActivityJoinStartup(nil, nil, nil); e == nil || g != nil {
		t.Fatal("nil configuration lookup accepted")
	}
}
