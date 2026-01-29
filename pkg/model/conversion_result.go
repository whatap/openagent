package model

// ConversionResult represents the result of converting Prometheus metrics to OpenMx format
type ConversionResult struct {
	OpenMxList     []*OpenMx
	OpenMxHelpList []*OpenMxHelp
	Target         string
	CollectionTime int64
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

// GetTarget returns the target
func (cr *ConversionResult) GetTarget() string {
	return cr.Target
}

// SetTarget sets the target
func (cr *ConversionResult) SetTarget(target string) {
	cr.Target = target
}

// GetCollectionTime returns the collection time
func (cr *ConversionResult) GetCollectionTime() int64 {
	return cr.CollectionTime
}

// SetCollectionTime sets the collection time
func (cr *ConversionResult) SetCollectionTime(time int64) {
	cr.CollectionTime = time
}
