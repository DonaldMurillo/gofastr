package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/core/cliplugin"
)

func buildMockPluginBinary(t *testing.T, src string, dir, name string) string {
	t.Helper()
	srcPath := filepath.Join(dir, name+".go")
	if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", binPath, srcPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build mock plugin %s: %v\n%s", name, err, out)
	}
	return binPath
}

const mockNativePluginSrc = `package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "--gofastr-plugin-manifest" {
		fmt.Print(` + "`" + `{"protocol_version":1,"name":"native","version":"v0.2.0","summary":"Build and run iOS/Android native apps","subcommands":[{"name":"doctor","summary":"Check toolchains"},{"name":"build","summary":"Build mobile app"},{"name":"run","summary":"Run mobile app"}],"env_passthrough":["ANDROID_*","JAVA_HOME","GOFASTR_BUNDLETOOL"],"env_from_flags":["--keystore-pass-env"]}` + "`" + `)
		return
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Println("gofastr-native mock help: doctor, build, run")
		return
	}
	switch args[0] {
	case "doctor", "build", "run":
		wd, _ := os.Getwd()
		env := map[string]string{}
		for _, kv := range os.Environ() {
			k, v, ok := strings.Cut(kv, "=")
			if ok {
				env[k] = v
			}
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"cmd":  args[0],
			"args": args[1:],
			"wd":   wd,
			"env":  env,
		})
	case "fail":
		code := 1
		if len(args) > 1 {
			if n, err := strconv.Atoi(args[1]); err == nil {
				code = n
			}
		}
		fmt.Fprintln(os.Stderr, "mock native failure")
		os.Exit(code)
	default:
		fmt.Fprintf(os.Stderr, "unknown native subcommand %q\n", args[0])
		os.Exit(2)
	}
}
`

const mockSeamPluginSrc = `package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "--gofastr-plugin-manifest" {
		fmt.Print(` + "`" + `{"protocol_version":1,"name":"seamer","version":"v0.1.0","summary":"Hook and override test plugin","env_passthrough":["SEAM_LOG","SEAM_FAIL_PRE"],"hooks":[{"phase":"pre","command":"validate"},{"phase":"post","command":"validate"}],"overrides":["validate"]}` + "`" + `)
		return
	}
	logPath := os.Getenv("SEAM_LOG")
	appendLog := func(line string) {
		if logPath == "" {
			return
		}
		_ = os.MkdirAll(filepath.Dir(logPath), 0o700)
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintln(f, line)
			_ = f.Close()
		}
	}
	if ov := os.Getenv("GOFASTR_OVERRIDE_COMMAND"); ov != "" {
		appendLog("override:" + ov + ":" + strings.Join(args, "|"))
		fmt.Println("OVERRIDDEN:" + ov)
		return
	}
	if phase := os.Getenv("GOFASTR_HOOK_PHASE"); phase != "" {
		cmd := os.Getenv("GOFASTR_HOOK_COMMAND")
		appendLog("hook:" + phase + ":" + cmd + ":" + strings.Join(args, "|"))
		if phase == "pre" && os.Getenv("SEAM_FAIL_PRE") == "1" {
			fmt.Fprintln(os.Stderr, "pre-hook rejected command")
			os.Exit(9)
		}
		return
	}
	fmt.Println("seamer-namespace:" + strings.Join(args, "|"))
}
`

func isolatePluginEnv(t *testing.T) string {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "plugins.yml")
	t.Setenv(cliplugin.EnvConfigPath, cfgPath)
	t.Setenv(cliplugin.EnvBypassPlugins, "")
	return cfgPath
}

func runDispatch(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	exitCode = -1
	stdout = clitest.CovT_capStdout(t, func() {
		stderr = clitest.CovT_capStderr(t, func() {
			exitCode = clitest.CovT_capExit(t, func() {
				dispatch(args)
			})
		})
	})
	if exitCode == -1 {
		exitCode = 0
	}
	return stdout, stderr, exitCode
}

func TestNativeMissingAndCoreCLI(t *testing.T) {
	isolatePluginEnv(t)
	clitest.CovT_chdir(t, t.TempDir())

	// Core CLI continues working normally without Native.
	out, _, code := runDispatch(t, "version")
	if code != 0 || !strings.Contains(out, "GoFastr") {
		t.Fatalf("version failed without native: code=%d out=%q", code, out)
	}

	// `gofastr native doctor` gives a clear command-not-installed diagnostic.
	out, errOut, code := runDispatch(t, "native", "doctor")
	if code != 1 {
		t.Fatalf("native doctor without plugin: exit=%d, want 1", code)
	}
	combined := out + errOut
	if !strings.Contains(combined, "gofastr-native") || !strings.Contains(combined, "gofastr plugin add native") {
		t.Fatalf("missing helpful install/register diagnostic:\n%s", combined)
	}
}

func TestNativePluginLifecycle(t *testing.T) {
	isolatePluginEnv(t)
	work := t.TempDir()
	clitest.CovT_chdir(t, work)

	bin := buildMockPluginBinary(t, mockNativePluginSrc, t.TempDir(), "gofastr-native")

	// 1. Register native plugin.
	out, errOut, code := runDispatch(t, "plugin", "add", "native", bin)
	if code != 0 {
		t.Fatalf("plugin add native failed: code=%d out=%q err=%q", code, out, errOut)
	}

	// 2. Duplicate registration must be rejected.
	_, errOut, code = runDispatch(t, "plugin", "add", "native", bin)
	if code == 0 || !strings.Contains(errOut, "already registered") {
		t.Fatalf("duplicate registration accepted: code=%d err=%q", code, errOut)
	}

	// 3. Built-in shadowing must be rejected (canonical + alias + meta).
	for _, shadow := range []string{"build", "m", "gen", "plugin", "help", "version"} {
		_, errOut, code = runDispatch(t, "plugin", "add", shadow, bin)
		if code == 0 || !strings.Contains(errOut, "collides with built-in") {
			t.Fatalf("built-in shadow %q accepted: code=%d err=%q", shadow, code, errOut)
		}
	}

	// 4. `gofastr help` lists registered plugins; `gofastr native --help` and
	//    `gofastr help native` delegate to the plugin's own help.
	helpOut, _, code := runDispatch(t, "help")
	if code != 0 || !strings.Contains(helpOut, "Plugins:") || !strings.Contains(helpOut, "native") {
		t.Fatalf("gofastr help missing registered plugin:\n%s", helpOut)
	}
	for _, helpArgs := range [][]string{{"native", "--help"}, {"help", "native"}} {
		hOut, _, hCode := runDispatch(t, helpArgs...)
		if hCode != 0 || !strings.Contains(hOut, "gofastr-native mock help") {
			t.Fatalf("%v did not reach plugin help: code=%d out=%q", helpArgs, hCode, hOut)
		}
	}

	// 5. Run `gofastr native build` with whitespace args, approved SDK/flag env,
	//    and ambient secrets that MUST NOT leak.
	t.Setenv("ANDROID_HOME", "/sdk/android")
	t.Setenv("MY_KS_PASS", "signing-pass-123")
	t.Setenv("GOFASTR_SECRET", "super-secret-session-key")
	t.Setenv("DATABASE_URL", "postgres://super-secret")

	out, errOut, code = runDispatch(t, "native", "build", "--platform", "ios-device", "--name", "My Phone App", "--keystore-pass-env", "MY_KS_PASS")
	if code != 0 {
		t.Fatalf("native build failed: code=%d out=%q err=%q", code, out, errOut)
	}
	var payload struct {
		Cmd  string            `json:"cmd"`
		Args []string          `json:"args"`
		WD   string            `json:"wd"`
		Env  map[string]string `json:"env"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal native output: %v\nraw: %q", err, out)
	}
	if payload.Cmd != "build" || len(payload.Args) != 6 || payload.Args[3] != "My Phone App" {
		t.Fatalf("args mangled: %+v", payload)
	}
	if payload.Env["ANDROID_HOME"] != "/sdk/android" || payload.Env["MY_KS_PASS"] != "signing-pass-123" {
		t.Fatalf("approved env missing: ANDROID_HOME=%q MY_KS_PASS=%q", payload.Env["ANDROID_HOME"], payload.Env["MY_KS_PASS"])
	}
	if _, leaked := payload.Env["GOFASTR_SECRET"]; leaked {
		t.Fatalf("SECURITY: GOFASTR_SECRET leaked to native plugin")
	}
	if _, leaked := payload.Env["DATABASE_URL"]; leaked {
		t.Fatalf("SECURITY: DATABASE_URL leaked to native plugin")
	}

	// 6. Propagate exact non-zero exit code.
	_, errOut, code = runDispatch(t, "native", "fail", "7")
	if code != 7 || !strings.Contains(errOut, "mock native failure") {
		t.Fatalf("exit code propagation failed: code=%d err=%q", code, errOut)
	}

	// 7. `gofastr plugin doctor --json` reports healthy protocol v1.
	docOut, _, code := runDispatch(t, "plugin", "doctor", "--json")
	if code != 0 {
		t.Fatalf("plugin doctor --json failed: code=%d out=%q", code, docOut)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(docOut), &doc); err != nil || doc["ok"] != true {
		t.Fatalf("unexpected doctor JSON: err=%v doc=%v", err, doc)
	}

	// 8. Remove plugin.
	_, errOut, code = runDispatch(t, "plugin", "remove", "native")
	if code != 0 {
		t.Fatalf("plugin remove native failed: code=%d err=%q", code, errOut)
	}
}

func TestProjectPluginTrustAndTamper(t *testing.T) {
	isolatePluginEnv(t)
	proj := t.TempDir()
	clitest.CovT_chdir(t, proj)

	if err := os.WriteFile(filepath.Join(proj, "go.mod"), []byte("module example.com/proj\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	toolsDir := filepath.Join(proj, "tools")
	if err := os.MkdirAll(toolsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	bin := buildMockPluginBinary(t, mockNativePluginSrc, toolsDir, "local-native")
	relBin := "./tools/" + filepath.Base(bin)

	settingsBody := "version: 1\nplugins:\n  - name: localtool\n    description: Local helper\n    command: [\"" + relBin + "\"]\n"
	if err := os.WriteFile(filepath.Join(proj, "gofastr.settings.yml"), []byte(settingsBody), 0o600); err != nil {
		t.Fatal(err)
	}

	// 1. Untrusted project plugin refuses to run silently.
	_, errOut, code := runDispatch(t, "localtool", "doctor")
	if code == 0 || !strings.Contains(errOut, "not trusted") || !strings.Contains(errOut, "gofastr plugin trust localtool") {
		t.Fatalf("SECURITY: untrusted project plugin ran or lacked guidance: code=%d err=%q", code, errOut)
	}

	// 2. Trust it explicitly -> runs cleanly.
	_, errOut, code = runDispatch(t, "plugin", "trust", "localtool")
	if code != 0 {
		t.Fatalf("plugin trust failed: code=%d err=%q", code, errOut)
	}
	out, errOut, code := runDispatch(t, "localtool", "doctor")
	if code != 0 || !strings.Contains(out, `"cmd":"doctor"`) {
		t.Fatalf("trusted project plugin failed: code=%d out=%q err=%q", code, out, errOut)
	}

	// 3. Rebuild/modify binary on disk -> SHA-256 mismatch refuses execution!
	tamperedSrc := strings.Replace(mockNativePluginSrc, "v0.2.0", "v0.2.1", 1)
	_ = buildMockPluginBinary(t, tamperedSrc, toolsDir, "local-native")
	_, errOut, code = runDispatch(t, "localtool", "doctor")
	if code == 0 || !strings.Contains(errOut, "changed since it was trusted") {
		t.Fatalf("SECURITY: tampered project plugin was not blocked: code=%d err=%q", code, errOut)
	}

	// 4. Untrust removes the trust record.
	_, errOut, code = runDispatch(t, "plugin", "untrust", "localtool")
	if code != 0 {
		t.Fatalf("plugin untrust failed: code=%d err=%q", code, errOut)
	}
}

func TestOptInHooksOverridesAndBuiltinBypass(t *testing.T) {
	isolatePluginEnv(t)
	proj := t.TempDir()
	clitest.CovT_chdir(t, proj)

	specPath := filepath.Join(proj, "gofastr.yml")
	if err := os.WriteFile(specPath, []byte("app:\n  name: Demo\n  module: example.com/demo\nentities:\n  - name: posts\n    fields:\n      - name: title\n        type: string\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := buildMockPluginBinary(t, mockSeamPluginSrc, t.TempDir(), "seamer")
	logPath := filepath.Join(proj, "seam.log")
	t.Setenv("SEAM_LOG", logPath)

	// 1. Register WITHOUT --allow-hook or --allow-override: hooks & override MUST stay inactive!
	_, errOut, code := runDispatch(t, "plugin", "add", "seamer", bin)
	if code != 0 {
		t.Fatalf("plugin add seamer: code=%d err=%q", code, errOut)
	}
	out, errOut, code := runDispatch(t, "validate", specPath)
	if code != 0 {
		t.Fatalf("built-in validate failed: code=%d out=%q err=%q", code, out, errOut)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("SECURITY: unapproved hooks/overrides executed during validate!")
	}

	// 2. Re-register WITH explicit pre and post hooks on validate.
	_, _, _ = runDispatch(t, "plugin", "remove", "seamer")
	_, errOut, code = runDispatch(t, "plugin", "add", "seamer", bin, "--allow-hook", "pre:validate", "--allow-hook", "post:validate")
	if code != 0 {
		t.Fatalf("plugin add with hooks: code=%d err=%q", code, errOut)
	}
	out, errOut, code = runDispatch(t, "validate", specPath)
	if code != 0 {
		t.Fatalf("validate with hooks failed: code=%d out=%q err=%q", code, out, errOut)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected hook log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(logData)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "hook:pre:validate:") || !strings.HasPrefix(lines[1], "hook:post:validate:") {
		t.Fatalf("unexpected hook sequence: %#v", lines)
	}

	// 3. Failing pre-hook aborts command with its exit code.
	t.Setenv("SEAM_FAIL_PRE", "1")
	_, errOut, code = runDispatch(t, "validate", specPath)
	if code != 9 || !strings.Contains(errOut, "pre-hook rejected") {
		t.Fatalf("failing pre-hook did not abort: code=%d err=%q", code, errOut)
	}
	t.Setenv("SEAM_FAIL_PRE", "")

	// 4. Re-register WITH explicit override on validate.
	_, _, _ = runDispatch(t, "plugin", "remove", "seamer")
	_, errOut, code = runDispatch(t, "plugin", "add", "seamer", bin, "--allow-override", "validate")
	if code != 0 {
		t.Fatalf("plugin add with override: code=%d err=%q", code, errOut)
	}
	out, errOut, code = runDispatch(t, "validate", specPath)
	if code != 0 || !strings.Contains(out, "OVERRIDDEN:validate") {
		t.Fatalf("override did not intercept validate: code=%d out=%q err=%q", code, out, errOut)
	}

	// 5. `--builtin` bypasses the override and runs built-in validate directly!
	out, errOut, code = runDispatch(t, "--builtin", "validate", specPath)
	if code != 0 || strings.Contains(out, "OVERRIDDEN:") {
		t.Fatalf("--builtin did not bypass override: code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestPluginCLIVersionMismatchRefused(t *testing.T) {
	isolatePluginEnv(t)
	clitest.CovT_chdir(t, t.TempDir())

	futureBin := buildMockPluginBinary(t, `package main
import "fmt"
func main() {
	fmt.Print(`+"`"+`{"protocol_version":1,"name":"future","version":"v1.0.0","summary":"Needs future CLI","min_gofastr_version":"v99.0.0"}`+"`"+`)
}
`, t.TempDir(), "future")

	prev := cli.Version
	cli.Version = "v0.98.0"
	t.Cleanup(func() { cli.Version = prev })

	_, errOut, code := runDispatch(t, "plugin", "add", "future", futureBin)
	if code == 0 || !strings.Contains(errOut, "requires gofastr >= v99.0.0") {
		t.Fatalf("incompatible min_gofastr_version accepted: code=%d err=%q", code, errOut)
	}
}

func TestOverrideWrapperCallsBuiltin(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles gofastr binary for end-to-end override call-through")
	}
	dir := t.TempDir()
	gofastrBin := filepath.Join(dir, "gofastr")
	if runtime.GOOS == "windows" {
		gofastrBin += ".exe"
	}
	buildCmd := exec.Command("go", "build", "-o", gofastrBin, ".")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build gofastr: %v\n%s", err, out)
	}

	wrapperBin := buildMockPluginBinary(t, `package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

func main() {
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "--gofastr-plugin-manifest" {
		fmt.Print(`+"`"+`{"protocol_version":1,"name":"wrap","version":"v0.1.0","summary":"Wrapper plugin","overrides":["validate"]}`+"`"+`)
		return
	}
	if ov := os.Getenv("GOFASTR_OVERRIDE_COMMAND"); ov != "" {
		fmt.Println("WRAP_BEFORE:" + ov)
		bin := os.Getenv("GOFASTR_CLI_BIN")
		cmd := exec.CommandContext(context.Background(), bin, append([]string{"--builtin", ov}, args...)...)
		cmd.Env = append(os.Environ(), "GOFASTR_NO_PLUGINS=1")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			os.Exit(1)
		}
		fmt.Println("WRAP_AFTER:" + ov)
		return
	}
	fmt.Println("wrap-namespace")
}
`, dir, "wrap")

	cfgPath := filepath.Join(dir, "plugins.yml")
	specPath := filepath.Join(dir, "gofastr.yml")
	if err := os.WriteFile(specPath, []byte("app:\n  name: WrapDemo\n  module: example.com/wrap\nentities:\n  - name: posts\n    fields:\n      - name: title\n        type: string\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(), cliplugin.EnvConfigPath+"="+cfgPath, cliplugin.EnvBypassPlugins+"=")
	addCmd := exec.Command(gofastrBin, "plugin", "add", "wrap", wrapperBin, "--allow-override", "validate")
	addCmd.Dir = dir
	addCmd.Env = env
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("plugin add wrap: %v\n%s", err, out)
	}

	valCmd := exec.Command(gofastrBin, "validate", specPath)
	valCmd.Dir = dir
	valCmd.Env = env
	out, err := valCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("wrapped validate failed: %v\n%s", err, out)
	}
	s := string(out)
	if !strings.Contains(s, "WRAP_BEFORE:validate") || !strings.Contains(s, "WRAP_AFTER:validate") {
		t.Fatalf("wrapper did not execute before/after built-in validate:\n%s", s)
	}
}

func TestJSONOverrideWritesToStdout(t *testing.T) {
	isolatePluginEnv(t)
	clitest.CovT_chdir(t, t.TempDir())

	bin := buildMockPluginBinary(t, `package main
import (
	"fmt"
	"os"
)
func main() {
	if len(os.Args) == 2 && os.Args[1] == "--gofastr-plugin-manifest" {
		fmt.Print(`+"`"+`{"protocol_version":1,"name":"jwrap","version":"v0.1.0","summary":"JSON verify override","supports_json":true,"overrides":["verify"]}`+"`"+`)
		return
	}
	fmt.Println(`+"`"+`{"ok":true,"from":"override"}`+"`"+`)
}
`, t.TempDir(), "jwrap")

	_, errOut, code := runDispatch(t, "plugin", "add", "jwrap", bin, "--allow-override", "verify")
	if code != 0 {
		t.Fatalf("plugin add jwrap failed: code=%d err=%q", code, errOut)
	}

	out, errOut, code := runDispatch(t, "verify", "--json")
	if code != 0 {
		t.Fatalf("verify --json override failed: code=%d out=%q err=%q", code, out, errOut)
	}
	if strings.TrimSpace(out) != `{"ok":true,"from":"override"}` {
		t.Fatalf("override JSON did not land on stdout: stdout=%q stderr=%q", out, errOut)
	}
	if strings.Contains(errOut, `"from":"override"`) {
		t.Fatalf("override JSON leaked to stderr: %q", errOut)
	}
}

func TestTrustRequiresExplicitGrants(t *testing.T) {
	isolatePluginEnv(t)
	proj := t.TempDir()
	clitest.CovT_chdir(t, proj)

	if err := os.WriteFile(filepath.Join(proj, "go.mod"), []byte("module example.com/trust\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	toolsDir := filepath.Join(proj, "tools")
	if err := os.MkdirAll(toolsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	bin := buildMockPluginBinary(t, mockSeamPluginSrc, toolsDir, "seamer")
	relBin := "./tools/" + filepath.Base(bin)
	settingsBody := "version: 1\nplugins:\n  - name: seamer\n    command: [\"" + relBin + "\"]\n    hooks: [\"pre:validate\"]\n    overrides: [\"validate\"]\n"
	if err := os.WriteFile(filepath.Join(proj, "gofastr.settings.yml"), []byte(settingsBody), 0o600); err != nil {
		t.Fatal(err)
	}

	// Plain `gofastr plugin trust seamer` must refuse before approving hooks/overrides.
	out, errOut, code := runDispatch(t, "plugin", "trust", "seamer")
	if code == 0 || !strings.Contains(out+errOut, "--allow-hook pre:validate") || !strings.Contains(out+errOut, "--allow-override validate") {
		t.Fatalf("SECURITY: plain trust did not require explicit hook/override flags: code=%d out=%q err=%q", code, out, errOut)
	}

	// Passing explicit flags succeeds.
	_, errOut, code = runDispatch(t, "plugin", "trust", "seamer", "--allow-hook", "pre:validate", "--allow-override", "validate")
	if code != 0 {
		t.Fatalf("explicit trust failed: code=%d err=%q", code, errOut)
	}
}

func TestGlobalHookSurvivesHostileRepo(t *testing.T) {
	isolatePluginEnv(t)
	proj := t.TempDir()
	clitest.CovT_chdir(t, proj)

	specPath := filepath.Join(proj, "gofastr.yml")
	if err := os.WriteFile(specPath, []byte("app:\n  name: Demo\n  module: example.com/demo\nentities:\n  - name: posts\n    fields:\n      - name: title\n        type: string\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "go.mod"), []byte("module example.com/demo\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	bin := buildMockPluginBinary(t, mockSeamPluginSrc, t.TempDir(), "seamer")
	t.Setenv("SEAM_FAIL_PRE", "1")
	_, errOut, code := runDispatch(t, "plugin", "add", "seamer", bin, "--allow-hook", "pre:validate")
	if code != 0 {
		t.Fatalf("plugin add seamer: code=%d err=%q", code, errOut)
	}

	// 1. Hostile repo tries to shadow `seamer` with a bogus local command -> global pre:validate MUST still run and reject!
	if err := os.WriteFile(filepath.Join(proj, "gofastr.settings.yml"), []byte("version: 1\nplugins:\n  - name: seamer\n    command: [\"./nope\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, errOut, code = runDispatch(t, "validate", specPath)
	if code != 9 || !strings.Contains(errOut, "pre-hook rejected") {
		t.Fatalf("SECURITY: hostile repo plugin suppressed global pre:validate hook: code=%d err=%q", code, errOut)
	}

	// 2. Malformed gofastr.settings.yml -> global pre:validate MUST still run and reject!
	if err := os.WriteFile(filepath.Join(proj, "gofastr.settings.yml"), []byte("version: 99\nbogus: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, errOut, code = runDispatch(t, "validate", specPath)
	if code != 9 || !strings.Contains(errOut, "pre-hook rejected") {
		t.Fatalf("SECURITY: malformed project settings suppressed global pre:validate hook: code=%d err=%q", code, errOut)
	}
}

func TestMigrateBuiltinRefusesFilteredDB(t *testing.T) {
	isolatePluginEnv(t)
	clitest.CovT_chdir(t, t.TempDir())
	t.Setenv(cliplugin.EnvFilteredDB, "1")
	t.Setenv("DATABASE_URL", "")

	_, errOut, code := runDispatch(t, "--builtin", "migrate", "status")
	if code != 1 || !strings.Contains(errOut, "DATABASE_URL was stripped") {
		t.Fatalf("expected --builtin migrate refusal when DATABASE_URL was stripped: code=%d err=%q", code, errOut)
	}
}
