package discovery

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestProcessPodTarget_RefreshesAddressForSameIdentity(t *testing.T) {
	sd := &ServiceDiscoveryImpl{targets: make(map[string]*Target)}
	config := DiscoveryConfig{
		TargetName: "dcgm", Type: "PodMonitor", Enabled: true,
		Endpoints: []EndpointConfig{{
			Port: "9400", Path: "/metrics", Scheme: "http", Interval: "60s", AddNodeLabel: true,
		}},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "dcgm-exporter", Namespace: "gpu-operator", UID: "old-pod"},
		Spec:       corev1.PodSpec{NodeName: "gpu-worker-a"},
		Status: corev1.PodStatus{
			PodIP: "192.0.2.10", Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		},
	}
	sd.processPodTarget(pod, config, make(map[string]bool))
	targets := sd.GetReadyTargets()
	if len(targets) != 1 {
		t.Fatalf("initial ready targets = %d, want 1", len(targets))
	}
	oldTarget := targets[0]

	// A same-name replacement can be observed between two discovery cycles.
	pod = pod.DeepCopy()
	pod.UID = "new-pod"
	pod.Status.PodIP = "192.0.2.11"
	pod.Spec.NodeName = "gpu-worker-b"
	active := make(map[string]bool)
	sd.processPodTarget(pod, config, active)
	sd.cleanupStaleTargets(active)
	targets = sd.GetReadyTargets()
	if len(targets) != 1 {
		t.Fatalf("ready targets after replacement = %d, want 1", len(targets))
	}
	newTarget := targets[0]
	if newTarget.ID != oldTarget.ID {
		t.Fatal("same-name replacement changed the target identity")
	}
	if !reflect.DeepEqual(newTarget.Metadata["endpoint"], oldTarget.Metadata["endpoint"]) {
		t.Fatal("address change unexpectedly changed the endpoint configuration")
	}
	if newTarget.URL != "http://192.0.2.11:9400/metrics" || newTarget.Labels["instance"] != "192.0.2.11:9400" || newTarget.Labels["node"] != "gpu-worker-b" {
		t.Errorf("discovery returned a stale snapshot: URL=%s labels=%v", newTarget.URL, newTarget.Labels)
	}
	if oldTarget.URL != "http://192.0.2.10:9400/metrics" || oldTarget.Labels["node"] != "gpu-worker-a" {
		t.Error("discovery mutated a snapshot potentially held by an in-flight scrape")
	}

	pod.Status.Conditions[0].Status = corev1.ConditionFalse
	sd.processPodTarget(pod, config, make(map[string]bool))
	if len(sd.GetReadyTargets()) != 0 {
		t.Error("unready pod remained a ready scrape target")
	}
	sd.cleanupStaleTargets(map[string]bool{})
	if len(sd.targets) != 0 {
		t.Error("deleted pod remained in the discovery cache")
	}
}
