// sling-server is the Browser/Server workbench binary: HTTP API + SSE +
// embedded web UI over the sling core engine.
//
// Usage:
//
//	sling-server serve [--port 5666] [--host 127.0.0.1] [--workdir DIR] [--no-open] [--token T]
//	sling-server version
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/flarco/g"
	"github.com/slingdata-io/sling-cli/core"
	"github.com/slingdata-io/sling-cli/core/env"
	"github.com/slingdata-io/sling-cli/server"
	"github.com/slingdata-io/sling-cli/web"
	"golang.org/x/term"
)

var version = "dev"

func main() {
	// subcommand style: `sling-server version` short-circuits
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("sling-server %s (core %s, %s/%s)\n", version, core.Version, runtime.GOOS, runtime.GOARCH)
		return
	}

	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", 5666, "HTTP port (auto-increments when busy)")
	host := fs.String("host", envOr("SLING_SERVER_HOST", "127.0.0.1"), "listen host (0.0.0.0 for containers; env SLING_SERVER_HOST)")
	workdir := fs.String("workdir", "", "data directory (default ~/.sling)")
	noOpen := fs.Bool("no-open", false, "do not open the browser on start")
	token := fs.String("token", os.Getenv("SLING_SERVER_TOKEN"), "bearer token protecting the API (for LAN use; env SLING_SERVER_TOKEN)")
	concurrency := fs.Int("concurrency", 4, "max concurrently running jobs")
	streamConcurrency := fs.Int("stream-concurrency", 1, "replication stream parallelism")
	retention := fs.Int("retention", 90, "days to keep run history & logs (0 = forever)")
	setPassphrase := fs.Bool("set-passphrase", false, "interactively set/replace the vault passphrase")
	fs.Parse(os.Args[1:])

	// data dir: must be set before env caches paths
	if *workdir != "" {
		os.Setenv("SLING_HOME_DIR", *workdir)
	}
	env.LoadHomeDir()
	os.MkdirAll(env.HomeDir, 0755)

	// engine logger: wires the zerolog hook that fans out env.LogSink
	// (our SSE log stream) — must run before any task does.
	env.InitLogger()

	if *setPassphrase {
		setVaultPassphrase()
		return
	}

	// server mode: no terminal progress bars, quieter engine logs
	slingShowProgressOff()

	opts := server.Options{
		Token:             *token,
		Concurrency:       *concurrency,
		StreamConcurrency: *streamConcurrency,
		RetentionDays:     *retention,
		Version:           version,
	}

	app, err := server.New(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not start: %s\n", err)
		os.Exit(1)
	}

	// single-instance behavior: if an instance already serves on the port
	// (or the next few), open the browser to it and exit.
	addr, existing := findFreePort(*host, *port)
	if existing != "" {
		fmt.Printf("sling-server is already running → %s\n", existing)
		if !*noOpen {
			openBrowser(existing)
		}
		return
	}

	server.UIHandler = web.Handler
	handler := app.Handler()

	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 15 * time.Second}

	// graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			errCh <- err
			return
		}
		printBanner(fmt.Sprintf("http://%s", listener.Addr().String()), *token != "")
		if err = srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	go func() {
		if err := app.Boot(); err != nil {
			g.Warn("boot: %s", err)
		}
	}()

	if !*noOpen {
		go func() {
			time.Sleep(400 * time.Millisecond) // let the listener settle
			openBrowser(fmt.Sprintf("http://%s", addr))
		}()
	}

	select {
	case err := <-errCh:
		fmt.Fprintf(os.Stderr, "server error: %s\n", err)
		os.Exit(1)
	case <-ctx.Done():
		fmt.Println("\nShutting down… (Ctrl+C again to force)")
		go func() {
			<-ctx.Done()
			os.Exit(130)
		}()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
	app.Shutdown()
	fmt.Println("Exited safely.")
}

// findFreePort tries port, port+1... port+9. If a port answers as an
// existing sling-server (health check), returns its URL.
func findFreePort(host string, start int) (addr string, existing string) {
	for p := start; p < start+10; p++ {
		candidate := net.JoinHostPort(host, strconv.Itoa(p))
		ln, err := net.Listen("tcp", candidate)
		if err == nil {
			ln.Close()
			return candidate, ""
		}
		// busy: check if it is one of us
		if isSlingServer(fmt.Sprintf("http://%s", candidate)) {
			return "", fmt.Sprintf("http://%s", candidate)
		}
	}
	// all busy by strangers: just take the original port and let Serve fail
	return net.JoinHostPort(host, strconv.Itoa(start)), ""
}

func isSlingServer(baseURL string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL + "/api/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		g.Debug("could not open browser: %s", err)
	}
}

func printBanner(url string, hasToken bool) {
	fmt.Println()
	fmt.Println("  ┌─────────────────────────────────────────────┐")
	fmt.Println("  │     sling-server · Data Migration Workbench │")
	fmt.Println("  └─────────────────────────────────────────────┘")
	fmt.Printf("   ➜ URL:    %s\n", url)
	if hasToken {
		fmt.Println("   ➜ Auth:   token enabled (requests need Authorization: Bearer <token>)")
	}
	fmt.Printf("   ➜ Data:   %s\n", env.HomeDir)
	fmt.Println("   ➜ Quit:   Ctrl+C")
	fmt.Println()
}

func setVaultPassphrase() {
	fmt.Print("Set vault passphrase (input hidden, press Enter to cancel): ")
	pass, err := readPassword()
	if err != nil || pass == "" {
		fmt.Println("Cancelled.")
		return
	}
	fmt.Print("Confirm passphrase: ")
	pass2, _ := readPassword()
	if pass != pass2 {
		fmt.Println("Passphrases do not match. Cancelled.")
		os.Exit(1)
	}

	app, err := server.New(server.Options{Passphrase: pass, Version: version})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed: %s\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Vault encrypted and saved at %s\n", env.HomeDir)
	_ = app
}

// envOr returns the value of key when set (non-empty), else fallback. Flags
// read env defaults this way so containers can configure without overriding
// the command line.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func readPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Println()
		return string(b), err
	}
	var s string
	_, err := fmt.Scanln(&s)
	return s, err
}

func slingShowProgressOff() {
	os.Setenv("SLING_SHOW_PROGRESS", "false")
}
