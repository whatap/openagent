// Package dcgm derives GPU metrics that WhaTap screens query but the official
// NVIDIA dcgm-exporter does not publish.
package dcgm

import (
	"math"
	"strconv"
	"strings"

	"open-agent/pkg/model"
)

// WeightedGPUUtilMetric is the utilization ratio (0..1) of one physical GPU,
// including GPUs partitioned with MIG. The WhaTap dcgm-exporter build publishes
// it directly; for other exporters OpenAgent derives it from the same inputs
// with the same labels.
const WeightedGPUUtilMetric = "DCGM_FI_DEV_WEIGHTED_GPU_UTIL"

const (
	gpuUtilMetric        = "DCGM_FI_DEV_GPU_UTIL"
	grEngineActiveMetric = "DCGM_FI_PROF_GR_ENGINE_ACTIVE"

	// weightedGPUUtilHelp matches the WhaTap exporter's "# HELP" line.
	weightedGPUUtilHelp = "Weighted GPU Utilization"

	labelGPU         = "gpu"
	labelModelName   = "modelName"
	labelMigProfile  = "GPU_I_PROFILE"
	labelMigInstance = "GPU_I_ID"
	labelDeviceUUID  = "DCGM_FI_DEV_UUID"
	labelMaxSlices   = "DCGM_FI_DEV_MIG_MAX_SLICES"
	labelCalculation = "calculation_method"

	methodDirect         = "direct"
	methodWeightedSum    = "weighted_sum"
	methodGREngineActive = "prof_gr_engine_active"

	maxGPUUtilPercent = 100.0
	// defaultMIGMaxSlices applies to A100/H100-class GPUs when the scrape does
	// not carry the DCGM_FI_DEV_MIG_MAX_SLICES label.
	defaultMIGMaxSlices = 7.0
)

// exporterDeviceLabels are the identity labels dcgm-exporter renders before its
// sorted label map. The exporter writes DCGM label fields (DCGM_FI_*) into that
// map, then pod labels; Kubernetes attributes (container, namespace, pod) come
// last.
var exporterDeviceLabels = map[string]bool{
	labelGPU:         true,
	"UUID":           true,
	"uuid":           true,
	"pci_bus_id":     true,
	"device":         true,
	labelModelName:   true,
	labelMigProfile:  true,
	labelMigInstance: true,
	"hostname":       true,
	"Hostname":       true,
}

// hSeriesModelMarkers select GPUs whose whole-GPU utilization the WhaTap
// exporter reads from DCGM_FI_PROF_GR_ENGINE_ACTIVE instead of GPU_UTIL.
var hSeriesModelMarkers = []string{"H100", "H200", "H800", "H20"}

// AppendWeightedGPUUtil appends DCGM_FI_DEV_WEIGHTED_GPU_UTIL rows derived from
// a dcgm-exporter scrape and returns how many it added. A scrape that already
// carries the metric is left unchanged because its exporter owns the values.
//
// Call it on the converted scrape before relabeling and target labels so the
// derived rows go through the same pipeline as exporter-published rows.
func AppendWeightedGPUUtil(result *model.ConversionResult) int {
	if result == nil {
		return 0
	}
	var utilRows, activeRows []*model.OpenMx
	for _, m := range result.OpenMxList {
		switch m.Metric {
		case WeightedGPUUtilMetric:
			return 0
		case gpuUtilMetric:
			utilRows = append(utilRows, m)
		case grEngineActiveMetric:
			activeRows = append(activeRows, m)
		}
	}
	if len(utilRows) == 0 && len(activeRows) == 0 {
		return 0
	}
	for _, help := range result.OpenMxHelpList {
		if help != nil && help.Metric == WeightedGPUUtilMetric {
			return 0
		}
	}

	derived := deriveWeightedGPUUtil(utilRows, activeRows)
	if len(derived) == 0 {
		return 0
	}
	result.OpenMxList = append(result.OpenMxList, derived...)
	help := model.NewOpenMxHelp(WeightedGPUUtilMetric)
	help.Put("help", weightedGPUUtilHelp)
	help.Put("type", "gauge")
	result.OpenMxHelpList = append(result.OpenMxHelpList, help)
	return len(derived)
}

// deriveWeightedGPUUtil follows the WhaTap exporter's three cases:
//   - whole GPU: DCGM_FI_DEV_GPU_UTIL / 100, capped at 100%
//   - MIG GPU: sum of instance GR_ENGINE_ACTIVE x (instance slices / max slices)
//   - whole H-series GPU with profiling data: GR_ENGINE_ACTIVE as is
func deriveWeightedGPUUtil(utilRows, activeRows []*model.OpenMx) []*model.OpenMx {
	hSeries := hSeriesGPUs(utilRows, activeRows)
	derived := make([]*model.OpenMx, 0, len(utilRows)+len(activeRows))

	for _, m := range utilRows {
		if hSeries[labelValue(m, labelGPU)] || !isFinite(m.Value) {
			continue
		}
		derived = append(derived, copyRow(m, math.Min(m.Value, maxGPUUtilPercent)/100.0, methodDirect))
	}

	derived = append(derived, migGPURows(activeRows)...)

	for _, m := range activeRows {
		if labelValue(m, labelMigProfile) != "" || !hSeries[labelValue(m, labelGPU)] || !isFinite(m.Value) {
			continue
		}
		derived = append(derived, copyRow(m, m.Value, methodGREngineActive))
	}
	return derived
}

// hSeriesGPUs returns the GPU indexes reported as H-series by GPU_UTIL that
// also have whole-GPU GR_ENGINE_ACTIVE rows. Without profiling rows such a GPU
// falls back to GPU_UTIL.
func hSeriesGPUs(utilRows, activeRows []*model.OpenMx) map[string]bool {
	candidates := make(map[string]bool)
	for _, m := range utilRows {
		if isHSeriesModel(labelValue(m, labelModelName)) {
			candidates[labelValue(m, labelGPU)] = true
		}
	}
	selected := make(map[string]bool)
	if len(candidates) == 0 {
		return selected
	}
	for _, m := range activeRows {
		gpu := labelValue(m, labelGPU)
		if candidates[gpu] && labelValue(m, labelMigProfile) == "" {
			selected[gpu] = true
		}
	}
	return selected
}

func isHSeriesModel(modelName string) bool {
	upper := strings.ToUpper(modelName)
	for _, marker := range hSeriesModelMarkers {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}

// migGPURows emits one row per MIG-enabled physical GPU.
//
// The WhaTap exporter computes this row before Kubernetes pod mapping and keys
// it by the physical GPU UUID, which kubelet never allocates while MIG is
// enabled, so the row never carries pod labels. An official exporter's MIG
// instance rows do carry the labels of the instance's pod; keep only device
// identity and DCGM label fields so they do not leak onto the GPU-level row.
func migGPURows(activeRows []*model.OpenMx) []*model.OpenMx {
	type gpuSum struct {
		sum         float64
		contributed bool
	}
	var order []string
	templates := make(map[string]*model.OpenMx)
	sums := make(map[string]*gpuSum)
	counted := make(map[[2]string]bool)

	for _, m := range activeRows {
		gpu := labelValue(m, labelGPU)
		if _, ok := templates[gpu]; !ok {
			templates[gpu] = m // the exporter labels the GPU row like its first row
		}
		profile := labelValue(m, labelMigProfile)
		if profile == "" {
			continue
		}
		// The exporter can repeat an instance row for each container sharing it.
		instance := [2]string{gpu, labelValue(m, labelMigInstance)}
		if counted[instance] {
			continue
		}
		counted[instance] = true

		slices := migProfileSlices(profile)
		if slices == 0 || !isFinite(m.Value) {
			continue
		}
		s, ok := sums[gpu]
		if !ok {
			s = &gpuSum{}
			sums[gpu] = s
			order = append(order, gpu)
		}
		// The conversion rounds the product before the addition, so arm64 builds
		// (where Go may fuse x*y+z into an FMA) produce the same bits as amd64.
		s.sum += float64(m.Value * (slices / migMaxSlices(m)))
		s.contributed = true
	}

	rows := make([]*model.OpenMx, 0, len(order))
	for _, gpu := range order {
		if !sums[gpu].contributed {
			continue
		}
		template := templates[gpu]
		row := model.NewOpenMx(WeightedGPUUtilMetric, template.Timestamp, sums[gpu].sum)
		for _, label := range template.Labels {
			if label.Key == labelMigProfile || label.Key == labelMigInstance {
				continue
			}
			if !exporterDeviceLabels[label.Key] && !strings.HasPrefix(label.Key, "DCGM_") {
				continue
			}
			row.AddLabel(label.Key, label.Value)
		}
		setGPUUUIDLabel(row, template)
		setLabel(row, labelCalculation, methodWeightedSum)
		rows = append(rows, row)
	}
	return rows
}

// migProfileSlices returns the compute slices of a GPU instance profile such as
// "3g.20gb" (3), or 0 when the profile has no "<n>g." prefix.
func migProfileSlices(profile string) float64 {
	end := strings.Index(profile, "g.")
	if end <= 0 {
		return 0
	}
	slices, err := strconv.ParseFloat(profile[:end], 64)
	if err != nil || slices <= 0 || !isFinite(slices) {
		return 0
	}
	return slices
}

func migMaxSlices(m *model.OpenMx) float64 {
	maxSlices, err := strconv.ParseFloat(labelValue(m, labelMaxSlices), 64)
	if err != nil || maxSlices <= 0 || !isFinite(maxSlices) {
		return defaultMIGMaxSlices
	}
	return maxSlices
}

// copyRow derives a whole-GPU row that keeps every source label, including the
// pod labels the exporter attached to the source GPU.
func copyRow(source *model.OpenMx, value float64, method string) *model.OpenMx {
	row := model.NewOpenMx(WeightedGPUUtilMetric, source.Timestamp, value)
	row.Labels = append(row.Labels, source.Labels...)
	setGPUUUIDLabel(row, source)
	setLabel(row, labelCalculation, method)
	return row
}

// setGPUUUIDLabel sets DCGM_FI_DEV_UUID to the physical GPU UUID, replacing
// the MIG device UUID that instance rows carry.
func setGPUUUIDLabel(row, source *model.OpenMx) {
	uuid := labelValue(source, "UUID")
	if uuid == "" {
		uuid = labelValue(source, "uuid")
	}
	if uuid != "" {
		setLabel(row, labelDeviceUUID, uuid)
	}
}

// setLabel replaces key in place, or inserts it where dcgm-exporter renders
// it: inside the sorted label map that follows the device identity labels.
func setLabel(m *model.OpenMx, key, value string) {
	for i := range m.Labels {
		if m.Labels[i].Key == key {
			m.Labels[i].Value = value
			return
		}
	}
	at := 0
	for at < len(m.Labels) && exporterDeviceLabels[m.Labels[at].Key] {
		at++
	}
	for at < len(m.Labels) && m.Labels[at].Key < key {
		at++
	}
	m.Labels = append(m.Labels, model.Label{})
	copy(m.Labels[at+1:], m.Labels[at:])
	m.Labels[at] = model.Label{Key: key, Value: value}
}

func labelValue(m *model.OpenMx, key string) string {
	for _, label := range m.Labels {
		if label.Key == key {
			return label.Value
		}
	}
	return ""
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
