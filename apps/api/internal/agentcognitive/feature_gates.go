package agentcognitive

import (
	"context"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// FeatureGatedDomains only adds server rollout brakes to existing self-review
// reads. It is not an AgentProfile/Memory/model reader, resolver or executor.
// Permission/source guards remain owned by the wrapped native domains.
type FeatureGatedDomains struct {
	controller *agentfeature.Controller
	current    *CurrentDomainAdapter
	ports      UnavailableCognitivePorts
}

var _ OwnProfileReader = (*FeatureGatedDomains)(nil)
var _ OwnContextReader = (*FeatureGatedDomains)(nil)
var _ MemoryReader = (*FeatureGatedDomains)(nil)
var _ CandidateSubmitter = (*FeatureGatedDomains)(nil)

func (FeatureGatedDomains) MarshalJSON() ([]byte, error) { return nil, ErrAuthorityJSON }
func (v *FeatureGatedDomains) UnmarshalJSON([]byte) error {
	*v = FeatureGatedDomains{}
	return ErrAuthorityJSON
}

func NewFeatureGatedDomains(controller *agentfeature.Controller, store CurrentDomainStore) (*FeatureGatedDomains, error) {
	if controller == nil {
		return nil, ErrUnavailable
	}
	current, err := NewCurrentDomainAdapter(store)
	if err != nil {
		return nil, err
	}
	return &FeatureGatedDomains{controller: controller, current: current}, nil
}

func (g *FeatureGatedDomains) begin(ctx context.Context, feature agentfeature.Feature) (agentfeature.Ticket, error) {
	if ctx == nil {
		return agentfeature.Ticket{}, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return agentfeature.Ticket{}, err
	}
	if g == nil || g.controller == nil || g.current == nil {
		return agentfeature.Ticket{}, ErrUnavailable
	}
	ticket, err := g.controller.Capture(feature)
	if err != nil {
		return agentfeature.Ticket{}, ErrUnavailable
	}
	return ticket, nil
}

func (g *FeatureGatedDomains) ReadOwnProfile(ctx context.Context, request SessionAccess) (identity.Profile, error) {
	ticket, err := g.begin(ctx, agentfeature.Enrichment)
	if err != nil {
		return identity.Profile{}, err
	}
	profile, err := g.current.ReadOwnProfile(ctx, request)
	if cancelErr := ctx.Err(); cancelErr != nil {
		return identity.Profile{}, cancelErr
	}
	if !g.controller.Current(ticket) {
		return identity.Profile{}, ErrUnavailable
	}
	if err != nil {
		return identity.Profile{}, err
	}
	return profile, nil
}

func (g *FeatureGatedDomains) ReadOwnContextDeclarations(ctx context.Context, request SessionAccess) ([]contextgraph.Declaration, error) {
	ticket, err := g.begin(ctx, agentfeature.Enrichment)
	if err != nil {
		return nil, err
	}
	declarations, err := g.current.ReadOwnContextDeclarations(ctx, request)
	if cancelErr := ctx.Err(); cancelErr != nil {
		return nil, cancelErr
	}
	if !g.controller.Current(ticket) {
		return nil, ErrUnavailable
	}
	if err != nil {
		return nil, err
	}
	return declarations, nil
}

func (g *FeatureGatedDomains) ReadMemory(ctx context.Context, request ReadRequest) (KnowledgeView, error) {
	ticket, err := g.begin(ctx, agentfeature.Memory)
	if err != nil {
		return KnowledgeView{}, err
	}
	view, err := g.ports.ReadMemory(ctx, request)
	if cancelErr := ctx.Err(); cancelErr != nil {
		return KnowledgeView{}, cancelErr
	}
	if !g.controller.Current(ticket) {
		return KnowledgeView{}, ErrUnavailable
	}
	if err != nil {
		return KnowledgeView{}, err
	}
	return view, nil
}

func (g *FeatureGatedDomains) SubmitMemoryCandidate(ctx context.Context, request CandidateSubmission) (CandidateReceipt, error) {
	ticket, err := g.begin(ctx, agentfeature.Memory)
	if err != nil {
		return CandidateReceipt{Unavailable, "memory_candidate_port_unavailable"}, err
	}
	receipt, err := g.ports.SubmitMemoryCandidate(ctx, request)
	if cancelErr := ctx.Err(); cancelErr != nil {
		return CandidateReceipt{Unavailable, "memory_candidate_port_unavailable"}, cancelErr
	}
	if !g.controller.Current(ticket) {
		return CandidateReceipt{Unavailable, "memory_candidate_port_unavailable"}, ErrUnavailable
	}
	if err != nil {
		return receipt, err
	}
	return receipt, nil
}

type featureBoundaryKey struct{}

// FeatureBoundary binds the actual server adapter into every request context.
// No header/body chooses flags, creates authority facts or selects a store.
// Existing direct human APIs remain unchanged; this installs no cognitive HTTP
// route. Native middleware/handlers must opt into these additional brakes.
func FeatureBoundary(next http.Handler, domains *FeatureGatedDomains) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if next == nil || domains == nil || domains.controller == nil || domains.current == nil {
			http.Error(w, "Agent 功能边界不可用", http.StatusServiceUnavailable)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), featureBoundaryKey{}, domains)))
	})
}

func FeatureDomainsFromContext(ctx context.Context) (*FeatureGatedDomains, bool) {
	if ctx == nil {
		return nil, false
	}
	domains, ok := ctx.Value(featureBoundaryKey{}).(*FeatureGatedDomains)
	return domains, ok && domains != nil && domains.controller != nil && domains.current != nil
}
