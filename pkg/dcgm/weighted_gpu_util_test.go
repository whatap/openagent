package dcgm

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"open-agent/pkg/converter"
	"open-agent/pkg/model"
)

const collectionTime int64 = 1700000000000

// whatapExporterFixture is one real scrape of the WhaTap dcgm-exporter build
// (4.6.0-4.8.3) on an A100 node: five whole GPUs plus MIG GPUs 1g.5gb x7,
// 2g.10gb x3 and 3g.20gb x2, with pods on most MIG instances. Host, workload,
// UUID and serial values are placeholders.
const whatapExporterFixture = "testdata/whatap-exporter-4.6.0-4.8.3-mixed-mig.prom"

func convert(t *testing.T, text string) *model.ConversionResult {
	t.Helper()
	result, err := converter.ConvertWithTimestamp(text, collectionTime)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	return result
}

// splitExporterRows removes the exporter-published weighted rows and metadata,
// leaving the scrape an official exporter would return.
func splitExporterRows(result *model.ConversionResult) (published []*model.OpenMx) {
	kept := result.OpenMxList[:0:0]
	for _, m := range result.OpenMxList {
		if m.Metric == WeightedGPUUtilMetric {
			published = append(published, m)
			continue
		}
		kept = append(kept, m)
	}
	result.OpenMxList = kept
	helps := result.OpenMxHelpList[:0:0]
	for _, h := range result.OpenMxHelpList {
		if h.Metric != WeightedGPUUtilMetric {
			helps = append(helps, h)
		}
	}
	result.OpenMxHelpList = helps
	return published
}

// sliceShare is the float64 arithmetic the exporter uses for one instance.
// Go folds constant expressions exactly, so tests must not write 0.7*(1.0/7);
// the conversion keeps a following addition from fusing into an FMA.
func sliceShare(active, slices, maxSlices float64) float64 {
	return float64(active * (slices / maxSlices))
}

func rowsOf(result *model.ConversionResult, metric string) []*model.OpenMx {
	var rows []*model.OpenMx
	for _, m := range result.OpenMxList {
		if m.Metric == metric {
			rows = append(rows, m)
		}
	}
	return rows
}

// orderedLabels keeps label order: the derived rows must match the exporter's
// rendering, not only its label set.
func orderedLabels(m *model.OpenMx) string {
	parts := make([]string, 0, len(m.Labels))
	for _, l := range m.Labels {
		parts = append(parts, l.Key+"="+l.Value)
	}
	return strings.Join(parts, ",")
}

func labels(m *model.OpenMx) map[string]string {
	out := make(map[string]string, len(m.Labels))
	for _, l := range m.Labels {
		out[l.Key] = l.Value
	}
	return out
}

func TestAppendWeightedGPUUtilMatchesWhaTapExporter(t *testing.T) {
	text, err := os.ReadFile(whatapExporterFixture)
	if err != nil {
		t.Fatal(err)
	}
	result := convert(t, string(text))
	published := splitExporterRows(result)
	if len(published) != 8 {
		t.Fatalf("fixture has %d published rows, want 8", len(published))
	}
	inputRows := len(result.OpenMxList)

	if added := AppendWeightedGPUUtil(result); added != len(published) {
		t.Fatalf("added %d rows, want %d", added, len(published))
	}
	if len(result.OpenMxList) != inputRows+len(published) {
		t.Fatalf("input rows changed: %d -> %d", inputRows, len(result.OpenMxList)-len(published))
	}

	want := make(map[string]*model.OpenMx, len(published))
	for _, m := range published {
		want[orderedLabels(m)] = m
	}
	for _, got := range rowsOf(result, WeightedGPUUtilMetric) {
		key := orderedLabels(got)
		exp, ok := want[key]
		if !ok {
			t.Errorf("derived row has no exporter counterpart with identical ordered labels:\n%s", key)
			continue
		}
		delete(want, key)
		// Same inputs and arithmetic as the exporter. Its MIG summation order
		// varies between scrapes (the renderer re-sorts rows), so a MIG sum can
		// differ in the last bit on other scrapes; on this one every value is exact.
		if got.Value != exp.Value || got.Timestamp != exp.Timestamp {
			t.Errorf("gpu %s: value/timestamp %v/%d, exporter %v/%d",
				labels(got)["gpu"], got.Value, got.Timestamp, exp.Value, exp.Timestamp)
		}
	}
	for key := range want {
		t.Errorf("exporter row not derived:\n%s", key)
	}

	var help *model.OpenMxHelp
	for _, h := range result.OpenMxHelpList {
		if h.Metric == WeightedGPUUtilMetric {
			help = h
		}
	}
	if help == nil || help.Get("type") != "gauge" || help.Get("help") != "Weighted GPU Utilization" {
		t.Fatalf("help = %#v, want gauge with the exporter's help text", help)
	}
}

func TestAppendWeightedGPUUtilKeepsExporterPublishedRows(t *testing.T) {
	text, err := os.ReadFile(whatapExporterFixture)
	if err != nil {
		t.Fatal(err)
	}
	result := convert(t, string(text))
	rows, helps := len(result.OpenMxList), len(result.OpenMxHelpList)

	if added := AppendWeightedGPUUtil(result); added != 0 {
		t.Fatalf("added %d rows to a scrape that already publishes %s", added, WeightedGPUUtilMetric)
	}
	if len(result.OpenMxList) != rows || len(result.OpenMxHelpList) != helps {
		t.Fatalf("scrape changed: rows %d -> %d, helps %d -> %d",
			rows, len(result.OpenMxList), helps, len(result.OpenMxHelpList))
	}
}

func TestAppendWeightedGPUUtilKeepsScrapeWithOnlyExporterMetadata(t *testing.T) {
	result := convert(t, "# TYPE DCGM_FI_DEV_WEIGHTED_GPU_UTIL gauge\n"+
		`DCGM_FI_DEV_GPU_UTIL{gpu="0",UUID="GPU-a",modelName="NVIDIA A100-SXM4-40GB"} 50`+"\n")
	if added := AppendWeightedGPUUtil(result); added != 0 {
		t.Fatalf("added %d rows although the exporter declares the metric", added)
	}
}

func TestAppendWeightedGPUUtilIgnoresOtherScrapes(t *testing.T) {
	result := convert(t, "# TYPE node_load1 gauge\nnode_load1 0.5\n")
	if added := AppendWeightedGPUUtil(result); added != 0 {
		t.Fatalf("added %d rows to a non-DCGM scrape", added)
	}
	if len(result.OpenMxList) != 1 || len(result.OpenMxHelpList) != 1 {
		t.Fatalf("non-DCGM scrape changed: %d rows, %d helps", len(result.OpenMxList), len(result.OpenMxHelpList))
	}
	if AppendWeightedGPUUtil(nil) != 0 {
		t.Fatal("nil result must be a no-op")
	}
}

func TestAppendWeightedGPUUtilWholeGPU(t *testing.T) {
	result := convert(t, strings.Join([]string{
		`DCGM_FI_DEV_GPU_UTIL{gpu="0",UUID="GPU-a",modelName="NVIDIA A100-SXM4-40GB",hostname="n1",project="team-a",container="c",namespace="ns",pod="p"} 75`,
		`DCGM_FI_DEV_GPU_UTIL{gpu="1",UUID="GPU-b",modelName="NVIDIA A100-SXM4-40GB",hostname="n1"} 150`,
	}, "\n"))

	if added := AppendWeightedGPUUtil(result); added != 2 {
		t.Fatalf("added %d rows, want 2", added)
	}
	rows := rowsOf(result, WeightedGPUUtilMetric)
	if got, want := orderedLabels(rows[0]),
		"gpu=0,UUID=GPU-a,modelName=NVIDIA A100-SXM4-40GB,hostname=n1,DCGM_FI_DEV_UUID=GPU-a,"+
			"calculation_method=direct,project=team-a,container=c,namespace=ns,pod=p"; got != want {
		t.Errorf("labels\n got %s\nwant %s", got, want)
	}
	if rows[0].Value != 0.75 {
		t.Errorf("75%% -> %v, want 0.75", rows[0].Value)
	}
	if rows[1].Value != 1 {
		t.Errorf("GPU_UTIL above 100%% -> %v, want capped 1", rows[1].Value)
	}
}

func TestAppendWeightedGPUUtilHSeriesUsesGREngineActive(t *testing.T) {
	result := convert(t, strings.Join([]string{
		`DCGM_FI_DEV_GPU_UTIL{gpu="0",UUID="GPU-h",modelName="NVIDIA H100 80GB HBM3"} 40`,
		`DCGM_FI_DEV_GPU_UTIL{gpu="1",UUID="GPU-k",modelName="NVIDIA H200"} 30`,
		`DCGM_FI_PROF_GR_ENGINE_ACTIVE{gpu="0",UUID="GPU-h",modelName="NVIDIA H100 80GB HBM3"} 0.85`,
	}, "\n"))

	if added := AppendWeightedGPUUtil(result); added != 2 {
		t.Fatalf("added %d rows, want 2", added)
	}
	byGPU := map[string]*model.OpenMx{}
	for _, m := range rowsOf(result, WeightedGPUUtilMetric) {
		byGPU[labels(m)["gpu"]] = m
	}
	if m := byGPU["0"]; m == nil || m.Value != 0.85 || labels(m)[labelCalculation] != methodGREngineActive {
		t.Errorf("H100 with profiling: %+v, want GR_ENGINE_ACTIVE 0.85", m)
	}
	// Without a profiling row an H-series GPU falls back to GPU_UTIL.
	if m := byGPU["1"]; m == nil || m.Value != 0.3 || labels(m)[labelCalculation] != methodDirect {
		t.Errorf("H200 without profiling: %+v, want GPU_UTIL/100 = 0.3", m)
	}
}

func TestAppendWeightedGPUUtilMIGRow(t *testing.T) {
	// Instance 1 serves a pod; its pod labels must not reach the GPU-level row.
	result := convert(t, strings.Join([]string{
		`DCGM_FI_PROF_GR_ENGINE_ACTIVE{gpu="0",UUID="GPU-m",pci_bus_id="0000:07:00.0",device="nvidia0",modelName="NVIDIA A30",GPU_I_PROFILE="2g.12gb",GPU_I_ID="1",hostname="n1",DCGM_FI_DEV_MIG_MAX_SLICES="4",DCGM_FI_DEV_NAME="2g.12gb",DCGM_FI_DEV_UUID="MIG-1",project="team-a",container="c",namespace="ns",pod="p"} 1`,
		`DCGM_FI_PROF_GR_ENGINE_ACTIVE{gpu="0",UUID="GPU-m",pci_bus_id="0000:07:00.0",device="nvidia0",modelName="NVIDIA A30",GPU_I_PROFILE="1g.6gb",GPU_I_ID="2",hostname="n1",DCGM_FI_DEV_MIG_MAX_SLICES="4",DCGM_FI_DEV_NAME="1g.6gb",DCGM_FI_DEV_UUID="MIG-2"} 0.5`,
		`DCGM_FI_PROF_GR_ENGINE_ACTIVE{gpu="0",UUID="GPU-m",pci_bus_id="0000:07:00.0",device="nvidia0",modelName="NVIDIA A30",GPU_I_PROFILE="1g.6gb",GPU_I_ID="3",hostname="n1",DCGM_FI_DEV_MIG_MAX_SLICES="4",DCGM_FI_DEV_NAME="1g.6gb",DCGM_FI_DEV_UUID="MIG-3"} 0`,
	}, "\n"))

	if added := AppendWeightedGPUUtil(result); added != 1 {
		t.Fatalf("added %d rows, want 1", added)
	}
	row := rowsOf(result, WeightedGPUUtilMetric)[0]
	if got, want := orderedLabels(row),
		"gpu=0,UUID=GPU-m,pci_bus_id=0000:07:00.0,device=nvidia0,modelName=NVIDIA A30,hostname=n1,"+
			"DCGM_FI_DEV_MIG_MAX_SLICES=4,DCGM_FI_DEV_NAME=2g.12gb,DCGM_FI_DEV_UUID=GPU-m,calculation_method=weighted_sum"; got != want {
		t.Errorf("labels\n got %s\nwant %s", got, want)
	}
	// 1 x 2/4 + 0.5 x 1/4 + 0 x 1/4: an A30 has four slices, not seven.
	if want := sliceShare(1, 2, 4) + sliceShare(0.5, 1, 4) + sliceShare(0, 1, 4); row.Value != want {
		t.Errorf("value %v, want %v", row.Value, want)
	}
}

func TestAppendWeightedGPUUtilMIGWithoutMaxSlicesLabel(t *testing.T) {
	result := convert(t, `DCGM_FI_PROF_GR_ENGINE_ACTIVE{gpu="0",UUID="GPU-m",GPU_I_PROFILE="1g.5gb",GPU_I_ID="7"} 0.7`)
	if added := AppendWeightedGPUUtil(result); added != 1 {
		t.Fatalf("added %d rows, want 1", added)
	}
	if got, want := rowsOf(result, WeightedGPUUtilMetric)[0].Value, sliceShare(0.7, 1, 7); got != want {
		t.Errorf("value %v, want %v with the 7-slice default", got, want)
	}
}

func TestAppendWeightedGPUUtilCountsSharedMIGInstanceOnce(t *testing.T) {
	// The exporter repeats an instance row for every container that shares it.
	result := convert(t, strings.Join([]string{
		`DCGM_FI_PROF_GR_ENGINE_ACTIVE{gpu="0",UUID="GPU-m",GPU_I_PROFILE="3g.20gb",GPU_I_ID="1",DCGM_FI_DEV_MIG_MAX_SLICES="7",pod="a"} 0.7`,
		`DCGM_FI_PROF_GR_ENGINE_ACTIVE{gpu="0",UUID="GPU-m",GPU_I_PROFILE="3g.20gb",GPU_I_ID="1",DCGM_FI_DEV_MIG_MAX_SLICES="7",pod="b"} 0.7`,
	}, "\n"))
	if added := AppendWeightedGPUUtil(result); added != 1 {
		t.Fatalf("added %d rows, want 1", added)
	}
	if got, want := rowsOf(result, WeightedGPUUtilMetric)[0].Value, sliceShare(0.7, 3, 7); got != want {
		t.Errorf("value %v, want %v (instance counted once)", got, want)
	}
}

func TestAppendWeightedGPUUtilSkipsUnusableInputs(t *testing.T) {
	result := convert(t, strings.Join([]string{
		`DCGM_FI_DEV_GPU_UTIL{gpu="0",UUID="GPU-a"} +Inf`,
		`DCGM_FI_PROF_GR_ENGINE_ACTIVE{gpu="1",UUID="GPU-m",GPU_I_PROFILE="unknown",GPU_I_ID="1"} 0.5`,
		`DCGM_FI_PROF_GR_ENGINE_ACTIVE{gpu="2",UUID="GPU-n",GPU_I_PROFILE="1g.5gb",GPU_I_ID="1"} NaN`,
		`DCGM_FI_PROF_GR_ENGINE_ACTIVE{gpu="2",UUID="GPU-n",GPU_I_PROFILE="1g.5gb",GPU_I_ID="2"} 0.7`,
	}, "\n"))
	if added := AppendWeightedGPUUtil(result); added != 1 {
		t.Fatalf("added %d rows, want only GPU 2", added)
	}
	row := rowsOf(result, WeightedGPUUtilMetric)[0]
	if want := sliceShare(0.7, 1, 7); labels(row)["gpu"] != "2" || row.Value != want {
		t.Errorf("row %s = %v, want gpu 2 = %v", orderedLabels(row), row.Value, want)
	}
}

func TestMIGProfileSlices(t *testing.T) {
	for profile, want := range map[string]float64{
		"1g.5gb": 1, "1g.10gb+me": 1, "2g.10gb": 2, "3g.40gb": 3, "4g.24gb": 4, "7g.80gb": 7,
		"": 0, "gpu": 0, "g.5gb": 0, "0g.5gb": 0, "xg.5gb": 0,
	} {
		if got := migProfileSlices(profile); got != want {
			t.Errorf("migProfileSlices(%q) = %v, want %v", profile, got, want)
		}
	}
}

func ExampleAppendWeightedGPUUtil() {
	result, _ := converter.ConvertWithTimestamp(strings.Join([]string{
		`DCGM_FI_DEV_GPU_UTIL{gpu="0",UUID="GPU-a"} 60`,
		`DCGM_FI_PROF_GR_ENGINE_ACTIVE{gpu="1",UUID="GPU-b",GPU_I_PROFILE="3g.20gb",GPU_I_ID="1",DCGM_FI_DEV_MIG_MAX_SLICES="7"} 0.7`,
		`DCGM_FI_PROF_GR_ENGINE_ACTIVE{gpu="1",UUID="GPU-b",GPU_I_PROFILE="3g.20gb",GPU_I_ID="2",DCGM_FI_DEV_MIG_MAX_SLICES="7"} 0`,
	}, "\n"), collectionTime)
	AppendWeightedGPUUtil(result)
	for _, m := range rowsOf(result, WeightedGPUUtilMetric) {
		l := labels(m)
		fmt.Printf("gpu %s %s %.1f\n", l["gpu"], l[labelCalculation], m.Value)
	}
	// Output:
	// gpu 0 direct 0.6
	// gpu 1 weighted_sum 0.3
}
