package processor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/util/queue"

	"open-agent/pkg/model"
)

// Operators confirm EC2 state forwarding from OpenAgent logs, so a scrape that
// queues observations must be reported at INFO with its count and source.
func TestEC2StateForwardingLogsQueuedCountAtInfo(t *testing.T) {
	p, processed := stateProcessor(t, stateSettings("http://fixture.invalid/metrics"))
	var infos []string
	p.infof = func(id, format string, v ...interface{}) {
		if id == "EC2_STATE_FORWARD" {
			infos = append(infos, fmt.Sprintf(format, v...))
		}
	}
	oldQueue := secure.TcpQueue
	secure.TcpQueue = queue.NewRequestDoubleQueue(8, 1)
	t.Cleanup(func() { secure.TcpQueue = oldQueue })
	raw := &model.ScrapeRawData{TargetURL: "http://fixture.invalid/metrics", RawData: stateBody, CollectionTime: 1700000000000}

	p.processRawData(raw)
	assertOrdinaryMetric(t, processed)
	if len(infos) != 1 || !strings.Contains(infos[0], "locally queued=2") || !strings.Contains(infos[0], raw.TargetURL) {
		t.Fatalf("successful forward was not reported at INFO with its count and source: %q", infos)
	}

	p.processRawData(raw) // Unchanged state inside the resend interval queues nothing.
	assertOrdinaryMetric(t, processed)
	if len(infos) != 1 {
		t.Fatalf("a scrape that queued nothing must not add an INFO line: %q", infos)
	}
}
