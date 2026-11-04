package redact

import (
	"bytes"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

func run(t *testing.T, values []string, chunks ...string) string {
	t.Helper()
	var out bytes.Buffer
	w := NewWriter(&out, Variants(values))
	for _, c := range chunks {
		if _, err := w.Write([]byte(c)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestMasksPlainValue(t *testing.T) {
	got := run(t, []string{"s3cretValue"}, "token=s3cretValue end\n")
	if strings.Contains(got, "s3cretValue") || !strings.Contains(got, Marker) {
		t.Fatalf("got %q", got)
	}
}

func TestMasksValueSplitAcrossWrites(t *testing.T) {
	got := run(t, []string{"s3cretValue"}, "token=s3cr", "etValue end\n")
	if strings.Contains(got, "s3cretValue") || strings.Contains(got, "s3cr"+"etValue") {
		t.Fatalf("leaked across chunks: %q", got)
	}
	if got != "token="+Marker+" end\n" {
		t.Fatalf("got %q", got)
	}
}

func TestMasksEncodings(t *testing.T) {
	v := "s3cret/Value+=x"
	for name, enc := range map[string]string{
		"std":    base64.StdEncoding.EncodeToString([]byte(v)),
		"rawurl": base64.RawURLEncoding.EncodeToString([]byte(v)),
		"query":  url.QueryEscape(v),
	} {
		got := run(t, []string{v}, "x "+enc+" y")
		if strings.Contains(got, enc) {
			t.Errorf("%s variant leaked: %q", name, got)
		}
	}
}

func TestShortValueNotMasked(t *testing.T) {
	got := run(t, []string{"abc"}, "abc abc")
	if got != "abc abc" {
		t.Fatalf("short value must not be masked: %q", got)
	}
}

func TestNoVariantsPassThrough(t *testing.T) {
	if got := run(t, nil, "hello"); got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestLongerVariantWinsOverShorter(t *testing.T) {
	// the base64 form of a value must be masked as a whole, not have the
	// plain value's characters replaced inside it first.
	v := "abcdEFGHijkl"
	enc := base64.StdEncoding.EncodeToString([]byte(v))
	got := run(t, []string{v}, enc)
	if got != Marker {
		t.Fatalf("got %q", got)
	}
}
