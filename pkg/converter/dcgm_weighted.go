package converter

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"

	"open-agent/pkg/model"
)

const (
	dcgmWeightedUtil = "DCGM_FI_DEV_WEIGHTED_GPU_UTIL"
	dcgmGPUUtil      = "DCGM_FI_DEV_GPU_UTIL"
	dcgmGRActive     = "DCGM_FI_PROF_GR_ENGINE_ACTIVE"
	dcgmMaxSlices    = "DCGM_FI_DEV_MIG_MAX_SLICES"
	dcgmMIGMode      = "DCGM_FI_DEV_MIG_MODE"
)

var dcgmProfile = regexp.MustCompile(`^([1-9][0-9]*)g\.[1-9][0-9]*gb(?:\+me)?$`)

// addDCGMWeightedUtil appends compatibility metrics without changing the source.
// It is called once per successful scrape, before relabeling and target labels.
func addDCGMWeightedUtil(result *model.ConversionResult, sources []*model.OpenMx) {
	groups := make(map[string][]*model.OpenMx)
	for _, src := range sources {
		if !strings.HasPrefix(src.Metric, "DCGM_FI_") {
			continue
		}
		labels := dcgmLabels(src)
		_, err := strconv.ParseUint(labels["gpu"], 10, 32)
		if err != nil || !strings.HasPrefix(dcgmUUID(labels), "GPU-") {
			continue
		}
		identity := dcgmPhysicalLabels(labels, false)
		// Device/model metadata must agree within the same scoped GPU, not
		// split an ambiguous physical device into multiple derived rows.
		delete(identity, "device")
		delete(identity, "pci_bus_id")
		delete(identity, "modelName")
		key := dcgmLabelKey(identity)
		groups[key] = append(groups[key], src)
	}
	added := false
	groupKeys := make([]string, 0, len(groups))
	for key := range groups {
		groupKeys = append(groupKeys, key)
	}
	sort.Strings(groupKeys)
	for _, key := range groupKeys {
		if out := dcgmWeightedGPU(groups[key]); len(out) > 0 {
			result.OpenMxList = append(result.OpenMxList, out...)
			added = true
		}
	}
	if added {
		for _, help := range result.OpenMxHelpList {
			if help.Metric == dcgmWeightedUtil {
				return
			}
		}
		help := model.NewOpenMxHelp(dcgmWeightedUtil)
		help.Put("type", "gauge")
		help.Put("help", "Weighted GPU Utilization")
		result.OpenMxHelpList = append(result.OpenMxHelpList, help)
	}
}

// dcgmWeightedGPU requires a complete, internally consistent physical snapshot.
// No guessed device capacities, clamping, or missing-as-zero observations.
func dcgmWeightedGPU(rows []*model.OpenMx) []*model.OpenMx {
	for _, src := range rows {
		// Producer-owned output is authoritative even if it is unavailable/NaN.
		if src.Metric == dcgmWeightedUtil {
			return nil
		}
	}
	var whole, active, parent, template *model.OpenMx
	instances := make(map[string]*model.OpenMx)
	profiles := make(map[string]string)
	maxSlices, mode := -1.0, -1.0
	identity := dcgmLabelKey(dcgmPhysicalLabels(dcgmLabels(rows[0]), false))
	metadata := make(map[string]string)
	metadataTimes := make(map[int64]bool)
	hasUUIDLabel := false
	for _, src := range rows {
		labels := dcgmLabels(src)
		hasUUIDLabel = hasUUIDLabel || labels["UUID"] != ""
		if labels["modelName"] == "" || (labels["device"] == "" && labels["pci_bus_id"] == "") {
			return nil
		}
		if dcgmLabelKey(dcgmPhysicalLabels(labels, false)) != identity {
			return nil
		}
		if uuid, ok := labels["DCGM_FI_DEV_UUID"]; ok && uuid != dcgmUUID(labels) {
			return nil
		}
		for key, value := range labels {
			if !dcgmPhysicalMetadata(key) {
				continue
			}
			if old, ok := metadata[key]; ok && old != value {
				return nil
			}
			metadata[key] = value
			if key == dcgmMIGMode || key == dcgmMaxSlices {
				metadataTimes[src.Timestamp] = true
			}
		}
		if src.Metric == dcgmMIGMode || src.Metric == dcgmMaxSlices {
			metadataTimes[src.Timestamp] = true
		}
		if text, ok := labels[dcgmMIGMode]; ok {
			v, err := strconv.ParseFloat(text, 64)
			if err != nil || (v != 0 && v != 1) || (mode != -1 && mode != v) {
				return nil
			}
			mode = v
		}
		if text, ok := labels[dcgmMaxSlices]; ok {
			v, err := strconv.ParseFloat(text, 64)
			// Below DCGM's int32 sentinel range; this is not a GPU model limit.
			if err != nil || !dcgmInRange(v, 0, 2147483631) || math.Trunc(v) != v || (maxSlices != -1 && maxSlices != v) {
				return nil
			}
			maxSlices = v
		}
		id, profile := labels["GPU_I_ID"], labels["GPU_I_PROFILE"]
		if src.Metric == dcgmMIGMode {
			v := src.Value
			if (v != 0 && v != 1) || (mode != -1 && mode != v) {
				return nil
			}
			mode = v
		}
		if src.Metric == dcgmMaxSlices {
			v := src.Value
			if !dcgmInRange(v, 0, 2147483631) || math.Trunc(v) != v || (maxSlices != -1 && maxSlices != v) {
				return nil
			}
			maxSlices = v
			if id == "" && profile == "" {
				parent = src
			}
		}
		if id != "" || profile != "" {
			if _, err := strconv.ParseUint(id, 10, 32); err != nil || !dcgmProfile.MatchString(profile) {
				return nil
			}
			if old, ok := profiles[id]; ok && old != profile {
				return nil
			}
			profiles[id] = profile
			if src.Metric == dcgmGRActive {
				if !dcgmInRange(src.Value, 0, 1) {
					return nil
				}
				if old := instances[id]; old != nil && (old.Value != src.Value || old.Timestamp != src.Timestamp) {
					return nil
				}
				instances[id] = src
			}
		} else if src.Metric == dcgmGPUUtil {
			if whole != nil && (whole.Value != src.Value || whole.Timestamp != src.Timestamp) {
				return nil
			}
			whole = src
		} else if src.Metric == dcgmGRActive {
			if active != nil && (active.Value != src.Value || active.Timestamp != src.Timestamp) {
				return nil
			}
			active = src
		}
	}
	value, method := 0.0, "direct"
	if len(profiles) > 0 || mode == 1 {
		if mode == 0 || maxSlices <= 0 || len(profiles) == 0 || len(instances) != len(profiles) {
			return nil
		}
		totalSlices := 0.0
		ids := make([]string, 0, len(instances))
		for id := range instances {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			src := instances[id]
			slices, _ := strconv.ParseFloat(dcgmProfile.FindStringSubmatch(profiles[id])[1], 64)
			totalSlices += slices
			if template != nil && template.Timestamp != src.Timestamp {
				return nil
			}
			template = src
			value += src.Value * slices
		}
		if totalSlices > maxSlices {
			return nil
		}
		value /= maxSlices
		method = "weighted_sum"
		if parent != nil {
			if parent.Timestamp != template.Timestamp {
				return nil
			}
			template = parent
		}
	} else {
		if whole == nil {
			return nil
		}
		if dcgmHSeries(dcgmLabels(whole)["modelName"]) && active != nil {
			if !dcgmInRange(active.Value, 0, 1) || active.Timestamp != whole.Timestamp {
				return nil
			}
			value, template, method = active.Value, active, "prof_gr_engine_active"
		} else {
			if !dcgmInRange(whole.Value, 0, 100) {
				return nil
			}
			value, template = whole.Value/100, whole
		}
	}
	for ts := range metadataTimes {
		if ts != template.Timestamp {
			return nil
		}
	}
	templates := []*model.OpenMx{template}
	if method != "weighted_sum" {
		// Equal physical observations can have distinct workload attribution.
		// Keep every selected source row, not just the last one validated above.
		templates = nil
		for _, src := range rows {
			if src.Metric == template.Metric {
				templates = append(templates, src)
			}
		}
	}
	outputs := make(map[string]*model.OpenMx)
	for _, src := range templates {
		labels := dcgmLabels(src)
		if method == "weighted_sum" {
			labels = dcgmPhysicalLabels(labels, true)
		}
		for key, value := range metadata {
			labels[key] = value
		}
		if hasUUIDLabel {
			labels["UUID"] = dcgmUUID(dcgmLabels(rows[0]))
		}
		labels["DCGM_FI_DEV_UUID"] = dcgmUUID(labels)
		labels["calculation_method"] = method
		out := model.NewOpenMx(dcgmWeightedUtil, src.Timestamp, value)
		keys := make([]string, 0, len(labels))
		for k := range labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out.AddLabel(k, labels[k])
		}
		// Value and timestamp equality were validated before deduplicating.
		outputs[dcgmLabelKey(labels)] = out
	}
	keys := make([]string, 0, len(outputs))
	for key := range outputs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out []*model.OpenMx
	for _, key := range keys {
		out = append(out, outputs[key])
	}
	return out
}

// Parse only the DCGM subset strictly. The legacy converter intentionally
// tolerates malformed lines; deriving a partial sum from those would be unsafe.
// Original metrics and non-DCGM parsing are left untouched.
func dcgmTextSamples(data string, timestamp int64) []*model.OpenMx {
	if !strings.Contains(data, "DCGM_FI_") {
		return nil
	}
	var text strings.Builder
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		include := strings.HasPrefix(line, "DCGM_FI_")
		if strings.HasPrefix(line, "#") {
			parts := strings.Fields(strings.TrimSpace(line[1:]))
			include = len(parts) >= 2 && (parts[0] == "HELP" || parts[0] == "TYPE") && strings.HasPrefix(parts[1], "DCGM_FI_")
		}
		if include {
			text.WriteString(line)
			text.WriteByte('\n')
		}
	}
	if text.Len() == 0 {
		return nil
	}
	var parser expfmt.TextParser
	families, err := parser.TextToMetricFamilies(strings.NewReader(text.String()))
	if err != nil {
		return nil
	}
	var samples []*model.OpenMx
	for _, family := range families {
		rows, ok := dcgmFamilySamples(family, timestamp)
		if !ok {
			return nil
		}
		samples = append(samples, rows...)
	}
	return samples
}

func dcgmFamilySamples(family *dto.MetricFamily, timestamp int64) ([]*model.OpenMx, bool) {
	name := family.GetName()
	if !strings.HasPrefix(name, "DCGM_FI_") {
		return nil, true
	}
	input := name == dcgmGPUUtil || name == dcgmGRActive || name == dcgmMIGMode || name == dcgmMaxSlices
	if input && family.GetType() != dto.MetricType_GAUGE && family.GetType() != dto.MetricType_UNTYPED {
		return nil, false
	}
	var samples []*model.OpenMx
	for _, metric := range family.Metric {
		if metric == nil {
			return nil, false
		}
		value := math.NaN() // A missing protobuf field is not a measured zero.
		if g := metric.GetGauge(); family.GetType() == dto.MetricType_GAUGE && g != nil && g.Value != nil {
			value = g.GetValue()
		}
		if u := metric.GetUntyped(); family.GetType() == dto.MetricType_UNTYPED && u != nil && u.Value != nil {
			value = u.GetValue()
		}
		ts := timestamp
		if metric.TimestampMs != nil {
			ts = metric.GetTimestampMs()
		}
		sample := newOpenMxWithLabels(name, ts, value, metric.Label)
		seen := make(map[string]bool)
		for _, label := range sample.Labels {
			if seen[label.Key] {
				return nil, false
			}
			seen[label.Key] = true
		}
		samples = append(samples, sample)
	}
	return samples, true
}

func dcgmInRange(v, min, max float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= min && v <= max
}

// Match the fork's model branch, not a guessed architecture/capacity table.
func dcgmHSeries(name string) bool {
	name = strings.ToUpper(name)
	for _, model := range []string{"H100", "H200", "H800", "H20"} {
		if strings.Contains(name, model) {
			return true
		}
	}
	return false
}

func dcgmLabels(src *model.OpenMx) map[string]string {
	labels := make(map[string]string, len(src.Labels))
	for _, l := range src.Labels {
		labels[l.Key] = l.Value
	}
	return labels
}

func dcgmUUID(labels map[string]string) string {
	if uuid := labels["UUID"]; uuid != "" {
		return uuid
	}
	return labels["DCGM_FI_DEV_UUID"]
}

// Only physical identity/source scope and exporter-owned device metadata may
// reach the parent. In particular GI and workload attribution are not copied.
func dcgmPhysicalLabels(labels map[string]string, metadata bool) map[string]string {
	out := make(map[string]string)
	for k, v := range labels {
		switch k {
		case "gpu", "UUID", "pci_bus_id", "device", "modelName", "hostname", "Hostname", "node", "job", "instance", "cluster", "exported_job", "exported_instance", "endpoint":
			out[k] = v
		default:
			if metadata && dcgmPhysicalMetadata(k) {
				out[k] = v
			}
		}
	}
	// Use either official UUID spelling for matching, but preserve the actual
	// exposition label name in emitted output (metadata=true).
	if !metadata {
		out["UUID"] = dcgmUUID(labels)
	}
	return out
}

// A DCGM prefix alone does not make a label physical: custom CSVs can label
// per-GI FB_TOTAL, for example. Only known device-wide metadata is promoted.
func dcgmPhysicalMetadata(name string) bool {
	switch name {
	case dcgmMIGMode, dcgmMaxSlices, "DCGM_FI_DEV_UUID", "DCGM_FI_DRIVER_VERSION":
		return true
	default:
		return false
	}
}

func dcgmLabelKey(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var key strings.Builder
	for _, k := range keys {
		key.WriteString(strconv.Quote(k))
		key.WriteString(strconv.Quote(labels[k]))
	}
	return key.String()
}
