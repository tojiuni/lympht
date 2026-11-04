// Package redact masks resolved secret values in a byte stream.
//
// The runtime-substitution mode (package runner) resolves {{lympht:...}}
// placeholders only inside the process that executes the command, so the
// value never appears in the PreToolUse hook output that the harness stores
// in the session transcript. This package covers the remaining path: a
// command that prints the value it was given. Output is passed through a
// Writer that replaces the value (and its common encodings) with a fixed
// marker before it reaches the harness (tool result, background output file).
//
// Limits: only the exact value and the encodings below are caught. A command
// that reverses, splits or otherwise transforms the value defeats masking.
package redact

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/url"
	"sort"
	"strings"
)

// Marker replaces every masked value.
const Marker = "{{lympht:redacted}}"

// minLen is the shortest value that is masked; shorter ones would mask
// ordinary text.
const minLen = 4

// Variants returns the byte strings to mask for the given secret values:
// the value, its trimmed form, base64 (std/url, padded/unpadded) and
// URL-escaped forms. Longest first so a long variant is not split by a short
// one.
func Variants(values []string) [][]byte {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if len(s) >= minLen && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, v := range values {
		if len(v) < minLen {
			continue
		}
		add(v)
		add(strings.TrimSpace(v))
		for _, e := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			add(e.EncodeToString([]byte(v)))
		}
		add(url.QueryEscape(v))
		add(url.PathEscape(v))
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	b := make([][]byte, len(out))
	for i, s := range out {
		b[i] = []byte(s)
	}
	return b
}

// Writer masks variants in everything written through it. A value that spans
// two Write calls is still caught because the last maxLen-1 bytes are held
// back until more data (or Close) arrives.
type Writer struct {
	dst      io.Writer
	variants [][]byte
	hold     int
	buf      []byte
}

// NewWriter returns a masking writer over dst. With no variants it is a
// plain pass-through.
func NewWriter(dst io.Writer, variants [][]byte) *Writer {
	w := &Writer{dst: dst, variants: variants}
	for _, v := range variants {
		if len(v)-1 > w.hold {
			w.hold = len(v) - 1
		}
	}
	return w
}

func (w *Writer) mask(b []byte) []byte {
	for _, v := range w.variants {
		b = bytes.ReplaceAll(b, v, []byte(Marker))
	}
	return b
}

// Write masks p. It always reports len(p) written unless dst fails.
func (w *Writer) Write(p []byte) (int, error) {
	if len(w.variants) == 0 {
		return w.dst.Write(p)
	}
	w.buf = w.mask(append(w.buf, p...))
	if len(w.buf) > w.hold {
		n := len(w.buf) - w.hold
		if _, err := w.dst.Write(w.buf[:n]); err != nil {
			return 0, err
		}
		w.buf = append([]byte(nil), w.buf[n:]...)
	}
	return len(p), nil
}

// Close flushes the held-back tail.
func (w *Writer) Close() error {
	if len(w.buf) == 0 {
		return nil
	}
	_, err := w.dst.Write(w.mask(w.buf))
	w.buf = nil
	return err
}
