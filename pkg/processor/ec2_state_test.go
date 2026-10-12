package processor

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/whatap/gointernal/lang/license"
	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/lang/value"
	"github.com/whatap/golib/util/hash"
	"github.com/whatap/golib/util/queue"

	"open-agent/pkg/model"
	"open-agent/pkg/scraper"
)

const stateBody = "# TYPE fixture_ec2_state gauge\nfixture_ec2_state{instanceId=\"i-0123456789abcdef0\",stateCode=\"48\",stateName=\"terminated\"} 1\nordinary_metric 7\n"

func stateSettings(target string) map[string]string {
	return map[string]string{
		"ec2_state_forwarding_enabled": "true",
		"ec2_state_exporter_urls":      target,
		"ec2_state_metric_name":        "fixture_ec2_state",
		"ec2_state_resend_interval":    "5m", // Explicit fixture interval.
		// Test-only command; the collector owner allocates the production ID.
		"ec2_state_param_id": "190001",
		"ec2_state_licenses": license.Build(101, bytes.Repeat([]byte{1}, 16)) + "," + license.Build(202, bytes.Repeat([]byte{2}, 16)),
	}
}

func stateProcessor(t *testing.T, settings map[string]string) (*Processor, chan *model.ConversionResult) {
	t.Helper()
	processed := make(chan *model.ConversionResult, 1)
	p := NewProcessor(nil, processed)
	p.securityMaster = func() *secure.SecurityMaster { return &secure.SecurityMaster{OID: -123} }
	p.ec2StateSettings = func() map[string]string { return settings }
	return p, processed
}

func assertOrdinaryMetric(t *testing.T, processed chan *model.ConversionResult) {
	t.Helper()
	select {
	case result := <-processed:
		for _, metric := range result.GetOpenMxList() {
			if metric.Metric == "ordinary_metric" && metric.Value == 7 {
				return
			}
		}
		t.Fatal("ordinary metric disappeared")
	default:
		t.Fatal("ordinary metric pipeline was interrupted")
	}
}

func TestEC2StateHTTPThroughProcessorToProjectQueue(t *testing.T) {
	exporter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprint(w, stateBody)
	}))
	defer exporter.Close()
	task := &scraper.ScraperTask{TargetName: "state-fixture", TargetType: scraper.DirectURLType, TargetURL: exporter.URL + "/metrics", Timeout: "2s"}
	raw, err := task.Run()
	if err != nil {
		t.Fatal(err)
	}
	settings := stateSettings(raw.TargetURL)
	p, processed := stateProcessor(t, settings)
	oldQueue := secure.TcpQueue
	secure.TcpQueue = queue.NewRequestDoubleQueue(4, 1)
	t.Cleanup(func() { secure.TcpQueue = oldQueue })
	var projects []int64
	p.enqueueEC2State = func(project, licenseHash int64, observation *pack.ParamPack) error {
		projects = append(projects, project)
		decoded := pack.ToPack(pack.ToBytesPack(observation)).(*pack.ParamPack)
		if decoded.Pcode != project || decoded.Oid != -123 || decoded.Time != raw.CollectionTime || decoded.Id != 190001 || decoded.Request != 0 || decoded.Response != 0 {
			t.Fatal("incorrect destination/observation envelope")
		}
		if decoded.GetString("instanceId") != "i-0123456789abcdef0" || decoded.GetLong("stateCode") != 48 || decoded.GetString("stateName") != "terminated" {
			t.Fatal("incorrect state payload")
		}
		if _, ok := decoded.Get("stateCode").(*value.DecimalValue); !ok {
			t.Fatal("state code lost integer wire type")
		}
		licenseIndex := 0
		if project == 202 {
			licenseIndex = 1
		}
		if licenseHash != hash.Hash64Str(strings.Split(settings["ec2_state_licenses"], ",")[licenseIndex]) {
			t.Fatal("destination identity was not derived from its license")
		}
		return secure.SendOneway(project, licenseHash, observation)
	}
	p.processRawData(raw)
	if len(projects) != 2 || projects[0] != 101 || projects[1] != 202 || secure.TcpQueue.Size1() != 2 {
		t.Fatalf("projects=%v locally queued=%d", projects, secure.TcpQueue.Size1())
	}
	assertOrdinaryMetric(t, processed)
}

func TestEC2StateFailuresPreserveOrdinaryMetrics(t *testing.T) {
	for _, mode := range []string{"disabled", "invalid config", "invalid observation", "unselected", "queue rejected"} {
		t.Run(mode, func(t *testing.T) {
			settings := stateSettings("http://fixture.invalid/metrics")
			body := stateBody
			wantCalls := 0
			switch mode {
			case "disabled":
				delete(settings, "ec2_state_forwarding_enabled")
			case "invalid config":
				settings["ec2_state_param_id"] = "0"
			case "invalid observation":
				body += "fixture_ec2_state{instanceId=\"i-0123456789abcdef0\",stateCode=\"16\",stateName=\"running\"} 1\n"
			case "unselected":
				settings["ec2_state_exporter_urls"] = "http://other.invalid/metrics"
			case "queue rejected":
				wantCalls = 1
			}
			p, processed := stateProcessor(t, settings)
			calls := 0
			p.enqueueEC2State = func(_ int64, _ int64, _ *pack.ParamPack) error {
				calls++
				return errors.New("private transport detail")
			}
			p.processRawData(&model.ScrapeRawData{TargetURL: "http://fixture.invalid/metrics", RawData: body, CollectionTime: 1700000000000})
			if calls != wantCalls {
				t.Fatalf("calls=%d want=%d", calls, wantCalls)
			}
			assertOrdinaryMetric(t, processed)
		})
	}
}
