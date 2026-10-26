package converter

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/proto"

	"open-agent/pkg/model"
)

// Synthetic fixtures model the public NVIDIA 4.6.0-4.8.3 exposition contract;
// these are not GPU hardware captures.
const weightedName = "DCGM_FI_DEV_WEIGHTED_GPU_UTIL"
const physicalLabels = `gpu="0",UUID="GPU-fixture",pci_bus_id="0000:01:00.0",device="nvidia0",modelName="NVIDIA A100-SXM4-40GB",hostname="node-a"`

func migSample(id, profile, load string) string {
	return fmt.Sprintf("DCGM_FI_PROF_GR_ENGINE_ACTIVE{%s,DCGM_FI_DEV_MIG_MODE=\"1\",DCGM_FI_DEV_MIG_MAX_SLICES=\"7\",GPU_I_ID=%q,GPU_I_PROFILE=%q,namespace=\"tenant-a\",pod=\"workload\",container=\"worker\"} %s\n", physicalLabels, id, profile, load)
}

func TestDCGMWeightedMIGMixedProfilesFlexLabels(t *testing.T) {
	body := migSample("0", "1g.5gb", "0.7") + migSample("1", "2g.10gb", "0.35") + migSample("2", "3g.20gb", "0.7")
	result, err := ConvertWithTimestamp(body, testTS)
	if err != nil {
		t.Fatal(err)
	}
	got := mustSingle(t, result.OpenMxList, weightedName, "", "")
	wantValue := (0.7 + 2*0.35 + 3*0.7) / 7
	if math.Abs(got.Value-wantValue) > 1e-12 {
		t.Fatalf("got %v want %v", got.Value, wantValue)
	}
	want := map[string]string{"gpu": "0", "UUID": "GPU-fixture", "DCGM_FI_DEV_UUID": "GPU-fixture", "pci_bus_id": "0000:01:00.0", "device": "nvidia0", "modelName": "NVIDIA A100-SXM4-40GB", "hostname": "node-a", "DCGM_FI_DEV_MIG_MODE": "1", "DCGM_FI_DEV_MIG_MAX_SLICES": "7", "calculation_method": "weighted_sum"}
	if !reflect.DeepEqual(labelMap(got), want) {
		t.Fatalf("labels = %v want %v", labelMap(got), want)
	}
	if len(result.OpenMxList) != 4 {
		t.Fatal("source samples not preserved")
	}
}

func TestDCGMWeightedRejectsIncompleteOrInvalidSnapshots(t *testing.T) {
	valid := migSample("0", "1g.5gb", "0.7")
	cases := map[string]string{
		"missing denominator":            strings.ReplaceAll(valid, `,DCGM_FI_DEV_MIG_MAX_SLICES="7"`, ""),
		"zero denominator":               strings.ReplaceAll(valid, `SLICES="7"`, `SLICES="0"`),
		"fractional denominator":         strings.ReplaceAll(valid, `SLICES="7"`, `SLICES="7.5"`),
		"sentinel denominator":           strings.ReplaceAll(valid, `SLICES="7"`, `SLICES="9223372036854775794"`),
		"invalid profile":                strings.ReplaceAll(valid, "1g.5gb", "1g.nonsense"),
		"fractional profile":             strings.ReplaceAll(valid, "1g.5gb", "1.5g.5gb"),
		"oversubscribed topology":        valid + migSample("1", "7g.40gb", "0"),
		"partial invalid sample":         valid + migSample("1", "2g.10gb", "NaN"),
		"missing known GI sample":        valid + strings.ReplaceAll(migSample("1", "2g.10gb", "1"), "DCGM_FI_PROF_GR_ENGINE_ACTIVE", "DCGM_FI_DEV_FB_USED"),
		"MIG enabled without GI samples": fmt.Sprintf("DCGM_FI_DEV_GPU_UTIL{%s,DCGM_FI_DEV_MIG_MODE=\"1\"} 0\n", physicalLabels),
		"MIG disabled with GI samples":   strings.ReplaceAll(valid, `MODE="1"`, `MODE="0"`),
	}
	for _, invalid := range []string{"NaN", "+Inf", "-Inf", "-1", "101", "9223372036854775794", "140737488355328"} {
		cases["whole "+invalid] = fmt.Sprintf("DCGM_FI_DEV_GPU_UTIL{%s} %s\n", physicalLabels, invalid)
	}
	for _, invalid := range []string{"NaN", "+Inf", "-Inf", "-0.1", "1.1", "9223372036854775794", "140737488355328"} {
		cases["MIG "+invalid] = migSample("0", "1g.5gb", invalid)
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := ConvertWithTimestamp(body, testTS)
			if err != nil {
				t.Fatal(err)
			}
			if rows := weightedRows(result); len(rows) != 0 {
				t.Fatalf("invalid snapshot produced %d weighted row(s): %v", len(rows), rows[0])
			}
		})
	}
}

func TestDCGMWeightedZeroIsAValidObservation(t *testing.T) {
	for _, body := range []string{migSample("0", "1g.5gb", "0"), fmt.Sprintf("DCGM_FI_DEV_GPU_UTIL{%s} 0\n", physicalLabels)} {
		result, err := ConvertWithTimestamp(body, testTS)
		if err != nil {
			t.Fatal(err)
		}
		if got := mustSingle(t, result.OpenMxList, weightedName, "", ""); got.Value != 0 {
			t.Fatal(got)
		}
	}
	result, _ := ConvertWithTimestamp("", testTS)
	if len(weightedRows(result)) != 0 {
		t.Fatal("empty scrape became zero")
	}
}

func TestDCGMWeightedPreservesExistingPerGPU(t *testing.T) {
	body := "# HELP " + weightedName + " producer help\n# TYPE " + weightedName + " gauge\n" + weightedName + "{" + physicalLabels + ",calculation_method=\"producer\"} 0.9\n" + migSample("0", "1g.5gb", "0.7")
	body += strings.ReplaceAll(strings.ReplaceAll(migSample("0", "1g.5gb", "0.7"), `gpu="0"`, `gpu="1"`), "GPU-fixture", "GPU-other")
	result, err := ConvertWithTimestamp(body, testTS)
	if err != nil {
		t.Fatal(err)
	}
	if len(weightedRows(result)) != 2 {
		t.Fatalf("weighted count = %d want 2", len(weightedRows(result)))
	}
	got := mustSingle(t, result.OpenMxList, weightedName, "gpu", "0")
	if got.Value != 0.9 || labelMap(got)["calculation_method"] != "producer" {
		t.Fatal("producer metric replaced")
	}
	helpCount := 0
	for _, help := range result.OpenMxHelpList {
		if help.Metric == weightedName {
			helpCount++
			if help.Get("help") != "producer help" {
				t.Fatal("producer help changed")
			}
		}
	}
	if helpCount != 1 {
		t.Fatalf("help count = %d", helpCount)
	}
}

func TestDCGMWeightedDuplicateAttributionAndParentRows(t *testing.T) {
	mig := migSample("0", "1g.5gb", "0.7") + migSample("1", "2g.10gb", "0.35")
	duplicate := strings.ReplaceAll(strings.ReplaceAll(mig, "tenant-a", "tenant-b"), `pod="workload"`, `pod="another"`)
	parents := fmt.Sprintf("DCGM_FI_PROF_GR_ENGINE_ACTIVE{%s,DCGM_FI_DEV_MIG_MODE=\"1\"} 0.8\nDCGM_FI_DEV_GPU_UTIL{%s,DCGM_FI_DEV_MIG_MODE=\"1\"} 90\n", physicalLabels, physicalLabels)
	for _, body := range []string{mig + duplicate + parents, parents + duplicate + mig} {
		result, err := ConvertWithTimestamp(body, testTS)
		if err != nil {
			t.Fatal(err)
		}
		got := mustSingle(t, result.OpenMxList, weightedName, "", "")
		if math.Abs(got.Value-0.2) > 1e-12 {
			t.Fatalf("double counted: %v", got.Value)
		}
		if len(result.OpenMxList) != 7 {
			t.Fatal("source rows lost")
		}
	}
}

func TestDCGMWeightedConflictingDuplicatesAreUnknown(t *testing.T) {
	whole := fmt.Sprintf("DCGM_FI_DEV_GPU_UTIL{%s} 50\n", physicalLabels)
	mig := migSample("0", "1g.5gb", "0.7")
	for _, body := range []string{
		whole + strings.ReplaceAll(whole, " 50\n", " 60\n"),
		mig + migSample("0", "1g.5gb", "0.8"),
		mig + migSample("0", "2g.10gb", "0.7"),
		mig + strings.ReplaceAll(mig, `SLICES="7"`, `SLICES="8"`),
		mig + strings.TrimSpace(mig) + " 1700000000001\n",
	} {
		result, err := ConvertWithTimestamp(body, testTS)
		if err != nil {
			t.Fatal(err)
		}
		if len(weightedRows(result)) != 0 {
			t.Fatal("conflicting duplicate became an observation")
		}
	}
}

func TestDCGMWeightedSignatureAndScope(t *testing.T) {
	for _, body := range []string{
		"ordinary_metric{gpu=\"0\",UUID=\"GPU-fixture\"} 50\n",
		"DCGM_FI_DEV_GPU_UTIL 50\n",
		"DCGM_FI_DEV_GPU_UTIL{gpu=\"0\",UUID=\"GPU-fixture\"} 50\n",
		fmt.Sprintf("DCGM_FI_DEV_GPU_UTIL{%s} 50\n", strings.ReplaceAll(physicalLabels, "GPU-fixture", "MIG-fixture")),
	} {
		result, err := ConvertWithTimestamp(body, testTS)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.OpenMxList) != 1 || len(weightedRows(result)) != 0 {
			t.Fatal("non-DCGM/ambiguous input enriched")
		}
	}
	base := migSample("0", "1g.5gb", "0.7")
	// A denominator on another host/exporter must not fill this host's gap.
	for _, change := range []func(string) string{
		func(s string) string { return strings.ReplaceAll(s, "node-a", "node-b") },
		func(s string) string { return strings.ReplaceAll(s, "GPU-fixture", "GPU-other") },
		func(s string) string { return strings.ReplaceAll(s, `gpu="0"`, `gpu="1"`) },
		func(s string) string { return strings.ReplaceAll(s, `instance="exporter-a"`, `instance="exporter-b"`) },
	} {
		original := strings.ReplaceAll(base, "{gpu=", `{instance="exporter-a",gpu=`)
		other := change(original)
		body := strings.ReplaceAll(original, `,DCGM_FI_DEV_MIG_MAX_SLICES="7"`, "") + other
		result, _ := ConvertWithTimestamp(body, testTS)
		if len(weightedRows(result)) != 1 {
			t.Fatalf("cross-scope join: %v", weightedRows(result))
		}
	}
	// A converter must not cache a previous scrape's capacity or existing output.
	_, _ = ConvertWithTimestamp(base, testTS)
	missing, _ := ConvertWithTimestamp(strings.ReplaceAll(base, `,DCGM_FI_DEV_MIG_MAX_SLICES="7"`, ""), testTS+1)
	if len(weightedRows(missing)) != 0 {
		t.Fatal("cross-scrape state leaked")
	}
}

func TestDCGMWeightedGaugeMetadata(t *testing.T) {
	mig := strings.ReplaceAll(migSample("0", "3g.20gb", "0.7"), `,DCGM_FI_DEV_MIG_MODE="1",DCGM_FI_DEV_MIG_MAX_SLICES="7"`, "")
	gauges := fmt.Sprintf("# TYPE %s gauge\n%s{%s} 7\n# TYPE %s gauge\n%s{%s} 1\n", "DCGM_FI_DEV_MIG_MAX_SLICES", "DCGM_FI_DEV_MIG_MAX_SLICES", physicalLabels, "DCGM_FI_DEV_MIG_MODE", "DCGM_FI_DEV_MIG_MODE", physicalLabels)
	for _, body := range []string{mig + gauges, gauges + mig} {
		result, err := ConvertWithTimestamp(body, testTS)
		if err != nil {
			t.Fatal(err)
		}
		got := mustSingle(t, result.OpenMxList, weightedName, "", "")
		if math.Abs(got.Value-0.3) > 1e-12 {
			t.Fatal(got.Value)
		}
		if _, ok := labelMap(got)["DCGM_FI_DEV_MIG_MAX_SLICES"]; ok {
			t.Fatal("gauge turned into a label")
		}
	}
	for _, suffix := range []string{
		fmt.Sprintf("DCGM_FI_DEV_MIG_MAX_SLICES{%s} 8\n", physicalLabels),
		fmt.Sprintf("DCGM_FI_DEV_MIG_MODE{%s} 0\n", physicalLabels),
		fmt.Sprintf("DCGM_FI_DEV_MIG_MAX_SLICES{%s} NaN\n", physicalLabels),
	} {
		result, _ := ConvertWithTimestamp(mig+gauges+suffix, testTS)
		if len(weightedRows(result)) != 0 {
			t.Fatal("conflicting gauge metadata accepted")
		}
	}
}

func TestDCGMWeightedWholeModelPriority(t *testing.T) {
	for _, modelName := range []string{"NVIDIA A100", "NVIDIA T4", "NVIDIA H100 80GB HBM3", "NVIDIA H200", "NVIDIA H800", "NVIDIA H20"} {
		t.Run(modelName, func(t *testing.T) {
			labels := strings.ReplaceAll(physicalLabels, "NVIDIA A100-SXM4-40GB", modelName)
			util := fmt.Sprintf("DCGM_FI_DEV_GPU_UTIL{%s} 63\n", labels)
			active := fmt.Sprintf("DCGM_FI_PROF_GR_ENGINE_ACTIVE{%s} 0.2\n", labels)
			result, _ := ConvertWithTimestamp(util+active, testTS)
			got := mustSingle(t, result.OpenMxList, weightedName, "", "")
			value, method := 0.63, "direct"
			if strings.HasPrefix(modelName, "NVIDIA H") {
				value, method = 0.2, "prof_gr_engine_active"
			}
			if got.Value != value || labelMap(got)["calculation_method"] != method {
				t.Fatalf("got %v/%s want %v/%s", got.Value, labelMap(got)["calculation_method"], value, method)
			}
			fallback, _ := ConvertWithTimestamp(util, testTS)
			if got := mustSingle(t, fallback.OpenMxList, weightedName, "", ""); got.Value != 0.63 {
				t.Fatal("missing active did not use GPU_UTIL")
			}
			if strings.HasPrefix(modelName, "NVIDIA H") {
				invalid, _ := ConvertWithTimestamp(util+strings.ReplaceAll(active, " 0.2\n", " NaN\n"), testTS)
				if len(weightedRows(invalid)) != 0 {
					t.Fatal("invalid preferred sample became fallback")
				}
			}
			noUtil, _ := ConvertWithTimestamp(active, testTS)
			if len(weightedRows(noUtil)) != 0 {
				t.Fatal("fork requires GPU_UTIL identity for whole-GPU model branch")
			}
		})
	}
}

func TestDCGMWeightedMalformedSourcesAreNotPartialSuccess(t *testing.T) {
	mig := migSample("0", "1g.5gb", "0.7")
	for name, body := range map[string]string{
		"invalid number":   mig + migSample("1", "2g.10gb", "N/A"),
		"duplicate label":  strings.ReplaceAll(mig, `gpu="0"`, `gpu="0",gpu="0"`),
		"wrong type":       "# TYPE DCGM_FI_PROF_GR_ENGINE_ACTIVE counter\n" + mig,
		"ambiguous device": mig + strings.ReplaceAll(migSample("1", "2g.10gb", "0.35"), "nvidia0", "nvidia1"),
		"UUID conflict":    strings.ReplaceAll(mig, `UUID="GPU-fixture"`, `UUID="GPU-fixture",DCGM_FI_DEV_UUID="GPU-other"`),
		"bad GI id":        migSample("unknown", "1g.5gb", "0.7"),
	} {
		t.Run(name, func(t *testing.T) {
			result, _ := ConvertWithTimestamp(body, testTS)
			if len(weightedRows(result)) != 0 {
				t.Fatal("malformed/ambiguous snapshot derived a value")
			}
		})
	}
}

func TestDCGMWeightedProtobufMissingValueIsNotZero(t *testing.T) {
	labels := []*dto.LabelPair{labelPair("gpu", "0"), labelPair("UUID", "GPU-fixture"), labelPair("device", "nvidia0"), labelPair("modelName", "NVIDIA A100")}
	mf := &dto.MetricFamily{Name: proto.String("DCGM_FI_DEV_GPU_UTIL"), Type: dto.MetricType_GAUGE.Enum(), Metric: []*dto.Metric{{Label: labels, Gauge: &dto.Gauge{}}}}
	result, err := ConvertProtobufWithTimestamp(encodeDelimited(t, mf), testTS)
	if err != nil {
		t.Fatal(err)
	}
	if len(weightedRows(result)) != 0 {
		t.Fatal("absent protobuf value was manufactured as zero")
	}
	mf.Metric[0].Gauge = nil
	mf.Metric[0].Untyped = &dto.Untyped{Value: proto.Float64(0)}
	result, err = ConvertProtobufWithTimestamp(encodeDelimited(t, mf), testTS)
	if err != nil {
		t.Fatal(err)
	}
	if len(weightedRows(result)) != 0 {
		t.Fatal("wrong protobuf value type became a gauge observation")
	}
}

func TestDCGMWeightedNonMIGWithZeroCapacity(t *testing.T) {
	result, err := ConvertWithTimestamp(fmt.Sprintf("DCGM_FI_DEV_GPU_UTIL{%s,DCGM_FI_DEV_MIG_MODE=\"0\",DCGM_FI_DEV_MIG_MAX_SLICES=\"0\"} 50\n", physicalLabels), testTS)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustSingle(t, result.OpenMxList, weightedName, "", ""); got.Value != 0.5 {
		t.Fatal(got)
	}
}

func TestDCGMWeightedEvidenceTimestampAndMetadata(t *testing.T) {
	mig := migSample("0", "1g.5gb", "0.7")
	for name, body := range map[string]string{
		"stale mode":                   mig + fmt.Sprintf("DCGM_FI_DEV_MIG_MODE{%s} 1 1699999999999\n", physicalLabels),
		"stale denominator":            mig + fmt.Sprintf("DCGM_FI_DEV_MIG_MAX_SLICES{%s} 7 1699999999999\n", physicalLabels),
		"inconsistent device metadata": strings.ReplaceAll(mig, "{gpu=", `{DCGM_FI_DRIVER_VERSION="one",gpu=`) + strings.ReplaceAll(migSample("1", "2g.10gb", "0.35"), "{gpu=", `{DCGM_FI_DRIVER_VERSION="two",gpu=`),
		"int32 sentinel":               strings.ReplaceAll(mig, `SLICES="7"`, `SLICES="2147483632"`),
	} {
		t.Run(name, func(t *testing.T) {
			result, _ := ConvertWithTimestamp(body, testTS)
			if len(weightedRows(result)) != 0 {
				t.Fatal("inconsistent evidence accepted")
			}
		})
	}
}

func TestDCGMWeightedUUIDAliasAndNonSevenCapacity(t *testing.T) {
	body := strings.ReplaceAll(migSample("0", "8g.80gb", "0.5"), `SLICES="7"`, `SLICES="8"`)
	body = strings.ReplaceAll(body, `UUID="GPU-fixture"`, `DCGM_FI_DEV_UUID="GPU-fixture"`)
	result, _ := ConvertWithTimestamp(body, testTS)
	got := mustSingle(t, result.OpenMxList, weightedName, "", "")
	if got.Value != 0.5 || labelMap(got)["DCGM_FI_DEV_UUID"] != "GPU-fixture" {
		t.Fatal(got)
	}
	if _, ok := labelMap(got)["UUID"]; ok {
		t.Fatal("invented an unconfigured UUID label")
	}
}

func TestDCGMWeightedNoMutationAndDeterministicOrder(t *testing.T) {
	body := migSample("0", "1g.5gb", "0.7") + strings.ReplaceAll(migSample("1", "2g.10gb", "0.35"), "GPU-fixture", "GPU-other")
	sources := dcgmTextSamples(body, testTS)
	before := make([]model.OpenMx, len(sources))
	for i, row := range sources {
		before[i] = *row
		before[i].Labels = append([]model.Label(nil), row.Labels...)
	}
	result := model.NewConversionResult(append([]*model.OpenMx(nil), sources...), nil)
	addDCGMWeightedUtil(result, sources)
	for i, row := range sources {
		if !reflect.DeepEqual(*row, before[i]) {
			t.Fatal("source was changed")
		}
	}
	want := weightedRows(result)
	for i, j := 0, len(sources)-1; i < j; i, j = i+1, j-1 {
		sources[i], sources[j] = sources[j], sources[i]
	}
	for attempt := 0; attempt < 20; attempt++ {
		reordered := model.NewConversionResult(nil, nil)
		addDCGMWeightedUtil(reordered, sources)
		if !reflect.DeepEqual(weightedRows(reordered), want) {
			t.Fatal("input/map order changed derived rows")
		}
	}
	want[0].Labels[0].Value = "changed-output"
	for i, row := range result.OpenMxList[:len(before)] {
		if !reflect.DeepEqual(*row, before[i]) {
			t.Fatal("derived labels alias source")
		}
	}
}

func TestDCGMWeightedDuplicateMetadataOrder(t *testing.T) {
	plain := migSample("0", "1g.5gb", "0.7")
	withMetadata := strings.ReplaceAll(plain, "{gpu=", `{DCGM_FI_DRIVER_VERSION="fixture-driver",gpu=`)
	first, _ := ConvertWithTimestamp(plain+withMetadata, testTS)
	second, _ := ConvertWithTimestamp(withMetadata+plain, testTS)
	if !reflect.DeepEqual(weightedRows(first), weightedRows(second)) {
		t.Fatal("duplicate order changed physical labels")
	}
	if labelMap(weightedRows(first)[0])["DCGM_FI_DRIVER_VERSION"] != "fixture-driver" {
		t.Fatal("physical metadata was lost")
	}
}

func TestDCGMWeightedNeverPromotesInstanceMetadata(t *testing.T) {
	body := strings.ReplaceAll(migSample("0", "1g.5gb", "0.7"), "{gpu=", `{DCGM_FI_DEV_FB_TOTAL="5120",GPU_CI_ID="0",gpu=`)
	result, _ := ConvertWithTimestamp(body, testTS)
	labels := labelMap(mustSingle(t, result.OpenMxList, weightedName, "", ""))
	for _, name := range []string{"DCGM_FI_DEV_FB_TOTAL", "GPU_CI_ID"} {
		if _, ok := labels[name]; ok {
			t.Fatalf("instance metadata %s copied to parent", name)
		}
	}
}

func TestDCGMWeightedMixedUUIDSpellingsAreStable(t *testing.T) {
	original := migSample("0", "1g.5gb", "0.7")
	alias := strings.ReplaceAll(original, `UUID="`, `DCGM_FI_DEV_UUID="`)
	first, _ := ConvertWithTimestamp(original+alias, testTS)
	second, _ := ConvertWithTimestamp(alias+original, testTS)
	if !reflect.DeepEqual(weightedRows(first), weightedRows(second)) {
		t.Fatal("UUID spelling order changed labels")
	}
	labels := labelMap(mustSingle(t, first.OpenMxList, weightedName, "", ""))
	if labels["UUID"] != "GPU-fixture" || labels["DCGM_FI_DEV_UUID"] != "GPU-fixture" {
		t.Fatal(labels)
	}
}

func TestDCGMWeightedSaturationRemainsRatio(t *testing.T) {
	for _, capacity := range []int{7, 8, 11, 63} {
		var body strings.Builder
		for id := 0; id < capacity; id++ {
			body.WriteString(strings.ReplaceAll(migSample(fmt.Sprint(id), "1g.5gb", "1"), `SLICES="7"`, fmt.Sprintf(`SLICES="%d"`, capacity)))
		}
		result, _ := ConvertWithTimestamp(body.String(), testTS)
		got := mustSingle(t, result.OpenMxList, weightedName, "", "")
		if got.Value != 1 {
			t.Fatalf("saturated capacity=%d value=%.18g want exactly 1", capacity, got.Value)
		}
	}
}

func weightedRows(result *model.ConversionResult) []*model.OpenMx {
	return findSeries(result.OpenMxList, weightedName, "", "")
}

func labelMap(m *model.OpenMx) map[string]string {
	out := make(map[string]string)
	for _, l := range m.Labels {
		out[l.Key] = l.Value
	}
	return out
}

func TestDCGMWeightedWholeWorkloadLabels(t *testing.T) {
	for _, tc := range []struct {
		name, modelName, active string
		value                   float64
		method, source          string
	}{
		{"ordinary", "NVIDIA A100", "", 0.63, "direct", dcgmGPUUtil},
		{"H-series selected GR", "NVIDIA H100", "0.2", 0.2, "prof_gr_engine_active", dcgmGRActive},
		{"H-series fallback", "NVIDIA H200", "", 0.63, "direct", dcgmGPUUtil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			labels := strings.ReplaceAll(physicalLabels, "NVIDIA A100-SXM4-40GB", tc.modelName)
			body := fmt.Sprintf("%s{%s,namespace=\"tenant\",pod=\"util-pod\",container=\"worker\",uid=\"util-uid\",custom=\"util-label\"} 63\n", dcgmGPUUtil, labels)
			if tc.active != "" {
				body += fmt.Sprintf("%s{%s,namespace=\"gr-tenant\",pod=\"gr-pod\",container=\"gr-worker\",uid=\"gr-uid\",custom=\"gr-label\"} %s\n", dcgmGRActive, labels, tc.active)
			}
			sources := dcgmTextSamples(body, testTS)
			before := make([]model.OpenMx, len(sources))
			for i, row := range sources {
				before[i] = *row
				before[i].Labels = append([]model.Label(nil), row.Labels...)
			}
			result := model.NewConversionResult(append([]*model.OpenMx(nil), sources...), nil)
			addDCGMWeightedUtil(result, sources)
			got := mustSingle(t, result.OpenMxList, weightedName, "", "")
			want := labelMap(mustSingle(t, sources, tc.source, "", ""))
			want["DCGM_FI_DEV_UUID"] = "GPU-fixture"
			want["calculation_method"] = tc.method
			if got.Value != tc.value || got.Timestamp != testTS || !reflect.DeepEqual(labelMap(got), want) {
				t.Errorf("whole-GPU attribution/value lost: got %v/%v, want %v/%v", labelMap(got), got.Value, want, tc.value)
			}
			got.Labels[0].Value = "changed-output"
			for i, row := range sources {
				if !reflect.DeepEqual(*row, before[i]) {
					t.Fatal("source changed or derived labels alias source")
				}
			}
		})
	}
}

func TestDCGMWeightedWholeWorkloadCardinality(t *testing.T) {
	for _, modelName := range []string{"NVIDIA A100", "NVIDIA H100"} {
		t.Run(modelName, func(t *testing.T) {
			labels := strings.ReplaceAll(physicalLabels, "NVIDIA A100-SXM4-40GB", modelName)
			metric, value, wantValue, method := dcgmGPUUtil, "63", 0.63, "direct"
			prefix := ""
			if modelName == "NVIDIA H100" {
				metric, value, wantValue, method = dcgmGRActive, "0.2", 0.2, "prof_gr_engine_active"
				prefix = fmt.Sprintf("%s{%s,pod=\"util-only\"} 63\n", dcgmGPUUtil, labels)
			}
			a := fmt.Sprintf("%s{%s,namespace=\"tenant-a\",pod=\"pod-a\",container=\"worker-a\",uid=\"uid-a\",custom=\"label-a\"} %s\n", metric, labels, value)
			b := strings.NewReplacer("tenant-a", "tenant-b", "pod-a", "pod-b", "worker-a", "worker-b", "uid-a", "uid-b", "label-a", "label-b").Replace(a)
			var expected []*model.OpenMx
			for _, body := range []string{prefix + a + b + a, b + a + prefix + a} {
				sources := dcgmTextSamples(body, testTS)
				families := make(map[string]*dto.MetricFamily)
				for _, src := range sources {
					if families[src.Metric] == nil {
						families[src.Metric] = &dto.MetricFamily{Name: proto.String(src.Metric), Type: dto.MetricType_GAUGE.Enum()}
					}
					row := &dto.Metric{Gauge: &dto.Gauge{Value: proto.Float64(src.Value)}, TimestampMs: proto.Int64(src.Timestamp)}
					for _, label := range src.Labels {
						row.Label = append(row.Label, labelPair(label.Key, label.Value))
					}
					families[src.Metric].Metric = append(families[src.Metric].Metric, row)
				}
				var encoded []*dto.MetricFamily
				for _, family := range families {
					encoded = append(encoded, family)
				}
				for _, format := range []string{"text", "protobuf"} {
					var result *model.ConversionResult
					var err error
					if format == "text" {
						result, err = ConvertWithTimestamp(body, testTS)
					} else {
						result, err = ConvertProtobufWithTimestamp(encodeDelimited(t, encoded...), testTS)
					}
					if err != nil {
						t.Fatal(err)
					}
					got := weightedRows(result)
					if len(got) != 2 {
						t.Errorf("%s: distinct workloads collapsed: got %d weighted rows, want 2", format, len(got))
						continue
					}
					if len(result.OpenMxList) != len(sources)+2 {
						t.Fatal("source rows lost")
					}
					for _, pod := range []string{"pod-a", "pod-b"} {
						out := mustSingle(t, got, weightedName, "pod", pod)
						want := labelMap(findSeries(sources, metric, "pod", pod)[0])
						want["calculation_method"], want["DCGM_FI_DEV_UUID"] = method, "GPU-fixture"
						if out.Value != wantValue || out.Timestamp != testTS || !reflect.DeepEqual(labelMap(out), want) {
							t.Fatalf("%s: wrong workload output: %+v", format, out)
						}
					}
					if expected != nil && !reflect.DeepEqual(got, expected) {
						t.Fatal("format/input order changed derived attribution")
					}
					expected = got
				}
			}
		})
	}
}

func TestDCGMWeightedWholeWorkloadConflictsAndProducerPriority(t *testing.T) {
	for _, modelName := range []string{"NVIDIA A100", "NVIDIA H100"} {
		labels := strings.ReplaceAll(physicalLabels, "NVIDIA A100-SXM4-40GB", modelName)
		metric, value, prefix := dcgmGPUUtil, "63", ""
		if modelName == "NVIDIA H100" {
			metric, value = dcgmGRActive, "0.2"
			prefix = fmt.Sprintf("%s{%s} 63\n", dcgmGPUUtil, labels)
		}
		a := fmt.Sprintf("%s{%s,pod=\"a\"} %s\n", metric, labels, value)
		b := strings.ReplaceAll(a, `pod="a"`, `pod="b"`)
		for name, body := range map[string]string{
			"different workload value":     prefix + a + strings.ReplaceAll(b, " "+value+"\n", " 0.4\n"),
			"different workload timestamp": prefix + a + strings.TrimSpace(b) + " 1700000000001\n",
		} {
			t.Run(modelName+"/"+name, func(t *testing.T) {
				result, err := ConvertWithTimestamp(body, testTS)
				if err != nil || len(weightedRows(result)) != 0 {
					t.Fatalf("conflicting workload observations must fail closed: %v", err)
				}
			})
		}
		t.Run(modelName+"/producer", func(t *testing.T) {
			body := prefix + a + b + fmt.Sprintf("%s{%s,pod=\"producer\",calculation_method=\"producer\"} NaN\n", weightedName, labels)
			result, err := ConvertWithTimestamp(body, testTS)
			if err != nil {
				t.Fatal(err)
			}
			got := mustSingle(t, result.OpenMxList, weightedName, "", "")
			if !math.IsNaN(got.Value) || labelMap(got)["pod"] != "producer" || labelMap(got)["calculation_method"] != "producer" {
				t.Fatal("producer-owned unavailable observation changed")
			}
		})
	}
}

func TestDCGMWeightedWholeGPU(t *testing.T) {
	body := "# TYPE DCGM_FI_DEV_GPU_UTIL gauge\nDCGM_FI_DEV_GPU_UTIL{" + physicalLabels + "} 63\n"
	result, err := ConvertWithTimestamp(body, testTS)
	if err != nil {
		t.Fatal(err)
	}
	got := mustSingle(t, result.OpenMxList, weightedName, "", "")
	if got.Value != 0.63 || got.Timestamp != testTS {
		t.Fatalf("weighted value/time = %v/%d", got.Value, got.Timestamp)
	}
	original := mustSingle(t, result.OpenMxList, "DCGM_FI_DEV_GPU_UTIL", "", "")
	want := labelMap(original)
	want["DCGM_FI_DEV_UUID"] = "GPU-fixture"
	want["calculation_method"] = "direct"
	if !reflect.DeepEqual(labelMap(got), want) {
		t.Fatalf("labels = %v, want %v", labelMap(got), want)
	}
	if original.Value != 63 || len(original.Labels) != 6 {
		t.Fatal("source metric was modified")
	}
	foundHelp := false
	for _, help := range result.OpenMxHelpList {
		if help.Metric == weightedName {
			foundHelp = help.Get("type") == "gauge" && help.Get("help") == "Weighted GPU Utilization"
		}
	}
	if !foundHelp {
		t.Fatal("missing fork-compatible HELP/TYPE")
	}
}
