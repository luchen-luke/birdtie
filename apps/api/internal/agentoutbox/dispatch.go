package agentoutbox

import (
	"errors"

	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
)

// These numerical bounds reuse the native run policy, but count only the
// independent metadata-control leases. They grant no run or source authority.
const MaxConcurrentControls = ar.MaxConcurrentRuns
const MaxConcurrentControlsPerOwner = ar.MaxConcurrentRunsPerOwner
const MaxControlTenantHeads = ar.MaxDispatchTenantHeads

var ErrDispatchBusy = errors.New("当前事件维护额度已占用，请在下一轮核实")
