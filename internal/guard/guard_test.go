package guard

import "testing"

func TestCheckCommand_Blocks(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
	}{
		{"kubectl get secret -o yaml", `kubectl get secret github-credentials -n tekton-pipelines -o yaml`},
		{"kubectl get secrets (plural) -o json", `kubectl get secrets foo -o json`},
		{"kubectl get secret --output=yaml", `kubectl get secret foo --output=yaml`},
		{"kubectl get secret -o YAML uppercase", `kubectl GET secret foo -O YAML`},
		{"vault kv get", `vault kv get secret/cloudbro/jadara`},
		{"vault kv get with flags", `vault kv get -field=password secret/neunexus/iam/accounts/lyckabc`},
		{"lympht inject", `lympht inject -- echo {{lympht:vault:ns/foo#bar}}`},
		{"credentials in git URL", `git clone https://x-access-token:ghs_abc123@github.com/tojiuni/toji-cd.git`},
		{"credentials in curl URL", `curl https://admin:hunterpassword2@vault.example.com/v1/secret`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckCommand(tc.cmd); err == nil {
				t.Errorf("expected CheckCommand to block %q, got nil error", tc.cmd)
			}
		})
	}
}

func TestCheckCommand_AllowsLegitimateUse(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
	}{
		{"jsonpath extraction redirected to file", `kubectl get secret iam-db -n iam -o jsonpath='{.data.DATABASE_URL}' | base64 -d > /tmp/dsn`},
		{"kubectl get pods", `kubectl get pods -n tekton-pipelines`},
		{"kubectl get secret without -o", `kubectl get secret iam-db -n iam`},
		{"lympht check", `lympht check vault:neunexus/iam/accounts/lyckabc`},
		{"lympht hook-intercept itself", `lympht hook-intercept`},
		{"vault kv list", `vault kv list secret/neunexus/`},
		{"plain git clone", `git clone https://github.com/tojiuni/lympht.git`},
		{"placeholder usage", `echo {{lympht:vault:ns/foo#bar}}`},
		{"ordinary command", `echo hello world`},
		{"go test", `go test ./...`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckCommand(tc.cmd); err != nil {
				t.Errorf("expected CheckCommand to allow %q, got error: %v", tc.cmd, err)
			}
		})
	}
}
