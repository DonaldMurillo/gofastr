package service

import (
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"
)

func TestManagerForEachOS(t *testing.T) {
	want := map[string]string{"darwin": "launchd", "linux": "systemd --user", "windows": "Task Scheduler", "freebsd": "none"}
	for goos, kind := range want {
		if got := managerFor(goos).kind(); got != kind {
			t.Errorf("managerFor(%s).kind() = %q; want %q", goos, got, kind)
		}
	}
}

func TestUnsupportedOSRefusesInstall(t *testing.T) {
	b, ft, _ := testBattery(t, "freebsd", Config{})
	err := b.install(b.cfg.Port)
	if err == nil || !strings.Contains(err.Error(), "not supported on freebsd") {
		t.Fatalf("install on freebsd error = %v", err)
	}
	if len(ft.got()) != 0 {
		t.Fatalf("ran tools on an unsupported OS: %v", ft.got())
	}
}

// sudo <app> service install would register the service in root's
// account, not the user's: refused before any tool runs.
func TestInstallRefusesRoot(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		b, ft, _ := testBattery(t, goos, Config{})
		geteuid = func() int { return 0 }
		if err := b.install(b.cfg.Port); err == nil || !strings.Contains(err.Error(), "as root") {
			t.Errorf("%s install as root error = %v", goos, err)
		}
		if len(ft.got()) != 0 {
			t.Errorf("%s install as root ran tools: %v", goos, ft.got())
		}
	}
}

func setHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	return home
}

func TestLaunchdInstallArgv(t *testing.T) {
	home := setHome(t)
	b, ft, _ := testBattery(t, "darwin", Config{})
	prev := currentUID
	currentUID = func() int { return 501 }
	t.Cleanup(func() { currentUID = prev })

	if err := (launchd{}).install(b, "/opt/hello", 47811); err != nil {
		t.Fatal(err)
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", "dev.gofastr.svctest.plist")
	want := []string{
		"launchctl bootout gui/501/dev.gofastr.svctest",
		"launchctl enable gui/501/dev.gofastr.svctest",
		"launchctl bootstrap gui/501 " + plist,
	}
	if got := ft.got(); !reflect.DeepEqual(got, want) {
		t.Fatalf("argv\n got %q\nwant %q", got, want)
	}
	filepermtest.AssertOwnerOnly(t, plist, 0o600)
}

func TestLaunchdStartFallsBackToBootstrap(t *testing.T) {
	home := setHome(t)
	b, ft, _ := testBattery(t, "darwin", Config{})
	prev := currentUID
	currentUID = func() int { return 501 }
	t.Cleanup(func() { currentUID = prev })
	ft.fail["launchctl kickstart"] = errors.New("exit status 113")

	if err := (launchd{}).start(b); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"launchctl kickstart gui/501/dev.gofastr.svctest",
		"launchctl bootstrap gui/501 " + filepath.Join(home, "Library", "LaunchAgents", "dev.gofastr.svctest.plist"),
	}
	if got := ft.got(); !reflect.DeepEqual(got, want) {
		t.Fatalf("argv\n got %q\nwant %q", got, want)
	}
}

// plistDoc decodes the subset of a plist the tests read: the top dict's
// keys and values in order.
type plistDoc struct {
	Dict struct {
		Items []struct {
			XMLName xml.Name
			Value   string   `xml:",chardata"`
			Strings []string `xml:"string"`
		} `xml:",any"`
	} `xml:"dict"`
}

// An executable path carrying XML metacharacters stays one argv word.
func TestLaunchdPlistEscapes(t *testing.T) {
	b, _, _ := testBattery(t, "darwin", Config{})
	exe := `/Users/a&b/<x>/"q"/hello`
	raw := (launchd{}).plist(b, daemonArgs(exe, 47811, "/base"), "/logs/service.log")
	var doc plistDoc
	if err := xml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("plist is not well-formed XML: %v\n%s", err, raw)
	}
	var args []string
	for i, it := range doc.Dict.Items {
		if it.XMLName.Local == "key" && it.Value == "ProgramArguments" {
			args = doc.Dict.Items[i+1].Strings
		}
	}
	want := []string{exe, "service", "daemon", "--port", "47811", "--base-dir", "/base"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("ProgramArguments = %q; want %q", args, want)
	}
	for _, s := range []string{"<key>RunAtLoad</key>", "<key>SuccessfulExit</key>\n\t\t<false/>", "<string>Background</string>"} {
		if !strings.Contains(string(raw), s) {
			t.Errorf("plist lacks %q", s)
		}
	}
}

func TestLaunchdUninstallRemovesPlist(t *testing.T) {
	home := setHome(t)
	b, _, _ := testBattery(t, "darwin", Config{})
	if err := (launchd{}).install(b, "/opt/hello", 47811); err != nil {
		t.Fatal(err)
	}
	if ok, _ := (launchd{}).installed(b); !ok {
		t.Fatal("installed() false after install")
	}
	if err := (launchd{}).uninstall(b); err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(home, "Library", "LaunchAgents", "dev.gofastr.svctest.plist")) {
		t.Fatal("plist left behind")
	}
	if err := (launchd{}).uninstall(b); err != nil {
		t.Fatalf("second uninstall: %v", err)
	}
}

func bootedSystemd(t *testing.T, booted bool) {
	t.Helper()
	prev := systemdBooted
	systemdBooted = func() bool { return booted }
	t.Cleanup(func() { systemdBooted = prev })
}

func TestSystemdInstallArgv(t *testing.T) {
	setHome(t)
	bootedSystemd(t, true)
	b, ft, _ := testBattery(t, "linux", Config{})
	if err := (systemdUser{}).install(b, "/opt/hello", 47811); err != nil {
		t.Fatal(err)
	}
	base := os.Getenv(dataDirEnv)
	path := filepath.Join(base, "dev.gofastr.svctest", "dev.gofastr.svctest.service")
	want := []string{
		"systemctl --user daemon-reload",
		"systemctl --user enable " + path,
		"systemctl --user restart dev.gofastr.svctest.service",
	}
	if got := ft.got(); !reflect.DeepEqual(got, want) {
		t.Fatalf("argv\n got %q\nwant %q", got, want)
	}
	unit, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	exec := `ExecStart="/opt/hello" "service" "daemon" "--port" "47811" "--base-dir" "` + strings.ReplaceAll(base, `\`, `\\`) + `"`
	for _, s := range []string{"Type=notify", "NotifyAccess=main", exec, "Restart=on-failure", "WantedBy=default.target"} {
		if !strings.Contains(string(unit), s) {
			t.Errorf("unit lacks %q:\n%s", s, unit)
		}
	}
}

func TestSystemdInstallNeedsSystemd(t *testing.T) {
	setHome(t)
	bootedSystemd(t, false)
	b, ft, _ := testBattery(t, "linux", Config{})
	err := (systemdUser{}).install(b, "/opt/hello", 47811)
	if err == nil || !strings.Contains(err.Error(), "systemd is not running") {
		t.Fatalf("error = %v", err)
	}
	if len(ft.got()) != 0 {
		t.Fatalf("ran %q without systemd", ft.got())
	}
}

func TestSystemdUninstallRemovesDropIns(t *testing.T) {
	home := setHome(t)
	bootedSystemd(t, true)
	b, ft, _ := testBattery(t, "linux", Config{})
	if err := (systemdUser{}).install(b, "/opt/hello", 47811); err != nil {
		t.Fatal(err)
	}
	dropIn := filepath.Join(home, ".config", "systemd", "user", "dev.gofastr.svctest.service.d")
	if err := os.MkdirAll(dropIn, 0o700); err != nil {
		t.Fatal(err)
	}
	unit, _ := (systemdUser{}).unitPath(b)
	if err := (systemdUser{}).uninstall(b); err != nil {
		t.Fatal(err)
	}
	if fileExists(dropIn) || fileExists(unit) {
		t.Fatal("unit or drop-in dir left behind")
	}
	if got := ft.got(); !slices.Contains(got, "systemctl --user disable --now dev.gofastr.svctest.service") {
		t.Fatalf("uninstall did not disable the unit: %q", got)
	}
}

// A unit that will not stop is not deleted: it may still be running.
func TestSystemdUninstallKeepsUnitOnDisableFailure(t *testing.T) {
	setHome(t)
	bootedSystemd(t, true)
	b, ft, _ := testBattery(t, "linux", Config{})
	if err := (systemdUser{}).install(b, "/opt/hello", 47811); err != nil {
		t.Fatal(err)
	}
	ft.fail["systemctl --user disable"] = errors.New("exit status 1")
	if err := (systemdUser{}).uninstall(b); err == nil {
		t.Fatal("uninstall reported success after disable failed")
	}
	if unit, _ := (systemdUser{}).unitPath(b); !fileExists(unit) {
		t.Fatal("unit deleted after disable failed")
	}
}

func TestSystemdLogsArgv(t *testing.T) {
	b, ft, _ := testBattery(t, "linux", Config{})
	if err := (systemdUser{}).logs(b, 50, true); err != nil {
		t.Fatal(err)
	}
	want := "journalctl --user --unit dev.gofastr.svctest.service --no-pager --lines 50 --follow"
	if got := ft.got(); len(got) != 1 || got[0] != want {
		t.Fatalf("argv %q; want %q", got, want)
	}
}

func TestQuoteSystemd(t *testing.T) {
	cases := map[string]string{
		"/opt/hello":     `"/opt/hello"`,
		"/opt/my app":    `"/opt/my app"`,
		`/a"b`:           `"/a\"b"`,
		`/a\b`:           `"/a\\b"`,
		"/100%/x":        `"/100%%/x"`,
		"/$HOME/x":       `"/$$HOME/x"`,
		"/${HOME}%h\\\"": `"/$${HOME}%%h\\\""`,
	}
	for in, want := range cases {
		if got := quoteSystemd(in); got != want {
			t.Errorf("quoteSystemd(%q) = %s; want %s", in, got, want)
		}
	}
}

// taskDoc decodes the parts of a task definition the tests read.
type taskDoc struct {
	Triggers struct {
		Logon struct {
			UserID string `xml:"UserId"`
		} `xml:"LogonTrigger"`
	} `xml:"Triggers"`
	Principal struct {
		LogonType string `xml:"LogonType"`
		RunLevel  string `xml:"RunLevel"`
	} `xml:"Principals>Principal"`
	Exec struct {
		Command   string `xml:"Command"`
		Arguments string `xml:"Arguments"`
	} `xml:"Actions>Exec"`
}

func TestTaskXML(t *testing.T) {
	b, _, _ := testBattery(t, "windows", Config{})
	exe := `C:\Users\a&b\AppData\Local\Programs\dev.gofastr.svctest\hello.exe`
	raw := (taskScheduler{}).taskXML(b, daemonArgs(exe, 47811, `C:\Users\a b\AppData\Roaming`), `PC\a&b`)
	// encoding/xml refuses a UTF-16 declaration it cannot transcode;
	// the body is plain XML once the declaration is set aside.
	body := raw[strings.Index(raw, "?>")+2:]
	var doc taskDoc
	if err := xml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("task XML is not well-formed: %v", err)
	}
	if doc.Exec.Command != exe {
		t.Errorf("Command = %q; want %q", doc.Exec.Command, exe)
	}
	if doc.Exec.Arguments != `service daemon --port 47811 --base-dir "C:\Users\a b\AppData\Roaming"` {
		t.Errorf("Arguments = %q", doc.Exec.Arguments)
	}
	if doc.Triggers.Logon.UserID != `PC\a&b` {
		t.Errorf("LogonTrigger UserId = %q", doc.Triggers.Logon.UserID)
	}
	if doc.Principal.LogonType != "InteractiveToken" || doc.Principal.RunLevel != "LeastPrivilege" {
		t.Errorf("principal = %+v; want InteractiveToken, LeastPrivilege", doc.Principal)
	}
}

func TestEncodeUTF16HasBOM(t *testing.T) {
	got := encodeUTF16("<é>")
	if got[0] != 0xFF || got[1] != 0xFE {
		t.Fatalf("no UTF-16LE BOM: % x", got[:2])
	}
	u := make([]uint16, 0, (len(got)-2)/2)
	for i := 2; i < len(got); i += 2 {
		u = append(u, uint16(got[i])|uint16(got[i+1])<<8)
	}
	if s := string(utf16.Decode(u)); s != "<é>" {
		t.Fatalf("round trip = %q", s)
	}
}

func TestQuoteWindowsArg(t *testing.T) {
	cases := map[string]string{
		"daemon":          "daemon",
		"":                `""`,
		`C:\My App\x.exe`: `"C:\My App\x.exe"`,
		`a"b`:             `"a\"b"`,
		`C:\dir\ x\`:      `"C:\dir\ x\\"`,
		`a\\"b c`:         `"a\\\\\"b c"`,
	}
	for in, want := range cases {
		if got := quoteWindowsArg(in); got != want {
			t.Errorf("quoteWindowsArg(%q) = %s; want %s", in, got, want)
		}
	}
}

// parseWindowsArgs splits a command line by the CommandLineToArgvW
// rules: 2n backslashes before a quote are n backslashes and a quote
// toggle, 2n+1 are n backslashes and a literal quote, and backslashes
// elsewhere are literal.
func parseWindowsArgs(s string) []string {
	var args []string
	var cur strings.Builder
	inQuote, have := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\':
			n := 0
			for i < len(s) && s[i] == '\\' {
				n++
				i++
			}
			if i < len(s) && s[i] == '"' {
				cur.WriteString(strings.Repeat(`\`, n/2))
				if n%2 == 1 {
					cur.WriteByte('"')
				} else {
					inQuote = !inQuote
				}
			} else {
				cur.WriteString(strings.Repeat(`\`, n))
				i--
			}
			have = true
		case c == '"':
			inQuote = !inQuote
			have = true
		case (c == ' ' || c == '\t') && !inQuote:
			if have {
				args = append(args, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteByte(c)
			have = true
		}
	}
	if have {
		args = append(args, cur.String())
	}
	return args
}

func TestQuoteWindowsArgRoundTrips(t *testing.T) {
	words := []string{"", "a", `a b`, `a"b`, `a\b`, `a\"b`, `a\\"b`, `a\`, `a\\`, `a b\`, `"`, `\"`, `\\\"`, "\t", `C:\Program Files\x\`}
	var quoted []string
	for _, w := range words {
		quoted = append(quoted, quoteWindowsArg(w))
	}
	got := parseWindowsArgs(strings.Join(quoted, " "))
	if !reflect.DeepEqual(got, words) {
		t.Fatalf("round trip\n got %q\nwant %q", got, words)
	}
}

// With nothing answering the control channel, Windows stop still ends
// the task (it may be stuck before the channel opened), then the task
// is deleted. Task names carry the account SID: they are machine-wide.
func TestTaskSchedulerUninstallArgv(t *testing.T) {
	b, ft, _ := testBattery(t, "windows", Config{})
	if err := (taskScheduler{}).uninstall(b); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"schtasks /End /TN dev.gofastr.svctest-S-1-5-21-7",
		"schtasks /Query /TN dev.gofastr.svctest-S-1-5-21-7",
		"schtasks /Delete /TN dev.gofastr.svctest-S-1-5-21-7 /F",
	}
	if got := ft.got(); !reflect.DeepEqual(got, want) {
		t.Fatalf("argv\n got %q\nwant %q", got, want)
	}
}

// A tool's refusal reaches the user with its words, minus control
// bytes.
func TestToolErrorScrubsOutput(t *testing.T) {
	b, ft, _ := testBattery(t, "linux", Config{})
	ft.fail["systemctl"] = errors.New("exit status 1")
	err := (systemdUser{}).start(b)
	if err == nil {
		t.Fatal("no error")
	}
	if strings.ContainsRune(err.Error(), 0x1b) {
		t.Fatalf("error carries an escape byte: %q", err)
	}
	if !strings.Contains(err.Error(), "refused") {
		t.Fatalf("error lost the tool's words: %q", err)
	}
}
