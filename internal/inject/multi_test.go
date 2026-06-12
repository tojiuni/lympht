package inject_test

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/tojiuni/lympht/internal/inject"
)

type stubBackend struct {
	data map[string]string // "path#field" → value
}

func (s *stubBackend) GetField(path, field string) (string, error) {
	val, ok := s.data[path+"#"+field]
	if !ok {
		return "", fmt.Errorf("not found: %s#%s", path, field)
	}
	return val, nil
}

func (s *stubBackend) ListFields(path string) ([]string, error) {
	prefix := path + "#"
	var fields []string
	for k := range s.data {
		if strings.HasPrefix(k, prefix) {
			fields = append(fields, strings.TrimPrefix(k, prefix))
		}
	}
	sort.Strings(fields)
	return fields, nil
}

func TestMultiFetcher_GetField_Vault(t *testing.T) {
	vaultB := &stubBackend{data: map[string]string{"neunexus/foo#password": "secret"}}
	multi := &inject.MultiFetcher{Vault: vaultB, Kube: &stubBackend{}}

	val, err := multi.GetField("vault:neunexus/foo", "password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "secret" {
		t.Errorf("got %q, want %q", val, "secret")
	}
}

func TestMultiFetcher_GetField_Kube(t *testing.T) {
	kubeB := &stubBackend{data: map[string]string{"cogito-svc/cogito-s3#access-key": "AKID"}}
	multi := &inject.MultiFetcher{Vault: &stubBackend{}, Kube: kubeB}

	val, err := multi.GetField("k8s:cogito-svc/cogito-s3", "access-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "AKID" {
		t.Errorf("got %q, want %q", val, "AKID")
	}
}

func TestMultiFetcher_GetField_UnknownBackend(t *testing.T) {
	multi := &inject.MultiFetcher{Vault: &stubBackend{}, Kube: &stubBackend{}}

	_, err := multi.GetField("neunexus/foo", "password") // missing prefix
	if err == nil {
		t.Fatal("expected error for path without backend prefix")
	}
	if !strings.Contains(err.Error(), "unknown backend") {
		t.Errorf("error %q should contain 'unknown backend'", err.Error())
	}
}

func TestMultiFetcher_ListFields_Vault(t *testing.T) {
	vaultB := &stubBackend{data: map[string]string{
		"neunexus/foo#password": "s",
		"neunexus/foo#token":    "t",
	}}
	multi := &inject.MultiFetcher{Vault: vaultB, Kube: &stubBackend{}}

	fields, err := multi.ListFields("vault:neunexus/foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"password", "token"}
	if !reflect.DeepEqual(fields, want) {
		t.Errorf("got %v, want %v", fields, want)
	}
}

func TestMultiFetcher_ListFields_Kube(t *testing.T) {
	kubeB := &stubBackend{data: map[string]string{
		"myns/mysecret#key1": "v",
		"myns/mysecret#key2": "v",
	}}
	multi := &inject.MultiFetcher{Vault: &stubBackend{}, Kube: kubeB}

	fields, err := multi.ListFields("k8s:myns/mysecret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"key1", "key2"}
	if !reflect.DeepEqual(fields, want) {
		t.Errorf("got %v, want %v", fields, want)
	}
}

func TestMultiFetcher_ListFields_UnknownBackend(t *testing.T) {
	multi := &inject.MultiFetcher{Vault: &stubBackend{}, Kube: &stubBackend{}}

	_, err := multi.ListFields("neunexus/foo") // missing prefix
	if err == nil {
		t.Fatal("expected error for path without backend prefix")
	}
}
