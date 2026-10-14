package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
)

func TestNowRetentionPendingBeforeAnyProviderDatabaseOrBudget(t *testing.T) {
	n := &ownNowLiveAnswers{} // no Store or provider; a gate bypass would panic.
	r, e := n.Execute(context.Background(), [32]byte{1}, agentworkspace.Task{})
	if r != nil || !errors.Is(e, agentworkspace.ErrSearchRetentionPending) {
		t.Fatalf("gate not first: %v %v", r, e)
	}
	n.options.SearchRetentionConfirmed = true
	if r, e = n.Execute(context.Background(), [32]byte{1}, agentworkspace.Task{}); r != nil || errors.Is(e, agentworkspace.ErrSearchRetentionPending) {
		t.Fatal("confirmed synthetic scope still pending")
	}
}
