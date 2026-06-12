package inject

import (
	"fmt"
	"sort"
	"strings"
)

// Lister lists available field names at a path without revealing values.
type Lister interface {
	ListFields(path string) ([]string, error)
}

// Backend is a secret backend that can fetch fields and list keys.
type Backend interface {
	Fetcher
	Lister
}

// MultiFetcher routes GetField and ListFields to vault or kube backend
// based on the path prefix ("vault:" or "k8s:").
// Vault and Kube must not be nil.
type MultiFetcher struct {
	Vault Backend
	Kube  Backend
}

// GetField implements Fetcher.
func (m *MultiFetcher) GetField(path, field string) (string, error) {
	backend, stripped, err := m.resolve(path)
	if err != nil {
		return "", err
	}
	return backend.GetField(stripped, field)
}

// ListFields implements Lister. Returns sorted field names.
func (m *MultiFetcher) ListFields(path string) ([]string, error) {
	backend, stripped, err := m.resolve(path)
	if err != nil {
		return nil, err
	}
	fields, err := backend.ListFields(stripped)
	if err != nil {
		return nil, err
	}
	sort.Strings(fields)
	return fields, nil
}

func (m *MultiFetcher) resolve(path string) (Backend, string, error) {
	switch {
	case strings.HasPrefix(path, "vault:"):
		return m.Vault, strings.TrimPrefix(path, "vault:"), nil
	case strings.HasPrefix(path, "k8s:"):
		return m.Kube, strings.TrimPrefix(path, "k8s:"), nil
	default:
		return nil, "", fmt.Errorf("lympht: unknown backend in path %q — use vault: or k8s: prefix", path)
	}
}
