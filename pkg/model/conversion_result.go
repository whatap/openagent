package model

// ConversionResult represents the result of converting Prometheus metrics to OpenMx format
type ConversionResult struct {
	OpenMxList     []*OpenMx
	OpenMxHelpList []*OpenMxHelp
}

// NewConversionResult creates a new ConversionResult instance
func NewConversionResult(openMxList []*OpenMx, openMxHelpList []*OpenMxHelp) *ConversionResult {
	return &ConversionResult{
		OpenMxList:     openMxList,
		OpenMxHelpList: openMxHelpList,
	}
}

// GetOpenMxList returns the list of OpenMx instances
func (cr *ConversionResult) GetOpenMxList() []*OpenMx {
	return cr.OpenMxList
}

// GetOpenMxHelpList returns the list of OpenMxHelp instances
func (cr *ConversionResult) GetOpenMxHelpList() []*OpenMxHelp {
	return cr.OpenMxHelpList
}