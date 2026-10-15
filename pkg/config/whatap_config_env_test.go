package config

import (
	"reflect"
	"testing"
)

func TestConfigMapWithEnvPrefixUsesEnvironmentOnlyAsFallback(t *testing.T) {
	wc := &WhatapConfig{values: map[string]string{
		"debug":              "true",
		"ec2_state_licenses": "",
		"ec2_state_param_id": "1002",
	}}
	t.Setenv("ec2_state_forwarding_enabled", "true") // absent from whatap.conf
	t.Setenv("ec2_state_licenses", "from-env")       // empty in whatap.conf, as with Get
	t.Setenv("ec2_state_param_id", "56")             // a non-empty whatap.conf entry wins
	t.Setenv("unrelated_secret", "must-not-leak")

	want := map[string]string{
		"debug":                        "true",
		"ec2_state_forwarding_enabled": "true",
		"ec2_state_licenses":           "from-env",
		"ec2_state_param_id":           "1002",
	}
	if got := wc.GetConfigMapWithEnvPrefix("ec2_state_"); !reflect.DeepEqual(got, want) {
		t.Fatalf("settings = %v, want %v", got, want)
	}
	// Remote configuration views read GetConfigMap; container ENV must not appear there.
	fileOnly := map[string]string{"debug": "true", "ec2_state_licenses": "", "ec2_state_param_id": "1002"}
	if got := wc.GetConfigMap(); !reflect.DeepEqual(got, fileOnly) {
		t.Fatalf("whatap.conf snapshot absorbed environment values: %v", got)
	}
	if _, leaked := wc.GetConfigMapWithEnvPrefix("")["unrelated_secret"]; leaked {
		t.Fatal("an empty prefix imported the whole process environment")
	}
}
