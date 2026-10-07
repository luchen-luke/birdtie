package agentruntime

import (
	"errors"
)

// ContextSnapshot carries process-local native evidence, not permission. The
// native store must recognize its own unexported payload. It cannot be restored
// from wire JSON, reused by another store, or substituted for a purpose grant.
type ContextSnapshot struct{ native any }

func NewContextSnapshot(native any) ContextSnapshot { return ContextSnapshot{native: native} }
func (s ContextSnapshot) NativeValue() any          { return s.native }
func (ContextSnapshot) MarshalJSON() ([]byte, error) {
	return nil, errors.New("native context snapshot is not wire data")
}
func (*ContextSnapshot) UnmarshalJSON([]byte) error {
	return errors.New("native context snapshot cannot be restored")
}
