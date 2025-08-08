package k8s

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"open-agent/tools/util/logutil"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// K8sClient is a wrapper around the Kubernetes client
type K8sClient struct {
	clientset         *kubernetes.Clientset
	podInformer       cache.SharedIndexInformer
	endpointInformer  cache.SharedIndexInformer
	serviceInformer   cache.SharedIndexInformer
	namespaceInformer cache.SharedIndexInformer
	configMapInformer cache.SharedIndexInformer
	secretInformer    cache.SharedIndexInformer
	podStore          cache.Store
	endpointStore     cache.Store
	serviceStore      cache.Store
	namespaceStore    cache.Store
	configMapStore    cache.Store
	secretStore       cache.Store
	stopCh            chan struct{}
	initialized       bool
	mu                sync.RWMutex
	configMapHandlers []func(*corev1.ConfigMap)
}

var (
	instance *K8sClient
	once     sync.Once
	// kubeconfigPath is the path to the kubeconfig file
	kubeconfigPath string
	// standaloneMode indicates if the client should skip initialization
	standaloneMode bool = false
)

// SetKubeconfigPath sets the path to the kubeconfig file
func SetKubeconfigPath(path string) {
	kubeconfigPath = path
}

// SetStandaloneMode sets the standalone mode flag
func SetStandaloneMode(standalone bool) {
	standaloneMode = standalone
}

// GetInstance returns the singleton instance of K8sClient
func GetInstance() *K8sClient {
	once.Do(func() {
		instance = &K8sClient{
			stopCh:      make(chan struct{}),
			initialized: false,
		}
		// Don't initialize if in standalone mode
		if !standaloneMode {
			instance.initialize()
		}
	})
	return instance
}

// initialize initializes the Kubernetes client and informers
func (c *K8sClient) initialize() {
	var config *rest.Config
	var err error

	// Try to use in-cluster config
	config, err = rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		kubeconfig := kubeconfigPath
		if kubeconfig == "" {
			kubeconfig = os.Getenv("KUBECONFIG")
			if kubeconfig == "" {
				home := os.Getenv("HOME")
				kubeconfig = filepath.Join(home, ".kube", "config")
			}
		}
		logutil.Infof("K8S", "Using kubeconfig: %s", kubeconfig)
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			logutil.Infof("K8S", "Error building kubeconfig: %v", err)
			return
		}
	}

	// Create the clientset
	c.clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		logutil.Infof("K8S", "Error creating Kubernetes client: %v", err)
		return
	}

	// Create a factory for informers
	factory := informers.NewSharedInformerFactory(c.clientset, 10*time.Minute)

	// Create pod informer
	c.podInformer = factory.Core().V1().Pods().Informer()
	c.podStore = c.podInformer.GetStore()

	// Create endpoint informer
	c.endpointInformer = factory.Core().V1().Endpoints().Informer()
	c.endpointStore = c.endpointInformer.GetStore()

	// Create service informer
	c.serviceInformer = factory.Core().V1().Services().Informer()
	c.serviceStore = c.serviceInformer.GetStore()

	// Create namespace informer
	c.namespaceInformer = factory.Core().V1().Namespaces().Informer()
	c.namespaceStore = c.namespaceInformer.GetStore()

	// Create configmap informer
	c.configMapInformer = factory.Core().V1().ConfigMaps().Informer()
	c.configMapStore = c.configMapInformer.GetStore()

	// Create secret informer
	c.secretInformer = factory.Core().V1().Secrets().Informer()
	c.secretStore = c.secretInformer.GetStore()

	// Add event handler for ConfigMap changes
	c.configMapInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldConfigMap := oldObj.(*corev1.ConfigMap)
			newConfigMap := newObj.(*corev1.ConfigMap)

			// Only trigger handlers if the ConfigMap data has changed
			if !configMapsEqual(oldConfigMap, newConfigMap) {
				c.handleConfigMapChange(newConfigMap)
			}
		},
	})

	// Start the informers
	go c.podInformer.Run(c.stopCh)
	go c.endpointInformer.Run(c.stopCh)
	go c.serviceInformer.Run(c.stopCh)
	go c.namespaceInformer.Run(c.stopCh)
	go c.configMapInformer.Run(c.stopCh)
	go c.secretInformer.Run(c.stopCh)

	// Wait for the caches to sync
	if !cache.WaitForCacheSync(c.stopCh,
		c.podInformer.HasSynced,
		c.endpointInformer.HasSynced,
		c.serviceInformer.HasSynced,
		c.namespaceInformer.HasSynced,
		c.configMapInformer.HasSynced,
		c.secretInformer.HasSynced) {
		logutil.Infof("K8S", "Timed out waiting for caches to sync")
		return
	}

	c.mu.Lock()
	c.initialized = true
	c.mu.Unlock()

	logutil.Infof("K8S", "Kubernetes client initialized successfully")
}

// IsInitialized returns true if the client is initialized
func (c *K8sClient) IsInitialized() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.initialized
}

// Stop stops the informers
func (c *K8sClient) Stop() {
	close(c.stopCh)
}

// configMapsEqual checks if two ConfigMaps have the same data
func configMapsEqual(cm1, cm2 *corev1.ConfigMap) bool {
	if len(cm1.Data) != len(cm2.Data) {
		return false
	}

	for k, v1 := range cm1.Data {
		if v2, ok := cm2.Data[k]; !ok || v1 != v2 {
			return false
		}
	}

	return true
}

// handleConfigMapChange calls all registered handlers for ConfigMap changes
func (c *K8sClient) handleConfigMapChange(configMap *corev1.ConfigMap) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, handler := range c.configMapHandlers {
		handler(configMap)
	}
}

// RegisterConfigMapHandler registers a handler function to be called when a ConfigMap changes
func (c *K8sClient) RegisterConfigMapHandler(handler func(*corev1.ConfigMap)) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.configMapHandlers = append(c.configMapHandlers, handler)
}

// GetConfigMap returns a ConfigMap by name and namespace
func (c *K8sClient) GetConfigMap(namespace, name string) (*corev1.ConfigMap, error) {
	if !c.IsInitialized() {
		return nil, fmt.Errorf("kubernetes client not initialized")
	}

	for _, obj := range c.configMapStore.List() {
		cm := obj.(*corev1.ConfigMap)
		if cm.Namespace == namespace && cm.Name == name {
			return cm, nil
		}
	}

	return nil, fmt.Errorf("configmap %s/%s not found", namespace, name)
}

// GetSecret returns a Secret by name and namespace
func (c *K8sClient) GetSecret(namespace, name string) (*corev1.Secret, error) {
	if !c.IsInitialized() {
		return nil, fmt.Errorf("kubernetes client not initialized")
	}

	for _, obj := range c.secretStore.List() {
		secret := obj.(*corev1.Secret)
		if secret.Namespace == namespace && secret.Name == name {
			return secret, nil
		}
	}

	return nil, fmt.Errorf("secret %s/%s not found", namespace, name)
}

// GetPodsInNamespace returns all pods in the specified namespace
func (c *K8sClient) GetPodsInNamespace(namespace string) ([]*corev1.Pod, error) {
	if !c.IsInitialized() {
		return nil, nil
	}

	var pods []*corev1.Pod
	for _, obj := range c.podStore.List() {
		pod := obj.(*corev1.Pod)
		if pod.Namespace == namespace {
			pods = append(pods, pod)
		}
	}
	return pods, nil
}

// GetPodsByLabels returns pods matching the specified labels in the specified namespace
func (c *K8sClient) GetPodsByLabels(namespace string, labelSelector map[string]string) ([]*corev1.Pod, error) {
	if !c.IsInitialized() {
		logutil.Printf("DEBUG", "K8s client not initialized for GetPodsByLabels")
		return nil, nil
	}

	selector := labels.SelectorFromSet(labelSelector)
	logutil.Printf("DEBUG", "GetPodsByLabels - namespace: %s, labelSelector: %+v, selector: %s", namespace, labelSelector, selector.String())

	var pods []*corev1.Pod
	totalPodsInStore := 0
	podsInNamespace := 0

	for _, obj := range c.podStore.List() {
		pod := obj.(*corev1.Pod)
		totalPodsInStore++

		if pod.Namespace == namespace {
			podsInNamespace++
			logutil.Printf("DEBUG", "GetPodsByLabels - Checking pod %s/%s with labels: %+v", pod.Namespace, pod.Name, pod.Labels)

			if selector.Matches(labels.Set(pod.Labels)) {
				logutil.Printf("DEBUG", "GetPodsByLabels - Pod %s/%s MATCHES selector", pod.Namespace, pod.Name)
				pods = append(pods, pod)
			} else {
				logutil.Printf("DEBUG", "GetPodsByLabels - Pod %s/%s does NOT match selector", pod.Namespace, pod.Name)
			}
		}
	}

	logutil.Printf("DEBUG", "GetPodsByLabels - Total pods in store: %d, pods in namespace %s: %d, matching pods: %d",
		totalPodsInStore, namespace, podsInNamespace, len(pods))

	return pods, nil
}

// GetServicesByLabels returns services matching the specified labels in the specified namespace
func (c *K8sClient) GetServicesByLabels(namespace string, labelSelector map[string]string) ([]*corev1.Service, error) {
	if !c.IsInitialized() {
		return nil, nil
	}

	selector := labels.SelectorFromSet(labelSelector)
	var services []*corev1.Service
	for _, obj := range c.serviceStore.List() {
		svc := obj.(*corev1.Service)
		if svc.Namespace == namespace && selector.Matches(labels.Set(svc.Labels)) {
			services = append(services, svc)
		}
	}
	return services, nil
}

// GetEndpointsForService returns endpoints for the specified service
func (c *K8sClient) GetEndpointsForService(namespace, serviceName string) (*corev1.Endpoints, error) {
	if !c.IsInitialized() {
		return nil, nil
	}

	key := namespace + "/" + serviceName
	obj, exists, err := c.endpointStore.GetByKey(key)
	if err != nil || !exists {
		return nil, err
	}
	return obj.(*corev1.Endpoints), nil
}

// GetNamespacesByLabels returns namespaces matching the specified labels
func (c *K8sClient) GetNamespacesByLabels(labelSelector map[string]string) ([]*corev1.Namespace, error) {
	if !c.IsInitialized() {
		return nil, nil
	}

	selector := labels.SelectorFromSet(labelSelector)
	var namespaces []*corev1.Namespace
	for _, obj := range c.namespaceStore.List() {
		namespace := obj.(*corev1.Namespace)
		if selector.Matches(labels.Set(namespace.Labels)) {
			namespaces = append(namespaces, namespace)
		}
	}
	return namespaces, nil
}

// GetNamespacesByNames returns namespaces with the specified names
func (c *K8sClient) GetNamespacesByNames(names []string) ([]*corev1.Namespace, error) {
	if !c.IsInitialized() {
		return nil, nil
	}

	var namespaces []*corev1.Namespace
	for _, name := range names {
		key := name
		obj, exists, err := c.namespaceStore.GetByKey(key)
		if err != nil || !exists {
			logutil.Infof("K8S", "Namespace %s not found in cache: %v", name, err)
			continue
		}
		namespaces = append(namespaces, obj.(*corev1.Namespace))
	}
	return namespaces, nil
}

// GetPodPort returns the container port for the specified port name or number
func (c *K8sClient) GetPodPort(pod *corev1.Pod, portName string) (int32, error) {
	// Try to parse the port as a number
	var port int32
	_, err := fmt.Sscanf(portName, "%d", &port)
	if err == nil {
		return port, nil
	}

	// If it's not a number, look for the port by name
	for _, container := range pod.Spec.Containers {
		for _, p := range container.Ports {
			if p.Name == portName {
				return p.ContainerPort, nil
			}
		}
	}

	return 0, fmt.Errorf("port %s not found in pod %s", portName, pod.Name)
}

// GetServicePort returns the target port for the specified port name or number
func (c *K8sClient) GetServicePort(service *corev1.Service, portName string) (int32, error) {
	// Try to parse the port as a number
	var port int32
	_, err := fmt.Sscanf(portName, "%d", &port)
	if err == nil {
		// Find the service port with this port number
		for _, p := range service.Spec.Ports {
			if p.Port == port {
				// If the target port is a number, return it
				if p.TargetPort.Type == intstr.Int {
					return p.TargetPort.IntVal, nil
				}
				// If the target port is a name, we need to look it up in the pod
				return 0, fmt.Errorf("target port is a name: %s", p.TargetPort.StrVal)
			}
		}
		return 0, fmt.Errorf("port %d not found in service %s", port, service.Name)
	}

	// If it's not a number, look for the port by name
	for _, p := range service.Spec.Ports {
		if p.Name == portName {
			// If the target port is a number, return it
			if p.TargetPort.Type == intstr.Int {
				return p.TargetPort.IntVal, nil
			}
			// If the target port is a name, we need to look it up in the pod
			return 0, fmt.Errorf("target port is a name: %s", p.TargetPort.StrVal)
		}
	}

	return 0, fmt.Errorf("port %s not found in service %s", portName, service.Name)
}
