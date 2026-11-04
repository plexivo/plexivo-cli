package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

const issued = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://api.plexivo.io/k8s
  name: plexivo
contexts:
- context:
    cluster: plexivo
    namespace: ns-abc
    user: tenant-1
  name: plexivo
current-context: plexivo
users:
- name: tenant-1
  user:
    token: tok.123
`

func TestParseKubeconfig(t *testing.T) {
	p, err := parseKubeconfig(issued)
	if err != nil {
		t.Fatal(err)
	}
	if p.Server != "https://api.plexivo.io/k8s" || p.Namespace != "ns-abc" || p.Token != "tok.123" {
		t.Fatalf("parsed %+v", p)
	}
	if _, err := parseKubeconfig("apiVersion: v1\n"); err == nil {
		t.Fatal("a kubeconfig without a token must be refused")
	}
}

func TestExecCredential(t *testing.T) {
	b, err := execCredential("tok", "2026-11-05T10:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	st := v["status"].(map[string]any)
	if v["apiVersion"] != "client.authentication.k8s.io/v1" || v["kind"] != "ExecCredential" || st["token"] != "tok" || st["expirationTimestamp"] != "2026-11-05T10:00:00Z" {
		t.Fatalf("got %s", b)
	}
}

func TestContextNameAndSteps(t *testing.T) {
	if got := contextName("Ludo Saga!"); got != "plexivo-ludo-saga" {
		t.Fatalf("context name %q", got)
	}
	steps := kubectlSteps("plexivo-x", "https://api.plexivo.io/k8s", "ns-1", "/usr/local/bin/plexivo", "wid", true)
	joined := []string{}
	for _, s := range steps {
		joined = append(joined, strings.Join(s, " "))
	}
	all := strings.Join(joined, "\n")
	for _, want := range []string{
		"config set-cluster plexivo-x --server=https://api.plexivo.io/k8s",
		"--exec-command=/usr/local/bin/plexivo --exec-arg=k8s --exec-arg=token --exec-arg=wid",
		"config set-context plexivo-x --cluster=plexivo-x --user=plexivo-x --namespace=ns-1",
		"config use-context plexivo-x",
	} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q in\n%s", want, all)
		}
	}
	// Never a whole-file write: only `kubectl config` edits of its own names.
	for _, s := range steps {
		if s[0] != "config" {
			t.Fatalf("unexpected kubectl call %v", s)
		}
	}
	if len(kubectlSteps("a", "s", "n", "p", "i", false)) != 3 {
		t.Fatal("--keep-current must not switch the current context")
	}
}

func TestTokenCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	if err := saveCachedToken("w1", "tok", now.Add(time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if got, ok := usableToken("w1", now); !ok || got.Token != "tok" {
		t.Fatalf("fresh token not reused: %+v %v", got, ok)
	}
	if _, ok := usableToken("w1", now.Add(56*time.Minute)); ok {
		t.Fatal("a token with under five minutes left must be refreshed")
	}
	fi, err := os.Stat(tokenCachePath("w1"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("token cache mode %v, want 0600", fi.Mode().Perm())
	}
}
