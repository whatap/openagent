package model

// OpenMxHelp represents help information for a metric
type OpenMxHelp struct {
	Metric   string
	Property map[string]string
}

// NewOpenMxHelp creates a new OpenMxHelp instance
func NewOpenMxHelp(metric string) *OpenMxHelp {
	return &OpenMxHelp{
		Metric:   metric,
		Property: make(map[string]string),
	}
}

// Put adds a property to the help information
func (omh *OpenMxHelp) Put(key, value string) {
	omh.Property[key] = value
}

// Get retrieves a property from the help information
func (omh *OpenMxHelp) Get(key string) string {
	return omh.Property[key]
}