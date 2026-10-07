package agentattention

// ValidateSettingsSpecification reuses the original closed model validator.
// It validates a configuration shape, never identity, source or processing.
func ValidateSettingsSpecification(spec Specification) error { return validateSpec(spec) }
