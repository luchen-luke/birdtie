package agentfeature

import "sync"

// Controller is a process-local rollout brake owned by the server. Revision
// identifies configuration changes only; it must not be used as source/consent
// authority, approval version, event sourceVersion or a durable effect key.
type Controller struct {
	mu       sync.RWMutex
	config   Config
	revision uint64
}

// Ticket is an opaque check of one current switch, not a tool grant. Requiring
// the same controller and revision prevents another instance/late response
// from reusing it. It is deliberately not JSON transferable.
type Ticket struct {
	controller *Controller
	revision   uint64
	feature    Feature
}

func (Ticket) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (v *Ticket) UnmarshalJSON([]byte) error {
	*v = Ticket{}
	return ErrServerOnly
}

func NewController(config Config) (*Controller, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	return &Controller{config: config.clone(), revision: 1}, nil
}

func (c *Controller) Revision() uint64 {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.revision
}

func (c *Controller) Capture(feature Feature) (Ticket, error) {
	if c == nil || !known(feature) {
		return Ticket{}, ErrDisabled
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.config.enabled(feature) {
		return Ticket{}, ErrDisabled
	}
	return Ticket{controller: c, revision: c.revision, feature: feature}, nil
}

func (c *Controller) Current(ticket Ticket) bool {
	if c == nil || ticket.controller != c || !known(ticket.feature) {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ticket.revision == c.revision && c.config.enabled(ticket.feature)
}

// Replace has no HTTP/client caller. Trusted server administration must perform
// CAS against the current configuration revision; invalid updates are atomic.
func (c *Controller) Replace(expectedRevision uint64, config Config) error {
	if c == nil {
		return ErrInvalidConfig
	}
	if err := config.validate(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if expectedRevision == 0 || c.revision != expectedRevision || c.revision == ^uint64(0) {
		return ErrConflict
	}
	c.config = config.clone()
	c.revision++
	return nil
}

// Disable is a one-way brake per feature. Disabling the enrichment parent also
// suppresses every dependent feature without rewriting unrelated flag values.
// It cannot enable anything, produce consent or revoke a completed effect.
func (c *Controller) Disable(feature Feature) error {
	if c == nil || !known(feature) {
		return ErrInvalidConfig
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.config.flags[feature] {
		return nil
	}
	if c.revision == ^uint64(0) {
		return ErrConflict
	}
	c.config.flags[feature] = false
	c.revision++
	return nil
}
