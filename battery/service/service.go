package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/internal/gatepath"
	"github.com/DonaldMurillo/gofastr/internal/localuser"
	// The SQLite driver Runtime.AppOptions opens app.db with.
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Config constructs a service Battery. ID and Port are required.
type Config struct {
	// ID is the reverse-DNS app id ("dev.gofastr.hello"). It names the
	// launchd label, the systemd unit, the Windows task, and the state
	// directory, so its grammar is strict: ASCII letters, digits, dots
	// and dashes, at least one dot, starting and ending with a letter or
	// digit, no empty segment.
	ID string

	// Name is the installed executable's base name and the name the CLI
	// prints in its hints. Default: the last segment of ID.
	Name string

	// Port is the loopback port the installed daemon binds. It is fixed
	// so bookmarks survive restarts; `install --port` overrides it per
	// installation. If another program holds the port the daemon exits
	// with an error naming it. There is no fallback to another port.
	Port int

	// DevPort is the port of the foreground dev run (`service run`).
	// Zero picks a free port; worktree isolation applies as for any App.
	DevPort int

	// SessionTTL bounds a browser session minted by `open`. Zero means
	// seven days. A negative value is refused by New.
	SessionTTL time.Duration

	// AllowHosts adds Host header values the daemon accepts besides its
	// own loopback address, for a reverse proxy in front of it. Each
	// entry is a host[:port] and is logged at every start. Prefer this
	// to turning the Host check off: there is no way to turn it off.
	// A caller in Docker or WSL2 that reaches the app through
	// BindAddress names the host as it sees it ("host.docker.internal:
	// 8080"); list that value here.
	AllowHosts []string

	// OpenPrefixes lists path prefixes ("/api") the session gate lets
	// through without a browser session, for a client with no browser
	// that the app authenticates itself (an API key, a bearer token,
	// battery/auth). The Host check still applies to them, and so does
	// the refusal of a forged cross-site write (a cross-origin JSON call
	// is preflighted, so CORS decides it), and a request that does carry a
	// session is still signed in as the local user. Everything else
	// stays gated. Each entry is an absolute path of plain segments;
	// "/" and anything under /__gofastr/ are refused, and the match is
	// on whole segments of the clean, unescaped path, so /apix and
	// /api/../admin stay gated. Logged as a warning at every start.
	OpenPrefixes []string

	// BindAddress is the IP address the daemon and the dev run listen
	// on. Empty means 127.0.0.1. Set it ("0.0.0.0", or one interface's
	// address) when a caller outside this machine's loopback has to
	// reach the app: a container, a WSL2 guest, another machine. The
	// Host check stays: Host must still name 127.0.0.1, localhost, a
	// specific BindAddress, or an AllowHosts entry, so DNS rebinding
	// stays blocked. The browser session still only comes from
	// `service open` on this machine; pair this with OpenPrefixes for
	// the routes those callers use. Logged as a warning at every start.
	BindAddress string

	// InsecureTokenInURL lets `open --print` print a one-shot sign-in
	// URL instead of opening a local page, for a headless machine
	// reached over an SSH tunnel. The token is still single-use and
	// expires in 60 seconds, but it passes through argv, terminal
	// scrollback and browser history. Logged as a warning at every
	// start.
	InsecureTokenInURL bool

	// Stdout and Stderr receive the CLI's output. Nil means os.Stdout
	// and os.Stderr.
	Stdout, Stderr io.Writer

	// Stdin feeds `service mcp`. Nil means os.Stdin.
	Stdin io.Reader
}

// Runtime is what the host's builder receives: the state directory
// and secret of the mode being run, resolved before the App exists.
type Runtime struct {
	// DataDir is the 0700 state directory. Put the app's database here.
	DataDir string
	// Secret is the persisted app secret (32 random bytes, base64url),
	// minted on first run.
	Secret string
	// Dev is true for the foreground dev run.
	Dev bool
}

// MCPCommand is the argv an agent's MCP config runs to reach this
// daemon or dev run: `<exe> service mcp [--dev] --state-dir <DataDir>`.
// It is what the run's local record carries as mcp_command. Nil when
// the executable path is unknown.
func (rt Runtime) MCPCommand() []string {
	return mcpCommand(rt.Dev, rt.DataDir)
}

// AppOptions opens the mode's SQLite database (app.db in DataDir)
// and returns it with the persisted secret, so sessions and signed
// tokens survive a restart. An app with its own database skips this
// and passes framework.WithSecret(rt.Secret) and its own WithDB.
func (rt Runtime) AppOptions() ([]framework.AppOption, error) {
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(filepath.Join(rt.DataDir, "app.db")))
	if err != nil {
		return nil, fmt.Errorf("service: open app database: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("service: app database unreachable: %w", err)
	}
	return []framework.AppOption{framework.WithDB(db), framework.WithSecret(rt.Secret)}, nil
}

// Builder constructs the App for a daemon or dev run. It is never
// called for the other verbs.
type Builder func(Runtime) (*framework.App, error)

// Battery is the service host. Construct with New; call Run from main.
type Battery struct {
	cfg  Config
	err  error
	mgr  manager
	tool toolRunner

	gate *gate

	// baseOverride is the daemon's --base-dir; empty elsewhere.
	baseOverride string

	mu  sync.Mutex
	app *framework.App
}

// defaultSessionTTL is the browser session lifetime when
// Config.SessionTTL is zero.
const defaultSessionTTL = 7 * 24 * time.Hour

// New validates cfg and constructs the Battery. A config error is held
// and returned by Run, so main has one error path.
func New(cfg Config) *Battery {
	// Own copies of the slices validateConfig checks: a caller that
	// edits its slice after New must not change what the gate serves.
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

// Init implements framework.Battery: it installs the loopback gate,
// which also answers the sign-in path, in front of the whole server,
// and the owner extractor that reads the local identity the gate puts
// on each signed-in request.
// Outside a daemon or dev run (a test that registers the battery and
// serves the App itself) there is no gate, and Init leaves the App's
// routes open.
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
	// A signed session is the OS user the service runs as. An app that
	// links battery/auth keeps auth's identity end to end: the gate
	// then only gates, and auth signs callers in.
	if localuser.Install() {
		b.gate.user = nil
	} else {
		app.NoteAuthenticator()
	}
	app.WrapServer(b.gate.wrap)
	return nil
}

// Run dispatches the command line. args is os.Args[1:]. No arguments
// is the foreground dev run; "service <verb> ..." runs a verb;
// "--addr <addr>" (what `gofastr dev` passes its child) is the dev run
// on that loopback port; any other first argument is a usage error,
// so an app with its own subcommands checks for those before calling
// Run.
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
	// reAppID: letters/digits at both ends, letters/digits/dots/dashes
	// inside. The id reaches a plist label, a systemd unit name, a task
	// name, a path segment and argv of launchctl/systemctl/schtasks, so
	// a leading dash (option injection) and a leading dot are refused.
	reAppID = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`)
	// reName is the executable base name grammar.
	reName = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$`)
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

// bindHost is the IP the App listens on: Config.BindAddress, else
// 127.0.0.1.
func (b *Battery) bindHost() string {
	if b.cfg.BindAddress != "" {
		return b.cfg.BindAddress
	}
	return "127.0.0.1"
}

// boundHostOK reports whether host (the listener's reported address)
// is the address the battery asked for.
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

// browserAddr is the address `open`, `status` and `start` hand the
// user for a listener bound at addr. A wildcard bind is reached on this
// machine at its loopback, 127.0.0.1 for 0.0.0.0 and ::1 for ::, the
// host the gate pins and the session cookie lives on; a specific
// address is used as bound.
func browserAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if ip, err := netip.ParseAddr(host); err == nil && ip.IsUnspecified() {
		// "::" may be IPv6-only; its loopback is ::1.
		if ip.Is6() {
			return net.JoinHostPort("::1", port)
		}
		return net.JoinHostPort("127.0.0.1", port)
	}
	return addr
}

// isLoopbackIP reports whether s is a loopback IP literal.
func isLoopbackIP(s string) bool {
	ip, err := netip.ParseAddr(s)
	return err == nil && ip.IsLoopback()
}

// extraHosts is AllowHosts plus the specific BindAddress at port: a
// caller that reaches the app on that interface's address sends it as
// Host. A loopback address other than 127.0.0.1 (::1, 127.0.0.2) is
// pinned the same way: `open` hands the browser the address as bound. An unspecified BindAddress (0.0.0.0, ::) adds nothing: the
// address a caller uses is not knowable here, so it goes in AllowHosts.
func (b *Battery) extraHosts(port int) []string {
	hosts := append([]string(nil), b.cfg.AllowHosts...)
	if ip, err := netip.ParseAddr(b.cfg.BindAddress); err == nil {
		switch {
		case !ip.IsUnspecified():
			hosts = append(hosts, net.JoinHostPort(ip.String(), strconv.Itoa(port)))
		case ip.Is6():
			// browserAddr sends the browser to [::1] for a "::" bind.
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

// reHost is a host[:port] made of DNS characters or an IPv4 literal.
var reHost = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?(?::[0-9]{1,5})?$`)

func validateAllowHost(h string) error {
	if !reHost.MatchString(h) {
		return fmt.Errorf("service: Config.AllowHosts entry %q must be host or host:port in ASCII", h)
	}
	return nil
}
