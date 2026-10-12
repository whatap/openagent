package ec2state

import (
	"bytes"
	"strings"
	"testing"

	"github.com/whatap/gointernal/lang/license"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/util/hash"
)

const validStateBody = "fixture_ec2_state{instanceId=\"i-0123456789abcdef0\",stateCode=\"48\",stateName=\"terminated\"} 1\n"

func TestForwardValidatesEntireLicenseListBeforeSending(t *testing.T) {
	valid := license.Build(101, bytes.Repeat([]byte{1}, 16))
	otherKey := license.Build(101, bytes.Repeat([]byte{2}, 16))
	for name, input := range map[string]string{
		"missing":                        "",
		"invalid token":                  "invalid-test-license",
		"empty entry":                    valid + ",",
		"later invalid":                  valid + ",invalid-test-license",
		"same project different license": valid + "," + otherKey,
		"zero project":                   license.Build(0, bytes.Repeat([]byte{1}, 16)),
		"negative project":               license.Build(-1, bytes.Repeat([]byte{1}, 16)),
		"empty key":                      license.Build(101, nil),
		"oversize input":                 strings.Repeat("x", 257),
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() != nil {
					t.Error("malformed license caused a panic")
				}
			}()
			settings := fixtureSettings()
			settings["ec2_state_licenses"] = input
			calls := 0
			n, err := new(Forwarder).Forward(settings, fixtureScrape(validStateBody), -123, func(_ int64, _ int64, _ *pack.ParamPack) error {
				calls++
				return nil
			})
			if n != 0 || calls != 0 || err == nil {
				t.Fatalf("invalid license list reached queue: accepted=%d calls=%d hasError=%v", n, calls, err != nil)
			}
			if err != nil && (strings.Contains(err.Error(), valid) || strings.Contains(err.Error(), otherKey)) {
				t.Fatal("license leaked into error")
			}
		})
	}
}

func TestForwardDeduplicatesExactLicenseAndKeepsHash(t *testing.T) {
	valid := license.Build(101, bytes.Repeat([]byte{1}, 16))
	settings := fixtureSettings()
	settings["ec2_state_licenses"] = " " + valid + " , " + valid + " "
	calls := 0
	n, err := new(Forwarder).Forward(settings, fixtureScrape(validStateBody), -123, func(pcode, licenseHash int64, _ *pack.ParamPack) error {
		calls++
		if pcode != 101 || licenseHash != hash.Hash64Str(valid) {
			t.Fatal("wrong destination license identity")
		}
		return nil
	})
	if err != nil || n != 1 || calls != 1 {
		t.Fatalf("accepted=%d calls=%d err=%v", n, calls, err)
	}
}
