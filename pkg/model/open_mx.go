package model

import (
	"time"
)

// Label represents a key-value pair label for a metric
type Label struct {
	Key   string
	Value string
}

// OpenMx represents a single metric
type OpenMx struct {
	Metric    string
	Timestamp int64
	Value     float64
	Labels    []Label
}

// NewOpenMx creates a new OpenMx instance
func NewOpenMx(metric string, timestamp int64, value float64) *OpenMx {
	return &OpenMx{
		Metric:    metric,
		Timestamp: timestamp,
		Value:     value,
		Labels:    make([]Label, 0),
	}
}

// AddLabel adds a label to the metric
func (om *OpenMx) AddLabel(key, value string) {
	om.Labels = append(om.Labels, Label{Key: key, Value: value})
}

// NewOpenMxWithCurrentTime creates a new OpenMx instance with the current time
func NewOpenMxWithCurrentTime(metric string, value float64) *OpenMx {
	return NewOpenMx(metric, time.Now().UnixMilli(), value)
}