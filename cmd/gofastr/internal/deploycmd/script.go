package deploycmd

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	exitRolledBack = 3
	exitDown       = 4
	exitNoPrevious = 5
)

type plan struct {
	root string

	name    string
	release string
	upload  string
	hasEnv  bool
	socket  bool
	port    int
	health  string
	wait    int
	keep    int
}

func (p plan) unit() string {
	held := ""
	if p.socket {
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
EnvironmentFile=-` + p.root + `/opt/` + p.name + `/current/env
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

func (p plan) socketUnit() string {
	return `[Unit]
Description=` + p.name + ` port

[Socket]
ListenStream=` + strconv.Itoa(p.port) + `

[Install]
WantedBy=sockets.target
`
}

func (p plan) appPath() string {
	return fmt.Sprintf("set -eu\nNAME=%s\nROOT=%s\nAPP=\"$ROOT/opt/$NAME\"\n", p.name, shellQuote(p.root))
}

func (p plan) preamble() string {
	var b strings.Builder
	b.WriteString(p.appPath())
	b.WriteString(`STATE="$ROOT/var/lib/$NAME"
UNITS="$ROOT/etc/systemd/system"
LEGACY_ENV="$ROOT/etc/$NAME/env"
`)
	fmt.Fprintf(&b, "DEFAULT_PORT=%d\nDEFAULT_HEALTH=%s\nWAIT=%d\n", p.port, shellQuote(p.health), p.wait)
	b.WriteString(`read_health() {
  PORT="$DEFAULT_PORT" HEALTH="$DEFAULT_HEALTH"
  if [ -n "$1" ] && [ -f "$APP/releases/$1/health" ]; then
    IFS=' ' read -r PORT HEALTH < "$APP/releases/$1/health"
  fi
}
probe() {
  timeout --foreground 2 bash -c '
    { exec 3<>"/dev/tcp/127.0.0.1/$1"; } 2>/dev/null || exit 1
    printf "GET %s HTTP/1.0\r\nHost: localhost\r\n\r\n" "$2" >&3 || exit 1
    IFS= read -r line <&3 || exit 1
    case "$line" in "HTTP/"*" 2"[0-9][0-9]*) exit 0 ;; esac
    exit 1
  ' bash "$1" "$2"
}
healthy() {
  export -f probe
  # The foreground probe stays in this timeout's group, so the deadline
  # also cuts off a probe or sleep already in progress.
  timeout "$WAIT" bash -c '
    until systemctl is-active --quiet "$1" && probe "$2" "$3"; do
      sleep 1
    done
  ' bash "$NAME" "$PORT" "$HEALTH"
}
point() {
  ln -sfn "releases/$1" "$APP/current.$1.new" || return
  mv -T "$APP/current.$1.new" "$APP/current"
}
# said prints what the app itself last wrote: its own lines, without
# systemd's account of each restart around them.
said() {
  journalctl "_SYSTEMD_UNIT=$NAME.service" -n 20 --no-pager -o cat 2>/dev/null || true
}
start() {
  if [ "${1:-}" = enable ]; then
    systemctl enable "$NAME" || return
  fi
  systemctl reset-failed "$NAME" >/dev/null 2>&1 || true
  systemctl restart "$NAME"
}
running() {
  if [ -L "$APP/current" ]; then basename "$(readlink "$APP/current")"; fi
}
apply() {
  local release="$APP/releases/$1" kept="$APP/releases/$1/units" socket_changed=no
  local socket="$UNITS/$NAME.socket" kept_socket="$kept/$NAME.socket"
  if [ ! -f "$release/env" ] && [ -f "$LEGACY_ENV" ]; then
    install -m 0640 "$LEGACY_ENV" "$release/env" || return
    chown "root:$NAME" "$release/env" || return
  fi
  point "$1" || return
  if [ -f "$kept/$NAME.service" ]; then
    install -m 0644 "$kept/$NAME.service" "$UNITS/$NAME.service" || return
    if [ -f "$kept_socket" ]; then
      if [ ! -f "$socket" ] || [ "$(< "$kept_socket")" != "$(< "$socket")" ]; then
        install -m 0644 "$kept_socket" "$socket" || return
        socket_changed=yes
      fi
      systemctl daemon-reload || return
      if [ "$socket_changed" = yes ] || ! systemctl is-active --quiet "$NAME.socket"; then
        systemctl stop "$NAME" || return
        if systemctl is-active --quiet "$NAME.socket"; then
          systemctl stop "$NAME.socket" || return
        fi
        systemctl enable --now "$NAME.socket" || return
      fi
    else
      if [ -f "$socket" ]; then
        systemctl disable --now "$NAME.socket" || return
        rm -f "$socket" || return
      fi
      systemctl daemon-reload || return
    fi
  fi
}
activate() {
  if ! read_health "$1"; then
    failure="could not read the health settings for release $1"
    return 1
  fi
  if ! apply "$1" || ! start "${2:-}"; then
    failure="the release could not be activated"
    return 1
  fi
  if ! healthy; then
    failure="the release did not pass its health check on port $PORT$HEALTH within $WAIT seconds"
    return 1
  fi
}
recover() {
  local reason="$failure"
  if release_ready "$1"; then
    if activate "$1"; then
      outcome="ROLLED-BACK $1 $reason"
      return ` + strconv.Itoa(exitRolledBack) + `
    fi
    printf 'Recovery failed: %s\n' "$failure"
  fi
  outcome="DOWN $(running) $reason"
  return ` + strconv.Itoa(exitDown) + `
}
`)
	return b.String()
}

func (p plan) locked(script string) string {
	return p.appPath() + `install -d -m 0755 "$APP"
exec flock -x -o "$APP/.deploy.lock" bash -s <<'GOFASTR_LOCKED'
` + p.preamble() + `set -o pipefail
ORDER="$APP/.release-order"
release_dirs() {
  find "$APP/releases" -mindepth 1 -maxdepth 1 -type d ! -name '.*' -printf '%f\n'
}
release_ready() {
  local dir="$APP/releases/$1"
  [ -f "$dir/$NAME" ] && [ -x "$dir/$NAME" ] && [ ! -e "$dir/.installing" ] && [ ! -e "$dir/.failed" ]
}
reconcile_order() {
  local current="" recorded=/dev/null
  if [ -f "$ORDER" ]; then
    recorded="$ORDER"
    current="$1"
  fi
  if [ -d "$APP/releases" ]; then
    release_dirs | LC_ALL=C sort -r | while IFS= read -r release; do
      if release_ready "$release"; then
        printf '%s\n' "$release"
      fi
    done | awk -v cur="$current" '
      FILENAME != "-" { if (NF && !($0 in known)) { known[$0] = 1; recorded[++n] = $0 }; next }
      { retained[$0] = 1; if (!($0 in known)) missing[++m] = $0 }
      END {
        if (cur in retained && !(cur in known)) print cur
        for (i = 1; i <= m; i++) if (missing[i] != cur) print missing[i]
        for (i = 1; i <= n; i++) if (recorded[i] in retained) print recorded[i]
      }' "$recorded" - > "$ORDER.new"
  else
    : > "$ORDER.new"
  fi
  mv -T "$ORDER.new" "$ORDER"
}
` + script + "GOFASTR_LOCKED\n"
}

func (p plan) install() string {
	var b strings.Builder
	fmt.Fprintf(&b, "REL=%s\nUPLOAD=%s\nKEEP=%d\n", p.release, shellQuote(p.upload), p.keep)
	b.WriteString(`
PREV="$(running)"
reconcile_order "$PREV"
# The app's own user: no login, no shell, home in its state directory.
if ! id -u "$NAME" >/dev/null 2>&1; then
  useradd --system --home-dir "$STATE" --shell /usr/sbin/nologin "$NAME"
fi

install -d -m 0755 "$APP/releases"
if ! mkdir -m 0755 "$APP/releases/$REL"; then
  rm -rf "$UPLOAD"
  exit 1
fi
: > "$APP/releases/$REL/.installing"
install -m 0755 "$UPLOAD/bin" "$APP/releases/$REL/$NAME"
install -d -m 0750 "$STATE"
chown "$NAME:$NAME" "$STATE"
`)
	if p.hasEnv {
		b.WriteString(`env_source="$UPLOAD/env"
`)
	} else {
		b.WriteString(`if [ -n "$PREV" ] && [ -f "$APP/releases/$PREV/env" ]; then
  env_source="$APP/releases/$PREV/env"
elif [ -f "$LEGACY_ENV" ]; then
  env_source="$LEGACY_ENV"
else
  env_source=/dev/null
fi
`)
	}
	b.WriteString(`install -m 0640 "$env_source" "$APP/releases/$REL/env"
chown "root:$NAME" "$APP/releases/$REL/env"
printf '%s %s\n' "$DEFAULT_PORT" "$DEFAULT_HEALTH" > "$APP/releases/$REL/health"
rm -rf "$UPLOAD"

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
{
  printf '%s\n' "$REL"
  head -n "$KEEP" "$ORDER"
} > "$ORDER.new"

if ! activate "$REL" enable; then
  if ! touch "$APP/releases/$REL/.failed"; then
    echo "Could not record failed release $REL" >&2
  fi
  echo "--- the last lines the new release logged ---"
  said
  if [ -n "$PREV" ] && ! release_ready "$PREV"; then
    PREV="$(head -n 1 "$ORDER")"
  fi
  if [ -n "$PREV" ]; then
    code=0
    recover "$PREV" || code=$?
    if [ "$(running)" != "$REL" ]; then
      rm -rf "$APP/releases/$REL"
    fi
    printf '%s\n' "$outcome"
    exit "$code"
  fi
  printf 'DOWN %s %s\n' "$REL" "$failure"
  exit ` + strconv.Itoa(exitDown) + `
fi

unlink "$APP/releases/$REL/.installing"
mv -T "$ORDER.new" "$ORDER"
retained="$(cat "$ORDER")"
release_dirs | while IFS= read -r old; do
  if ! grep -Fxq -- "$old" <<< "$retained"; then rm -rf "$APP/releases/$old"; fi
done
echo "DEPLOYED $REL"
`)
	return p.locked(b.String())
}

func (p plan) rollback() string {
	return p.locked(`
CUR="$(running)"
reconcile_order "$CUR"
PREV="$(
  awk -v cur="$CUR" '
    NR == 1 { latest = $0 }
    $0 == cur { found = 1; next }
    found && !done { print; done = 1 }
    END { if (!found && NR > 0) print latest }' "$ORDER"
)"
if [ -z "$CUR" ] || [ -z "$PREV" ]; then
  echo "NO-PREVIOUS"
  exit ` + strconv.Itoa(exitNoPrevious) + `
fi
if ! activate "$PREV"; then
  said
  code=0
  recover "$CUR" || code=$?
  printf '%s\n' "$outcome"
  exit "$code"
fi
echo "DEPLOYED $PREV"
`)
}

func (p plan) status() string {
	return p.preamble() + `
REL="$(running)"
echo "release: $REL"
state="$(systemctl is-active "$NAME" 2>/dev/null || true)"
echo "service: $state"
if [ "$state" != active ]; then
  echo "health: service is not active"
else
  read_health "$REL"
  if probe "$PORT" "$HEALTH"; then echo "health: ok"; else echo "health: no answer on port $PORT$HEALTH"; fi
fi
echo "kept:"
ls -1 "$APP/releases" 2>/dev/null | sort -r | sed 's/^/  /'
`
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
