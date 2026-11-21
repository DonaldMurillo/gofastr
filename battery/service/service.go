package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/internal/gatepath"
)

// Config constructs a service Battery.
type Config struct {
	// ID is the reverse-DNS app id ("dev.gofastr.hello").
	ID string

	// Name is the installed executable's base name and the name the CLI
	// prints in its hints.
	Name string

	// Port is the loopback port the installed daemon binds.
	Port int

	// DevPort is the port of the foreground dev run (`service run`).
	DevPort int

	// SessionTTL bounds a browser session minted by `open`.
	SessionTTL time.Duration

	// AllowHosts adds Host header values the daemon accepts besides its own
	// loopback address, for a reverse proxy in front of it.
	AllowHosts []string

	// OpenPrefixes lists path prefixes ("/api") the session gate lets through
	// without a browser session, for a client with no browser that the app
	// authenticates itself (an API key, a bearer token, battery/auth).
	OpenPrefixes []string

	// BindAddress is the IP address the daemon and the dev run listen on.
	BindAddress string

	// InsecureTokenInURL lets `open --print` print a one-shot sign-in URL
	// instead of opening a local page, for a headless machine reached over an
	// SSH tunnel.
	InsecureTokenInURL bool

	// Stdout and Stderr receive the CLI's output.
	Stdout, Stderr io.Writer

	// Stdin feeds `service mcp`.
	Stdin io.Reader
}

// Runtime is what the host's builder receives: the state directory and
// secret of the mode being run, resolved before the App exists.
type Runtime struct {
	// DataDir is the 0700 state directory.
	DataDir string
	// Secret is the persisted app secret (32 random bytes, base64url), minted
	// on first run.
	Secret string
	// Dev is true for the foreground dev run.
	Dev bool
}

// MCPCommand is the argv an agent's MCP config runs to reach this daemon
// or dev run: `<exe> service mcp [--dev] --state-dir <DataDir>`.
func (rt Runtime) MCPCommand() []string {
	return mcpCommand(rt.Dev, rt.DataDir)
}

// AppOptions opens the mode's SQLite database (app.db in DataDir) and
// returns it with the persisted secret, so sessions and signed tokens
// survive a restart.
func (rt Runtime) AppOptions() ([]framework.AppOption, error) {
	db, err := localapp.OpenDB(rt.DataDir)
	if err != nil {
		return nil, fmt.Errorf("service: %w", err)
	}
	return []framework.AppOption{framework.WithDB(db), framework.WithSecret(rt.Secret)}, nil
}

// Builder constructs the App for a daemon or dev run.
type Builder func(Runtime) (*framework.App, error)

// Battery is the service host.
type Battery struct {
	cfg  Config
	err  error
	mgr  manager
	tool toolRunner

	gate *gate

	baseOverride string

	mu  sync.Mutex
	app *framework.App
}

const defaultSessionTTL = 7 * 24 * time.Hour

// New validates cfg and constructs the Battery.
func New(cfg Config) *Battery {
	cfg.OpenPrefixes = append([]string(nil), cfg.OpenPrefixes...)
	cfg.AllowHosts = append([]string(nil), cfg.AllowHosts...)
	b := &Battery{cfg: cfg, tool: execTool}
	if cfg.Stdout == nil {
		b.cfg.Stdout = os.Stdout
	}
	if cfg.Stderr == nil {
		b.cfg.Stderr = os.Stderr
	}
	if cfg.Stdin == nil {
		b.cfg.Stdin = os.Stdin
	}
	if b.cfg.Name == "" && cfg.ID != "" {
		b.cfg.Name = cfg.ID[strings.LastIndexByte(cfg.ID, '.')+1:]
	}
	if b.cfg.SessionTTL == 0 {
		b.cfg.SessionTTL = defaultSessionTTL
	}
	b.err = validateConfig(b.cfg)
	b.mgr = managerFor(currentOS)
	return b
}

// Name implements framework.Battery.
func (b *Battery) Name() string { return "service" }

// Init implements framework.Battery: it installs the loopback gate, which
// also answers the sign-in path, in front of the whole server, and the
// owner extractor that reads the local identity the gate puts on each
// signed-in request.
func (b *Battery) Init(app *framework.App) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.app != nil {
		return errors.New("service: Init called twice")
	}
	b.app = app
	if b.gate == nil {
		return nil
	}
	if localapp.Install() {
		b.gate.user = nil
	} else {
		app.NoteAuthenticator()
	}
	app.WrapServer(b.gate.wrap)
	return nil
}

// Run dispatches the command line.
func (b *Battery) Run(ctx context.Context, args []string, build Builder) error {
	if b.err != nil {
		return b.err
	}
	if len(args) == 0 {
		return b.runForeground(ctx, nil, build)
	}
	if args[0] == "--addr" || strings.HasPrefix(args[0], "--addr=") {
		return b.runDevLoop(ctx, args, build)
	}
	if args[0] != "service" {
		b.usage(b.cfg.Stderr)
		return fmt.Errorf("service: unknown command %q", args[0])
	}
	return b.dispatch(ctx, args[1:], build)
}

var (
	reAppID = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`)
	reName  = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$`)
)

func validateConfig(cfg Config) error {
	if !reAppID.MatchString(cfg.ID) || !strings.Contains(cfg.ID, ".") || strings.Contains(cfg.ID, "..") {
		return fmt.Errorf("service: Config.ID %q must be reverse-DNS: ASCII letters, digits, dots and dashes, at least one dot, no empty segment, starting and ending with a letter or digit", cfg.ID)
	}
	if len(cfg.ID) > 200 {
		return fmt.Errorf("service: Config.ID %q is longer than 200 characters", cfg.ID)
	}
	if !reName.MatchString(cfg.Name) || len(cfg.Name) > 64 {
		return fmt.Errorf("service: Config.Name %q must be 1-64 ASCII letters, digits, dots, dashes or underscores, starting and ending with a letter or digit", cfg.Name)
	}
	if err := validatePort("Config.Port", cfg.Port, false); err != nil {
		return err
	}
	if err := validatePort("Config.DevPort", cfg.DevPort, true); err != nil {
		return err
	}
	if cfg.SessionTTL < 0 {
		return fmt.Errorf("service: Config.SessionTTL %s is negative", cfg.SessionTTL)
	}
	for _, h := range cfg.AllowHosts {
		if err := validateAllowHost(h); err != nil {
			return err
		}
	}
	for _, p := range cfg.OpenPrefixes {
		if err := gatepath.Validate("service: Config.OpenPrefixes", p); err != nil {
			return err
		}
	}
	if cfg.BindAddress != "" {
		ip, err := netip.ParseAddr(cfg.BindAddress)
		if err != nil || ip.Zone() != "" {
			return fmt.Errorf("service: Config.BindAddress %q must be an IP address such as 0.0.0.0 or 192.168.1.20", cfg.BindAddress)
		}
	}
	return nil
}

func (b *Battery) bindHost() string {
	if b.cfg.BindAddress != "" {
		return b.cfg.BindAddress
	}
	return "127.0.0.1"
}

func (b *Battery) boundHostOK(host string) bool {
	want, err := netip.ParseAddr(b.bindHost())
	if err != nil {
		return false
	}
	got, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return got.Unmap() == want.Unmap() || (want.IsUnspecified() && got.IsUnspecified())
}

func browserAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if ip, err := netip.ParseAddr(host); err == nil && ip.IsUnspecified() {
		if ip.Is6() {
			return net.JoinHostPort("::1", port)
		}
		return net.JoinHostPort("127.0.0.1", port)
	}
	return addr
}

func isLoopbackIP(s string) bool {
	ip, err := netip.ParseAddr(s)
	return err == nil && ip.IsLoopback()
}

func (b *Battery) extraHosts(port int) []string {
	hosts := append([]string(nil), b.cfg.AllowHosts...)
	if ip, err := netip.ParseAddr(b.cfg.BindAddress); err == nil {
		switch {
		case !ip.IsUnspecified():
			hosts = append(hosts, net.JoinHostPort(ip.String(), strconv.Itoa(port)))
		case ip.Is6():
			hosts = append(hosts, net.JoinHostPort("::1", strconv.Itoa(port)))
		}
	}
	return hosts
}

func validatePort(field string, p int, zeroOK bool) error {
	if p == 0 && zeroOK {
		return nil
	}
	if p < 1 || p > 65535 {
		return fmt.Errorf("service: %s %d must be between 1 and 65535", field, p)
	}
	return nil
}

var reHost = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?(?::[0-9]{1,5})?$`)

func validateAllowHost(h string) error {
	if !reHost.MatchString(h) {
		return fmt.Errorf("service: Config.AllowHosts entry %q must be host or host:port in ASCII", h)
	}
	return nil
}
