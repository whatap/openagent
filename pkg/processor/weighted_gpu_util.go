package processor

import (
	"regexp"
	"strconv"

	"open-agent/pkg/model"
)

// parseComputeSlices extracts the number before 'g.' from a MIG profile like "2g.10gb"
var migProfileRe = regexp.MustCompile(`^([0-9]+)g\.`)

func parseComputeSlices(profile string) int {
	m := migProfileRe.FindStringSubmatch(profile)
	if len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n
		}
	}
	return 0
}

func getLabel(mx *model.OpenMx, k string) string {
	if mx.Labels == nil {
		return ""
	}
	for _, lab := range mx.Labels {
		if lab.Key == k {
			return lab.Value
		}
	}
	return ""
}

// getLabelAny returns the first non-empty label among list items, or default if none found
func getLabelAny(list []*model.OpenMx, key string, def string) string {
	for _, mx := range list {
		if v := getLabel(mx, key); v != "" {
			return v
		}
	}
	return def
}

func atoiSafe(s string) int {
	if s == "" {
		return 0
	}
	n, _ := strconv.Atoi(s)
	return n
}

// copyBaseGPULabels copies common GPU-level labels from src to dst, while dropping MIG instance labels
func copyBaseGPULabels(dst *model.OpenMx, src *model.OpenMx) {
	if src.Labels == nil {
		return
	}
	for _, lab := range src.Labels {
		switch lab.Key {
		case "GPU_I_PROFILE", "GPU_I_ID":
			// drop MIG-instance labels
		default:
			dst.AddLabel(lab.Key, lab.Value)
		}
	}
}

// aggregateWeightedGPUUtil computes per-GPU DCGM_FI_DEV_WEIGHTED_GPU_UTIL from input OpenMx metrics.
// It mirrors test/integration/weighted.go's computeWeightedFromInputs but is localized to the processor.
func aggregateWeightedGPUUtil(inputs []*model.OpenMx) []*model.OpenMx {
	// Group by UUID (fallback to DCGM_FI_DEV_UUID if UUID absent)
	byUUID := map[string][]*model.OpenMx{}
	for _, mx := range inputs {
		uuid := getLabel(mx, "UUID")
		if uuid == "" {
			uuid = getLabel(mx, "DCGM_FI_DEV_UUID")
		}
		if uuid == "" {
			continue
		}
		byUUID[uuid] = append(byUUID[uuid], mx)
	}

	out := make([]*model.OpenMx, 0, len(byUUID))

	for uuid, list := range byUUID {
		migMode := getLabelAny(list, "DCGM_FI_DEV_MIG_MODE", "0")
		if migMode == "1" {
			maxSlices := atoiSafe(getLabelAny(list, "DCGM_FI_DEV_MIG_MAX_SLICES", "0"))
			if maxSlices <= 0 {
				continue
			}
			var sum float64
			var base *model.OpenMx
			for _, mx := range list {
				if base == nil {
					base = mx
				}
				if mx.Metric != "DCGM_FI_PROF_GR_ENGINE_ACTIVE" {
					continue
				}
				slices := parseComputeSlices(getLabel(mx, "GPU_I_PROFILE"))
				if slices <= 0 {
					continue
				}
				sum += mx.Value * float64(slices) / float64(maxSlices)
			}
			if base == nil {
				continue
			}
			agg := model.NewOpenMx("DCGM_FI_DEV_WEIGHTED_GPU_UTIL", base.Timestamp, sum)
			copyBaseGPULabels(agg, base)
			agg.AddLabel("UUID", uuid)
			agg.AddLabel("calculation_method", "weighted_sum")
			out = append(out, agg)
		} else {
			// Non-MIG: GPU_UTIL / 100
			var base *model.OpenMx
			var util float64
			for _, mx := range list {
				if base == nil {
					base = mx
				}
				if mx.Metric == "DCGM_FI_DEV_GPU_UTIL" {
					util = mx.Value / 100.0
				}
			}
			if base == nil {
				continue
			}
			agg := model.NewOpenMx("DCGM_FI_DEV_WEIGHTED_GPU_UTIL", base.Timestamp, util)
			copyBaseGPULabels(agg, base)
			agg.AddLabel("UUID", uuid)
			agg.AddLabel("DCGM_FI_DEV_MIG_MODE", "0")
			agg.AddLabel("calculation_method", "direct")
			out = append(out, agg)
		}
	}
	return out
}
