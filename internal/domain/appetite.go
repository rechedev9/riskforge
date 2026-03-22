package domain

// AppetiteRule represents a carrier's appetite for a specific risk profile.
// Maps to the Spanner AppetiteRules table.
type AppetiteRule struct {
	RuleID              string
	CarrierID           string
	State               string // 2-char state code (e.g., "CA", "TX")
	LineOfBusiness      string // e.g., "auto", "homeowners", "commercial_property"
	ClassCode           string // optional industry/risk class code
	MinPremium          float64
	MaxPremium          float64
	IsActive            bool
	EligibilityCriteria map[string]any // parsed from JSON
}

// RiskClassification is the input for appetite matching.
// Provided by the caller to filter carriers before fan-out.
type RiskClassification struct {
	State            string  // required, 2-char
	LineOfBusiness   string  // required
	ClassCode        string  // optional
	EstimatedPremium float64 // optional, 0 means no premium filter
}
