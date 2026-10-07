package agentsocialpolicy

// ValidateSettingsSpecification is shape-only; preferences grant no consent.
func ValidateSettingsSpecification(spec Specification) error { return validateSpec(spec) }
