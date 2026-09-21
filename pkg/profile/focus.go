package profile

// FocusedMetrics keeps primary and explicitly requested related-project
// denominators separate. ScopeID binds every measurement to one focus plan.
type FocusedMetrics struct {
	ScopeID string                  `json:"scope_id"`
	Primary *MetricsReport          `json:"primary,omitempty"`
	Related []FocusedProjectMetrics `json:"related"`
}

type FocusedProjectMetrics struct {
	Project string         `json:"project"`
	Metrics *MetricsReport `json:"metrics"`
}
