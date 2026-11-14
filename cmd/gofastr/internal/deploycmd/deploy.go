// Package deploycmd implements `gofastr deploy`: putting an app on one
// Linux server over SSH, as a systemd service.
//
// The model is the smallest that holds up: one binary, copied to
// /opt/<name>/releases/<time>/, a "current" link that names the
// release running, a systemd unit that runs it as its own user and
// brings it back after a crash, and a health check that decides
// whether a new release stays. A release that fails its check is
// replaced by the one before it, in the same run.
//
// Nothing is installed on the server but the app: no agent, no
// container runtime. The server needs sshd, systemd and bash, and the
// SSH user needs root or passwordless sudo.
package deploycmd

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
	"github.com/DonaldMurillo/gofastr/internal/sdlisten"
)

const usage = `
  gofastr deploy <user@host> [flags]     build, copy, start, check
  gofastr deploy rollback <user@host>    go back to the release before
  gofastr deploy status <user@host>      what is running

  Flags:
      --name <name>       service name, user and directory (default: this
                          directory's name)
      --pkg <path>        main package to build (default ".")
      --bin <file>        deploy this Linux binary instead of building one
      --env-file <file>   settings and secrets, as KEY=value lines; stored at
                          /etc/<name>/env, readable by root and the app only
      --port <n>          port the app listens on, for the health check
                          (default 8080)
      --health <path>     path that answers 2xx when the app is up
                          (default /healthz)
      --wait <seconds>    how long a new release has to pass its check
                          (default 30)
      --keep <n>          earlier releases kept for rollback (default 5)
      --ssh-port <n>      the server's SSH port
      -i <file>           SSH identity file

  The server needs sshd, systemd and bash. The SSH user needs to be root or
  to have passwordless sudo. See "gofastr docs deploy".

`

// errUsage marks a mistake in the command line.
var errUsage = errors.New("usage")

// RunDeploy runs the command and exits non-zero when it fails.
func RunDeploy(args []string) {
	err := Run(os.Stdout, args, time.Now(), execRunner{})
	if err == nil {
		return
	}
	if !errors.Is(err, flag.ErrHelp) {
		cli.Fail("%s", cli.ScrubTerminalOutput(err.Error()))
	}
	if errors.Is(err, errUsage) || errors.Is(err, flag.ErrHelp) {
		fmt.Print(usage)
	}
	if errors.Is(err, flag.ErrHelp) {
		cli.OsExit(0)
	}
	cli.OsExit(1)
}

type options struct {
	target   string
	name     string
	pkg      string
	bin      string
	envFile  string
	port     int
	health   string
	wait     int
	keep     int
	sshPort  int
	identity string
}

var (
	// A name is a systemd unit, a Unix user and a directory at once, so
	// it is held to what all three take: lower-case letters, digits and
	// hyphens, starting with a letter.
	nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)
	// user@host, or a host or an alias from ~/.ssh/config. It never
	// starts with a hyphen: ssh would read it as an option.
	targetRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*(@[A-Za-z0-9\[][A-Za-z0-9.:_\]-]*)?$`)
	healthRe = regexp.MustCompile(`^/[A-Za-z0-9/_.~-]*$`)
	// What "mktemp -d" answers with on the server.
	uploadRe = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)
)

func (o *options) validate() error {
	switch {
	case !targetRe.MatchString(o.target):
		return fmt.Errorf("%w: %q is not a server: give user@host, or a host from your SSH config", errUsage, o.target)
	case !nameRe.MatchString(o.name):
		return fmt.Errorf("%w: the name %q cannot be a service, a user and a directory: use lower-case letters, digits and hyphens, starting with a letter (--name)", errUsage, o.name)
	case o.port < 1 || o.port > 65535:
		return fmt.Errorf("%w: --port %d is not a port", errUsage, o.port)
	case !healthRe.MatchString(o.health):
		return fmt.Errorf("%w: --health %q is not a plain path such as /healthz", errUsage, o.health)
	case o.wait < 1 || o.wait > 600:
		return fmt.Errorf("%w: --wait takes 1 to 600 seconds", errUsage)
	case o.keep < 0 || o.keep > 100:
		return fmt.Errorf("%w: --keep takes 0 to 100", errUsage)
	case o.sshPort < 0 || o.sshPort > 65535:
		return fmt.Errorf("%w: --ssh-port %d is not a port", errUsage, o.sshPort)
	}
	return nil
}

// runner runs things: ssh on the server, go build here. Tests put a
// recording one in its place.
type runner interface {
	// ssh runs command on the target with stdin, and returns what it
	// printed and its exit code. A non-nil error is ssh itself failing
	// (no route, refused, no such key), not the command.
	ssh(ctx context.Context, o *options, command string, stdin io.Reader) (out string, code int, err error)
	// build compiles pkg for linux/arch into out.
	build(ctx context.Context, pkg, arch, out string) error
	// root is where the server's files start: empty on a server, a
	// directory for a test that runs the scripts on this machine.
	root() string
}

// Run executes one `gofastr deploy` command line, writing to w.
func Run(w io.Writer, args []string, now time.Time, r runner) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: name a server", errUsage)
	}
	sub := "deploy"
	switch args[0] {
	case "--help", "-h", "help":
		return flag.ErrHelp
	case "rollback", "status":
		sub, args = args[0], args[1:]
	}
	o := options{}
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.name, "name", "", "")
	fs.StringVar(&o.pkg, "pkg", ".", "")
	fs.StringVar(&o.bin, "bin", "", "")
	fs.StringVar(&o.envFile, "env-file", "", "")
	fs.IntVar(&o.port, "port", 8080, "")
	fs.StringVar(&o.health, "health", "/healthz", "")
	fs.IntVar(&o.wait, "wait", 30, "")
	fs.IntVar(&o.keep, "keep", 5, "")
	fs.IntVar(&o.sshPort, "ssh-port", 0, "")
	fs.StringVar(&o.identity, "i", "", "")
	// The server may come before or after the flags.
	var positional []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return flag.ErrHelp
			}
			return fmt.Errorf("%w: %v", errUsage, err)
		}
		args = fs.Args()
		if len(args) > 0 {
			positional = append(positional, args[0])
			args = args[1:]
		}
	}
	if len(positional) != 1 {
		return fmt.Errorf("%w: name one server", errUsage)
	}
	o.target = positional[0]
	if o.name == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		o.name = asciicase.Lower(filepath.Base(wd))
	}
	if err := o.validate(); err != nil {
		return err
	}
	ctx := context.Background()
	p := plan{root: r.root(), name: o.name, port: o.port, health: o.health, wait: o.wait, keep: o.keep}
	switch sub {
	case "status":
		out, code, err := r.ssh(ctx, &o, asRoot, strings.NewReader(p.status()))
		if err != nil {
			return err
		}
		fmt.Fprint(w, cli.ScrubTerminalOutput(out))
		if code != 0 {
			return fmt.Errorf("the server could not be asked (exit %d)", code)
		}
		return nil
	case "rollback":
		out, code, err := r.ssh(ctx, &o, asRoot, strings.NewReader(p.rollback()))
		if err != nil {
			return err
		}
		return report(w, &o, out, code)
	}
	return deploy(ctx, w, &o, p, now, r)
}

// asRoot runs the script on stdin as root: directly for root, through
// sudo for anyone else. sudo -n fails at once where it would ask for a
// password, which no one is there to type.
const asRoot = `if [ "$(id -u)" = 0 ]; then bash -s; else sudo -n bash -s; fi`

func deploy(ctx context.Context, w io.Writer, o *options, p plan, now time.Time, r runner) error {
	// One round trip says what to build for and where to put it. The
	// directory is the server's own choice of a fresh, private one.
	out, code, err := r.ssh(ctx, o, `uname -m && umask 077 && mktemp -d`, nil)
	if err != nil {
		return err
	}
	lines := strings.Fields(out)
	if code != 0 || len(lines) != 2 {
		return fmt.Errorf("the server did not answer as a Linux machine would (exit %d): %s", code, strings.TrimSpace(out))
	}
	arch, ok := map[string]string{"x86_64": "amd64", "aarch64": "arm64", "arm64": "arm64"}[lines[0]]
	if !ok {
		return fmt.Errorf("the server is a %s machine; this builds for x86_64 and aarch64", lines[0])
	}
	p.upload = lines[1]
	if !uploadRe.MatchString(p.upload) {
		return fmt.Errorf("the server named an upload directory this will not use: %q", p.upload)
	}
	p.release = now.UTC().Format("20060102T150405Z")

	bin := o.bin
	if bin == "" {
		tmp, err := os.MkdirTemp("", "gofastr-deploy-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		bin = filepath.Join(tmp, o.name)
		fmt.Fprintf(w, "  building %s for linux/%s\n", o.pkg, arch)
		if err := r.build(ctx, o.pkg, arch, bin); err != nil {
			return err
		}
	}
	// A binary that can take a socket from systemd gets its port held
	// for it; any other opens its own, as before.
	if p.socket, err = takesSocket(bin); err != nil {
		return err
	}
	if p.socket {
		fmt.Fprintf(w, "  systemd holds port %d: restarts do not close it\n", o.port)
	}
	if err := send(ctx, o, r, bin, p.upload+"/bin"); err != nil {
		return err
	}
	if o.envFile != "" {
		if err := send(ctx, o, r, o.envFile, p.upload+"/env"); err != nil {
			return err
		}
		p.hasEnv = true
	}
	fmt.Fprintf(w, "  starting release %s on %s\n", p.release, o.target)
	out, code, err = r.ssh(ctx, o, asRoot, strings.NewReader(p.install()))
	if err != nil {
		return err
	}
	return report(w, o, out, code)
}

// takesSocket reports whether the binary at path links the framework's
// socket hand-off, by the marker that code leaves in it.
func takesSocket(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	marker := []byte(sdlisten.Marker)
	// Read in pieces that overlap by a marker's length, so one that
	// straddles two reads is still found.
	buf := make([]byte, 1<<20)
	keep := 0
	for {
		n, err := f.Read(buf[keep:])
		if bytes.Contains(buf[:keep+n], marker) {
			return true, nil
		}
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		keep = copy(buf, buf[max(keep+n-len(marker)+1, 0):keep+n])
	}
}

// send copies a local file to path on the server through ssh's stdin:
// one program to depend on, and the file is created private.
func send(ctx context.Context, o *options, r runner, local, path string) error {
	f, err := os.Open(local)
	if err != nil {
		return err
	}
	defer f.Close()
	out, code, err := r.ssh(ctx, o, "umask 077 && cat > "+shellQuote(path), f)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("copying %s to the server failed (exit %d): %s", filepath.Base(local), code, strings.TrimSpace(out))
	}
	return nil
}

// report turns a script's last line and exit code into what happened.
func report(w io.Writer, o *options, out string, code int) error {
	out = cli.ScrubTerminalOutput(strings.TrimRight(out, "\n"))
	body, last := out, ""
	if i := strings.LastIndexByte(out, '\n'); i >= 0 {
		body, last = out[:i], out[i+1:]
	} else {
		body, last = "", out
	}
	word, release, _ := strings.Cut(last, " ")
	switch {
	case code == 0 && word == "DEPLOYED":
		fmt.Fprintf(w, "  %s is running release %s and answers its health check\n", o.name, release)
		return nil
	case code == exitRolledBack && word == "ROLLED-BACK":
		if body != "" {
			fmt.Fprintln(w, body)
		}
		return fmt.Errorf("the release did not pass its health check on port %d%s within %d seconds; %s is back on release %s", o.port, o.health, o.wait, o.name, release)
	case code == exitDown && word == "DOWN":
		if body != "" {
			fmt.Fprintln(w, body)
		}
		return fmt.Errorf("the release did not pass its health check on port %d%s within %d seconds, and no earlier release came up in its place: %s is not serving (release %s is the one in place)", o.port, o.health, o.wait, o.name, release)
	case code == exitNoPrevious:
		return fmt.Errorf("%s has no earlier release kept on %s", o.name, o.target)
	}
	if out != "" {
		fmt.Fprintln(w, out)
	}
	return fmt.Errorf("the server stopped part way (exit %d). If it says sudo needs a password, give %s passwordless sudo or deploy as root", code, o.target)
}

// execRunner is the real runner.
type execRunner struct{}

func (execRunner) ssh(ctx context.Context, o *options, command string, stdin io.Reader) (string, int, error) {
	args := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=15"}
	if o.sshPort != 0 {
		args = append(args, "-p", strconv.Itoa(o.sshPort))
	}
	if o.identity != "" {
		args = append(args, "-i", o.identity)
	}
	// "--" ends ssh's own options: the target is the next word whatever
	// it looks like.
	args = append(args, "--", o.target, command)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = stdin
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	// A child that keeps the pipe open must not hold Wait for ever.
	cmd.WaitDelay = 10 * time.Second
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out.String(), 0, nil
	case errors.As(err, &exit) && exit.ExitCode() == 255:
		// 255 is ssh's own failure, not the remote command's.
		return "", 255, fmt.Errorf("ssh could not reach %s: %s", o.target, strings.TrimSpace(cli.ScrubTerminalOutput(out.String())))
	case errors.As(err, &exit):
		return out.String(), exit.ExitCode(), nil
	}
	return "", 0, fmt.Errorf("ssh could not be run: %w", err)
}

func (execRunner) root() string { return "" }

func (execRunner) build(ctx context.Context, pkg, arch, out string) error {
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-ldflags", "-s -w", "-o", out, pkg)
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+arch, "CGO_ENABLED=0")
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	cmd.WaitDelay = 10 * time.Second
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("the build failed:\n%s", strings.TrimSpace(buf.String()))
	}
	return nil
}
