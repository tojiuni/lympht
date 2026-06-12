package kube_test

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/tojiuni/lympht/internal/kube"
)

func fakeRun(payload string) func(string, ...string) ([]byte, error) {
	return func(name string, args ...string) ([]byte, error) {
		return []byte(payload), nil
	}
}

func encoded(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func TestGetField_ReturnsDecodedValue(t *testing.T) {
	payload := fmt.Sprintf(`{"data":{"access-key":%q,"secret-key":%q}}`,
		encoded("AKID123"), encoded("superSecret"))

	client := kube.NewClientWithRunner(fakeRun(payload))

	val, err := client.GetField("cogito-svc/cogito-s3", "access-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "AKID123" {
		t.Errorf("got %q, want %q", val, "AKID123")
	}
}

func TestGetField_MissingFieldReturnsError(t *testing.T) {
	payload := fmt.Sprintf(`{"data":{"other-key":%q}}`, encoded("val"))
	client := kube.NewClientWithRunner(fakeRun(payload))

	_, err := client.GetField("ns/secret", "missing-field")
	if err == nil {
		t.Fatal("expected error for missing field")
	}
}

func TestGetField_InvalidPathReturnsError(t *testing.T) {
	client := kube.NewClientWithRunner(fakeRun(`{}`))

	_, err := client.GetField("no-slash", "key") // missing namespace/name separator
	if err == nil {
		t.Fatal("expected error for malformed path")
	}
}

func TestGetField_KubectlErrorReturnsError(t *testing.T) {
	client := kube.NewClientWithRunner(func(string, ...string) ([]byte, error) {
		return nil, fmt.Errorf("exit status 1")
	})

	_, err := client.GetField("ns/secret", "key")
	if err == nil {
		t.Fatal("expected error when kubectl fails")
	}
}

func TestListFields_ReturnsKeys(t *testing.T) {
	payload := fmt.Sprintf(`{"data":{"key1":%q,"key2":%q}}`, encoded("a"), encoded("b"))
	client := kube.NewClientWithRunner(fakeRun(payload))

	fields, err := client.ListFields("ns/mysecret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fields) != 2 {
		t.Errorf("got %d fields, want 2: %v", len(fields), fields)
	}
}

func TestGetField_KubectlArgsCorrect(t *testing.T) {
	payload := fmt.Sprintf(`{"data":{"k":%q}}`, encoded("v"))
	var gotArgs []string
	client := kube.NewClientWithRunner(func(name string, args ...string) ([]byte, error) {
		gotArgs = append([]string{name}, args...)
		return []byte(payload), nil
	})

	_, err := client.GetField("mynamespace/mysecret", "k")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"kubectl", "get", "secret", "mysecret", "-n", "mynamespace", "-o", "json"}
	for i, w := range want {
		if i >= len(gotArgs) || gotArgs[i] != w {
			t.Errorf("args[%d] = %q, want %q (full: %v)", i, gotArgs[i], w, gotArgs)
		}
	}
}
