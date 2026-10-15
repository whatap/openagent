package processor

import (
	"bytes"
	"testing"

	"github.com/whatap/gointernal/lang/license"
	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/util/queue"

	"open-agent/pkg/config"
	"open-agent/pkg/model"
)

// The WhaTap Operator configures OpenAgent through container ENV
// (spec.features.openAgent.envs) and does not render whatap.conf.
func TestEC2StateForwardingAcceptsOperatorEnvironment(t *testing.T) {
	const running = "aws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"running\",region=\"ap-northeast-2\"} 16\nordinary_metric 7\n"
	const stopped = "aws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"stopped\",region=\"ap-northeast-2\"} 80\nordinary_metric 7\n"
	original := config.GetConfigMap()
	t.Cleanup(func() { config.GetInstance().SetValues(&original) })
	operatorConf := map[string]string{} // The container-created whatap.conf has no EC2 keys.
	config.GetInstance().SetValues(&operatorConf)
	t.Setenv("ec2_state_forwarding_enabled", "true")
	t.Setenv("ec2_state_licenses", license.Build(101, bytes.Repeat([]byte{1}, 16)))

	oldQueue := secure.TcpQueue
	secure.TcpQueue = queue.NewRequestDoubleQueue(4, 1)
	t.Cleanup(func() { secure.TcpQueue = oldQueue })
	processed := make(chan *model.ConversionResult, 1)
	p := NewProcessor(nil, processed)
	p.securityMaster = func() *secure.SecurityMaster { return &secure.SecurityMaster{PCODE: 999, OID: -123} }
	p.enqueueEC2State = func(pcode, licenseHash int64, observation *pack.ParamPack) error {
		if pcode != 101 || observation.Id != 1002 {
			t.Fatalf("ENV-configured observation went to pcode=%d id=%d", pcode, observation.Id)
		}
		return secure.SendOneway(pcode, licenseHash, observation)
	}
	scrape := func(body string, collectionTime int64) {
		p.processRawData(&model.ScrapeRawData{TargetURL: "http://ec2-state-exporter.invalid:9107/metrics", RawData: body, CollectionTime: collectionTime})
		assertOrdinaryMetric(t, processed)
	}

	scrape(running, 1700000000000)
	if got := secure.TcpQueue.Size1(); got != 1 {
		t.Fatalf("container ENV did not enable forwarding: queued=%d want=1", got)
	}
	// A non-empty whatap.conf entry stays authoritative over container ENV.
	fileDisabled := map[string]string{"ec2_state_forwarding_enabled": "false"}
	config.GetInstance().SetValues(&fileDisabled)
	scrape(stopped, 1700000060000)
	if got := secure.TcpQueue.Size1(); got != 1 {
		t.Fatalf("whatap.conf disable was overridden by container ENV: queued=%d want=1", got)
	}
}
