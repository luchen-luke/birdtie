package httpapi

import (
	"errors"
	"testing"

	aca "github.com/birdtie/birdtie/apps/api/internal/agentcontextadapter"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
)

func TestContextAdapterHTTPPureHelperCannotUpgradeHumanOrEmptyBundle(t *testing.T) {
	for _, mode := range []acb.Mode{acb.HumanSelfReview, acb.RulesPublicQuery, acb.MachineTaskContext, ""} {
		v, e := consumeBudgetedTaskContext(acb.Bundle{Mode: mode}, aca.DefaultBudget())
		if !errors.Is(e, aca.ErrInvalid) || v.AdapterVersion != "" {
			t.Fatal("invalid was admitted", mode, e)
		}
	}
}
