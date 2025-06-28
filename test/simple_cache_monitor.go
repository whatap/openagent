package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

// ConfigMapCacheMonitor monitors the Informer cache for a ConfigMap
type ConfigMapCacheMonitor struct {
	clientset      *kubernetes.Clientset
	configMapStore cache.Store
	configMapName  string
	namespace      string
	stopCh         chan struct{}
}

// NewConfigMapCacheMonitor creates a new monitor instance
func NewConfigMapCacheMonitor() (*ConfigMapCacheMonitor, error) {
	// Setup Kubernetes client for local-minikube
	var config *rest.Config
	var err error

	// Try in-cluster config first
	log.Printf("[SETUP] Trying in-cluster configuration...")
	config, err = rest.InClusterConfig()
	if err != nil {
		log.Printf("[SETUP] Not in cluster, error: %v", err)
		log.Printf("[SETUP] Falling back to kubeconfig...")

		// Fall back to kubeconfig
		home := os.Getenv("HOME")
		kubeconfigPath := filepath.Join(home, ".kube", "config")
		log.Printf("[SETUP] Using kubeconfig at: %s", kubeconfigPath)

		// Check if kubeconfig exists
		if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("kubeconfig file not found at %s", kubeconfigPath)
		}

		// Set shorter timeout for client operations
		clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
			&clientcmd.ConfigOverrides{})

		config, err = clientConfig.ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes config from %s: %v", kubeconfigPath, err)
		}

		// Set shorter timeouts
		config.Timeout = 10 * time.Second
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %v", err)
	}

	return &ConfigMapCacheMonitor{
		clientset:     clientset,
		configMapName: "test-config",
		namespace:     "default",
		stopCh:        make(chan struct{}),
	}, nil
}

// StartInformer starts the ConfigMap informer
func (m *ConfigMapCacheMonitor) StartInformer() error {
	log.Printf("[SETUP] Creating informer factory...")

	// Create informer factory with shorter resync period
	factory := informers.NewSharedInformerFactory(m.clientset, 10*time.Second)

	log.Printf("[SETUP] Creating ConfigMap informer...")
	// Create ConfigMap informer
	configMapInformer := factory.Core().V1().ConfigMaps().Informer()
	m.configMapStore = configMapInformer.GetStore()

	log.Printf("[SETUP] Starting informer...")
	// Start the informer
	go configMapInformer.Run(m.stopCh)

	log.Printf("[SETUP] Waiting for cache to sync (timeout: 30s)...")
	// Wait for cache to sync with timeout
	synced := cache.WaitForCacheSync(m.stopCh, configMapInformer.HasSynced)
	if !synced {
		return fmt.Errorf("timed out waiting for cache to sync after 30 seconds")
	}

	log.Printf("[SETUP] ConfigMap informer started and cache synced successfully")

	// List all ConfigMaps in the store to verify
	storeItems := m.configMapStore.List()
	log.Printf("[SETUP] Cache contains %d ConfigMap objects", len(storeItems))

	return nil
}

// MonitorCacheValue monitors the cache value every 10 seconds
func (m *ConfigMapCacheMonitor) MonitorCacheValue() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	log.Printf("[MONITOR] Starting cache monitoring every 10 seconds...")
	log.Printf("[MONITOR] ConfigMap name: %s, namespace: %s", m.configMapName, m.namespace)

	// Get initial cache value
	log.Printf("[MONITOR] Initial cache value:")
	m.checkCacheValue()

	for {
		select {
		case <-ticker.C:
			log.Printf("[MONITOR] Checking cache value at %s:", time.Now().Format("15:04:05"))
			m.checkCacheValue()
		case <-m.stopCh:
			log.Printf("[MONITOR] Cache monitoring stopped")
			return
		}
	}
}

// checkCacheValue checks the current value in the informer cache
func (m *ConfigMapCacheMonitor) checkCacheValue() {
	// Get ConfigMap from informer cache
	key := fmt.Sprintf("%s/%s", m.namespace, m.configMapName)
	log.Printf("[CACHE_CHECK] Looking for ConfigMap with key: %s", key)

	// List all items in store first (for debugging)
	allItems := m.configMapStore.List()
	log.Printf("[CACHE_CHECK] Total items in cache: %d", len(allItems))

	// Print first few items for verification
	if len(allItems) > 0 {
		log.Printf("[CACHE_CHECK] First few items in cache:")
		maxItems := 3
		if len(allItems) < maxItems {
			maxItems = len(allItems)
		}

		for i := 0; i < maxItems; i++ {
			cm := allItems[i].(*corev1.ConfigMap)
			log.Printf("[CACHE_CHECK]   - %s/%s", cm.Namespace, cm.Name)
		}
	}

	// Try to get our specific ConfigMap
	obj, exists, err := m.configMapStore.GetByKey(key)

	if err != nil {
		log.Printf("[CACHE_CHECK] Error getting ConfigMap from cache: %v", err)
		return
	}

	if !exists {
		log.Printf("[CACHE_CHECK] ConfigMap %s not found in cache", key)

		// Try to list all ConfigMaps in the target namespace
		var nsItems []interface{}
		for _, item := range allItems {
			cm := item.(*corev1.ConfigMap)
			if cm.Namespace == m.namespace {
				nsItems = append(nsItems, item)
			}
		}

		log.Printf("[CACHE_CHECK] Found %d ConfigMaps in namespace %s", len(nsItems), m.namespace)
		if len(nsItems) > 0 {
			log.Printf("[CACHE_CHECK] Available ConfigMaps in namespace %s:", m.namespace)
			for _, item := range nsItems {
				cm := item.(*corev1.ConfigMap)
				log.Printf("[CACHE_CHECK]   - %s", cm.Name)
			}
		}

		return
	}

	// Successfully found the ConfigMap
	cm := obj.(*corev1.ConfigMap)
	log.Printf("[CACHE_CHECK] Found ConfigMap %s/%s in cache", cm.Namespace, cm.Name)
	log.Printf("[CACHE_CHECK] Cache data:")

	if len(cm.Data) == 0 {
		log.Printf("[CACHE_CHECK]   No data in ConfigMap")
	} else {
		// Print all data in the ConfigMap
		for key, value := range cm.Data {
			log.Printf("[CACHE_CHECK]   %s: %s", key, value)
		}
	}
}

// Stop stops the monitor
func (m *ConfigMapCacheMonitor) Stop() {
	close(m.stopCh)
}

func main() {
	log.Printf("=== Simple ConfigMap Cache Monitor ===")
	log.Printf("This program monitors the Informer cache for a ConfigMap without updating it")
	log.Printf("Target: ConfigMap '%s' in namespace '%s'", "test-config", "default")
	log.Printf("Monitoring interval: 10 seconds")

	// Set up logging with timestamps
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// Create monitor instance
	log.Printf("[MAIN] Creating monitor instance...")
	monitor, err := NewConfigMapCacheMonitor()
	if err != nil {
		log.Fatalf("[MAIN] Failed to create monitor: %v", err)
	}
	log.Printf("[MAIN] Monitor instance created successfully")

	// Start informer
	log.Printf("[MAIN] Starting informer...")
	if err := monitor.StartInformer(); err != nil {
		log.Fatalf("[MAIN] Failed to start informer: %v", err)
	}
	log.Printf("[MAIN] Informer started successfully")

	// Start monitoring cache values
	log.Printf("[MAIN] Starting cache value monitoring...")
	go monitor.MonitorCacheValue()
	log.Printf("[MAIN] Cache monitoring started in background")

	// Run for 5 minutes or until interrupted
	log.Printf("[MAIN] Running for 5 minutes or until Ctrl+C is pressed...")
	log.Printf("[MAIN] The program will periodically check the cache value without updating it")

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Wait for interrupt or timeout
	select {
	case <-time.After(5 * time.Minute):
		log.Printf("[MONITOR] Monitoring completed after 5 minutes")
	case sig := <-sigCh:
		log.Printf("[MONITOR] Received signal: %v", sig)
	}

	// Cleanup
	monitor.Stop()
	log.Printf("[MONITOR] Monitor stopped")
}
