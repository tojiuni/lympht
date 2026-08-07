package scope

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withTempState points LYMPHT_STATE_DIR at a fresh temp dir for the
// duration of the test, so tests never touch the real user cache dir and
// never see state left over from another test.
func withTempState(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LYMPHT_STATE_DIR", dir)
}

func TestExtractRequestedProvider(t *testing.T) {
	cases := []struct {
		name     string
		cmd      string
		wantProv string
		wantOK   bool
	}{
		{
			name: "realistic tokenhub credentials fetch",
			cmd: `TOKENHUB="http://tokenhub.ai-assistant.svc.cluster.local:8300"
CRED=$(curl -sf -X POST $TOKENHUB/v1/credentials -H "Authorization: Bearer $JWT" -H "Content-Type: application/json" -d '{"provider":"k8s-db-operator"}')`,
			wantProv: "k8s-db-operator",
			wantOK:   true,
		},
		{
			name:     "elevation-request credential fetch",
			cmd:      `curl -s "$TOKENHUB/v1/elevation-requests/$ELEV_ID/credential" -H "Authorization: Bearer $JWT"`,
			wantProv: "",
			wantOK:   false, // no "provider" field in THIS particular call — the request that created it had it
		},
		{
			name:     "unrelated command mentioning provider word",
			cmd:      `echo "cloud provider comparison"`,
			wantProv: "",
			wantOK:   false,
		},
		{
			name:     "provider field without tokenhub mention",
			cmd:      `curl https://example.com -d '{"provider":"aws"}'`,
			wantProv: "",
			wantOK:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ExtractRequestedProvider(tc.cmd)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if got != tc.wantProv {
				t.Errorf("provider = %q, want %q", got, tc.wantProv)
			}
		})
	}
}

func TestRecordIfProviderRequested_ThenActiveProviders(t *testing.T) {
	withTempState(t)

	RecordIfProviderRequested(`TOKENHUB="http://tokenhub.ai-assistant.svc.cluster.local:8300"; curl $TOKENHUB/v1/credentials -d '{"provider":"k8s-db-operator"}'`)

	active := ActiveProviders()
	if !active["k8s-db-operator"] {
		t.Fatalf("expected k8s-db-operator to be active, got %v", active)
	}
	if active["k8s-secret-inspector"] {
		t.Errorf("expected only the requested provider to be active, got %v", active)
	}
}

func TestActiveProviders_ExpiresAfterTTL(t *testing.T) {
	withTempState(t)

	// Seed state directly with a requestedAt older than k8s-secret-inspector's
	// 300s TTL, bypassing RecordIfProviderRequested (which always stamps
	// "now") so the test doesn't need to sleep.
	path, err := statePath()
	if err != nil {
		t.Fatalf("statePath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	old := time.Now().Add(-10 * time.Minute) // well past the 300s TTL
	data, _ := json.Marshal(persistedState{Active: map[string]time.Time{"k8s-secret-inspector": old}})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	active := ActiveProviders()
	if active["k8s-secret-inspector"] {
		t.Errorf("expected an entry older than its TTL to be expired, got active=%v", active)
	}
}

func TestCheckCommand_BlocksCoreActionWithoutActiveProvider(t *testing.T) {
	withTempState(t)

	if err := CheckCommand("kubectl exec -n taxon postgres-1 -- psql"); err == nil {
		t.Fatal("expected kubectl exec to be blocked with no active elevation")
	}
}

func TestCheckCommand_AllowsCoreActionWithActiveProvider(t *testing.T) {
	withTempState(t)
	RecordIfProviderRequested(`tokenhub curl -d '{"provider":"k8s-db-operator"}'`)

	if err := CheckCommand("kubectl exec -n taxon postgres-1 -- psql"); err != nil {
		t.Errorf("expected kubectl exec to be allowed with k8s-db-operator active, got: %v", err)
	}
}

func TestCheckCommand_WrongProviderStillBlocks(t *testing.T) {
	withTempState(t)
	// k8s-secret-inspector doesn't grant exec.
	RecordIfProviderRequested(`tokenhub curl -d '{"provider":"k8s-secret-inspector"}'`)

	if err := CheckCommand("kubectl exec -n taxon postgres-1 -- psql"); err == nil {
		t.Fatal("expected kubectl exec to stay blocked when only an unrelated provider is active")
	}
}

func TestCheckCommand_GenericReadsNeverGated(t *testing.T) {
	withTempState(t)
	// No elevation active at all.
	cases := []string{
		"kubectl get pods -n tekton-pipelines",
		"kubectl describe pod foo -n iam",
		"kubectl logs foo -n iam",
		"kubectl get pipelinerun toji-ci-lympht-abc -n tekton-pipelines",
	}
	for _, cmd := range cases {
		if err := CheckCommand(cmd); err != nil {
			t.Errorf("expected %q to be ungated (ambient RBAC scope), got: %v", cmd, err)
		}
	}
}

func TestCheckCommand_GetSecretGatedBySecretInspector(t *testing.T) {
	withTempState(t)

	if err := CheckCommand("kubectl get secret iam-db -n iam"); err == nil {
		t.Fatal("expected kubectl get secret to be blocked with no active elevation")
	}

	RecordIfProviderRequested(`tokenhub curl -d '{"provider":"k8s-secret-inspector"}'`)
	if err := CheckCommand("kubectl get secret iam-db -n iam"); err != nil {
		t.Errorf("expected kubectl get secret to be allowed with k8s-secret-inspector active, got: %v", err)
	}
}
