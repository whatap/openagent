package endpoint

import (
	"sync"
)

var (
	mu        sync.Mutex
	endpoints = make(map[string]struct{})
)

// Register adds an endpoint to the registry
func Register(ep string) {
	if len(ep) == 0 {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	endpoints[ep] = struct{}{}
}

// Snapshot returns the current endpoints and resets the registry
func Snapshot() []string {
	mu.Lock()
	defer mu.Unlock()
	if len(endpoints) == 0 {
		return nil
	}
	result := make([]string, 0, len(endpoints))
	for ep := range endpoints {
		result = append(result, ep)
	}
	endpoints = make(map[string]struct{})
	return result
}
