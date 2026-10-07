package agentrun

import (
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
)

// Bounds apply to the existing native Moment worker. They do not enable a new
// event consumer, model, source access or effect writer.
const MaxConcurrentRuns = 4
const MaxConcurrentRunsPerOwner = 1
const MaxDispatchTenantHeads = 256

var ErrDispatchBusy = errors.New("当前运行额度已占用，请在下一轮核实")

// DispatchTenant contains server-loaded queue control metadata only. Native
// source/session/approval validation still occurs at the original writer.
type DispatchTenant struct {
	Owner      string
	Active     int
	LastServed *time.Time
	NextDue    time.Time
}

// PickDispatchTenant uses durable last-claim time, rather than queue size, so a
// tenant's burst cannot take every dispatch while another tenant is ready.
func PickDispatchTenant(now time.Time, active int, heads []DispatchTenant) (string, error) {
	if !validTime(now) || active < 0 || len(heads) > MaxDispatchTenantHeads {
		return "", ErrUnavailable
	}
	if active >= MaxConcurrentRuns {
		return "", ErrDispatchBusy
	}
	seen := make(map[string]bool, len(heads))
	var best *DispatchTenant
	for i := range heads {
		h := &heads[i]
		if !agentenrichmentpurpose.ValidID(h.Owner) || seen[h.Owner] || h.Active < 0 || h.Active > active || !validTime(h.NextDue) || h.NextDue.After(now) || h.LastServed != nil && (!validTime(*h.LastServed) || h.LastServed.After(now)) {
			return "", ErrUnavailable
		}
		seen[h.Owner] = true
		if h.Active >= MaxConcurrentRunsPerOwner {
			continue
		}
		if best == nil || dispatchTenantBefore(*h, *best) {
			best = h
		}
	}
	if best != nil {
		return best.Owner, nil
	}
	if len(heads) != 0 {
		return "", ErrDispatchBusy
	}
	return "", ErrNotFound
}

func dispatchTenantBefore(a, b DispatchTenant) bool {
	if (a.LastServed == nil) != (b.LastServed == nil) {
		return a.LastServed == nil
	}
	if a.LastServed != nil && !a.LastServed.Equal(*b.LastServed) {
		return a.LastServed.Before(*b.LastServed)
	}
	if !a.NextDue.Equal(b.NextDue) {
		return a.NextDue.Before(b.NextDue)
	}
	return a.Owner < b.Owner
}
