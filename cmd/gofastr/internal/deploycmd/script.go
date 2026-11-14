package deploycmd

import (
	"fmt"
	"strconv"
	"strings"
)

// Exit codes of the remote scripts, which the command turns into what
// it tells the person.
const (
	exitRolledBack = 3 // the new release failed its check; the one before runs again
	exitDown       = 4 // the new release failed its check and there was none before
	exitNoPrevious = 5 // rollback: no earlier release is kept
)

// plan is everything the remote scripts are written from. Each value
// has passed its own check (see options.validate) before it gets here:
// the scripts are text a root shell runs, and a name is only ever a
// name.
type plan struct {
	// root prefixes every path the script touches. Empty on a server;
	// a test gives a temporary directory, so the script's own logic
	// (the swap, the rollback, the pruning) runs for real without root.
	root string

	name    string // service, user, directory
	release string // "20261114T033412Z"
	upload  string // remote directory holding "bin" and, with hasEnv, "env"
	hasEnv  bool
	// socket hands the port to systemd, which keeps it open through
	// restarts: set when the binary can take a socket passed to it.
	socket bool
	port   int
	health string // "/healthz"
	wait   int    // seconds to wait for the health check
	keep   int    // releases kept beside the running one
}

// unit is the systemd unit of an app. It runs as its own user, may
// write only its state directory, and comes back after a crash, every
// time: with systemd's default start limit an app that failed five
// times in ten seconds is left stopped until someone logs in. HOME
// and XDG_STATE_HOME point there, so anything the app keeps by default
// (the telemetry files among it) lands under /var/lib/<name>.
// GOFASTR_ENV says production, which turns the dev-only features off
// (the ungated MCP tools, livereload) whatever GOFASTR_DEV says: an env
// file copied from a laptop often carries it. The env file is read
// after, so a server that is meant to be something else can say so.
func (p plan) unit() string {
	held := ""
	if p.socket {
		// The service is started with the socket and never without it.
		held = "Requires=" + p.name + ".socket\nAfter=" + p.name + ".socket\n"
	}
	return `[Unit]
Description=` + p.name + `
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=0
` + held + `
[Service]
User=` + p.name + `
Group=` + p.name + `
WorkingDirectory=` + p.root + `/var/lib/` + p.name + `
StateDirectory=` + p.name + `
StateDirectoryMode=0750
Environment=HOME=` + p.root + `/var/lib/` + p.name + `
Environment=XDG_STATE_HOME=` + p.root + `/var/lib/` + p.name + `/state
Environment=GOFASTR_ENV=production
EnvironmentFile=-` + p.root + `/etc/` + p.name + `/env
ExecStart=` + p.root + `/opt/` + p.name + `/current/` + p.name + `
Restart=always
RestartSec=1
LimitNOFILE=65536
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectControlGroups=true
RestrictSUIDSGID=true
AmbientCapabilities=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
`
}

// socketUnit is the unit that holds the app's port. systemd opens it
// once and keeps it: while the app is down, between two releases or
// after a crash, connections wait in its queue for the app that starts
// next, where a port the app opened itself would refuse them.
func (p plan) socketUnit() string {
	return `[Unit]
Description=` + p.name + ` port

[Socket]
ListenStream=` + strconv.Itoa(p.port) + `

[Install]
WantedBy=sockets.target
`
}

// preamble is the head of every script: fail on the first error, and
// the paths and the health check the scripts share.
func (p plan) preamble() string {
	var b strings.Builder
	b.WriteString("set -eu\n")
	fmt.Fprintf(&b, "NAME=%s\n", p.name)
	fmt.Fprintf(&b, "ROOT=%s\n", shellQuote(p.root))
	b.WriteString(`APP="$ROOT/opt/$NAME"
STATE="$ROOT/var/lib/$NAME"
UNITS="$ROOT/etc/systemd/system"
`)
	fmt.Fprintf(&b, "PORT=%d\nHEALTH=%s\nWAIT=%d\n", p.port, shellQuote(p.health), p.wait)
	// The check asks the app itself, over loopback, with nothing but
	// bash: a small server has no curl to count on. A release is up
	// when systemd says the service is running and the app answers the
	// health path with a 2xx.
	b.WriteString(`healthy() {
  i=0
  while [ "$i" -lt "$WAIT" ]; do
    if systemctl is-active --quiet "$NAME" && probe; then return 0; fi
    i=$((i + 1))
    sleep 1
  done
  return 1
}
probe() {
  { exec 3<>"/dev/tcp/127.0.0.1/$PORT"; } 2>/dev/null || return 1
  printf 'GET %s HTTP/1.0\r\nHost: localhost\r\n\r\n' "$HEALTH" >&3
  # Two seconds for the first line: with systemd holding the port a
  # connection is accepted while the app is down, and waits.
  IFS= read -r -t 2 line <&3 || line=""
  exec 3<&- 3>&-
  case "$line" in "HTTP/"*" 2"[0-9][0-9]*) return 0 ;; esac
  return 1
}
point() {
  ln -sfn "releases/$1" "$APP/current.new"
  mv -T "$APP/current.new" "$APP/current"
}
# said prints what the app itself last wrote: its own lines, without
# systemd's account of each restart around them.
said() {
  journalctl "_SYSTEMD_UNIT=$NAME.service" -n 20 --no-pager -o cat 2>/dev/null || true
}
# start runs the release "current" names. A release that kept failing
# leaves the unit marked failed, and systemd then refuses the next start
# with an error: the mark is cleared first, and whether the app came up
# is the health check's to say, not this command's exit code.
start() {
  systemctl reset-failed "$NAME" >/dev/null 2>&1 || true
  systemctl restart "$NAME" >/dev/null 2>&1 || true
}
running() {
  if [ -L "$APP/current" ]; then basename "$(readlink "$APP/current")"; fi
}
# apply makes release $1 the one in place: its units as well as its
# binary. A release keeps the units it was deployed with, so going back
# to it goes back to how it was run, the port's owner included: a
# release that takes its port from systemd gets the socket unit, one
# that opens its own gets the port back. (A release from before units
# were kept has none, and the units in place stay.)
apply() {
  kept="$APP/releases/$1/units"
  if [ -f "$kept/$NAME.service" ]; then
    install -m 0644 "$kept/$NAME.service" "$UNITS/$NAME.service"
    if [ -f "$kept/$NAME.socket" ]; then
      install -m 0644 "$kept/$NAME.socket" "$UNITS/$NAME.socket"
      systemctl daemon-reload
      # The first time, the app running holds the port itself: it is
      # stopped so systemd can open it. That hand-over is the last time
      # the port closes.
      if ! systemctl is-active --quiet "$NAME.socket"; then
        systemctl stop "$NAME" >/dev/null 2>&1 || true
        systemctl enable --now "$NAME.socket" >/dev/null 2>&1
      fi
    else
      if [ -f "$UNITS/$NAME.socket" ]; then
        systemctl disable --now "$NAME.socket" >/dev/null 2>&1 || true
        rm -f "$UNITS/$NAME.socket"
      fi
      systemctl daemon-reload
    fi
  fi
  point "$1"
}
`)
	return b.String()
}

// install is the script that puts a release in place and starts it. It
// leaves the server running the new release, or the one before it.
func (p plan) install() string {
	var b strings.Builder
	b.WriteString(p.preamble())
	fmt.Fprintf(&b, "REL=%s\nUPLOAD=%s\nKEEP=%d\n", p.release, shellQuote(p.upload), p.keep)
	b.WriteString(`
# The app's own user: no login, no shell, home in its state directory.
if ! id -u "$NAME" >/dev/null 2>&1; then
  useradd --system --home-dir "$STATE" --shell /usr/sbin/nologin "$NAME"
fi

install -d -m 0755 "$APP" "$APP/releases" "$APP/releases/$REL"
install -m 0755 "$UPLOAD/bin" "$APP/releases/$REL/$NAME"
install -d -m 0750 "$STATE"
chown "$NAME:$NAME" "$STATE"

# Settings and secrets: readable by root and the app, by no one else.
install -d -m 0750 "$ROOT/etc/$NAME"
chown "root:$NAME" "$ROOT/etc/$NAME"
`)
	if p.hasEnv {
		b.WriteString(`install -m 0640 "$UPLOAD/env" "$ROOT/etc/$NAME/env"
chown "root:$NAME" "$ROOT/etc/$NAME/env"
`)
	}
	b.WriteString(`rm -rf "$UPLOAD"

install -d -m 0755 "$UNITS" "$APP/releases/$REL/units"
cat > "$APP/releases/$REL/units/$NAME.service" <<'GOFASTR_UNIT'
`)
	b.WriteString(p.unit())
	b.WriteString("GOFASTR_UNIT\n")
	if p.socket {
		b.WriteString("cat > \"$APP/releases/$REL/units/$NAME.socket\" <<'GOFASTR_SOCKET'\n")
		b.WriteString(p.socketUnit())
		b.WriteString("GOFASTR_SOCKET\n")
	}
	b.WriteString(`
PREV="$(running)"
apply "$REL"
systemctl enable "$NAME" >/dev/null 2>&1
start

if ! healthy; then
  echo "--- the last lines the new release logged ---"
  said
  if [ -n "$PREV" ] && [ "$PREV" != "$REL" ] && [ -x "$APP/releases/$PREV/$NAME" ]; then
    apply "$PREV"
    start
    rm -rf "$APP/releases/$REL"
    # Said only once it is true: the release before answers again.
    if healthy; then
      echo "ROLLED-BACK $PREV"
      exit ` + strconv.Itoa(exitRolledBack) + `
    fi
    echo "DOWN $PREV"
    exit ` + strconv.Itoa(exitDown) + `
  fi
  echo "DOWN $REL"
  exit ` + strconv.Itoa(exitDown) + `
fi

# Old releases go, newest kept: the running one and KEEP before it.
ls -1 "$APP/releases" | sort -r | tail -n "+$((KEEP + 2))" | while IFS= read -r old; do
  rm -rf "$APP/releases/$old"
done
echo "DEPLOYED $REL"
`)
	return b.String()
}

// rollback is the script that points the server at the release before
// the running one and starts it.
func (p plan) rollback() string {
	return p.preamble() + `
CUR="$(running)"
PREV="$(ls -1 "$APP/releases" 2>/dev/null | sort -r | awk -v cur="$CUR" 'seen && !done { print; done = 1 } $0 == cur { seen = 1 }')"
if [ -z "$CUR" ] || [ -z "$PREV" ]; then
  echo "NO-PREVIOUS"
  exit ` + strconv.Itoa(exitNoPrevious) + `
fi
apply "$PREV"
start
if ! healthy; then
  said
  apply "$CUR"
  start
  if healthy; then
    echo "ROLLED-BACK $CUR"
    exit ` + strconv.Itoa(exitRolledBack) + `
  fi
  echo "DOWN $CUR"
  exit ` + strconv.Itoa(exitDown) + `
fi
echo "DEPLOYED $PREV"
`
}

// status is the script that says what is running.
func (p plan) status() string {
	return p.preamble() + `
echo "release: $(running)"
echo "service: $(systemctl is-active "$NAME" 2>/dev/null || true)"
if probe; then echo "health: ok"; else echo "health: no answer on port $PORT$HEALTH"; fi
echo "kept:"
ls -1 "$APP/releases" 2>/dev/null | sort -r | sed 's/^/  /'
`
}

// shellQuote writes s as one single-quoted shell word.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
