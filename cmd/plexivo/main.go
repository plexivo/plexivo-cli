// Command plexivo is the Plexivo CLI.
//
//	plexivo login                 store an API key (Console → Settings → API keys)
//	plexivo logout
//	plexivo db list               your databases and caches
//	plexivo db connect <id>       a local port to a database, private or public
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
	case "login":
		return login()
	case "logout":
		return os.Remove(api.ConfigPath())
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

  plexivo login                 store an API key (Console → Settings → API keys)
  plexivo logout                forget it
  plexivo db list               your databases and caches
  plexivo db connect <id>       open a local port to a database or cache,
                                even a private one: --port N to choose the port
  plexivo version

PLEXIVO_TOKEN and PLEXIVO_API_URL override the stored login.
`)
}

func login() error {
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
	if err := api.Save(api.Config{Token: token, URL: os.Getenv("PLEXIVO_API_URL")}); err != nil {
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
	var ts struct {
		Tenants []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"tenants"`
	}
	if err := c.Do("GET", "/tenants", nil, &ts); err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tENGINE\tSTATUS\tACCESS\tORGANISATION")
	for _, t := range ts.Tenants {
		var ds struct {
			Deployments []deployment `json:"deployments"`
		}
		if err := c.Do("GET", "/tenants/"+t.ID+"/deployments", nil, &ds); err != nil {
			return err
		}
		for _, d := range ds.Deployments {
			if d.Type != "postgres" && d.Type != "mysql" && d.Type != "valkey" {
				continue
			}
			access := "public"
			if a, _ := d.Spec["access"].(string); a == "private" {
				access = "private"
			} else if ips, _ := d.Spec["trusted_ips"].([]any); len(ips) > 0 {
				access = fmt.Sprintf("trusted IPs (%d)", len(ips))
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", d.ID, d.Name, engineName(d.Type), d.Status, access, t.Name)
		}
	}
	return w.Flush()
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
		return errors.New("usage: plexivo db connect <id> [--port N]  (ids: plexivo db list)")
	}
	c, err := api.Load()
	if err != nil {
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
