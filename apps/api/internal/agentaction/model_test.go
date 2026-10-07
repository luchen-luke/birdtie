package agentaction

import (
	"context"
	"errors"
	"testing"
)

func TestActionServiceUnavailableDoesNotCreateAuthority(t *testing.T) {
	if _, e := NewService(nil).Reconcile(context.Background(), zeroAccess(), "x", nil); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
}
