package processor

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/whatap/gointernal/net/secure"

	"open-agent/pkg/config"
	"open-agent/pkg/dcgm"
	"open-agent/pkg/model"
)

// The WhaTap dcgm-exporter build publishes DCGM_FI_DEV_WEIGHTED_GPU_UTIL; the
// official exporter returns the same scrape without those rows.
const whatapExporterScrape = "../dcgm/testdata/whatap-exporter-4.6.0-4.8.3-mixed-mig.prom"

func dcgmScrapes(t *testing.T) (whatapExporter, officialExporter string) {
	t.Helper()
	text, err := os.ReadFile(whatapExporterScrape)
	if err != nil {
		t.Fatal(err)
	}
	var official []string
	for _, line := range strings.Split(string(text), "\n") {
		if !strings.Contains(line, dcgm.WeightedGPUUtilMetric) {
			official = append(official, line)
		}
	}
	return string(text), strings.Join(official, "\n")
}

// dcgmTargetRawData mirrors the Operator's dcgm-exporter-auto PodMonitor target.
func dcgmTargetRawData(body string) *model.ScrapeRawData {
	relabel := func(action, source, regex, target, replacement string) *model.RelabelConfig {
		c := model.NewRelabelConfig()
		c.Action, c.TargetLabel = action, target
		if source != "" {
			c.SourceLabels = []string{source}
		}
		if regex != "" {
			c.Regex = regex
		}
		if replacement != "" {
			c.Replacement = replacement
		}
		return c
	}
	return &model.ScrapeRawData{
		TargetURL: "http://10.0.0.7:9400/metrics",
		RawData:   body,
		MetricRelabelConfigs: model.RelabelConfigs{
			relabel("replace", "", "", "wtp_src", "true"),
			relabel("keep", "__name__", "DCGM.*", "", ""),
			relabel("replace", "", "", "cluster", "gpu-cluster"),
			relabel("replace", "project", "", "whatap_kube_label_gpu_group", ""),
		},
		Labels:         map[string]string{"job": "dcgm-exporter-auto", "instance": "10.0.0.7:9400"},
		NodeName:       "gpu-node-01",
		AddNodeLabel:   true,
		CollectionTime: 1700000000000,
	}
}

type processedWeighted struct {
	rows  map[string]float64 // ordered labels -> value
	helps int
	infos []string
}

func processDCGMScrape(t *testing.T, body string, settings map[string]string) processedWeighted {
	t.Helper()
	original := config.GetConfigMap()
	t.Cleanup(func() { config.GetInstance().SetValues(&original) })
	config.GetInstance().SetValues(&settings)

	processed := make(chan *model.ConversionResult, 1)
	p := NewProcessor(make(chan *model.ScrapeRawData), processed)
	p.securityMaster = func() *secure.SecurityMaster { return &secure.SecurityMaster{PCODE: 999, OID: -123} }
	var out processedWeighted
	p.infof = func(_, format string, v ...interface{}) { out.infos = append(out.infos, fmt.Sprintf(format, v...)) }

	out.rows = map[string]float64{}
	for scrape := 0; scrape < 2; scrape++ {
		p.processRawData(dcgmTargetRawData(body))
		result := <-processed
		if scrape == 1 {
			break // the second scrape only checks the one-time log
		}
		for _, m := range result.GetOpenMxList() {
			if m.Metric != dcgm.WeightedGPUUtilMetric {
				continue
			}
			parts := make([]string, 0, len(m.Labels))
			for _, l := range m.Labels {
				parts = append(parts, l.Key+"="+l.Value)
			}
			// Target labels come from a map, so the pipeline's label order varies
			// per scrape; compare label sets here (pkg/dcgm checks exporter order).
			sort.Strings(parts)
			out.rows[strings.Join(parts, ",")] = m.Value
		}
		for _, h := range result.GetOpenMxHelpList() {
			if h.Metric == dcgm.WeightedGPUUtilMetric {
				out.helps++
			}
		}
	}
	return out
}

func TestProcessorDerivesWeightedGPUUtilLikeWhaTapExporter(t *testing.T) {
	whatapExporter, officialExporter := dcgmScrapes(t)

	published := processDCGMScrape(t, whatapExporter, map[string]string{})
	derived := processDCGMScrape(t, officialExporter, map[string]string{})

	if len(published.rows) != 8 || published.helps != 1 {
		t.Fatalf("WhaTap exporter scrape: %d rows, %d helps; want 8 and 1", len(published.rows), published.helps)
	}
	if len(published.infos) != 0 {
		t.Fatalf("exporter-published rows must not be derived again: %q", published.infos)
	}
	// Relabeling, target labels, pcode and the node label apply to derived
	// rows exactly as to exporter-published rows.
	for labels, value := range published.rows {
		got, ok := derived.rows[labels]
		if !ok {
			t.Errorf("no derived row with labels\n%s", labels)
		} else if got != value {
			t.Errorf("derived %v, exporter %v for\n%s", got, value, labels)
		}
	}
	if len(derived.rows) != len(published.rows) || derived.helps != 1 {
		t.Errorf("official exporter scrape: %d rows, %d helps; want %d and 1", len(derived.rows), derived.helps, len(published.rows))
	}
	if len(derived.infos) != 1 || !strings.Contains(derived.infos[0], "Derived 8 "+dcgm.WeightedGPUUtilMetric) {
		t.Errorf("want exactly one activation log over two scrapes, got %q", derived.infos)
	}
}

func TestProcessorWeightedGPUUtilCanBeDisabled(t *testing.T) {
	_, officialExporter := dcgmScrapes(t)
	for _, value := range []string{"false", "0", "no"} {
		got := processDCGMScrape(t, officialExporter, map[string]string{dcgmWeightedGPUUtilKey: value})
		if len(got.rows) != 0 || got.helps != 0 || len(got.infos) != 0 {
			t.Errorf("%s=%s: %d rows, %d helps, logs %q; want none", dcgmWeightedGPUUtilKey, value, len(got.rows), got.helps, got.infos)
		}
	}
}
