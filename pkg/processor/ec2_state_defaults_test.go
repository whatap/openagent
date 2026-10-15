package processor

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/whatap/gointernal/lang/license"
	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/lang/value"
	"github.com/whatap/golib/util/hash"
	"github.com/whatap/golib/util/queue"

	"open-agent/pkg/config"
	"open-agent/pkg/model"
	"open-agent/pkg/scraper"
)

func TestEC2StateMinimalConfigThroughScraperAndDefaultProcessor(t *testing.T) {
	const pending = "# TYPE aws_ec2_instance_state gauge\naws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"pending\",region=\"ap-northeast-2\"} 0\n"
	const running = "# TYPE aws_ec2_instance_state gauge\naws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"running\",region=\"ap-northeast-2\"} 16\n"
	const invalid = "aws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"terminated\"} 16\n"
	const ordinary = "ordinary_metric 7\naws_ec2_cpuutilization_average 0\naws_ec2_status_check_failed_maximum 1\n"
	var body atomic.Value
	body.Store(pending + ordinary)
	var status, requests atomic.Int32
	exporter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.RequestURI() != "/custom/metrics?region=test" {
			t.Error("unexpected exporter request URL")
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		if code := status.Load(); code != 0 {
			w.WriteHeader(int(code))
			return
		}
		fmt.Fprint(w, body.Load().(string))
	}))
	defer exporter.Close()
	task := &scraper.ScraperTask{TargetName: "canonical-ec2", TargetType: scraper.DirectURLType, TargetURL: exporter.URL + "/custom/metrics?region=test", Timeout: "2s"}

	licenses := []string{license.Build(101, bytes.Repeat([]byte{1}, 16)), license.Build(202, bytes.Repeat([]byte{2}, 16))}
	settings := map[string]string{
		"ec2_state_forwarding_enabled": "true",
		"ec2_state_licenses":           licenses[0] + "," + licenses[1],
	}
	original := config.GetConfigMap()
	t.Cleanup(func() { config.GetInstance().SetValues(&original) })
	config.GetInstance().SetValues(&settings)
	oldQueue := secure.TcpQueue
	secure.TcpQueue = queue.NewRequestDoubleQueue(8, 1)
	t.Cleanup(func() { secure.TcpQueue = oldQueue })
	processed := make(chan *model.ConversionResult, 1)
	p := NewProcessor(nil, processed)
	p.securityMaster = func() *secure.SecurityMaster { return &secure.SecurityMaster{PCODE: 999, OID: -123} }

	var wantCode, observationTime int64
	var wantName string
	p.enqueueEC2State = func(pcode, licenseHash int64, observation *pack.ParamPack) error {
		decoded := pack.ToPack(pack.ToBytesPack(observation)).(*pack.ParamPack)
		index := 0
		if pcode == 202 {
			index = 1
		} else if pcode != 101 {
			t.Fatal("unexpected project destination")
		}
		if licenseHash != hash.Hash64Str(licenses[index]) || decoded.Pcode != pcode || decoded.Oid != -123 || decoded.Id != 1002 || decoded.Time != observationTime || decoded.Request != 0 || decoded.Response != 0 {
			t.Fatal("minimal configuration lost the destination/default oneway envelope")
		}
		code, ok := decoded.Get("stateCode").(*value.DecimalValue)
		if !ok || code.Val != wantCode || decoded.GetString("stateName") != wantName || decoded.GetString("instanceId") != "i-0123456789abcdef0" {
			t.Fatal("canonical payload lost identity, state or numeric wire type")
		}
		return secure.SendOneway(pcode, licenseHash, observation)
	}

	steps := []struct {
		name, body string
		status     int32
		disable    bool
		wantQueued int
		code       int64
		state      string
	}{
		{"pending zero", pending, 0, false, 2, 0, "pending"},
		{"unchanged", pending, 0, false, 2, 0, "pending"},
		{"running change", running, 0, false, 4, 16, "running"},
		{"HTTP failure", "", http.StatusServiceUnavailable, false, 4, 0, ""},
		{"missing snapshot", "", 0, false, 4, 0, ""},
		{"reappeared", running, 0, false, 6, 16, "running"},
		{"invalid snapshot", invalid, 0, false, 6, 0, ""},
		{"disabled reload", pending, 0, true, 6, 0, ""},
	}
	for i, step := range steps {
		body.Store(step.body + ordinary)
		status.Store(step.status)
		if step.disable {
			disabled := map[string]string{"ec2_state_forwarding_enabled": "false"}
			config.GetInstance().SetValues(&disabled)
		}
		raw, err := task.Run()
		if step.status != 0 {
			if err == nil {
				t.Fatal("failed HTTP scrape returned a successful snapshot")
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			wantCode, wantName, observationTime = step.code, step.state, raw.CollectionTime
			p.processRawData(raw)
			assertOrdinaryMetric(t, processed)
		}
		if got := secure.TcpQueue.Size1(); got != step.wantQueued {
			t.Fatalf("step=%s queued=%d want=%d", step.name, got, step.wantQueued)
		}
		if got := requests.Load(); got != int32(i+1) {
			t.Fatalf("step=%s requests=%d want=%d; forwarding must not probe the exporter again", step.name, got, i+1)
		}
	}
}
