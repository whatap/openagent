// Package ec2state forwards exporter observations, never Server lifecycle commands.
package ec2state

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/lang/pack"

	"open-agent/pkg/model"
)

// Enqueue accepts a one-way observation for local transmission. It is not a Yard ACK.
type Enqueue func(pcode, licenseHash int64, observation *pack.ParamPack) error

// CommandName identifies the observation contract. Its default numeric command
// is allocated by the Yard owner in SERVER-2139 (comment 85923).
const CommandName = "INFRA_EC2_INSTANCE_STATE"

const (
	maxEnqueuesPerScrape  = 16384
	defaultResendInterval = 5 * time.Minute
)

// Forward queues first, changed, or refresh-due observations per destination.
// Only locally accepted observations update history; no receiver ACK is implied.
// The input metric name must match the exporter. The command defaults to the
// owner-allocated ID; an explicit override must match the destination collector.
func (f *Forwarder) Forward(settings map[string]string, raw *model.ScrapeRawData, senderOID int32, enqueue Enqueue) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	flag := settings["ec2_state_forwarding_enabled"]
	if flag == "" {
		f.reset()
		return 0, nil
	}
	enabled, err := strconv.ParseBool(flag)
	if err != nil {
		return 0, errors.New("EC2_STATE_CONFIG: invalid enable flag")
	}
	if !enabled {
		f.reset()
		return 0, nil
	}
	id := int64(secure.INFRA_EC2_INSTANCE_STATE)
	if configured := strings.TrimSpace(settings["ec2_state_param_id"]); configured != "" {
		id, err = strconv.ParseInt(configured, 10, 32)
		if err != nil || id <= 0 {
			return 0, errors.New("EC2_STATE_CONFIG: invalid ParamPack command ID")
		}
	}
	if raw == nil || enqueue == nil || senderOID == 0 || raw.CollectionTime <= 0 {
		return 0, errors.New("EC2_STATE_INPUT: missing scrape, sender, time, or queue")
	}
	if settings["ec2_state_exporter_urls"] == "" || settings["ec2_state_metric_name"] == "" {
		return 0, errors.New("EC2_STATE_CONFIG: exporter URL and metric name are required")
	}
	sources := make(map[string]bool)
	for _, targetURL := range strings.Split(settings["ec2_state_exporter_urls"], ",") {
		sources[strings.TrimSpace(targetURL)] = true
	}
	if !sources[raw.TargetURL] {
		return 0, nil
	}
	resendInterval := defaultResendInterval
	if configured := strings.TrimSpace(settings["ec2_state_resend_interval"]); configured != "" {
		resendInterval, err = time.ParseDuration(configured)
		if err != nil || resendInterval <= 0 {
			return 0, errors.New("EC2_STATE_CONFIG: resend interval must be a positive Go duration")
		}
	}
	destinations, err := parseDestinations(settings["ec2_state_licenses"])
	if err != nil {
		return 0, err
	}
	observations, err := parseObservations(settings, raw)
	if err != nil {
		return 0, err
	}
	if len(observations)*len(destinations) > maxEnqueuesPerScrape {
		return 0, errors.New("EC2_STATE_INPUT: observation fanout exceeds per-scrape limit")
	}
	f.prune(sources, destinations, raw.TargetURL, observations, senderOID, int32(id))
	if f.sent == nil {
		f.sent = make(map[deliveryKey]*sentState)
	}
	if f.now == nil {
		f.now = time.Now
	}
	now := f.now()
	accepted := 0
	for _, observation := range observations {
		for _, destination := range destinations {
			key := deliveryKey{sourceURL: raw.TargetURL, pcode: destination.pcode, licenseHash: destination.licenseHash, senderOID: senderOID, commandID: int32(id), instanceID: observation.instanceID}
			state := sentState{stateCode: observation.stateCode, stateName: observation.stateName}
			if previous, exists := f.sent[key]; exists {
				unchanged := previous.stateCode == state.stateCode && previous.stateName == state.stateName
				elapsed := now.Sub(previous.lastAccepted)
				if unchanged && elapsed >= 0 && elapsed < resendInterval {
					f.order.MoveToFront(previous.position)
					continue
				}
			}
			p := pack.NewParamPack()
			p.Pcode = destination.pcode
			p.Oid = senderOID
			p.Time = observation.observedAt
			p.Id = int32(id)
			p.PutString("instanceId", observation.instanceID)
			p.PutLong("stateCode", observation.stateCode)
			p.PutString("stateName", observation.stateName)
			if err := enqueue(destination.pcode, destination.licenseHash, p); err != nil {
				return accepted, errors.New("EC2_STATE_SEND: local queue rejected observation")
			}
			state.lastAccepted = f.now()
			f.remember(key, state)
			accepted++
		}
	}
	return accepted, nil
}
