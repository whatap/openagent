package processor

import (
	"os"
	"testing"
	"time"

	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/util/queue"

	"open-agent/pkg/config"
	"open-agent/pkg/model"
)

func TestMain(m *testing.M) {
	config.Cleanup() // Isolate config fixtures from the background file watcher.
	os.Exit(m.Run())
}

func TestDefaultProcessorUsesConfigSnapshotsAndOnewayQueue(t *testing.T) {
	original := config.GetConfigMap()
	t.Cleanup(func() { config.GetInstance().SetValues(&original) })
	oldQueue := secure.TcpQueue
	secure.TcpQueue = queue.NewRequestDoubleQueue(4, 1)
	t.Cleanup(func() { secure.TcpQueue = oldQueue })
	settings := stateSettings("http://fixture.invalid/metrics")
	delete(settings, "ec2_state_resend_interval")
	delete(settings, "ec2_state_param_id")
	config.GetInstance().SetValues(&settings)

	rawQueue := make(chan *model.ScrapeRawData, 1)
	processed := make(chan *model.ConversionResult, 1)
	p := NewProcessor(rawQueue, processed)
	// Only replace the primary agent identity, not configuration or transport.
	p.securityMaster = func() *secure.SecurityMaster { return &secure.SecurityMaster{PCODE: 999, OID: -123} }
	done := make(chan struct{})
	go func() { p.processLoop(); close(done) }()
	t.Cleanup(func() {
		close(rawQueue)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("processor did not stop when its input closed")
		}
	})
	for scrape := 0; scrape < 2; scrape++ {
		if scrape == 1 {
			disabled := map[string]string{"ec2_state_forwarding_enabled": "false"}
			config.GetInstance().SetValues(&disabled)
		}
		rawQueue <- &model.ScrapeRawData{TargetURL: "http://fixture.invalid/metrics", RawData: stateBody, CollectionTime: 1700000000000}
		select {
		case result := <-processed:
			found := false
			for _, metric := range result.GetOpenMxList() {
				if metric.Metric != "ordinary_metric" {
					continue
				}
				for _, label := range metric.Labels {
					if label.Key == "pcode" && label.Value == "999" {
						found = true
					}
				}
			}
			if !found {
				t.Fatal("ordinary metrics lost the primary project identity")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("processor did not emit ordinary metrics")
		}
		if secure.TcpQueue.Size1() != 2 {
			t.Fatalf("scrape=%d locally queued=%d, want 2; disable must affect the next scrape", scrape, secure.TcpQueue.Size1())
		}
	}
}
