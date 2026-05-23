// Package isolation resolves worktree-specific local runtime resources.
package isolation

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

const (
	envIsolation       = "GOFASTR_ISOLATION"
	envApplied         = "GOFASTR_ISOLATION_APPLIED"
	envID              = "GOFASTR_ISOLATION_ID"
	envPortPrefix      = "GOFASTR_ISOLATION_PORT_"
	envRewriteExplicit = "GOFASTR_ISOLATION_REWRITE"

	// portScanMax bounds the linear scan that follows a hashed candidate.
	// Each iteration costs a net.Listen, so user-set ceilings need a sane cap.
	portScanMax = 64
)

// Runtime is the resolved isolation context for a project.
type Runtime struct {
	projectDir string
	gitRoot    string
	configPath string
	cfg        Config
	active     bool
	id         string

	portsMu sync.Mutex
	ports   map[int]int // base port → mapped port, in-process only
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
	configPath, configDir, err := discoverConfig(start)
	if err != nil {
		return nil, err
	}
	if configPath != "" {
		loaded, err := loadConfig(configPath, cfg)
		if err != nil {
			return nil, err
		}
		cfg = loaded
		start = configDir
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
	return &Runtime{
		projectDir: start,
		gitRoot:    gitRoot,
		configPath: configPath,
		cfg:        cfg,
		active:     active,
		id:         id,
		ports:      map[int]int{},
	}, nil
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

// Addr returns addr with its port remapped when isolation is active.
func (r *Runtime) Addr(addr string) (string, error) {
	if !r.Active() || addr == "" {
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
	next := r.portFor(port, host)
	r.recordPort(port, next)
	return joinAddr(host, next, shape), nil
}

// Env returns env with configured local resources isolated.
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
	for key, tmpl := range r.cfg.Env {
		if values[key] != "" && !rewriteExplicit {
			continue
		}
		values[key] = r.expandTemplate(tmpl, basePort)
	}
	values[envApplied] = "1"
	values[envID] = r.id
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

func discoverConfig(start string) (path, dir string, err error) {
	dir = start
	for {
		for _, name := range []string{"gofastr.yml", "gofastr.yaml"} {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil {
				return path, dir, nil
			} else if err != nil && !os.IsNotExist(err) {
				return "", "", err
			}
		}
		// Stop at the git root: a parent workspace's gofastr.yml must not
		// silently apply to a nested project.
		if _, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil {
			return "", start, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", start, nil
		}
		dir = parent
	}
}

func loadConfig(path string, base Config) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	node, err := coreyaml.Parse(string(data))
	if err != nil {
		return Config{}, err
	}
	if node.Kind != coreyaml.Map || node.Map["isolation"] == nil {
		return base, nil
	}
	m, err := expectMap(node.Map["isolation"], "isolation")
	if err != nil {
		return Config{}, err
	}
	cfg := base
	if v := m["enabled"]; v != nil {
		enabled, err := boolValue(v, "isolation.enabled")
		if err != nil {
			return Config{}, err
		}
		cfg.Enabled = enabled
	}
	if v := m["mode"]; v != nil {
		cfg.Mode = scalarString(v)
	}
	if v := m["port"]; v != nil {
		port, err := expectMap(v, "isolation.port")
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
		db, err := expectMap(v, "isolation.database")
		if err != nil {
			return Config{}, err
		}
		if n := db["strategy"]; n != nil {
			cfg.Database.Strategy = scalarString(n)
		}
	}
	if v := m["services"]; v != nil {
		services, err := expectMap(v, "isolation.services")
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
		env, err := expectMap(v, "isolation.env")
		if err != nil {
			return Config{}, err
		}
		cfg.Env = map[string]string{}
		for key, n := range env {
			cfg.Env[key] = scalarString(n)
		}
	}
	return cfg, nil
}

func findGitRoot(start string) (string, bool) {
	dir := start
	for {
		git := filepath.Join(dir, ".git")
		info, err := os.Stat(git)
		if err == nil {
			if info.IsDir() {
				return dir, false
			}
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

// lookupPort returns a previously recorded mapping for base, checking
// env first (so child processes inherit parent's mapping) and then this
// Runtime's in-memory cache.
func (r *Runtime) lookupPort(base int) (int, bool) {
	if p, ok := envAppliedPort(strconv.Itoa(base)); ok {
		return p, true
	}
	r.portsMu.Lock()
	defer r.portsMu.Unlock()
	p, ok := r.ports[base]
	return p, ok
}

// recordPort stores base → mapped in this Runtime's in-memory cache only.
// Child-process inheritance happens through Env(), which writes the env
// markers separately.
func (r *Runtime) recordPort(base, mapped int) {
	r.portsMu.Lock()
	defer r.portsMu.Unlock()
	r.ports[base] = mapped
}

// isResolvedPort reports whether port is already the mapped form for some
// base — used by Addr to short-circuit when a child process is handed an
// already-offset port and re-resolving would double-offset.
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
	r.portsMu.Lock()
	defer r.portsMu.Unlock()
	for _, mapped := range r.ports {
		if mapped == port {
			return true
		}
	}
	return false
}

// envAppliedPort reads the parent-process port mapping from env (set by
// Env() in the parent). Always process-wide — that's the inheritance hook.
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
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
	suffix := "_" + strings.ReplaceAll(r.id, "-", "_")
	if strings.HasSuffix(db, suffix) {
		return u.String(), nil
	}
	// Postgres silently truncates identifiers at NAMEDATALEN-1 (63 bytes by
	// default). Trim the base name so the suffix survives — two worktrees
	// must not collide on a truncated form.
	const pgMaxIdentifier = 63
	if len(db)+len(suffix) > pgMaxIdentifier {
		db = db[:pgMaxIdentifier-len(suffix)]
	}
	u.Path = "/" + db + suffix
	return u.String(), nil
}

func expectMap(node *coreyaml.Node, label string) (map[string]*coreyaml.Node, error) {
	if node == nil || node.Kind != coreyaml.Map {
		return nil, fmt.Errorf("%s must be a map", label)
	}
	return node.Map, nil
}

func scalarString(node *coreyaml.Node) string {
	if node == nil {
		return ""
	}
	return fmt.Sprint(node.Value)
}

func boolValue(node *coreyaml.Node, label string) (bool, error) {
	if node == nil || node.Kind != coreyaml.Scalar {
		return false, fmt.Errorf("%s must be a boolean", label)
	}
	v, ok := node.Value.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean", label)
	}
	return v, nil
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
