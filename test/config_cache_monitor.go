package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

// ConfigMapCacheTest tests if Informer cache automatically syncs without handlers
type ConfigMapCacheTest struct {
	clientset      *kubernetes.Clientset
	configMapStore cache.Store
	configMapName  string
	namespace      string
	stopCh         chan struct{}
}

// NewConfigMapCacheTest creates a new test instance
func NewConfigMapCacheTest() (*ConfigMapCacheTest, error) {
	// Setup Kubernetes client for local-minikube
	var config *rest.Config
	var err error

	// Try in-cluster config first
	config, err = rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		home := os.Getenv("HOME")
		kubeconfigPath := filepath.Join(home, ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes config: %v", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %v", err)
	}

	return &ConfigMapCacheTest{
		clientset:     clientset,
		configMapName: "test-config",
		namespace:     "default",
		stopCh:        make(chan struct{}),
	}, nil
}

// CreateTestConfigMap creates the test-config ConfigMap
func (t *ConfigMapCacheTest) CreateTestConfigMap() error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      t.configMapName,
			Namespace: t.namespace,
		},
		Data: map[string]string{
			"test-data": fmt.Sprintf("initial-value-%d", time.Now().Unix()),
			"timestamp": time.Now().Format("2006-01-02 15:04:05"),
		},
	}

	_, err := t.clientset.CoreV1().ConfigMaps(t.namespace).Create(context.TODO(), configMap, metav1.CreateOptions{})
	if err != nil {
		// If already exists, update it
		_, err = t.clientset.CoreV1().ConfigMaps(t.namespace).Update(context.TODO(), configMap, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create/update test ConfigMap: %v", err)
		}
	}

	log.Printf("[TEST] Created/Updated test-config ConfigMap with initial data")
	return nil
}

// StartInformer starts the ConfigMap informer and cache monitoring
func (t *ConfigMapCacheTest) StartInformer() error {
	// Create informer factory
	factory := informers.NewSharedInformerFactory(t.clientset, 30*time.Second)

	// Create ConfigMap informer
	configMapInformer := factory.Core().V1().ConfigMaps().Informer()
	t.configMapStore = configMapInformer.GetStore()
	log.Printf("t.configMapStore=%v", t.configMapStore)
	// Add event handlers for debugging (optional - to see when events occur)
	//configMapInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
	//	AddFunc: func(obj interface{}) {
	//		cm := obj.(*corev1.ConfigMap)
	//		if cm.Name == t.configMapName && cm.Namespace == t.namespace {
	//			log.Printf("[HANDLER] ADD event for %s/%s", cm.Namespace, cm.Name)
	//		}
	//	},
	//	UpdateFunc: func(oldObj, newObj interface{}) {
	//		oldCM := oldObj.(*corev1.ConfigMap)
	//		newCM := newObj.(*corev1.ConfigMap)
	//		if newCM.Name == t.configMapName && newCM.Namespace == t.namespace {
	//			log.Printf("[HANDLER] UPDATE event for %s/%s", newCM.Namespace, newCM.Name)
	//			if oldCM.Data["test-data"] != newCM.Data["test-data"] {
	//				log.Printf("[HANDLER] Data changed: %s -> %s", oldCM.Data["test-data"], newCM.Data["test-data"])
	//			}
	//		}
	//	},
	//	DeleteFunc: func(obj interface{}) {
	//		cm := obj.(*corev1.ConfigMap)
	//		if cm.Name == t.configMapName && cm.Namespace == t.namespace {
	//			log.Printf("[HANDLER] DELETE event for %s/%s", cm.Namespace, cm.Name)
	//		}
	//	},
	//})

	// Start the informer
	go configMapInformer.Run(t.stopCh)

	// Wait for cache to sync
	if !cache.WaitForCacheSync(t.stopCh, configMapInformer.HasSynced) {
		return fmt.Errorf("timed out waiting for cache to sync")
	}

	log.Printf("[TEST] ConfigMap informer started and cache synced")
	return nil
}

// MonitorCacheChanges monitors the cache every 10 seconds to see if it reflects changes
func (t *ConfigMapCacheTest) MonitorCacheChanges() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	log.Printf("[TEST] Starting cache monitoring every 10 seconds...")
	log.Printf("[TEST] ConfigMap name: %s, namespace: %s", t.configMapName, t.namespace)

	for {
		select {
		case <-ticker.C:
			t.checkCacheValue()
		case <-t.stopCh:
			log.Printf("[TEST] Cache monitoring stopped")
			return
		}
	}
}

// checkCacheValue checks the current value in the informer cache
func (t *ConfigMapCacheTest) checkCacheValue() {
	// Get ConfigMap from informer cache
	key := fmt.Sprintf("%s/%s", t.namespace, t.configMapName)
	obj, exists, err := t.configMapStore.GetByKey(key)

	if err != nil {
		log.Printf("[CACHE_CHECK] Error getting ConfigMap from cache: %v", err)
		return
	}

	if !exists {
		log.Printf("[CACHE_CHECK] ConfigMap %s not found in cache", key)
		return
	}

	cm := obj.(*corev1.ConfigMap)
	testData := cm.Data["test-data"]
	timestamp := cm.Data["timestamp"]

	log.Printf("[CACHE_CHECK] Cache value - test-data: %s, timestamp: %s", testData, timestamp)

	// Also get directly from API server for comparison
	apiCM, err := t.clientset.CoreV1().ConfigMaps(t.namespace).Get(context.TODO(), t.configMapName, metav1.GetOptions{})
	if err != nil {
		log.Printf("[API_CHECK] Error getting ConfigMap from API: %v", err)
		return
	}

	apiTestData := apiCM.Data["test-data"]
	apiTimestamp := apiCM.Data["timestamp"]

	log.Printf("[API_CHECK] API value - test-data: %s, timestamp: %s", apiTestData, apiTimestamp)

	// Compare cache vs API
	if testData == apiTestData && timestamp == apiTimestamp {
		log.Printf("[SYNC_STATUS] ✅ Cache and API are in sync")
	} else {
		log.Printf("[SYNC_STATUS] ❌ Cache and API are NOT in sync!")
		log.Printf("[SYNC_STATUS] Cache: test-data=%s, timestamp=%s", testData, timestamp)
		log.Printf("[SYNC_STATUS] API: test-data=%s, timestamp=%s", apiTestData, apiTimestamp)
	}
}


// Stop stops the test
func (t *ConfigMapCacheTest) Stop() {
	close(t.stopCh)
}

// CleanupTestConfigMap deletes the test ConfigMap
func (t *ConfigMapCacheTest) CleanupTestConfigMap() error {
	err := t.clientset.CoreV1().ConfigMaps(t.namespace).Delete(context.TODO(), t.configMapName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete test ConfigMap: %v", err)
	}
	log.Printf("[TEST] Deleted test-config ConfigMap")
	return nil
}

func main() {
	log.Printf("=== ConfigMap Cache Sync Test ===")
	log.Printf("This test verifies if Informer cache automatically syncs without handlers")

	// Create test instance
	test, err := NewConfigMapCacheTest()
	if err != nil {
		log.Fatalf("Failed to create test: %v", err)
	}

	// Create test ConfigMap
	if err := test.CreateTestConfigMap(); err != nil {
		log.Fatalf("Failed to create test ConfigMap: %v", err)
	}

	// Start informer
	if err := test.StartInformer(); err != nil {
		log.Fatalf("Failed to start informer: %v", err)
	}

	// Start cache monitoring in background
	go test.MonitorCacheChanges()

	// Wait a bit for initial sync
	time.Sleep(10 * time.Second)

	// Simulate ConfigMap updates every 20 seconds
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				}
			case <-test.stopCh:
				return
			}
		}
	}()

	// Run for 2 minutes
	log.Printf("[TEST] Running test for 2 minutes...")
	log.Printf("[TEST] You can manually update the ConfigMap using:")
	log.Printf("[TEST] kubectl patch configmap test-config -n default --patch '{\"data\":{\"test-data\":\"manual-update-%d\",\"timestamp\":\"%s\"}}'", time.Now().Unix(), time.Now().Format("2006-01-02 15:04:05"))

	time.Sleep(2 * time.Minute)

	// Cleanup
	log.Printf("[TEST] Test completed, cleaning up...")
	test.Stop()

}
