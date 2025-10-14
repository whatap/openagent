package main

import (
	"fmt"
	"os"
	"time"

	"open-agent/pkg/model"

	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/logger/logfile"
)

// This integration tool synthesizes OpenMx metrics for a mixed MIG / non‑MIG node,
// computes DCGM_FI_DEV_WEIGHTED_GPU_UTIL based on those inputs, and sends them
// to the Whatap server using the secure sender (similar to promax.go / dcgm_exporter.go).
//
// It demonstrates the same calculation semantics implemented in
// dcgm-exporter/internal/pkg/collector/weighted_util_collector.go:
//  - MIG GPU: sum over instances (GR_ENGINE_ACTIVE × computeSlices/maxSlices)
//  - Non‑MIG GPU: GPU_UTIL / 100
// The output metric is exposed as a 0..1 ratio (not percent).

func main() {
	// Read server credentials from environment or fall back to explicit exit if missing
	license := os.Getenv("WHATAP_LICENSE")
	host := os.Getenv("WHATAP_HOST")
	port := os.Getenv("WHATAP_PORT")

	if license == "" || host == "" || port == "" {
		fmt.Println("Please set WHATAP_LICENSE, WHATAP_HOST, WHATAP_PORT environment variables")
		os.Exit(1)
	}

	// Logger
	logger := logfile.NewFileLogger()
	logMsg(logger, "Weighted", "Starting weighted integration sender")

	// Start secure networking
	servers := []string{fmt.Sprintf("%s:%s", host, port)}
	secure.StartNet(secure.WithLogger(logger), secure.WithAccessKey(license), secure.WithServers(servers), secure.WithOname("weighted-demo"))

	// Prepare one-shot send with current timestamp
	now := time.Now()

	// 1) Build input OpenMx metrics (example data)
	// We create:
	//  - One MIG GPU (UUID: GPU-MIG-UUID-1) with 2 instances and maxSlices=7
	//  - One non‑MIG GPU (UUID: GPU-NONMIG-UUID-1)

	inputMetrics := make([]*model.OpenMx, 0)

	// Common labels for the node
	nodeName := "node-01"
	hostname := "node-01"

	// MIG GPU example
	// Max slices for the parent GPU (label on any metric is OK; we include on each for clarity)
	migMaxSlices := "7"
	migGPUUUID := "GPU-MIG-UUID-1"

	// Instance A: profile 2g.10gb, engine active 0.60 (i.e., 60%)
	m1 := model.NewOpenMx("DCGM_FI_PROF_GR_ENGINE_ACTIVE", now.UnixMilli(), 0.60)
	m1.AddLabel("UUID", migGPUUUID)
	m1.AddLabel("gpu", "0")
	m1.AddLabel("pci_bus_id", "0000:00:01.0")
	m1.AddLabel("device", "nvidia0")
	m1.AddLabel("modelName", "NVIDIA A100")
	m1.AddLabel("Hostname", hostname)
	m1.AddLabel("DCGM_FI_DEV_MIG_MODE", "1")
	m1.AddLabel("DCGM_FI_DEV_MIG_MAX_SLICES", migMaxSlices)
	m1.AddLabel("GPU_I_PROFILE", "2g.10gb")
	m1.AddLabel("GPU_I_ID", "1")
	m1.AddLabel("node", nodeName)

	// Instance B: profile 3g.20gb, engine active 0.30
	m2 := model.NewOpenMx("DCGM_FI_PROF_GR_ENGINE_ACTIVE", now.UnixMilli(), 0.30)
	m2.AddLabel("UUID", migGPUUUID)
	m2.AddLabel("gpu", "0")
	m2.AddLabel("pci_bus_id", "0000:00:01.0")
	m2.AddLabel("device", "nvidia0")
	m2.AddLabel("modelName", "NVIDIA A100")
	m2.AddLabel("Hostname", hostname)
	m2.AddLabel("DCGM_FI_DEV_MIG_MODE", "1")
	m2.AddLabel("DCGM_FI_DEV_MIG_MAX_SLICES", migMaxSlices)
	m2.AddLabel("GPU_I_PROFILE", "3g.20gb")
	m2.AddLabel("GPU_I_ID", "2")
	m2.AddLabel("node", nodeName)

	inputMetrics = append(inputMetrics, m1, m2)

	// Non‑MIG GPU example
	nonMigGPUUUID := "GPU-NONMIG-UUID-1"
	m3 := model.NewOpenMx("DCGM_FI_DEV_GPU_UTIL", now.UnixMilli(), 75.0) // 75%
	m3.AddLabel("UUID", nonMigGPUUUID)
	m3.AddLabel("gpu", "1")
	m3.AddLabel("pci_bus_id", "0000:00:02.0")
	m3.AddLabel("device", "nvidia1")
	m3.AddLabel("modelName", "NVIDIA T4")
	m3.AddLabel("Hostname", hostname)
	m3.AddLabel("DCGM_FI_DEV_MIG_MODE", "0")
	m3.AddLabel("node", nodeName)

	inputMetrics = append(inputMetrics, m3)

	// 2) Compute weighted metrics per GPU (following dcgm-exporter logic)
	weighted := computeWeightedFromInputs(inputMetrics)

	// 3) Build and send help packs (for both input and output metrics)
	helps := make([]*model.OpenMxHelp, 0)

	// Help for input metrics
	h1 := model.NewOpenMxHelp("DCGM_FI_PROF_GR_ENGINE_ACTIVE")
	h1.Put("help", "Graphics engine active ratio (0..1) for MIG instances")
	h1.Put("type", "gauge")
	helps = append(helps, h1)

	h2 := model.NewOpenMxHelp("DCGM_FI_DEV_GPU_UTIL")
	h2.Put("help", "GPU utilization percent (0..100) for non‑MIG GPUs")
	h2.Put("type", "gauge")
	helps = append(helps, h2)

	// Help for output metric
	h3 := model.NewOpenMxHelp("DCGM_FI_DEV_WEIGHTED_GPU_UTIL")
	h3.Put("help", "Weighted GPU utilization (0..1). MIG: sum(GR_ENGINE_ACTIVE × slices/maxSlices), non‑MIG: GPU_UTIL/100")
	h3.Put("type", "gauge")
	helps = append(helps, h3)

	// Send help
	if len(helps) > 0 {
		hp := model.NewOpenMxHelpPack()
		sm := secure.GetSecurityMaster()
		if sm == nil {
			fmt.Println("security master not initialized")
			os.Exit(1)
		}
		hp.SetPCODE(sm.PCODE)
		hp.SetOID(sm.OID)
		hp.SetTime(now.UnixMilli())
		hp.SetRecords(helps)
		logMsg(logger, "Weighted", fmt.Sprintf("Sending %d help records", len(helps)))
		secure.Send(secure.NET_SECURE_HIDE, hp, true)
		time.Sleep(100 * time.Millisecond)
	}

	// 4) Send input and calculated metrics
	records := make([]*model.OpenMx, 0, len(inputMetrics)+len(weighted))
	records = append(records, inputMetrics...)
	records = append(records, weighted...)

	mp := model.NewOpenMxPack()
	mp.SetTime(now.UnixMilli())
	mp.SetRecords(records)

	sm := secure.GetSecurityMaster()
	if sm == nil {
		fmt.Println("security master not initialized")
		os.Exit(1)
	}
	mp.SetPCODE(sm.PCODE)
	mp.SetOID(sm.OID)

	logMsg(logger, "Weighted", fmt.Sprintf("Sending %d metrics (%d inputs + %d weighted)", len(records), len(inputMetrics), len(weighted)))
	secure.Send(secure.NET_SECURE_HIDE, mp, true)
}

// computeWeightedFromInputs groups input metrics by GPU UUID and computes
// DCGM_FI_DEV_WEIGHTED_GPU_UTIL per GPU. It returns newly created OpenMx metrics.
func computeWeightedFromInputs(inputs []*model.OpenMx) []*model.OpenMx {
	// Group by UUID
	byUUID := map[string][]*model.OpenMx{}
	for _, mx := range inputs {
		uuid := getLabel(mx, "UUID")
		if uuid == "" {
			// allow fallback to DCGM_FI_DEV_UUID if provided
			uuid = getLabel(mx, "DCGM_FI_DEV_UUID")
		}
		if uuid == "" {
			// skip if no grouping label
			continue
		}
		byUUID[uuid] = append(byUUID[uuid], mx)
	}

	out := make([]*model.OpenMx, 0)
	for uuid, list := range byUUID {
		migMode := getLabelAny(list, "DCGM_FI_DEV_MIG_MODE", "0")
		if migMode == "1" {
			// MIG mode: aggregate GR_ENGINE_ACTIVE × slices/maxSlices
			maxSlices := atoiSafe(getLabelAny(list, "DCGM_FI_DEV_MIG_MAX_SLICES", "0"))
			if maxSlices <= 0 {
				continue
			}
			var weightedSum float64
			for _, mx := range list {
				if mx.Metric != "DCGM_FI_PROF_GR_ENGINE_ACTIVE" {
					continue
				}
				// compute slices from GPU_I_PROFILE like "2g.10gb"
				profile := getLabel(mx, "GPU_I_PROFILE")
				slices := parseComputeSlices(profile)
				if slices <= 0 {
					continue
				}
				ratio := float64(slices) / float64(maxSlices)
				weightedSum += mx.Value * ratio
			}
			// create aggregated metric at GPU level (no MIG instance labels)
			aggregated := model.NewOpenMx("DCGM_FI_DEV_WEIGHTED_GPU_UTIL", list[0].Timestamp, weightedSum)
			copyGPUBaseLabels(aggregated, list[0])
			aggregated.AddLabel("UUID", uuid)
			aggregated.AddLabel("calculation_method", "weighted_sum")
			// Ensure MIG-instance specific labels are not added
			out = append(out, aggregated)
		} else {
			// Non‑MIG: find GPU_UTIL and divide by 100
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
			aggregated := model.NewOpenMx("DCGM_FI_DEV_WEIGHTED_GPU_UTIL", base.Timestamp, util)
			copyGPUBaseLabels(aggregated, base)
			aggregated.AddLabel("UUID", uuid)
			aggregated.AddLabel("DCGM_FI_DEV_MIG_MODE", "0")
			aggregated.AddLabel("calculation_method", "direct")
			out = append(out, aggregated)
		}
	}

	return out
}

// copyGPUBaseLabels copies a minimal set of common GPU labels from src to dst,
// excluding MIG-instance specific labels such as MigProfile and GPUInstanceID.
func copyGPUBaseLabels(dst, src *model.OpenMx) {
	// We copy commonly used ones for dashboards/search
	keys := []string{"gpu", "pci_bus_id", "device", "modelName", "Hostname", "node"}
	for _, k := range keys {
		if v := getLabel(src, k); v != "" {
			dst.AddLabel(k, v)
		}
	}
}

func parseComputeSlices(profile string) int {
	// Expected form: "<n>g.<size>gb"
	n := 0
	// simple fast parse without regex
	for i := 0; i < len(profile); i++ {
		c := profile[i]
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		} else if c == 'g' {
			break
		} else if n > 0 {
			// encountered non-digit before 'g'; stop
			break
		}
	}
	return n
}

func atoiSafe(s string) int {
	n := 0
	sign := 1
	i := 0
	if len(s) > 0 && (s[0] == '-' || s[0] == '+') {
		if s[0] == '-' {
			sign = -1
		}
		i = 1
	}
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return sign * n
}

func getLabel(mx *model.OpenMx, key string) string {
	for _, l := range mx.Labels {
		if l.Key == key {
			return l.Value
		}
	}
	return ""
}

// getLabelAny returns the first non-empty label among the provided keys, or def if none found.
func getLabelAny(list []*model.OpenMx, key string, def string) string {
	for _, mx := range list {
		for _, l := range mx.Labels {
			if l.Key == key && l.Value != "" {
				return l.Value
			}
		}
	}
	return def
}

func logMsg(logger *logfile.FileLogger, tag, message string) {
	logger.Println(tag, message)
	fmt.Printf("[%s] %s\n", tag, message)
}
