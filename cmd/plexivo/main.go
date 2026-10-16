// Command plexivo is the Plexivo CLI.
//
//	plexivo auth login            sign in through the browser (a limited, expiring session)
//	plexivo auth status           who, which organisation, what access, time left
//	plexivo auth logout           end the session
//	plexivo db list               your databases and caches
//	plexivo db connect <name|id>  a local port to a database, private or public
//	plexivo version
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/plexivo/plexivo-cli/internal/api"
	"github.com/plexivo/plexivo-cli/internal/auth"
	"github.com/plexivo/plexivo-cli/internal/tunnel"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "plexivo:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "auth":
		if len(args) < 2 {
			usage()
			return nil
		}
		switch args[1] {
		case "login":
			return authLogin(args[2:])
		case "status", "list":
			return authStatus()
		case "logout", "revoke":
			return authLogout()
		}
	case "login":
		return authLogin(args[1:])
	case "logout":
		return authLogout()
	case "version", "--version", "-v":
		fmt.Println("plexivo", version)
		return nil
	case "db":
		if len(args) < 2 {
			usage()
			return nil
		}
		switch args[1] {
		case "list", "ls":
			return dbList()
		case "connect":
			return dbConnect(args[2:])
		}
	case "help", "--help", "-h":
		usage()
		return nil
	}
	usage()
	return fmt.Errorf("unknown command %q", strings.Join(args, " "))
}

func usage() {
	fmt.Print(`Plexivo CLI

  plexivo auth login            sign in through your browser; the session is limited
                                to one organisation and access level and expires
                                (--no-browser on a server, --api-key for CI keys)
  plexivo auth status           your organisation, access and the time left
  plexivo auth logout           end the session
  plexivo db list               your databases and caches
  plexivo db connect <name|id>  open a local port to a database or cache,
                                even a private one: --port N to choose the port
  plexivo version

PLEXIVO_TOKEN and PLEXIVO_API_URL override the stored sign-in.
`)
}

func apiURL() string {
	if u := os.Getenv("PLEXIVO_API_URL"); u != "" {
		return strings.TrimSuffix(u, "/")
	}
	return api.DefaultURL
}

// authLogin signs in through the browser, like gcloud auth login.
func authLogin(args []string) error {
	fs := flag.NewFlagSet("auth login", flag.ContinueOnError)
	noBrowser := fs.Bool("no-browser", false, "print the link instead of opening a browser")
	withKey := fs.Bool("api-key", false, "store an API key instead (for CI and automation)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *withKey {
		return keyLogin()
	}
	httpc := &http.Client{Timeout: 30 * time.Second}
	d, err := auth.Start(httpc, apiURL())
	if err != nil {
		return fmt.Errorf("could not start the sign-in: %w", err)
	}
	fmt.Printf("\nYour code:  %s\n\n", d.UserCode)
	if *noBrowser || !auth.OpenBrowser(d.VerificationURIComplete) {
		fmt.Printf("Open this link on any device and approve the code:\n  %s\n\n", d.VerificationURIComplete)
	} else {
		fmt.Printf("Approve it in the browser window that just opened.\nNo window? Open %s\n\n", d.VerificationURIComplete)
	}
	fmt.Printf("Waiting for approval (the code works for %d minutes)…\n", d.ExpiresIn/60)
	s, err := auth.Wait(httpc, apiURL(), d, time.Sleep)
	if err != nil {
		return err
	}
	if err := api.Save(api.Config{
		Token: s.AccessToken, URL: os.Getenv("PLEXIVO_API_URL"), Kind: "session",
		ExpiresAt: s.ExpiresAt, Organisation: s.Organisation.Name, Access: s.Access,
	}); err != nil {
		return err
	}
	fmt.Printf("\nSigned in to %s with %s access until %s (%s).\n",
		s.Organisation.Name, accessName(s.Access), s.ExpiresAt.Local().Format("15:04 on 2 Jan"), left(s.ExpiresAt))
	return nil
}

func authStatus() error {
	if os.Getenv("PLEXIVO_TOKEN") != "" {
		fmt.Println("Using PLEXIVO_TOKEN from the environment.")
		return nil
	}
	cfg, err := api.Read()
	if err != nil || cfg.Token == "" {
		fmt.Println("Not signed in. Run: plexivo auth login")
		return nil
	}
	if cfg.Kind != "session" {
		fmt.Println("Signed in with an API key (no expiry set by the CLI).")
		return nil
	}
	if time.Now().After(cfg.ExpiresAt) {
		fmt.Printf("Your session for %s expired at %s. Run: plexivo auth login\n", cfg.Organisation, cfg.ExpiresAt.Local().Format("15:04 on 2 Jan"))
		return nil
	}
	fmt.Printf("Signed in to %s with %s access. The session ends at %s (%s).\n",
		cfg.Organisation, accessName(cfg.Access), cfg.ExpiresAt.Local().Format("15:04 on 2 Jan"), left(cfg.ExpiresAt))
	return nil
}

// authLogout ends the session on the server too, then forgets it.
func authLogout() error {
	cfg, err := api.Read()
	if err != nil || cfg.Token == "" {
		fmt.Println("Not signed in.")
		return nil
	}
	if cfg.Kind == "session" {
		_ = auth.Revoke(&http.Client{Timeout: 15 * time.Second}, apiURL(), cfg.Token)
	}
	if err := os.Remove(api.ConfigPath()); err != nil {
		return err
	}
	fmt.Println("Signed out.")
	return nil
}

func accessName(a string) string {
	switch a {
	case "read_only":
		return "read-only"
	case "operate":
		return "operate"
	case "full":
		return "full"
	}
	return a
}

func left(t time.Time) string {
	d := time.Until(t).Round(time.Minute)
	if d < time.Hour {
		return fmt.Sprintf("%d min left", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh %02dm left", int(d.Hours()), int(d.Minutes())%60)
}

func keyLogin() error {
	fmt.Print("API key (Console → Settings → API keys): ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return err
	}
	token := strings.TrimSpace(line)
	if !strings.HasPrefix(token, "plx_") {
		return errors.New("that is not a Plexivo API key (they start with plx_)")
	}
	c := &api.Client{URL: api.DefaultURL, Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}}
	if u := os.Getenv("PLEXIVO_API_URL"); u != "" {
		c.URL = strings.TrimSuffix(u, "/")
	}
	var out struct {
		Tenants []struct {
			Name string `json:"name"`
		} `json:"tenants"`
	}
	if err := c.Do("GET", "/tenants", nil, &out); err != nil {
		return fmt.Errorf("the key was not accepted: %w", err)
	}
	if err := api.Save(api.Config{Token: token, URL: os.Getenv("PLEXIVO_API_URL"), Kind: "api_key"}); err != nil {
		return err
	}
	names := []string{}
	for _, t := range out.Tenants {
		names = append(names, t.Name)
	}
	fmt.Printf("Logged in (%s). Stored in %s\n", strings.Join(names, ", "), api.ConfigPath())
	return nil
}

type deployment struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Type   string         `json:"type"`
	Status string         `json:"status"`
	Spec   map[string]any `json:"spec"`
}

func dbList() error {
	c, err := api.Load()
	if err != nil {
		return err
	}
	all, org, err := databases(c)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tENGINE\tSTATUS\tACCESS\tORGANISATION")
	for _, d := range all {
		access := "public"
		if a, _ := d.Spec["access"].(string); a == "private" {
			access = "private"
		} else if ips, _ := d.Spec["trusted_ips"].([]any); len(ips) > 0 {
			access = fmt.Sprintf("trusted IPs (%d)", len(ips))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", d.ID, d.Name, engineName(d.Type), d.Status, access, org[d.ID])
	}
	return w.Flush()
}

// databases lists every database and cache the key can see.
func databases(c *api.Client) ([]deployment, map[string]string, error) {
	var ts struct {
		Tenants []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"tenants"`
	}
	if err := c.Do("GET", "/tenants", nil, &ts); err != nil {
		return nil, nil, err
	}
	var all []deployment
	org := map[string]string{}
	for _, t := range ts.Tenants {
		var ds struct {
			Deployments []deployment `json:"deployments"`
		}
		if err := c.Do("GET", "/tenants/"+t.ID+"/deployments", nil, &ds); err != nil {
			return nil, nil, err
		}
		for _, d := range ds.Deployments {
			if d.Type == "postgres" || d.Type == "mysql" || d.Type == "valkey" {
				all = append(all, d)
				org[d.ID] = t.Name
			}
		}
	}
	return all, org, nil
}

// resolve turns a database's name into its id; an id is returned as it is.
func resolve(c *api.Client, ref string) (string, error) {
	all, _, err := databases(c)
	if err != nil {
		return "", err
	}
	var hits []string
	for _, d := range all {
		if d.ID == ref {
			return ref, nil
		}
		if d.Name == ref && d.Status != "deleted" {
			hits = append(hits, d.ID)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return "", fmt.Errorf("no database or cache named %q (plexivo db list)", ref)
	default:
		return "", fmt.Errorf("%d databases are named %q: use its id (plexivo db list)", len(hits), ref)
	}
}

func engineName(t string) string {
	switch t {
	case "postgres":
		return "Postgres"
	case "mysql":
		return "MySQL"
	case "valkey":
		return "Cache"
	}
	return t
}

func dbConnect(args []string) error {
	fs := flag.NewFlagSet("db connect", flag.ContinueOnError)
	port := fs.Int("port", 0, "local port (default: the engine's usual port, or a free one)")
	bind := fs.String("bind", "127.0.0.1", "local address to listen on")
	showPassword := fs.Bool("show-password", false, "print the password in the connection string")
	var id string
	rest := args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		id, rest = rest[0], rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id == "" && fs.NArg() > 0 {
		id = fs.Arg(0)
	}
	if id == "" {
		return errors.New("usage: plexivo db connect <name|id> [--port N]  (plexivo db list)")
	}
	c, err := api.Load()
	if err != nil {
		return err
	}
	if id, err = resolve(c, id); err != nil {
		return err
	}
	grant, err := c.NewTunnel(id)
	if err != nil {
		return err
	}
	// The first tunnel proves the path before a port is opened.
	probe, err := tunnel.Dial(grant)
	if err != nil {
		return err
	}
	probe.Close()

	want := *port
	if want == 0 {
		want = grant.LocalPort
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(*bind, fmt.Sprint(want)))
	if err != nil && *port == 0 {
		ln, err = net.Listen("tcp", net.JoinHostPort(*bind, "0"))
	}
	if err != nil {
		return err
	}
	local := ln.Addr().(*net.TCPAddr)
	fmt.Printf("Tunnel open: %s:%d → %s %s (encrypted to its cell)\n", *bind, local.Port, engineName(grant.Engine), id)
	if s := connectionHint(c, id, *bind, local.Port, *showPassword); s != "" {
		fmt.Println("Connect with:", s)
	}
	fmt.Println("Keep this running while you work; Ctrl-C closes it.")

	src := tunnel.NewSource(grant, func() (*api.Tunnel, error) { return c.NewTunnel(id) })
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		fmt.Println("\nTunnel closed.")
		ln.Close()
	}()
	err = tunnel.Serve(ln, src, func(f string, a ...any) {
		fmt.Printf("%s  %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(f, a...))
	})
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// connectionHint is the database's connection string pointed at the local port.
func connectionHint(c *api.Client, id, host string, port int, showPassword bool) string {
	var out struct {
		Connection struct {
			User     string `json:"user"`
			Password string `json:"password"`
			Database string `json:"database"`
			URI      string `json:"uri"`
		} `json:"connection"`
	}
	if err := c.Do("GET", "/deployments/"+id+"/connection", nil, &out); err != nil || out.Connection.URI == "" {
		return ""
	}
	u, err := url.Parse(out.Connection.URI)
	if err != nil {
		return ""
	}
	u.Host = net.JoinHostPort(host, fmt.Sprint(port))
	if u.User != nil && !showPassword {
		u.User = url.UserPassword(u.User.Username(), "PASSWORD")
	}
	s := u.String()
	if !showPassword {
		s = strings.Replace(s, ":PASSWORD@", ":<password>@", 1) + "   (--show-password to print it)"
	}
	return s
}
