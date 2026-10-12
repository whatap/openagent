package ec2state

import (
	"errors"
	"fmt"
	"strings"

	"github.com/whatap/gointernal/lang/license"
	wio "github.com/whatap/golib/io"
	"github.com/whatap/golib/util/hash"
	"github.com/whatap/golib/util/hexa32"
)

const (
	maxDestinations    = 64
	maxLicenseLength   = 256
	maxLicenseKeyBytes = 64
	licenseWordBytes   = 8
)

type destination struct {
	pcode       int64
	licenseHash int64
}

func parseDestinations(input string) ([]destination, error) {
	if len(input) > maxDestinations*(maxLicenseLength+1) {
		return nil, errors.New("EC2_STATE_CONFIG: license list too large")
	}
	entries := strings.Split(input, ",")
	if len(entries) > maxDestinations {
		return nil, errors.New("EC2_STATE_CONFIG: too many destination licenses")
	}
	seen := make(map[int64]string)
	result := make([]destination, 0, len(entries))
	for i, entry := range entries {
		lic := strings.TrimSpace(entry)
		pcode, err := parseLicense(lic)
		if err != nil {
			// Never include either the credential or an underlying decoder error.
			return nil, fmt.Errorf("EC2_STATE_CONFIG: invalid destination license at position %d", i+1)
		}
		if previous, exists := seen[pcode]; exists {
			if previous != lic {
				return nil, errors.New("EC2_STATE_CONFIG: conflicting licenses for one project")
			}
			continue
		}
		seen[pcode] = lic
		licenseHash := hash.Hash64Str(lic)
		if licenseHash == 0 {
			return nil, errors.New("EC2_STATE_CONFIG: invalid destination license hash")
		}
		result = append(result, destination{pcode: pcode, licenseHash: licenseHash})
	}
	return result, nil
}

// parseLicense validates the bounded encoding before calling the legacy decoder.
// License.Parse's ReadBlob otherwise trusts an attacker-controlled allocation size.
// This checks structure, not the collector's authentication/authorization decision.
func parseLicense(lic string) (pcode int64, err error) {
	invalid := errors.New("invalid license encoding")
	if len(lic) == 0 || len(lic) > maxLicenseLength {
		return 0, invalid
	}
	out := wio.NewDataOutputX()
	for _, token := range strings.Split(lic, "-") {
		word := hexa32.ToLong32(token)
		if token == "" || hexa32.ToString32(word) != token {
			return 0, invalid
		}
		out.WriteLong(word)
	}
	encoded := out.ToByteArray()
	decimalBytes := int(encoded[0])
	if decimalBytes < 1 || (decimalBytes > 5 && decimalBytes != 8) {
		return 0, invalid
	}
	keyOffset := 1 + decimalBytes
	if keyOffset >= len(encoded) {
		return 0, invalid
	}
	keyBytes := int(encoded[keyOffset])
	if keyBytes < 1 || keyBytes > maxLicenseKeyBytes {
		return 0, invalid
	}
	remaining := len(encoded) - keyOffset - 1
	if keyBytes > remaining || remaining-keyBytes >= licenseWordBytes {
		return 0, invalid
	}
	pcode, _ = license.Parse(lic)
	if pcode <= 0 {
		return 0, invalid
	}
	return pcode, nil
}
