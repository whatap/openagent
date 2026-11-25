package config

import (
	"reflect"
	"testing"

	"gopkg.in/yaml.v2"
)

func TestGetOpenAgentSection(t *testing.T) {
	var parsed map[string]interface{}
	doc := `
features:
  openAgent:
    enabled: false
    targets: []
    awsRdsEvents:
      enabled: true
      regions: [ap-northeast-2, us-east-1]
      interval: 60s
`
	if err := yaml.Unmarshal([]byte(doc), &parsed); err != nil {
		t.Fatal(err)
	}
	cm := &ConfigManager{config: parsed}

	want := map[string]interface{}{
		"enabled":  true,
		"regions":  []interface{}{"ap-northeast-2", "us-east-1"},
		"interval": "60s",
	}
	// The section carries its own enabled flag; openAgent.enabled does not gate it.
	if got := cm.GetOpenAgentSection("awsRdsEvents"); !reflect.DeepEqual(got, want) {
		t.Fatalf("section = %#v, want %#v", got, want)
	}
	if got := cm.GetOpenAgentSection("absent"); got != nil {
		t.Fatalf("absent section = %#v", got)
	}
	if got := (&ConfigManager{}).GetOpenAgentSection("awsRdsEvents"); got != nil {
		t.Fatalf("empty configuration = %#v", got)
	}
}
