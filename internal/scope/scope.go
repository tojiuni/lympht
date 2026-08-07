// Package scope implements phase 2 of
// infra-tokenhub-elevated-session-command-governance: gating specific
// high-privilege kubectl "core actions" (exec, secret reads, node/argocd/
// tekton mutations) on an actually-active tokenhub elevation for a provider
// whose stated purpose covers that action, instead of relying on the agent
// to only use an elevated credential for what it said it needed it for.
//
// This is heuristic, not a mirror of real K8s RBAC:
//   - It only recognizes the command SHAPES this cluster's own tokenhub
//     providers table documents (CLAUDE.md's provider table) — a provider
//     added later needs a matching entry here to be enforced.
//   - It tracks "active" purely from having observed a provider requested
//     via lympht's own PreToolUse interception (a curl to tokenhub naming a
//     provider) — it can't see whether tokenhub actually approved/denied an
//     elevation request, or when the resulting token truly expires
//     server-side. A denied request still marks the provider "active" here;
//     that's a deliberate fail-safe choice (more restrictive, not less).
//   - State lives in a shared per-user cache directory (see statePath), so
//     if multiple ssh-workspace sessions ever run concurrently as the same
//     OS user, they currently share one active-provider set. Acceptable for
//     a single-operator environment; a real multi-session deployment would
//     need to scope state per session.
//
// This is a second layer alongside guard's always-on exposure-pattern
// blocks (internal/guard), not a replacement for them: guard's rules (e.g.
// "vault kv get always prints the value, so it's always blocked") apply
// regardless of which provider is active, because the problem there is
// visibility, not authorization scope.
package scope

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// providerTTL mirrors this cluster's tokenhub provider TTL table (see
// CLAUDE.md). Providers not listed (e.g. one-off elevation-approved grants)
// use defaultTTL.
var providerTTL = map[string]time.Duration{
	"k8s-tekton-admin":     600 * time.Second,
	"k8s-cluster-reader":   3600 * time.Second,
	"k8s-rbac-reader":      3600 * time.Second,
	"k8s-app-restarter":    300 * time.Second,
	"k8s-argocd-syncer":    300 * time.Second,
	"k8s-secret-inspector": 300 * time.Second,
	"k8s-db-operator":      600 * time.Second,
	"k8s-node-manager":     300 * time.Second,
}

const defaultTTL = 600 * time.Second

var providerFieldRe = regexp.MustCompile(`"provider"\s*:\s*"([a-zA-Z0-9_-]+)"`)

// ExtractRequestedProvider reports the provider name if cmd looks like this
// cluster's tokenhub credential-fetch shape: a command mentioning "tokenhub"
// (the service hostname every such curl in this cluster's convention
// includes) alongside a {"provider":"X"} JSON body — used for both
// /v1/credentials (pre-granted) and /v1/elevation-requests/.../credential
// (post-approval) calls.
func ExtractRequestedProvider(cmd string) (string, bool) {
	if !strings.Contains(strings.ToLower(cmd), "tokenhub") {
		return "", false
	}
	m := providerFieldRe.FindStringSubmatch(cmd)
	if m == nil {
		return "", false
	}
	return m[1], true
}

type persistedState struct {
	Active map[string]time.Time `json:"active"`
}

// statePath honors LYMPHT_STATE_DIR (mainly for test isolation) and
// otherwise defaults to the user's cache directory.
func statePath() (string, error) {
	dir := os.Getenv("LYMPHT_STATE_DIR")
	if dir == "" {
		userCache, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(userCache, "lympht")
	}
	return filepath.Join(dir, "active-providers.json"), nil
}

func loadState() (persistedState, error) {
	path, err := statePath()
	if err != nil {
		return persistedState{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return persistedState{Active: map[string]time.Time{}}, nil
		}
		return persistedState{}, err
	}
	var s persistedState
	if err := json.Unmarshal(data, &s); err != nil {
		// Corrupt state file: fail open to an empty set (never crash the
		// hook over a state-file problem) rather than fail closed — an
		// agent that must now re-declare its elevation is a minor
		// inconvenience, not a security hole.
		return persistedState{Active: map[string]time.Time{}}, nil
	}
	if s.Active == nil {
		s.Active = map[string]time.Time{}
	}
	return s, nil
}

func saveState(s persistedState) error {
	path, err := statePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RecordIfProviderRequested inspects cmd for a tokenhub credential-fetch
// shape and, if found, marks that provider active from now until its TTL
// elapses. Best-effort: any failure to persist state is swallowed — a
// legitimate credential-fetch command must never fail because of a
// bookkeeping side effect.
func RecordIfProviderRequested(cmd string) {
	provider, ok := ExtractRequestedProvider(cmd)
	if !ok {
		return
	}
	s, err := loadState()
	if err != nil {
		return
	}
	s.Active[provider] = time.Now()
	_ = saveState(s)
}

// ActiveProviders returns the set of providers currently considered active
// (requested within their TTL window). Expired entries are pruned from
// persisted state as a side effect.
func ActiveProviders() map[string]bool {
	s, err := loadState()
	if err != nil {
		return map[string]bool{}
	}
	now := time.Now()
	active := map[string]bool{}
	changed := false
	for provider, requestedAt := range s.Active {
		ttl := providerTTL[provider]
		if ttl == 0 {
			ttl = defaultTTL
		}
		if now.Sub(requestedAt) <= ttl {
			active[provider] = true
		} else {
			delete(s.Active, provider)
			changed = true
		}
	}
	if changed {
		_ = saveState(s)
	}
	return active
}

type coreAction struct {
	name         string
	re           *regexp.Regexp
	allowedIfAny []string
}

// coreActions lists the kubectl command shapes gated by this package.
// Deliberately narrow: generic reads (kubectl get pods, describe, logs, ...)
// are NOT gated here — that's ambient toji-dev SA scope, governed by real
// K8s RBAC, not by tokenhub elevation. Only the specific action families
// that this session actually needed an explicit tokenhub elevation for are
// covered.
var coreActions = []coreAction{
	{
		name:         "kubectl exec",
		re:           regexp.MustCompile(`(?i)\bkubectl\b[^|;\n]*\bexec\b`),
		allowedIfAny: []string{"k8s-db-operator"},
	},
	{
		name:         "kubectl get secret",
		re:           regexp.MustCompile(`(?i)\bkubectl\b[^|;\n]*\bget\s+secrets?\b`),
		allowedIfAny: []string{"k8s-secret-inspector"},
	},
	{
		name:         "kubectl node drain/cordon",
		re:           regexp.MustCompile(`(?i)\bkubectl\b[^|;\n]*\b(drain|cordon|uncordon)\b`),
		allowedIfAny: []string{"k8s-node-manager"},
	},
	{
		name:         "kubectl patch argocd application",
		re:           regexp.MustCompile(`(?i)\bkubectl\b[^|;\n]*\bpatch\b[^|;\n]*applications?\.argoproj\.io`),
		allowedIfAny: []string{"k8s-argocd-syncer"},
	},
	{
		name:         "kubectl rollout restart",
		re:           regexp.MustCompile(`(?i)\bkubectl\b[^|;\n]*\brollout\s+restart\b`),
		allowedIfAny: []string{"k8s-app-restarter"},
	},
	{
		name: "kubectl tekton.dev mutation",
		re: regexp.MustCompile(`(?i)\bkubectl\b[^|;\n]*\b(create|apply|delete|patch)\b[^|;\n]*` +
			`(pipelinerun|taskrun|pipelines?\.tekton\.dev|triggers?\.tekton\.dev|eventlistener)`),
		allowedIfAny: []string{"k8s-tekton-admin"},
	},
}

// CheckCommand returns an error if cmd matches a known core-action pattern
// and none of the providers it requires are currently active.
func CheckCommand(cmd string) error {
	active := ActiveProviders()
	for _, a := range coreActions {
		if !a.re.MatchString(cmd) {
			continue
		}
		for _, p := range a.allowedIfAny {
			if active[p] {
				return nil
			}
		}
		return fmt.Errorf(
			"lympht: blocked — %s requires an active tokenhub elevation for one of %v; none detected in this session. "+
				"Request one via tokenhub first (POST .../v1/credentials with the matching provider) — "+
				"make sure that request itself runs as a command lympht can see, so it gets recorded.",
			a.name, a.allowedIfAny)
	}
	return nil
}
