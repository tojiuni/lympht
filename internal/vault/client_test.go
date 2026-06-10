package vault_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/tojiuni/lympht/internal/vault"
)

func TestGetField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "test-token" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.URL.Path != "/v1/secret/data/neunexus/foo" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"data": map[string]string{
					"password": "secret123",
				},
			},
		})
	}))
	defer srv.Close()

	t.Setenv("VAULT_ADDR", srv.URL)
	t.Setenv("VAULT_TOKEN", "test-token")

	client, err := vault.NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	val, err := client.GetField("neunexus/foo", "password")
	if err != nil {
		t.Fatalf("GetField: %v", err)
	}
	if val != "secret123" {
		t.Errorf("got %q, want %q", val, "secret123")
	}
}

func TestGetField_MissingField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"data": map[string]string{"other": "value"},
			},
		})
	}))
	defer srv.Close()

	t.Setenv("VAULT_ADDR", srv.URL)
	t.Setenv("VAULT_TOKEN", "test-token")

	client, _ := vault.NewClient()
	_, err := client.GetField("neunexus/foo", "missing")
	if err == nil {
		t.Fatal("expected error for missing field")
	}
}

func TestNewClient_TokenFromFile(t *testing.T) {
	f, _ := os.CreateTemp("", "vault-token")
	f.WriteString("file-token")
	f.Close()
	defer os.Remove(f.Name())

	os.Unsetenv("VAULT_TOKEN")
	t.Setenv("VAULT_ADDR", "http://127.0.0.1:1")
	_, err := vault.NewClientFromTokenFile(f.Name())
	if err != nil {
		t.Fatalf("NewClientFromTokenFile: %v", err)
	}
}
