package kube

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// verify at compile time that Client satisfies inject.Backend signatures
var _ interface {
	GetField(string, string) (string, error)
	ListFields(string) ([]string, error)
} = (*Client)(nil)

// Client fetches Kubernetes secrets via kubectl.
// Vault and Kube must not be nil.
type Client struct {
	run func(name string, args ...string) ([]byte, error)
}

// NewClient returns a Client using the system kubectl and current kubeconfig context.
func NewClient() *Client {
	return &Client{run: func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).Output()
	}}
}

// NewClientWithRunner returns a Client using the given command runner. For testing only.
func NewClientWithRunner(run func(string, ...string) ([]byte, error)) *Client {
	return &Client{run: run}
}

type secretJSON struct {
	Data map[string]string `json:"data"`
}

// GetField fetches and base64-decodes a key from a Kubernetes secret.
// path format: "<namespace>/<secret-name>"
func (c *Client) GetField(path, field string) (string, error) {
	data, err := c.fetchData(path)
	if err != nil {
		return "", err
	}
	encoded, ok := data[field]
	if !ok {
		return "", fmt.Errorf("field %q not found in secret %q", field, path)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 decode %q in secret %q: %w", field, path, err)
	}
	return string(decoded), nil
}

// ListFields returns all key names in a Kubernetes secret without revealing values.
// path format: "<namespace>/<secret-name>"
func (c *Client) ListFields(path string) ([]string, error) {
	data, err := c.fetchData(path)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	return keys, nil
}

func (c *Client) fetchData(path string) (map[string]string, error) {
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("kube: path must be <namespace>/<secret-name>, got %q", path)
	}
	namespace, name := parts[0], parts[1]

	out, err := c.run("kubectl", "get", "secret", name, "-n", namespace, "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("kubectl get secret %s -n %s: %w", name, namespace, err)
	}

	var s secretJSON
	if err := json.Unmarshal(out, &s); err != nil {
		return nil, fmt.Errorf("parsing kubectl output for %q: %w", path, err)
	}
	if s.Data == nil {
		return nil, fmt.Errorf("secret %q has no data", path)
	}
	return s.Data, nil
}
