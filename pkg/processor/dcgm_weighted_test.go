package processor

import (
	"bytes"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/prometheus/common/expfmt"
	"github.com/whatap/gointernal/net/secure"
	golibio "github.com/whatap/golib/io"

	"open-agent/pkg/model"
	"open-agent/pkg/scraper"
)

// Synthetic NVIDIA-shaped response, not a hardware capture. All HTTP stays on
// loopback; the processor's sender queue is inspected without starting Sender.
const weightedScrapeFixture = `# TYPE DCGM_FI_PROF_GR_ENGINE_ACTIVE gauge
DCGM_FI_PROF_GR_ENGINE_ACTIVE{gpu="0",UUID="GPU-mig",device="nvidia0",modelName="NVIDIA A100",hostname="exporter-host",DCGM_FI_DEV_MIG_MODE="1",DCGM_FI_DEV_MIG_MAX_SLICES="7",GPU_I_ID="0",GPU_I_PROFILE="3g.20gb",namespace="tenant",pod="workload"} 0.7
DCGM_FI_PROF_GR_ENGINE_ACTIVE{gpu="1",UUID="GPU-whole",device="nvidia1",modelName="NVIDIA H100",hostname="exporter-host",namespace="whole-tenant",pod="whole-a",container="whole-worker"} 0.2
DCGM_FI_PROF_GR_ENGINE_ACTIVE{gpu="1",UUID="GPU-whole",device="nvidia1",modelName="NVIDIA H100",hostname="exporter-host",namespace="whole-tenant",pod="whole-b",container="whole-worker"} 0.2
# TYPE DCGM_FI_DEV_GPU_UTIL gauge
DCGM_FI_DEV_GPU_UTIL{gpu="1",UUID="GPU-whole",device="nvidia1",modelName="NVIDIA H100",hostname="exporter-host"} 65
ordinary_metric 9
`

func TestDCGMWeightedScrapeThroughProcessor(t *testing.T) {
	var parser expfmt.TextParser
	families, err := parser.TextToMetricFamilies(strings.NewReader(weightedScrapeFixture))
	if err != nil {
		t.Fatal(err)
	}
	var protoBody bytes.Buffer
	encoder := expfmt.NewEncoder(&protoBody, expfmt.NewFormat(expfmt.TypeProtoDelim))
	for _, family := range families {
		if err := encoder.Encode(family); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ name, body, contentType string }{
		{"text", weightedScrapeFixture, "text/plain; version=0.0.4"},
		{"protobuf", protoBody.String(), "application/vnd.google.protobuf; proto=io.prometheus.client.MetricFamily; encoding=delimited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exporter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				fmt.Fprint(w, tc.body)
			}))
			defer exporter.Close()
			task := &scraper.ScraperTask{
				TargetName: "dcgm-exporter-auto", TargetType: scraper.DirectURLType,
				TargetURL: exporter.URL + "/metrics", Timeout: "2s",
				Labels:   map[string]string{"job": "dcgm-exporter-auto", "instance": exporter.URL},
				NodeName: "kube-node-a", AddNodeLabel: true,
				MetricRelabelConfigs: model.RelabelConfigs{
					&model.RelabelConfig{Action: "keep", SourceLabels: []string{"__name__"}, Regex: "^(DCGM_FI_DEV_WEIGHTED_GPU_UTIL|ordinary_metric)$"},
					&model.RelabelConfig{Action: "replace", SourceLabels: []string{"UUID"}, Regex: "(.*)", Replacement: "$1", TargetLabel: "physical_gpu"},
				},
			}
			raw, err := task.Run()
			if err != nil {
				t.Fatal(err)
			}
			queue := make(chan *model.ConversionResult, 1)
			p := NewProcessor(nil, queue)
			p.ec2StateSettings = func() map[string]string {
				return map[string]string{"ec2_state_forwarding_enabled": "false"}
			}
			p.securityMaster = func() *secure.SecurityMaster { return &secure.SecurityMaster{PCODE: 123, OID: 456} }
			p.processRawData(raw)
			result := <-queue
			if raw.RawData != tc.body {
				t.Fatal("scrape input changed")
			}
			if result.Target != task.TargetURL || result.CollectionTime != raw.CollectionTime {
				t.Fatal("scrape identity lost")
			}
			if len(result.OpenMxList) != 4 {
				t.Fatalf("sender queue has %d rows, want 4 (MIG parent + 2 whole workloads + ordinary)", len(result.OpenMxList))
			}
			weighted := 0
			wholePods := make(map[string]bool)
			for _, metric := range result.OpenMxList {
				if metric.Metric != "DCGM_FI_DEV_WEIGHTED_GPU_UTIL" {
					continue
				}
				weighted++
				labels := map[string]string{}
				for _, l := range metric.Labels {
					labels[l.Key] = l.Value
				}
				want := 0.3
				if labels["UUID"] == "GPU-whole" {
					want = 0.2
					if labels["namespace"] != "whole-tenant" || labels["container"] != "whole-worker" {
						t.Fatalf("whole workload labels lost: %v", labels)
					}
					wholePods[labels["pod"]] = true
				}
				if math.Abs(metric.Value-want) > 1e-12 || metric.Timestamp != raw.CollectionTime {
					t.Fatalf("wrong weighted sample: %+v", metric)
				}
				if labels["physical_gpu"] != labels["UUID"] || labels["node"] != "kube-node-a" || labels["hostname"] != "exporter-host" || labels["pcode"] != "123" || labels["job"] != "dcgm-exporter-auto" || labels["instance"] != exporter.URL {
					t.Fatalf("downstream labels lost: %v", labels)
				}
				forbiddenLabels := []string{"GPU_I_ID", "GPU_I_PROFILE"}
				if labels["UUID"] == "GPU-mig" {
					forbiddenLabels = append(forbiddenLabels, "namespace", "pod", "container")
				}
				for _, forbidden := range forbiddenLabels {
					if _, ok := labels[forbidden]; ok {
						t.Fatalf("parent inherited %s", forbidden)
					}
				}
			}
			if weighted != 3 || len(wholePods) != 2 || !wholePods["whole-a"] || !wholePods["whole-b"] {
				t.Fatalf("weighted count = %d", weighted)
			}
			// Exercise the actual scalar pack used by sender.createMetricsPack,
			// without starting the secure sender or contacting a collector.
			wire := model.NewOpenMxPack().SetRecords(result.OpenMxList)
			wire.Endpoint = result.Target
			out := golibio.NewDataOutputX()
			wire.Write(out)
			decoded := model.NewOpenMxPack()
			decoded.Read(golibio.NewDataInputX(out.ToByteArray()))
			if decoded.Endpoint != result.Target || !reflect.DeepEqual(decoded.GetRecords(), result.OpenMxList) {
				t.Fatal("OpenMxPack wire round trip changed derived samples")
			}
		})
	}
}
