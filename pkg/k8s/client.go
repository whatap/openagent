package k8s

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

// K8sClient is a wrapper around the Kubernetes client
type K8sClient struct {
	clientset         *kubernetes.Clientset
	podInformer       cache.SharedIndexInformer
	endpointInformer  cache.SharedIndexInformer
	serviceInformer   cache.SharedIndexInformer
	namespaceInformer cache.SharedIndexInformer
	podStore          cache.Store
	endpointStore     cache.Store
	serviceStore      cache.Store
	namespaceStore    cache.Store
	stopCh            chan struct{}
	initialized       bool
	mu                sync.RWMutex
}

var (
	instance *K8sClient
	once     sync.Once
	// kubeconfigPath is the path to the kubeconfig file
	kubeconfigPath string
)

// SetKubeconfigPath sets the path to the kubeconfig file
func SetKubeconfigPath(path string) {
	kubeconfigPath = path
}

// GetInstance returns the singleton instance of K8sClient
func GetInstance() *K8sClient {
	once.Do(func() {
		instance = &K8sClient{
			stopCh:      make(chan struct{}),
			initialized: false,
		}
		instance.initialize()
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
		log.Printf("Using kubeconfig: %s", kubeconfig)
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			log.Printf("Error building kubeconfig: %v", err)
			return
		}
	}

	// Create the clientset
	c.clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		log.Printf("Error creating Kubernetes client: %v", err)
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

	// Start the informers
	go c.podInformer.Run(c.stopCh)
	go c.endpointInformer.Run(c.stopCh)
	go c.serviceInformer.Run(c.stopCh)
	go c.namespaceInformer.Run(c.stopCh)

	// Wait for the caches to sync
	if !cache.WaitForCacheSync(c.stopCh, c.podInformer.HasSynced, c.endpointInformer.HasSynced, c.serviceInformer.HasSynced, c.namespaceInformer.HasSynced) {
		log.Println("Timed out waiting for caches to sync")
		return
	}

	c.mu.Lock()
	c.initialized = true
	c.mu.Unlock()

	log.Println("Kubernetes client initialized successfully")
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
		return nil, nil
	}

	selector := labels.SelectorFromSet(labelSelector)
	var pods []*corev1.Pod
	for _, obj := range c.podStore.List() {
		pod := obj.(*corev1.Pod)
		if pod.Namespace == namespace && selector.Matches(labels.Set(pod.Labels)) {
			pods = append(pods, pod)
		}
	}
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
			log.Printf("Namespace %s not found in cache: %v", name, err)
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
