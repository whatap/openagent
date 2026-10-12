package ec2state

import (
	"encoding/binary"
	"errors"
	"mime"
	"strings"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"google.golang.org/protobuf/proto"

	"open-agent/pkg/model"
)

const maxExpositionBytes = 2 * 1024 * 1024

// decodeFamilies rejects a partial response before any observations are sent.
func decodeFamilies(raw *model.ScrapeRawData) (map[string]*dto.MetricFamily, error) {
	if len(raw.RawData) > maxExpositionBytes {
		return nil, errors.New("EC2_STATE_INPUT: exporter exposition exceeds size limit")
	}
	mediaType, params, err := mime.ParseMediaType(raw.ContentType)
	if raw.ContentType == "" {
		mediaType, err = "text/plain", nil
	}
	if err != nil {
		return nil, errors.New("EC2_STATE_INPUT: invalid exporter Content-Type")
	}
	switch mediaType {
	case "text/plain":
		if version := params["version"]; version != "" && version != "0.0.4" {
			return nil, errors.New("EC2_STATE_INPUT: unsupported text exposition version")
		}
		var parser expfmt.TextParser
		families, err := parser.TextToMetricFamilies(strings.NewReader(raw.RawData))
		if err != nil {
			return nil, errors.New("EC2_STATE_INPUT: invalid exporter exposition")
		}
		return families, nil
	case "application/vnd.google.protobuf":
		if params["proto"] != "io.prometheus.client.MetricFamily" || params["encoding"] != "delimited" {
			return nil, errors.New("EC2_STATE_INPUT: unsupported protobuf exposition")
		}
		return decodeProtobuf([]byte(raw.RawData))
	default:
		return nil, errors.New("EC2_STATE_INPUT: unsupported exporter Content-Type")
	}
}

func decodeProtobuf(data []byte) (map[string]*dto.MetricFamily, error) {
	families := make(map[string]*dto.MetricFamily)
	for len(data) > 0 {
		length, prefixBytes := binary.Uvarint(data)
		// The pinned expfmt protobuf decoder has no allocation limit. Validate the
		// prefix against remaining bytes before giving a bounded slice to proto.
		if prefixBytes <= 0 || length == 0 || length > uint64(len(data)-prefixBytes) {
			return nil, errors.New("EC2_STATE_INPUT: invalid protobuf frame length")
		}
		end := prefixBytes + int(length)
		family := &dto.MetricFamily{}
		if err := proto.Unmarshal(data[prefixBytes:end], family); err != nil || family.GetName() == "" {
			return nil, errors.New("EC2_STATE_INPUT: invalid protobuf metric family")
		}
		if _, exists := families[family.GetName()]; exists {
			return nil, errors.New("EC2_STATE_INPUT: duplicate protobuf metric family")
		}
		families[family.GetName()] = family
		data = data[end:]
	}
	return families, nil
}
