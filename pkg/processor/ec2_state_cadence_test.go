package processor

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/util/queue"

	"open-agent/pkg/scraper"
)

func TestEC2StateHTTPOnlyQueuesChangedObservations(t *testing.T) {
	var body atomic.Value
	body.Store(stateBody)
	exporter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprint(w, body.Load().(string))
	}))
	defer exporter.Close()
	task := &scraper.ScraperTask{TargetName: "cadence-fixture", TargetType: scraper.DirectURLType, TargetURL: exporter.URL + "/metrics", Timeout: "2s"}
	settings := stateSettings(task.TargetURL)
	delete(settings, "ec2_state_resend_interval") // Missing interval uses the production default.
	p, processed := stateProcessor(t, settings)
	oldQueue := secure.TcpQueue
	secure.TcpQueue = queue.NewRequestDoubleQueue(8, 1)
	t.Cleanup(func() { secure.TcpQueue = oldQueue })

	for i, wantQueued := range []int{2, 2, 4} {
		if i == 2 {
			body.Store(strings.ReplaceAll(strings.ReplaceAll(stateBody, "48", "16"), "terminated", "running"))
		}
		raw, err := task.Run()
		if err != nil {
			t.Fatal(err)
		}
		// Each scrape has a different observation time, but time is not state.
		raw.CollectionTime += int64(i)
		p.processRawData(raw)
		assertOrdinaryMetric(t, processed)
		if got := secure.TcpQueue.Size1(); got != wantQueued {
			t.Fatalf("scrape=%d locally queued=%d, want %d; unchanged state must not enqueue again", i, got, wantQueued)
		}
	}
}
