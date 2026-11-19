// Package isolation gives a linked Git worktree its own listen port, database
// and environment: Resolve, ListenAddr, Runtime.Addr, Runtime.Env and
// Runtime.Database.
package isolation

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/settings"
	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

const (
	envIsolation       = "GOFASTR_ISOLATION"
	envApplied         = "GOFASTR_ISOLATION_APPLIED"
	envID              = "GOFASTR_ISOLATION_ID"
	envPortPrefix      = "GOFASTR_ISOLATION_PORT_"
	envRewriteExplicit = "GOFASTR_ISOLATION_REWRITE"

	portScanMax = 64
)

// Runtime is the resolved isolation context for a project.
type Runtime struct {
	projectDir      string
	gitRoot         string
	configPath      string
	cfg             Config
	active          bool
	id              string
	rewriteExplicit bool

	ports *portTable
}

var portTables sync.Map

type portTableKey struct {
	projectDir string
	id         string
	port       PortConfig
}

type portTable struct {
	mu sync.Mutex
	m  map[int]int
}

func portTableFor(key portTableKey) *portTable {
	t, _ := portTables.LoadOrStore(key, &portTable{m: map[int]int{}})
	return t.(*portTable)
}

// Config describes local resource isolation.
type Config struct {
	Enabled  bool
	Mode     string
	Port     PortConfig
	Database DatabaseConfig
	Services map[string]int
	Env      map[string]string
}

// PortConfig controls deterministic port remapping.
type PortConfig struct {
	Strategy string
	Offset   int
	Range    int
	Scan     int
}

// DatabaseConfig controls default DSN rewriting.
type DatabaseConfig struct {
	Strategy string
}

// Resolve loads isolation config for projectDir and returns its runtime.
func Resolve(projectDir string) (*Runtime, error) {
	cfg := defaultConfig()
	start, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, err
	}
	settingsFile, found, err := settings.Find(start, "isolation")
	if err != nil {
		return nil, err
	}
	if found {
		loaded, err := loadConfig(settingsFile, cfg)
		if err != nil {
			return nil, err
		}
		cfg = loaded
		start = settingsFile.Dir
	}
	gitRoot, linkedWorktree := findGitRoot(start)
	id := os.Getenv(envID)
	if id == "" {
		id = makeID(start)
	}
	active := cfg.Enabled
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envIsolation))) {
	case "off", "false", "0", "disabled":
		active = false
	case "on", "true", "1", "always":
		active = true
	}
	if active {
		switch cfg.Mode {
		case "", "worktree":
			active = linkedWorktree
		case "always":
			active = true
		case "off", "disabled":
			active = false
		default:
			return nil, fmt.Errorf("isolation.mode %q is not supported", cfg.Mode)
		}
	}
	rewriteExplicit := os.Getenv(envRewriteExplicit) != "0" &&
		!strings.EqualFold(os.Getenv(envRewriteExplicit), "false")
	ports := &portTable{m: map[int]int{}}
	if active {
		ports = portTableFor(portTableKey{projectDir: start, id: id, port: cfg.Port})
	}
	return &Runtime{
		projectDir:      start,
		gitRoot:         gitRoot,
		configPath:      settingsFile.Path,
		cfg:             cfg,
		active:          active,
		id:              id,
		rewriteExplicit: rewriteExplicit,
		ports:           ports,
	}, nil
}

func normalizeBarePort(addr string) string {
	if addr == "" || strings.Contains(addr, ":") {
		return addr
	}
	if _, err := strconv.Atoi(addr); err == nil {
		return ":" + addr
	}
	return addr
}

// Active reports whether isolation applies to this runtime.
func (r *Runtime) Active() bool { return r != nil && r.active }

// ID returns the stable isolation identifier.
func (r *Runtime) ID() string {
	if r == nil {
		return ""
	}
	return r.id
}

// Addr returns addr, a bare port normalised to ":port", with its port
// remapped when isolation is active.
func (r *Runtime) Addr(addr string) (string, error) {
	addr = normalizeBarePort(addr)
	if !r.Active() || addr == "" {
		return addr, nil
	}
	if !r.rewriteExplicit {
		return addr, nil
	}
	host, port, shape, err := splitAddr(addr)
	if err != nil {
		return "", err
	}
	if mapped, ok := r.lookupPort(port); ok {
		return joinAddr(host, mapped, shape), nil
	}
	if r.isResolvedPort(port) {
		return addr, nil
	}
	next := r.recordPort(port, r.portFor(port, host))
	return joinAddr(host, next, shape), nil
}

// ListenAddr resolves isolation for projectDir and returns $PORT (or
// fallback when it is empty) with isolation's port remap applied.
func ListenAddr(projectDir, fallback string) (string, error) {
	rt, err := Resolve(projectDir)
	if err != nil {
		return "", err
	}
	addr := os.Getenv("PORT")
	if addr == "" {
		addr = fallback
	}
	return rt.Addr(addr)
}

// Env returns env with PORT, DATABASE_URL, the port markers and the
// settings env templates isolated. A variable already in env is never
// replaced by a settings value.
func (r *Runtime) Env(env []string) []string {
	if !r.Active() {
		return env
	}
	values := envMap(env)
	rewriteExplicit := values[envRewriteExplicit] != "0" && !strings.EqualFold(values[envRewriteExplicit], "false")
	basePort := 8080
	if v := values["PORT"]; v != "" {
		if _, p, _, err := splitAddr(v); err == nil {
			basePort = p
		} else if p, err := strconv.Atoi(v); err == nil {
			basePort = p
		}
	}
	appPort := r.portFor(basePort, "localhost")
	if rewriteExplicit || values["PORT"] == "" {
		values["PORT"] = renderPortValue(values["PORT"], appPort)
	}
	if dsn := values["DATABASE_URL"]; dsn != "" && rewriteExplicit {
		_, rewritten, err := r.Database("", dsn)
		if err == nil {
			values["DATABASE_URL"] = rewritten
		}
	}
	operatorSet := make(map[string]bool, len(env))
	for _, pair := range env {
		if key, _, ok := strings.Cut(pair, "="); ok {
			operatorSet[key] = true
		}
	}
	for key, tmpl := range r.cfg.Env {
		if operatorSet[key] {
			continue
		}
		values[key] = r.expandTemplate(tmpl, basePort)
	}
	values[envApplied] = "1"
	values[envID] = r.id
	for base, mapped := range r.recordedPorts() {
		values[envPortPrefix+strconv.Itoa(base)] = strconv.Itoa(mapped)
	}
	values[envPortPrefix+strconv.Itoa(basePort)] = strconv.Itoa(appPort)
	for name, port := range r.cfg.Services {
		values[envPortPrefix+name] = strconv.Itoa(r.portFor(port, "localhost"))
	}
	return flattenEnv(env, values)
}

// Database returns an isolated database driver and DSN when isolation is active.
func (r *Runtime) Database(driver, dsn string) (string, string, error) {
	if !r.Active() {
		return driver, dsn, nil
	}
	switch strings.ToLower(strings.TrimSpace(r.cfg.Database.Strategy)) {
	case "off", "none", "disabled":
		return driver, dsn, nil
	}
	if driver == "" {
		driver = inferDriver(dsn)
	}
	switch strings.ToLower(driver) {
	case "sqlite", "sqlite3":
		next, err := r.sqliteDSN(dsn)
		return driver, next, err
	case "postgres", "postgresql", "pgx":
		next, err := r.postgresDSN(dsn)
		return driver, next, err
	default:
		return driver, r.expandTemplate(dsn, 8080), nil
	}
}

func defaultConfig() Config {
	return Config{
		Enabled: true,
		Mode:    "worktree",
		Port: PortConfig{
			Strategy: "offset",
			Offset:   1000,
			Range:    1000,
			Scan:     20,
		},
		Database: DatabaseConfig{Strategy: "suffix"},
		Services: map[string]int{},
		Env:      map[string]string{},
	}
}

func loadConfig(f settings.File, base Config) (Config, error) {
	section := f.Section("isolation")
	if section == nil {
		return base, nil
	}
	m, err := coreyaml.ExpectMap(section, "isolation")
	if err != nil {
		return Config{}, err
	}
	cfg := base
	if v := m["enabled"]; v != nil {
		enabled, err := coreyaml.BoolValue(v, "isolation.enabled")
		if err != nil {
			return Config{}, err
		}
		cfg.Enabled = enabled
	}
	if v := m["mode"]; v != nil {
		cfg.Mode = scalarString(v)
	}
	if v := m["port"]; v != nil {
		port, err := coreyaml.ExpectMap(v, "isolation.port")
		if err != nil {
			return Config{}, err
		}
		if n := port["strategy"]; n != nil {
			cfg.Port.Strategy = scalarString(n)
		}
		if n := port["offset"]; n != nil {
			cfg.Port.Offset, err = intValue(n, "isolation.port.offset")
			if err != nil {
				return Config{}, err
			}
		}
		if n := port["range"]; n != nil {
			cfg.Port.Range, err = intValue(n, "isolation.port.range")
			if err != nil {
				return Config{}, err
			}
		}
		if n := port["scan"]; n != nil {
			cfg.Port.Scan, err = intValue(n, "isolation.port.scan")
			if err != nil {
				return Config{}, err
			}
			if cfg.Port.Scan > portScanMax {
				cfg.Port.Scan = portScanMax
			}
		}
	}
	if v := m["database"]; v != nil {
		db, err := coreyaml.ExpectMap(v, "isolation.database")
		if err != nil {
			return Config{}, err
		}
		if n := db["strategy"]; n != nil {
			cfg.Database.Strategy = scalarString(n)
		}
	}
	if v := m["services"]; v != nil {
		services, err := coreyaml.ExpectMap(v, "isolation.services")
		if err != nil {
			return Config{}, err
		}
		cfg.Services = map[string]int{}
		for key, n := range services {
			port, err := intValue(n, "isolation.services."+key)
			if err != nil {
				return Config{}, err
			}
			cfg.Services[key] = port
		}
	}
	if v := m["env"]; v != nil {
		env, err := coreyaml.ExpectMap(v, "isolation.env")
		if err != nil {
			return Config{}, err
		}
		cfg.Env = map[string]string{}
		for _, key := range slices.Sorted(maps.Keys(env)) {
			n := env[key]
			val := scalarString(n)
			if isEnvShapingName(key) || !isTemplateValue(val) {
				fmt.Fprintf(os.Stderr,
					"isolation: ignoring env %q from %s: only port/id/dir template values may come from the project settings file\n",
					key, f.Path)
				continue
			}
			cfg.Env[key] = val
		}
	}
	return cfg, nil
}

var envShapingNames = map[string]bool{
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "ALL_PROXY": true,
	"NO_PROXY": true, "FTP_PROXY": true, "WS_PROXY": true, "WSS_PROXY": true,
	"PATH": true, "LD_PRELOAD": true, "LD_LIBRARY_PATH": true,
	"LD_AUDIT": true, "DYLD_INSERT_LIBRARIES": true,
	"DYLD_LIBRARY_PATH": true, "DYLD_FORCE_FLAT_NAMESPACE": true,
	"HOME": true, "SHELL": true, "USER": true, "LOGNAME": true,
	"TMPDIR": true, "TMP": true, "TEMP": true, "XDG_CONFIG_HOME": true,
	"XDG_STATE_HOME": true, "XDG_DATA_HOME": true, "XDG_CACHE_HOME": true,
	"IFS": true, "ENV": true, "BASH_ENV": true, "ZDOTDIR": true,
	"SHELLOPTS": true, "PROMPT_COMMAND": true,
}

func isEnvShapingName(key string) bool {
	return envShapingNames[strings.ToUpper(key)] ||
		strings.HasPrefix(strings.ToUpper(key), "GOFASTR_ISOLATION")
}

func isTemplateValue(val string) bool {
	for _, marker := range []string{"{port}", "{port:", "{id}", "{project_dir}"} {
		if strings.Contains(val, marker) {
			return true
		}
	}
	return false
}

type gitKind int

const (
	gitNone gitKind = iota
	gitDir
	gitFile
)

func gitMarker(dir string) (gitKind, string) {
	git := filepath.Join(dir, ".git")
	if _, err := os.Stat(filepath.Join(git, "HEAD")); err == nil {
		return gitDir, ""
	}
	if info, err := os.Stat(git); err == nil && !info.IsDir() {
		return gitFile, git
	}
	return gitNone, ""
}

func findGitRoot(start string) (string, bool) {
	dir := start
	for {
		switch kind, git := gitMarker(dir); kind {
		case gitDir:
			return dir, false
		case gitFile:
			data, err := os.ReadFile(git)
			if err != nil {
				return dir, false
			}
			line := strings.TrimSpace(string(data))
			linked := strings.HasPrefix(line, "gitdir:") && strings.Contains(filepath.ToSlash(line), "/worktrees/")
			return dir, linked
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func makeID(projectDir string) string {
	sum := sha1.Sum([]byte(filepath.Clean(projectDir)))
	return "wt_" + hex.EncodeToString(sum[:])[:10]
}

func (r *Runtime) portFor(base int, host string) int {
	if base <= 0 {
		return base
	}
	if v, ok := r.lookupPort(base); ok {
		return v
	}
	pc := r.cfg.Port
	switch strings.ToLower(strings.TrimSpace(pc.Strategy)) {
	case "off", "none", "disabled":
		return base
	}
	if pc.Offset == 0 {
		pc.Offset = 1000
	}
	if pc.Range <= 0 {
		pc.Range = 1000
	}
	offset := hashInt(r.id+":"+strconv.Itoa(base), pc.Range)
	candidate := normalizePort(base + pc.Offset + offset)
	if pc.Scan < 0 {
		pc.Scan = 0
	}
	if pc.Scan > portScanMax {
		pc.Scan = portScanMax
	}
	for i := 0; i <= pc.Scan; i++ {
		p := normalizePort(candidate + i)
		if portAvailable(host, p) {
			return p
		}
	}
	return candidate
}

func hashInt(s string, mod int) int {
	sum := sha1.Sum([]byte(s))
	n := int(sum[0])<<24 | int(sum[1])<<16 | int(sum[2])<<8 | int(sum[3])
	if n < 0 {
		n = -n
	}
	return n % mod
}

func normalizePort(port int) int {
	if port > 0 && port <= 65535 {
		return port
	}
	const first = 1024
	const size = 65535 - first
	if port < 0 {
		port = -port
	}
	return first + (port % size)
}

func portAvailable(host string, port int) bool {
	if host == "" {
		host = "localhost"
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

type addrShape int

const (
	addrColon addrShape = iota
	addrHost
	addrPortOnly
)

func splitAddr(addr string) (string, int, addrShape, error) {
	if p, err := strconv.Atoi(addr); err == nil {
		return "", p, addrPortOnly, nil
	}
	if strings.HasPrefix(addr, ":") && strings.Count(addr, ":") == 1 {
		p, err := strconv.Atoi(strings.TrimPrefix(addr, ":"))
		return "", p, addrColon, err
	}
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, addrHost, fmt.Errorf("isolation: parse addr %q: %w", addr, err)
	}
	p, err := strconv.Atoi(portText)
	return host, p, addrHost, err
}

func joinAddr(host string, port int, shape addrShape) string {
	switch shape {
	case addrPortOnly:
		return strconv.Itoa(port)
	case addrColon:
		return ":" + strconv.Itoa(port)
	default:
		return net.JoinHostPort(host, strconv.Itoa(port))
	}
}

func (r *Runtime) lookupPort(base int) (int, bool) {
	if p, ok := envAppliedPort(strconv.Itoa(base)); ok {
		return p, true
	}
	r.ports.mu.Lock()
	defer r.ports.mu.Unlock()
	p, ok := r.ports.m[base]
	return p, ok
}

func (r *Runtime) recordPort(base, mapped int) int {
	r.ports.mu.Lock()
	defer r.ports.mu.Unlock()
	if p, ok := r.ports.m[base]; ok {
		return p
	}
	r.ports.m[base] = mapped
	return mapped
}

func (r *Runtime) recordedPorts() map[int]int {
	r.ports.mu.Lock()
	defer r.ports.mu.Unlock()
	return maps.Clone(r.ports.m)
}

func (r *Runtime) isResolvedPort(port int) bool {
	if os.Getenv(envApplied) == "1" {
		for _, pair := range os.Environ() {
			key, val, ok := strings.Cut(pair, "=")
			if !ok || !strings.HasPrefix(key, envPortPrefix) {
				continue
			}
			p, err := strconv.Atoi(val)
			if err == nil && p == port {
				return true
			}
		}
	}
	r.ports.mu.Lock()
	defer r.ports.mu.Unlock()
	for _, mapped := range r.ports.m {
		if mapped == port {
			return true
		}
	}
	return false
}

func envAppliedPort(base string) (int, bool) {
	if os.Getenv(envApplied) != "1" {
		return 0, false
	}
	v := os.Getenv(envPortPrefix + base)
	if v == "" {
		return 0, false
	}
	p, err := strconv.Atoi(v)
	return p, err == nil
}

func envMap(env []string) map[string]string {
	out := map[string]string{}
	for _, pair := range env {
		key, val, ok := strings.Cut(pair, "=")
		if ok {
			out[key] = val
		}
	}
	return out
}

func flattenEnv(original []string, values map[string]string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, pair := range original {
		key, _, ok := strings.Cut(pair, "=")
		if !ok || seen[key] {
			continue
		}
		if val, ok := values[key]; ok {
			out = append(out, key+"="+val)
			seen[key] = true
		}
	}
	for key, val := range values {
		if !seen[key] {
			out = append(out, key+"="+val)
		}
	}
	return out
}

func renderPortValue(current string, port int) string {
	if current == "" {
		return "localhost:" + strconv.Itoa(port)
	}
	if _, err := strconv.Atoi(current); err == nil {
		return strconv.Itoa(port)
	}
	host, _, shape, err := splitAddr(current)
	if err != nil {
		return "localhost:" + strconv.Itoa(port)
	}
	return joinAddr(host, port, shape)
}

func (r *Runtime) expandTemplate(tmpl string, basePort int) string {
	out := strings.ReplaceAll(tmpl, "{id}", r.id)
	out = strings.ReplaceAll(out, "{project_dir}", r.projectDir)
	out = strings.ReplaceAll(out, "{port}", strconv.Itoa(r.portFor(basePort, "localhost")))
	for name, port := range r.cfg.Services {
		out = strings.ReplaceAll(out, "{port:"+name+"}", strconv.Itoa(r.portFor(port, "localhost")))
	}
	return out
}

func inferDriver(dsn string) string {
	lower := strings.ToLower(dsn)
	switch {
	case strings.HasPrefix(lower, "postgres://"), strings.HasPrefix(lower, "postgresql://"):
		return "postgres"
	case strings.HasPrefix(lower, "file:"), strings.Contains(lower, ".db"), lower == ":memory:":
		return "sqlite3"
	default:
		return ""
	}
}

var chmodStateDir = os.Chmod

func (r *Runtime) sqliteDSN(dsn string) (string, error) {
	if dsn == "" || dsn == ":memory:" {
		return dsn, nil
	}
	prefix := ""
	path := dsn
	query := ""
	if strings.HasPrefix(path, "file:") {
		prefix = "file:"
		path = strings.TrimPrefix(path, "file:")
		if i := strings.IndexAny(path, "?#"); i >= 0 {
			query = path[i:]
			path = path[:i]
		}
	}
	name := filepath.Base(path)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = "app.db"
	}
	dir := filepath.Join(r.projectDir, ".gofastr", "isolation", r.id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := chmodStateDir(dir, 0o700); err != nil {
		if !errors.Is(err, fs.ErrPermission) {
			return "", err
		}
		slog.Warn("isolation: permission denied tightening the SQLite state directory to owner-only; using it as is, so local co-users may read it", "dir", dir, "err", err)
	}
	if err := fileperm.Restrict(dir, true); err != nil {
		return "", err
	}
	return prefix + filepath.Join(dir, name) + query, nil
}

func (r *Runtime) postgresDSN(dsn string) (string, error) {
	if dsn == "" {
		return dsn, nil
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return dsn, err
	}
	db := strings.TrimPrefix(u.Path, "/")
	if db == "" {
		return dsn, nil
	}
	const pgMaxIdentifier = 63
	suffix := "_" + strings.ReplaceAll(r.id, "-", "_")
	if len(suffix) > pgMaxIdentifier {
		sum := sha1.Sum([]byte(r.id))
		suffix = "_wt_" + hex.EncodeToString(sum[:])[:16]
	}
	if strings.HasSuffix(db, suffix) {
		return u.String(), nil
	}
	if len(db)+len(suffix) > pgMaxIdentifier {
		cut := pgMaxIdentifier - len(suffix)
		for cut > 0 && !utf8.RuneStart(db[cut]) {
			cut--
		}
		db = db[:cut]
	}
	u.Path = "/" + db + suffix
	return u.String(), nil
}

func scalarString(node *coreyaml.Node) string {
	if node == nil {
		return ""
	}
	return fmt.Sprint(node.Value)
}

func intValue(node *coreyaml.Node, label string) (int, error) {
	if node == nil || node.Kind != coreyaml.Scalar {
		return 0, fmt.Errorf("%s must be an integer", label)
	}
	switch v := node.Value.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	default:
		return strconv.Atoi(fmt.Sprint(v))
	}
}
