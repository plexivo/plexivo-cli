package main

// plexivo k8s: kubectl access to Plexivo Kubernetes, like
// `gcloud container clusters get-credentials`. `credentials` adds one
// context (plexivo-<name>) to the kubeconfig kubectl already uses, through
// `kubectl config`, so no other context is touched. kubectl then asks
// `plexivo k8s token <id>` for a short-lived token whenever it needs one
// (an exec credential plugin); the token is kept, private to this user,
// until shortly before it expires.

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/plexivo/plexivo-cli/internal/api"
)

type workload struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Namespace string `json:"namespace"`
}

type issuedKubeconfig struct {
	Kubeconfig string `json:"kubeconfig"`
	ExpiresAt  string `json:"expires_at"`
	Namespace  string `json:"namespace"`
}

// kubeconfigParts are the three values a context needs, read from the
// kubeconfig the Brain issues.
type kubeconfigParts struct {
	Server, Namespace, Token string
}

var (
	reServer    = regexp.MustCompile(`(?m)^\s+server:\s*(\S+)\s*$`)
	reNamespace = regexp.MustCompile(`(?m)^\s+namespace:\s*(\S+)\s*$`)
	reToken     = regexp.MustCompile(`(?m)^\s+token:\s*(\S+)\s*$`)
)

func parseKubeconfig(yaml string) (kubeconfigParts, error) {
	get := func(re *regexp.Regexp) string {
		if m := re.FindStringSubmatch(yaml); m != nil {
			return m[1]
		}
		return ""
	}
	p := kubeconfigParts{Server: get(reServer), Namespace: get(reNamespace), Token: get(reToken)}
	if p.Server == "" || p.Token == "" {
		return p, errors.New("the kubeconfig from Plexivo had no server or token")
	}
	return p, nil
}

// execCredential is what kubectl reads from an exec credential plugin.
func execCredential(token, expiresAt string) ([]byte, error) {
	status := map[string]any{"token": token}
	if expiresAt != "" {
		status["expirationTimestamp"] = expiresAt
	}
	return json.Marshal(map[string]any{
		"apiVersion": "client.authentication.k8s.io/v1",
		"kind":       "ExecCredential",
		"status":     status,
	})
}

func contextName(name string) string {
	s := strings.ToLower(regexp.MustCompile(`[^A-Za-z0-9-]+`).ReplaceAllString(name, "-"))
	s = strings.Trim(s, "-")
	if s == "" {
		s = "workload"
	}
	return "plexivo-" + s
}

func k8sCmd(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "list", "ls":
		return k8sList()
	case "credentials", "get-credentials":
		return k8sCredentials(args[1:])
	case "token":
		return k8sToken(args[1:])
	}
	usage()
	return fmt.Errorf("unknown command %q", "k8s "+strings.Join(args, " "))
}

func workloads(c *api.Client) ([]workload, error) {
	var ts struct {
		Tenants []struct {
			ID string `json:"id"`
		} `json:"tenants"`
	}
	if err := c.Do("GET", "/tenants", nil, &ts); err != nil {
		return nil, err
	}
	var all []workload
	for _, t := range ts.Tenants {
		var ws struct {
			Workloads []workload `json:"workloads"`
		}
		if err := c.Do("GET", "/tenants/"+t.ID+"/kubernetes", nil, &ws); err != nil {
			return nil, err
		}
		all = append(all, ws.Workloads...)
	}
	return all, nil
}

func k8sList() error {
	c, err := api.Load()
	if err != nil {
		return err
	}
	all, err := workloads(c)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tSTATUS")
	for _, x := range all {
		fmt.Fprintf(w, "%s\t%s\t%s\n", x.ID, x.Name, x.Status)
	}
	return w.Flush()
}

func resolveWorkload(c *api.Client, ref string) (workload, error) {
	all, err := workloads(c)
	if err != nil {
		return workload{}, err
	}
	var hits []workload
	for _, x := range all {
		if x.ID == ref {
			return x, nil
		}
		if x.Name == ref && x.Status != "deleted" {
			hits = append(hits, x)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return workload{}, fmt.Errorf("no Kubernetes workload named %q (plexivo k8s list)", ref)
	default:
		return workload{}, fmt.Errorf("%d workloads are named %q: use its id (plexivo k8s list)", len(hits), ref)
	}
}

// k8sCredentials adds a plexivo-<name> context and makes it current.
func k8sCredentials(args []string) error {
	fs := flag.NewFlagSet("k8s credentials", flag.ContinueOnError)
	keep := fs.Bool("keep-current", false, "add the context without switching kubectl to it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: plexivo k8s credentials <name|id>")
	}
	c, err := api.Load()
	if err != nil {
		return err
	}
	wl, err := resolveWorkload(c, fs.Arg(0))
	if err != nil {
		return err
	}
	var kc issuedKubeconfig
	if err := c.Do("GET", "/kubernetes/"+wl.ID+"/kubeconfig", nil, &kc); err != nil {
		return err
	}
	parts, err := parseKubeconfig(kc.Kubeconfig)
	if err != nil {
		return err
	}
	_ = saveCachedToken(wl.ID, parts.Token, kc.ExpiresAt)
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if p, err := filepath.EvalSymlinks(self); err == nil {
		self = p
	}
	name := contextName(wl.Name)
	ns := parts.Namespace
	if ns == "" {
		ns = kc.Namespace
	}
	steps := kubectlSteps(name, parts.Server, ns, self, wl.ID, !*keep)
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl is not installed: install it (https://kubernetes.io/docs/tasks/tools/), then run this again")
	}
	for _, s := range steps {
		out, err := exec.Command("kubectl", s...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("kubectl %s: %v: %s", s[0]+" "+s[1], err, strings.TrimSpace(string(out)))
		}
	}
	fmt.Printf("Added context %q (namespace %s). Your other contexts are unchanged.\n", name, ns)
	if *keep {
		fmt.Printf("Use it with: kubectl --context %s get pods\n", name)
	} else {
		fmt.Println("kubectl now uses it. Try: kubectl get pods")
	}
	fmt.Println("Tokens refresh by themselves while you stay signed in (plexivo auth login).")
	return nil
}

// kubectlSteps are the `kubectl config` calls that add the context.
func kubectlSteps(name, server, namespace, self, id string, use bool) [][]string {
	steps := [][]string{
		{"config", "set-cluster", name, "--server=" + server},
		{"config", "set-credentials", name,
			"--exec-api-version=client.authentication.k8s.io/v1",
			"--exec-command=" + self,
			"--exec-arg=k8s", "--exec-arg=token", "--exec-arg=" + id,
			"--exec-interactive-mode=Never"},
		{"config", "set-context", name, "--cluster=" + name, "--user=" + name, "--namespace=" + namespace},
	}
	if use {
		steps = append(steps, []string{"config", "use-context", name})
	}
	return steps
}

type cachedToken struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

func tokenCachePath(id string) string {
	return filepath.Join(filepath.Dir(api.ConfigPath()), "k8s", id+".json")
}

func saveCachedToken(id, token, expiresAt string) error {
	p := tokenCachePath(id)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(cachedToken{Token: token, ExpiresAt: expiresAt})
	return os.WriteFile(p, b, 0o600)
}

// usableToken is a cached token with at least five minutes left.
func usableToken(id string, now time.Time) (cachedToken, bool) {
	b, err := os.ReadFile(tokenCachePath(id))
	if err != nil {
		return cachedToken{}, false
	}
	var t cachedToken
	if json.Unmarshal(b, &t) != nil || t.Token == "" {
		return cachedToken{}, false
	}
	exp, err := time.Parse(time.RFC3339, t.ExpiresAt)
	if err != nil || exp.Sub(now) < 5*time.Minute {
		return cachedToken{}, false
	}
	return t, true
}

// k8sToken is the exec credential plugin kubectl runs.
func k8sToken(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: plexivo k8s token <workload id> (kubectl runs this for you)")
	}
	id := args[0]
	t, ok := usableToken(id, time.Now())
	if !ok {
		c, err := api.Load()
		if err != nil {
			return fmt.Errorf("%w (sign in again: plexivo auth login)", err)
		}
		var kc issuedKubeconfig
		if err := c.Do("GET", "/kubernetes/"+id+"/kubeconfig", nil, &kc); err != nil {
			return fmt.Errorf("%w (sign in again: plexivo auth login)", err)
		}
		parts, err := parseKubeconfig(kc.Kubeconfig)
		if err != nil {
			return err
		}
		t = cachedToken{Token: parts.Token, ExpiresAt: kc.ExpiresAt}
		_ = saveCachedToken(id, t.Token, t.ExpiresAt)
	}
	out, err := execCredential(t.Token, t.ExpiresAt)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(out, '\n'))
	return err
}
