package vault

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Client fetches secrets from Vault KV v2.
type Client struct {
	addr  string
	token string
}

// NewClient creates a Client using VAULT_ADDR and VAULT_TOKEN env vars.
// Falls back to ~/.vault-token if VAULT_TOKEN is not set.
func NewClient() (*Client, error) {
	addr := os.Getenv("VAULT_ADDR")
	if addr == "" {
		addr = "http://localhost:8200"
	}
	token := os.Getenv("VAULT_TOKEN")
	if token == "" {
		home, _ := os.UserHomeDir()
		data, err := os.ReadFile(home + "/.vault-token")
		if err != nil {
			return nil, fmt.Errorf("no vault token: set VAULT_TOKEN or create ~/.vault-token")
		}
		token = strings.TrimSpace(string(data))
	}
	return &Client{addr: addr, token: token}, nil
}

// NewClientFromTokenFile creates a Client reading the token from the given file path.
func NewClientFromTokenFile(tokenFile string) (*Client, error) {
	addr := os.Getenv("VAULT_ADDR")
	if addr == "" {
		addr = "http://localhost:8200"
	}
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("reading token file %s: %w", tokenFile, err)
	}
	return &Client{addr: addr, token: strings.TrimSpace(string(data))}, nil
}

type kvResponse struct {
	Data struct {
		Data map[string]string `json:"data"`
	} `json:"data"`
}

// GetField fetches field from Vault KV v2 at secret/data/<path>.
func (c *Client) GetField(path, field string) (string, error) {
	url := fmt.Sprintf("%s/v1/secret/data/%s", c.addr, path)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("X-Vault-Token", c.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("vault request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vault returned %d for path %q", resp.StatusCode, path)
	}

	body, _ := io.ReadAll(resp.Body)
	var kv kvResponse
	if err := json.Unmarshal(body, &kv); err != nil {
		return "", fmt.Errorf("parsing vault response: %w", err)
	}

	val, ok := kv.Data.Data[field]
	if !ok {
		return "", fmt.Errorf("field %q not found at path %q", field, path)
	}
	return val, nil
}

// ListFields returns all field names at a Vault path (no values).
func (c *Client) ListFields(path string) ([]string, error) {
	url := fmt.Sprintf("%s/v1/secret/data/%s", c.addr, path)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Vault-Token", c.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vault request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vault returned %d for path %q", resp.StatusCode, path)
	}

	body, _ := io.ReadAll(resp.Body)
	var kv kvResponse
	if err := json.Unmarshal(body, &kv); err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(kv.Data.Data))
	for k := range kv.Data.Data {
		keys = append(keys, k)
	}
	return keys, nil
}
