package agentautonomy

import "time"

// ValidateSettingsSpecification preserves the original finite-time and hard
// autonomous-action brake. This is not a source or action authorization.
func ValidateSettingsSpecification(spec Specification, now time.Time) error {
	return validateSpecification(spec, now)
}
