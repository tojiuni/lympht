package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestInstall_CopiesSelfToDestAndIsExecutable verifies `lympht install
// <dest>` copies the running binary to dest and it remains a working,
// executable copy — this is the mechanism a scratch-image (no shell, no
// cp) initContainer relies on to place lympht onto a shared volume.
func TestInstall_CopiesSelfToDestAndIsExecutable(t *testing.T) {
	bin := buildBinary(t)

	dir := t.TempDir()
	dest := filepath.Join(dir, "nested", "lympht")

	cmd := exec.Command(bin, "install", dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("dest not created: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Errorf("dest is not executable: mode=%v", info.Mode())
	}

	// The installed copy must itself be a working lympht binary.
	verify := exec.Command(dest, "check", "vault:nonexistent")
	verifyOut, _ := verify.CombinedOutput()
	if len(verifyOut) == 0 {
		t.Errorf("installed binary produced no output when run")
	}
}

// TestInstall_OverwritesExistingDestAtomically confirms a second install
// (simulating a redeploy) replaces the old binary cleanly.
func TestInstall_OverwritesExistingDestAtomically(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	dest := filepath.Join(dir, "lympht")

	if err := os.WriteFile(dest, []byte("old fake binary content"), 0755); err != nil {
		t.Fatalf("seeding old dest: %v", err)
	}

	cmd := exec.Command(bin, "install", dest)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}

	installedBytes, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading dest: %v", err)
	}
	origBytes, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("reading source binary: %v", err)
	}
	if len(installedBytes) != len(origBytes) {
		t.Errorf("installed binary size = %d, want %d (source binary size)", len(installedBytes), len(origBytes))
	}
	if string(installedBytes) == "old fake binary content" {
		t.Errorf("dest was not overwritten")
	}

	// No leftover .tmp file after a successful install.
	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("expected no leftover .tmp file, stat err = %v", err)
	}
}
