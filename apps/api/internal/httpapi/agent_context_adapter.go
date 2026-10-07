package httpapi

import (
	aca "github.com/birdtie/birdtie/apps/api/internal/agentcontextadapter"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
)

// No new HTTP ingress or authority fallback: the sole purpose Runtime calls
// this after native Build/relevance and revalidates its full original seal last.
func consumeBudgetedTaskContext(b acb.Bundle, budget aca.Budget) (aca.View, error) {
	return aca.Project(b, budget)
}

func consumeRelatedBudgetedTaskContext(b acb.Bundle, budget aca.Budget, excluded map[string]int) (aca.View, error) {
	return aca.ProjectRelated(b, budget, excluded)
}
