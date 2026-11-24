package processor

import (
	"reflect"
	"strings"
	"testing"

	"github.com/whatap/gointernal/net/secure"
	"gopkg.in/yaml.v2"

	"open-agent/pkg/config"
	"open-agent/pkg/model"
)

// apacheScrape is a trimmed apache_exporter page (Lusitaniae/apache_exporter).
const apacheScrape = `# HELP apache_up Could the apache server be reached
# TYPE apache_up gauge
apache_up 1
# HELP apache_workers Apache worker statuses
# TYPE apache_workers gauge
apache_workers{state="busy"} 1
apache_workers{state="idle"} 74
`

func relabelRule(action string, source []string, regex, target, replacement string) *model.RelabelConfig {
	c := model.NewRelabelConfig()
	c.Action, c.SourceLabels, c.TargetLabel = action, source, target
	if regex != "" {
		c.Regex = regex
	}
	if replacement != "" {
		c.Replacement = replacement
	}
	return c
}

// staticEndpointRawData mirrors a StaticEndpoints target (service_discovery.go):
// job comes from targetName and instance from the endpoint address.
func staticEndpointRawData(rules ...*model.RelabelConfig) *model.ScrapeRawData {
	return &model.ScrapeRawData{
		TargetURL:            "http://127.0.0.1:9117/metrics",
		RawData:              apacheScrape,
		MetricRelabelConfigs: model.RelabelConfigs(rules),
		Labels:               map[string]string{"job": "apache-exporter", "instance": "127.0.0.1:9117"},
		NodeName:             "web-node",
		AddNodeLabel:         true,
		CollectionTime:       1700000000000,
	}
}

// processOnce runs one scrape through the processor and returns each emitted
// row as its metric name plus a key -> values view of its labels. Values are a
// slice so a duplicated key (two instance labels) stays visible.
func processOnce(t *testing.T, raw *model.ScrapeRawData) []map[string][]string {
	t.Helper()
	original := config.GetConfigMap()
	t.Cleanup(func() { config.GetInstance().SetValues(&original) })
	settings := map[string]string{}
	config.GetInstance().SetValues(&settings)

	processed := make(chan *model.ConversionResult, 1)
	p := NewProcessor(make(chan *model.ScrapeRawData), processed)
	p.securityMaster = func() *secure.SecurityMaster { return &secure.SecurityMaster{PCODE: 999, OID: -123} }
	p.infof = func(string, string, ...interface{}) {}
	p.processRawData(raw)
	result := <-processed

	var rows []map[string][]string
	for _, m := range result.GetOpenMxList() {
		row := map[string][]string{"__name__": {m.Metric}}
		for _, l := range m.Labels {
			row[l.Key] = append(row[l.Key], l.Value)
		}
		rows = append(rows, row)
	}
	return rows
}

func assertLabel(t *testing.T, rows []map[string][]string, key string, want ...string) {
	t.Helper()
	if len(rows) == 0 {
		t.Fatal("no rows emitted")
	}
	for _, row := range rows {
		if got := row[key]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s %s=%q, want %q", row["__name__"][0], key, got, want)
		}
	}
}

// The helpdesk case: a StaticEndpoints user replaces instance with a server
// name. Before 1.8.14 the rule ran before instance existed, so it added an
// empty instance and the address was appended after it.
func TestMetricRelabelCanReplaceInstance(t *testing.T) {
	rows := processOnce(t, staticEndpointRawData(
		relabelRule("replace", []string{"instance"}, "", "instance", "web-01")))

	if len(rows) != 3 {
		t.Fatalf("%d rows, want 3", len(rows))
	}
	assertLabel(t, rows, "instance", "web-01")
	assertLabel(t, rows, "job", "apache-exporter")
	assertLabel(t, rows, "pcode", "999")
	assertLabel(t, rows, "node", "web-node")
}

func TestMetricRelabelReplacesInstanceWithConstant(t *testing.T) {
	rows := processOnce(t, staticEndpointRawData(
		relabelRule("replace", nil, "", "instance", "web-01")))
	assertLabel(t, rows, "instance", "web-01")
}

func TestMetricRelabelStripsPortFromInstance(t *testing.T) {
	rows := processOnce(t, staticEndpointRawData(
		relabelRule("replace", []string{"instance"}, `(.*):\d+`, "instance", "${1}")))
	assertLabel(t, rows, "instance", "127.0.0.1")
}

// Without rules the emitted label set is unchanged: exporter labels plus job,
// instance, pcode and node, each exactly once.
func TestTargetLabelsWithoutMetricRelabel(t *testing.T) {
	rows := processOnce(t, staticEndpointRawData())
	if len(rows) != 3 {
		t.Fatalf("%d rows, want 3", len(rows))
	}
	assertLabel(t, rows, "job", "apache-exporter")
	assertLabel(t, rows, "instance", "127.0.0.1:9117")
	assertLabel(t, rows, "pcode", "999")
	assertLabel(t, rows, "node", "web-node")
	for _, row := range rows {
		keys := make([]string, 0, len(row))
		for k := range row {
			keys = append(keys, k)
		}
		joined := "," + strings.Join(keys, ",") + ","
		for _, extra := range []string{",exported_instance,", ",exported_job,"} {
			if strings.Contains(joined, extra) {
				t.Errorf("%s has unexpected %s", row["__name__"][0], extra)
			}
		}
	}
}

// keep/drop rules now see the target labels, as metric_relabel_configs do in
// Prometheus.
func TestMetricRelabelReadsTargetLabels(t *testing.T) {
	kept := processOnce(t, staticEndpointRawData(
		relabelRule("keep", []string{"job"}, "apache-exporter", "", "")))
	if len(kept) != 3 {
		t.Errorf("keep on job: %d rows, want 3", len(kept))
	}
	dropped := processOnce(t, staticEndpointRawData(
		relabelRule("drop", []string{"instance"}, `127\.0\.0\.1:9117`, "", "")))
	if len(dropped) != 0 {
		t.Errorf("drop on instance: %d rows, want 0", len(dropped))
	}
}

// A separate label (the workaround offered before 1.8.14) keeps working.
func TestMetricRelabelAddsSeparateLabel(t *testing.T) {
	rows := processOnce(t, staticEndpointRawData(
		relabelRule("replace", nil, "", "server_name", "web-01")))
	assertLabel(t, rows, "server_name", "web-01")
	assertLabel(t, rows, "instance", "127.0.0.1:9117")
}

// Targets whose discovery sets no instance fall back to the target URL, and
// that fallback is relabelable too.
func TestInstanceFallbackIsRelabelable(t *testing.T) {
	raw := staticEndpointRawData()
	raw.Labels = map[string]string{"job": "apache-exporter"}
	assertLabel(t, processOnce(t, raw), "instance", "http://127.0.0.1:9117/metrics")

	raw = staticEndpointRawData(relabelRule("replace", nil, "", "instance", "web-01"))
	raw.Labels = map[string]string{"job": "apache-exporter"}
	assertLabel(t, processOnce(t, raw), "instance", "web-01")
}

func TestTargetLabelsOfIsSortedAndAddsFallback(t *testing.T) {
	got := targetLabelsOf(&model.ScrapeRawData{
		TargetURL: "http://10.0.0.1:9100/metrics",
		Labels:    map[string]string{"namespace": "web", "job": "node"},
	})
	want := []model.Label{
		{Key: "instance", Value: "http://10.0.0.1:9100/metrics"},
		{Key: "job", Value: "node"},
		{Key: "namespace", Value: "web"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("targetLabelsOf = %v, want %v", got, want)
	}
}

// readmeInstanceRelabel is the README "대상 레이블 (job, instance)" example. It
// goes through yaml.v2 and ParseRelabelConfigs, as the config manager does, so
// the quoting of the regex and the ${1} replacement is checked too.
const readmeInstanceRelabel = `
metricRelabelConfigs:
  # instance를 서버 이름으로 바꾸기
  - target_label: instance
    replacement: "web-01"
    action: replace
  # 또는 instance에서 포트만 떼기
  - source_labels: [instance]
    regex: '(.*):\d+'
    target_label: instance
    replacement: '${1}'
    action: replace
`

func readmeRules(t *testing.T) []interface{} {
	t.Helper()
	var doc map[string]interface{}
	if err := yaml.Unmarshal([]byte(readmeInstanceRelabel), &doc); err != nil {
		t.Fatal(err)
	}
	var rules []interface{}
	for _, r := range doc["metricRelabelConfigs"].([]interface{}) {
		m := map[string]interface{}{}
		for k, v := range r.(map[interface{}]interface{}) {
			m[k.(string)] = v
		}
		rules = append(rules, m)
	}
	return rules
}

func TestReadmeInstanceRelabelExample(t *testing.T) {
	rules := readmeRules(t)
	if len(rules) != 2 {
		t.Fatalf("%d rules, want 2", len(rules))
	}
	// Each rule alone, then both in order (the port rule leaves web-01 as is).
	for _, tc := range []struct {
		name  string
		rules []interface{}
		want  string
	}{
		{"server name", rules[:1], "web-01"},
		{"strip port", rules[1:], "127.0.0.1"},
		{"both", rules, "web-01"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := staticEndpointRawData(model.ParseRelabelConfigs(tc.rules)...)
			assertLabel(t, processOnce(t, raw), "instance", tc.want)
		})
	}
}
