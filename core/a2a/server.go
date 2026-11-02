package a2a

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

// Defaults for Config's zero fields.
const (
	defaultMaxBodyBytes    = 1 << 20
	defaultTaskTimeout     = 5 * time.Minute
	defaultMaxHistory      = 200
	defaultDefaultPageSize = 50
	defaultMaxPageSize     = 200
	// defaultTerminalTaskRetention, defaultPausedTaskRetention and
	// defaultMaxPushConfigsPerTask bound the rows one owner's cheap
	// writes retain (Config.TerminalTaskRetention / PausedTaskRetention /
	// MaxPushConfigsPerTask; 0 = these).
	defaultTerminalTaskRetention = 64
	defaultPausedTaskRetention   = 64
	defaultMaxPushConfigsPerTask = 8
	// defaultMaxConcurrentRunsPerOwner bounds handler goroutines started
	// by one authenticated owner. TaskTimeout limits their lifetime; this
	// limit bounds how many can be live at once.
	defaultMaxConcurrentRunsPerOwner = 16
	// keepAliveEvery paces the SSE comment line while a stream waits
	// for the task's next event, so proxies do not idle the connection
	// out from under a long-running skill.
	keepAliveEvery = 15 * time.Second
	// pollEvery paces the multi-replica subscribe fallback.
	pollEvery = 500 * time.Millisecond
	// sseWriteDeadline bounds one SSE write; see sseStream.write.
	sseWriteDeadline = 10 * time.Second
)

// Config constructs a Server. Owner is required: multi-user scoping is
// not optional, so a nil Owner is a NewServer error rather than a
// silently single-tenant server.
type Config struct {
	// Skills is what the agent can do; at least one, unique ids, and
	// every Handler non-nil.
	Skills []Skill
	// Store persists tasks and push configs. Nil → NewMemoryStore().
	Store Store
	// Router picks the skill for a message. Nil → DefaultRouter.
	Router Router
	// Owner resolves the caller for every request. ok=false → HTTP 401
	// and CodeUnauthenticated. Nil is a NewServer error.
	Owner func(r *http.Request) (owner string, ok bool)
	// Scopes reports the scopes the caller's credential is narrowed to
	// (an API token's or an embed grant's) and true, or false for a
	// caller no scope narrows. A task records its creator's scopes, and a
	// scoped caller reads, resumes, cancels and configures only the tasks
	// made under the same scopes, so a narrow token of the owner cannot
	// read what a stronger credential ran. Nil: every caller is unscoped.
	Scopes func(r *http.Request) (scopes []string, scoped bool)
	// Tenant reports the tenant the caller acts in, "" for none. A task
	// records its creator's tenant, and a caller reads, lists, resumes,
	// cancels and configures only the tasks made in the tenant it is in
	// now, so a member removed from a tenant, or switched to another,
	// stops reading the results its tasks returned there. A stream ends
	// when the refreshed principal resolves to another tenant. Nil: no
	// tenant check, every task of the owner is visible.
	Tenant func(r *http.Request) string
	// ExtendedCard, when set, serves GetExtendedAgentCard. Nil → the
	// method answers CodeExtendedAgentCardNotConfigured and
	// Capabilities().ExtendedAgentCard is false.
	ExtendedCard func(ctx context.Context, owner string) (map[string]any, error)
	// Push tunes push-notification delivery; see PushOptions.
	Push PushOptions
	// Logger for handler failures, drops, and push errors. Nil →
	// slog.Default().
	Logger *slog.Logger
	// AllowedOrigins permits browser Origins that are not same-origin
	// with the request: tunnels (ngrok, Codespaces) and split-origin
	// web clients. The default posture refuses a present Origin naming
	// any authority other than the request's own Host, mirroring the
	// MCP transport's cross-origin gate; native A2A clients send no
	// Origin and always pass.
	AllowedOrigins []string
	// MaxBodyBytes caps one request body. Default 1 MiB.
	MaxBodyBytes int64
	// TaskTimeout is the ceiling on one skill-handler run. Default
	// 5 minutes.
	TaskTimeout time.Duration
	// MaxHistory caps stored history per task (oldest dropped).
	// Default 200 messages.
	MaxHistory int
	// DefaultPageSize and MaxPageSize bound ListTasks paging.
	// Defaults 50 / 200.
	DefaultPageSize, MaxPageSize int

	// TerminalTaskRetention bounds how many TERMINAL task rows
	// (completed / failed / canceled / rejected) one owner retains in
	// each tier (RetentionTier: the tenant and scope set a task was made
	// under): past the bound the tier's OLDEST terminal rows are
	// deleted after each settle in that tier, and no other tier's rows
	// are counted or touched. An owner therefore retains up to this
	// many per tenant and scope set it acts in. 0 = 64 (every row carries the caller's message body, and
	// the default store is in-RAM, so cheap authenticated writes must
	// not grow it without end); negative disables trimming for hosts
	// that own retention themselves. Applies to stores implementing
	// RetentionTrimmer — both built-ins do.
	TerminalTaskRetention int

	// PausedTaskRetention bounds how many PAUSED task rows (input or
	// auth required) one owner retains in each tier, as
	// TerminalTaskRetention counts: a skill that pauses on every send
	// would otherwise mint a row per send that terminal retention never
	// reaches. Past the bound the tier's OLDEST paused tasks are
	// canceled after each pause in that tier, and terminal retention
	// then trims them. 0 = 64; negative disables.
	PausedTaskRetention int

	// MaxPushConfigsPerTask bounds push-notification configs per task
	// (each one fans a delivery goroutine out per event): past the
	// bound the OLDEST configs are deleted. 0 = 8; negative disables.
	MaxPushConfigsPerTask int

	// MaxStreamSeatsPerOwner bounds the concurrent SSE streams
	// (SubscribeToTask + SendStreamingMessage) ONE owner may hold: each
	// pins a relay goroutine, a bus channel, and tickers for the
	// stream's whole life. 0 = 16 (the default every stream surface in
	// the tree shares); negative lifts the cap for deployments that
	// bound streams elsewhere.
	MaxStreamSeatsPerOwner int

	// MaxConcurrentRunsPerOwner bounds the concurrent skill-handler runs
	// one owner may start, including ReturnImmediately runs. 0 = 16;
	// negative disables the cap for deployments that bound runs elsewhere.
	MaxConcurrentRunsPerOwner int

	// StreamSeatOverflow selects what an owner at MaxStreamSeatsPerOwner
	// does with their next stream: stream.SeatOverflowRefuse (the
	// default) answers 429 at connect; stream.SeatOverflowEvictOldest
	// closes that owner's oldest stream and seats the new one — the
	// multi-tab-friendly policy for clients that reconnect faster than
	// a half-open connection's seat is reclaimed.
	StreamSeatOverflow stream.SeatOverflowPolicy
}

// Server is the A2A task-exchange HTTP handler. Construct with
// NewServer and mount behind the middleware that establishes the
// principal Config.Owner resolves.
type Server struct {
	skills   []Skill
	byID     map[string]Skill
	store    Store
	router   Router
	owner    func(*http.Request) (string, bool)
	scopes   func(*http.Request) ([]string, bool)
	tenant   func(*http.Request) string
	extended func(context.Context, string) (map[string]any, error)
	push     *pusher
	log      *slog.Logger
	maxBody  int64
	timeout  time.Duration
	maxHist  int
	defPage  int
	maxPage  int

	// maxTerminalTasks / maxPausedTasks / maxPushConfigs mirror Config.
	// TerminalTaskRetention / PausedTaskRetention / MaxPushConfigsPerTask
	// (0 resolved to the defaults in NewServer; negative kept as
	// "disabled").
	maxTerminalTasks int
	maxPausedTasks   int
	maxPushConfigs   int
	keepAlive        time.Duration
	pollEvery        time.Duration

	// maxStreamSeats / streamSeatOverflow mirror Config
	// MaxStreamSeatsPerOwner / StreamSeatOverflow; seats is the registry
	// both stream families (SubscribeToTask and SendStreamingMessage)
	// admit through.
	maxStreamSeats            int
	streamSeatOverflow        stream.SeatOverflowPolicy
	seats                     *streamSeatRegistry
	maxConcurrentRunsPerOwner int

	// allowedOrigins mirrors core/mcp's allow list: Origins that name a
	// foreign authority yet may still reach the dispatcher. Guarded by
	// mu (the same mutex the run registry uses; both are cold paths).
	allowedOrigins []string

	// now and newID are indirections for tests: a fixed clock and a
	// counter make golden frames byte-stable.
	now   func() time.Time
	newID func() string

	mu          sync.Mutex
	runs        map[string]*run
	runsByOwner map[string]int
	// closing is set by Shutdown under mu: no run is claimed after it.
	closing bool
	// life ends every run when Shutdown cancels it. A run is detached
	// from its request (see startRun), so nothing else reaches it.
	life     context.Context
	stopLife context.CancelFunc
}

// run tracks one in-process task run: the context CancelTask reaches,
// the event bus streaming subscribers attach to, and the done channel
// closed (after the final event is published) when the run settles.
// halted is set by CancelTask before it stores CANCELED: from then on
// the run's writes are refused, so neither a handler that completes on
// wake nor one writing in a loop can beat the cancel to the store. The
// CancelTask that set it closes cancelDone once it has published, and a
// halted run's teardown waits for that, so a handler that returns on a
// refused write cannot close the stream before the CANCELED event.
type run struct {
	id         string
	owner      string
	cancel     context.CancelFunc
	halted     atomic.Bool
	cancelDone chan struct{}
	bus        *taskBus
	done       chan struct{}
	// released closes when the run leaves the registry, the signal
	// Shutdown waits on: a claim refused later releases without ever
	// closing done.
	released chan struct{}
	// pausedAt is the version of this run's latest write once its
	// handler paused the task (RequireInput/RequireAuth), zero before.
	// A stored version past it was written by a run elsewhere that
	// resumed the task, which this run's bus never hears.
	pausedAt atomic.Int64
}

// NewServer validates cfg and applies its defaults.
func NewServer(cfg Config) (*Server, error) {
	if len(cfg.Skills) == 0 {
		return nil, errors.New("a2a: Config.Skills must have at least one skill")
	}
	byID := map[string]Skill{}
	for _, sk := range cfg.Skills {
		if sk.ID == "" {
			return nil, errors.New("a2a: skill with empty id")
		}
		if sk.Handler == nil {
			return nil, fmt.Errorf("a2a: skill %q has no Handler", sk.ID)
		}
		if _, dup := byID[sk.ID]; dup {
			return nil, fmt.Errorf("a2a: duplicate skill id %q", sk.ID)
		}
		byID[sk.ID] = sk
	}
	if cfg.Owner == nil {
		return nil, errors.New("a2a: Config.Owner is required (resolve the caller or refuse it; there is no anonymous posture)")
	}
	store := cfg.Store
	if store == nil {
		store = NewMemoryStore()
	}
	router := cfg.Router
	if router == nil {
		router = DefaultRouter
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	maxBody := cfg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxBodyBytes
	}
	timeout := cfg.TaskTimeout
	if timeout <= 0 {
		timeout = defaultTaskTimeout
	}
	maxTerminal := cfg.TerminalTaskRetention
	if maxTerminal == 0 {
		maxTerminal = defaultTerminalTaskRetention
	}
	maxPaused := cfg.PausedTaskRetention
	if maxPaused == 0 {
		maxPaused = defaultPausedTaskRetention
	}
	maxPush := cfg.MaxPushConfigsPerTask
	if maxPush == 0 {
		maxPush = defaultMaxPushConfigsPerTask
	}
	maxConcurrentRuns := cfg.MaxConcurrentRunsPerOwner
	if maxConcurrentRuns == 0 {
		maxConcurrentRuns = defaultMaxConcurrentRunsPerOwner
	}
	maxHist := cfg.MaxHistory
	if maxHist <= 0 {
		maxHist = defaultMaxHistory
	}
	maxPage := cfg.MaxPageSize
	if maxPage <= 0 {
		maxPage = defaultMaxPageSize
	}
	defPage := cfg.DefaultPageSize
	if defPage <= 0 {
		defPage = defaultDefaultPageSize
	}
	if defPage > maxPage {
		return nil, fmt.Errorf("a2a: Config.DefaultPageSize %d exceeds MaxPageSize %d", defPage, maxPage)
	}
	life, stopLife := context.WithCancel(context.Background())
	return &Server{
		life:                      life,
		stopLife:                  stopLife,
		skills:                    slices.Clone(cfg.Skills),
		byID:                      byID,
		store:                     store,
		router:                    router,
		owner:                     cfg.Owner,
		scopes:                    cfg.Scopes,
		tenant:                    cfg.Tenant,
		extended:                  cfg.ExtendedCard,
		push:                      newPusher(cfg.Push, log),
		log:                       log,
		maxBody:                   maxBody,
		timeout:                   timeout,
		maxHist:                   maxHist,
		defPage:                   defPage,
		maxPage:                   maxPage,
		maxTerminalTasks:          maxTerminal,
		maxPausedTasks:            maxPaused,
		maxPushConfigs:            maxPush,
		keepAlive:                 keepAliveEvery,
		pollEvery:                 pollEvery,
		maxStreamSeats:            cfg.MaxStreamSeatsPerOwner,
		streamSeatOverflow:        cfg.StreamSeatOverflow,
		seats:                     newStreamSeatRegistry(),
		maxConcurrentRunsPerOwner: maxConcurrentRuns,
		allowedOrigins:            slices.Clone(cfg.AllowedOrigins),
		now:                       time.Now,
		newID:                     newUUID,
		runs:                      map[string]*run{},
		runsByOwner:               map[string]int{},
	}, nil
}

// Skills returns a copy of the registered skills in registration order.
func (s *Server) Skills() []Skill {
	out := make([]Skill, len(s.skills))
	for i, sk := range s.skills {
		cp := sk
		cp.Tags = slices.Clone(sk.Tags)
		cp.Examples = slices.Clone(sk.Examples)
		cp.InputModes = slices.Clone(sk.InputModes)
		cp.OutputModes = slices.Clone(sk.OutputModes)
		out[i] = cp
	}
	return out
}

// Capabilities is what the agent card copies: this server always
// streams, and push/extended card follow configuration.
func (s *Server) Capabilities() Capabilities {
	return Capabilities{
		Streaming:         true,
		PushNotifications: !s.push.disable,
		ExtendedAgentCard: s.extended != nil,
	}
}

// rpcRequest and rpcResponse are the JSON-RPC 2.0 envelope. The id is
// kept as raw JSON so whatever the client sent (number, string, null)
// is echoed byte-for-byte.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// ServeHTTP answers one JSON-RPC request. POST only; the caller's
// Origin must be absent or name the request's own authority, the body
// is capped, the content type must be JSON, and the caller must resolve
// to an owner before any method runs.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeTransport(w, http.StatusMethodNotAllowed,
			Errorf(CodeInvalidRequest, "POST required"))
		return
	}
	if !s.originOK(r) {
		s.writeTransport(w, http.StatusForbidden,
			Errorf(CodeUnauthorized, "cross-origin request refused"))
		return
	}
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		s.writeTransport(w, http.StatusUnsupportedMediaType,
			Errorf(CodeContentTypeNotSupported, "content type must be application/json or application/a2a+json"))
		return
	}
	// MaxBytesReader caps the read AND makes the server close the
	// connection after an over-large body, so a client cannot keep
	// streaming.
	body := http.MaxBytesReader(w, r.Body, s.maxBody)
	raw, err := io.ReadAll(body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			s.writeTransport(w, http.StatusRequestEntityTooLarge,
				Errorf(CodeInvalidRequest, "request body exceeds %d bytes", s.maxBody))
			return
		}
		s.writeTransport(w, http.StatusBadRequest, Errorf(CodeParseError, "read body: %v", err))
		return
	}
	trimmed := strings.TrimLeft(string(raw), " \t\r\n")
	if strings.HasPrefix(trimmed, "[") {
		// A batch has no single id to answer; refuse at the transport
		// level. A2A v1.0 carries no batch semantics.
		s.writeTransport(w, http.StatusBadRequest,
			Errorf(CodeInvalidRequest, "batch requests are not supported"))
		return
	}
	var req rpcRequest
	// handler.UnmarshalStrict refuses duplicate and case-folded
	// top-level keys (stdlib json keeps the last duplicate and folds
	// key case), so no first-occurrence parser — proxy, WAF, audit
	// logger — can disagree with the executor's decode.
	if err := handler.UnmarshalStrict(raw, &req); err != nil {
		s.writeTransport(w, http.StatusBadRequest, Errorf(CodeParseError, "parse request: %v", err))
		return
	}
	if len(req.ID) == 0 || string(req.ID) == "null" {
		s.writeResult(w, nil, nil, Errorf(CodeInvalidRequest, "request id required"))
		return
	}
	// An id is a string or a number (JSON-RPC 2.0 §4). Any other type is
	// refused before it can be echoed: the reply carries a null id, since
	// a boolean, object or array cannot stand in as one.
	if c := req.ID[0]; c != '"' && c != '-' && (c < '0' || c > '9') {
		s.writeResult(w, nil, nil, Errorf(CodeInvalidRequest, "request id must be a string or a number"))
		return
	}
	if req.JSONRPC != "2.0" {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidRequest, `jsonrpc must be "2.0"`))
		return
	}
	// The owner check precedes dispatch for every method, so no method
	// — not even the card — leaks anything to an unidentified caller.
	owner, ok := s.owner(r)
	if !ok {
		s.writeTransport(w, http.StatusUnauthorized, ErrUnauthenticated())
		return
	}
	if s.tenant != nil {
		// The tenant at connect, which a stream's re-check compares the
		// refreshed principal's tenant against.
		r = r.WithContext(context.WithValue(r.Context(), connectTenantKey{}, s.tenant(r)))
	}
	s.dispatch(w, r, &req, owner)
}

func (s *Server) dispatch(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string) {
	switch req.Method {
	case MethodSendMessage:
		s.handleSend(w, r, req, owner, false)
	case MethodSendStreamingMessage:
		s.handleSend(w, r, req, owner, true)
	case MethodGetTask:
		s.handleGetTask(w, r, req, owner, s.callerScope(r))
	case MethodListTasks:
		s.handleListTasks(w, r, req, owner, s.callerScope(r))
	case MethodCancelTask:
		s.handleCancelTask(w, r, req, owner, s.callerScope(r))
	case MethodSubscribeToTask:
		s.handleSubscribe(w, r, req, owner)
	case MethodCreateTaskPushNotificationConfig:
		s.handleCreatePushConfig(w, r, req, owner, s.callerScope(r))
	case MethodGetTaskPushNotificationConfig:
		s.handleGetPushConfig(w, r, req, owner, s.callerScope(r))
	case MethodListTaskPushNotificationConfigs:
		s.handleListPushConfigs(w, r, req, owner, s.callerScope(r))
	case MethodDeleteTaskPushNotificationConfig:
		s.handleDeletePushConfig(w, r, req, owner, s.callerScope(r))
	case MethodGetExtendedAgentCard:
		s.handleExtendedCard(w, r, req, owner)
	default:
		s.writeResult(w, req.ID, nil, Errorf(CodeMethodNotFound, "unknown method %q", req.Method))
	}
}

// ---- response writing -------------------------------------------------

func (s *Server) writeResult(w http.ResponseWriter, id json.RawMessage, result any, aerr *Error) {
	s.writeStatus(w, http.StatusOK, id, result, aerr)
}

// writeStatus writes one JSON-RPC response. A non-200 status is only
// for transport failures (401/405/413/415/400-parse); JSON-RPC-level
// errors ride a 200 per convention.
func (s *Server) writeStatus(w http.ResponseWriter, status int, id json.RawMessage, result any, aerr *Error) {
	resp := rpcResponse{JSONRPC: "2.0", ID: id}
	if aerr != nil {
		resp.Error = aerr
	} else {
		b, err := json.Marshal(result)
		if err != nil {
			s.log.Error("a2a: marshal result", "err", err)
			resp.Error = Errorf(CodeInternalError, "internal error")
		} else {
			resp.Result = b
		}
	}
	b, err := json.Marshal(resp)
	if err != nil {
		// One of the fixed error shapes; unreachable in practice. The
		// JSON-RPC message is protocol text, English like the rest of
		// the agent contract.
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		//gofastr:allow(rawtext) JSON-RPC protocol text for an agent, beside its numeric code
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"internal error"}}`)
		return
	}
	// Owner/session-scoped JSON-RPC bodies must not be retained by a
	// shared cache: a back/forward cache or non-conforming proxy holding
	// one owner's task body is the leak the SSE arm (newSSEStream) and
	// core/mcp's transport already pin no-store against.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// writeTransport answers a transport-level failure (non-200): the
// request could not be addressed, so there is no id to echo.
func (s *Server) writeTransport(w http.ResponseWriter, status int, aerr *Error) {
	s.writeStatus(w, status, nil, nil, aerr)
}

// originOK reports whether r may reach the JSON-RPC dispatcher.
//
// A browser sets Origin on every cross-site fetch. If it is present and
// names a different authority than the request itself (Origin vs Host),
// this is a cross-site call and is refused before the body is read:
// the endpoint resolves an owner from the caller's session credential
// and returns the caller's task data, exactly the shape the four
// 2025-26 MCP transport CVEs made a MUST-grade gate. Absent Origin
// passes: curl and native A2A clients never send one, and its absence
// cannot prove an attack. Mirrors core/mcp's transport posture.
func (s *Server) originOK(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || !strings.EqualFold(u.Host, r.Host) {
		return s.originAllowListed(origin)
	}
	return true
}

// originAllowListed reports whether origin was explicitly permitted via
// Config.AllowedOrigins or SetAllowedOrigins: the escape hatch for
// tunnels (ngrok, Codespaces) and split-origin web clients.
func (s *Server) originAllowListed(origin string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.ContainsFunc(s.allowedOrigins, func(o string) bool {
		return strings.EqualFold(origin, o)
	})
}

// SetAllowedOrigins permits browser Origins that are not same-origin
// with the request, for servers constructed without going through
// Config.AllowedOrigins. Mirrors core/mcp's SetAllowedOrigins.
func (s *Server) SetAllowedOrigins(origins []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.allowedOrigins = slices.Clone(origins)
}

// internalErr logs err and answers the generic JSON-RPC internal error:
// a Store or handler error's text (driver messages, paths, SQL) never
// reaches the client.
func (s *Server) internalErr(w http.ResponseWriter, id json.RawMessage, what string, err error) {
	s.log.Error("a2a: "+what, "err", err)
	s.writeResult(w, id, nil, Errorf(CodeInternalError, "internal error"))
}

func decodeParams(raw json.RawMessage, v any) *Error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	// The envelope was decoded strictly; params are the second level
	// of the same body and get the same no-ambiguity rule (a nested
	// duplicate or case-folded key pair resolves by parser accident).
	// Unknown fields stay tolerated: the protocol reserves the right to
	// add them.
	if err := handler.CheckObjectKeys(raw, handler.FoldJSONKey); err != nil {
		return Errorf(CodeInvalidParams, "invalid params: %v", err)
	}
	if err := jsonexact.Unmarshal(raw, v); err != nil {
		return Errorf(CodeInvalidParams, "invalid params: %v", err)
	}
	return nil
}

func isJSONContentType(ct string) bool {
	if ct == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return mediaType == "application/json" || mediaType == "application/a2a+json"
}

// applyHistoryLength keeps the last n history messages on a task copy;
// nil keeps everything. Only response copies are trimmed — the store
// always holds the full history.
func applyHistoryLength(t *Task, n *int) {
	if n == nil {
		return
	}
	k := *n
	if k < 0 {
		k = 0
	}
	if len(t.History) > k {
		t.History = t.History[len(t.History)-k:]
	}
}

// encodePageToken/decodePageToken make the page cursor opaque: base64
// of the decimal offset. Nothing up to MaxPageSize*2^31 rows depends on
// the encoding being clever.
func encodePageToken(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodePageToken(tok string) (int, error) {
	b, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(string(b))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("bad token")
	}
	return n, nil
}

// clampPageSize applies the configured bounds. A supplied size outside
// [1, MaxPageSize] is clamped, not refused: paging is not a security
// posture, and silently shrinking is friendlier than erroring.
func (s *Server) clampPageSize(n *int) int {
	if n == nil || *n < 1 {
		return s.defPage
	}
	return min(*n, s.maxPage)
}

// ---- run registry -----------------------------------------------------

type runClaimResult uint8

const (
	runClaimed runClaimResult = iota
	runClaimTaskAlreadyActive
	runClaimOwnerAtLimit
	runClaimShuttingDown
)

// claimRun registers a placeholder run for taskID and owner. Claiming
// BEFORE the WORKING transition prevents concurrent resumes from
// interleaving writes; counting claims under the same lock bounds the
// owner's handler goroutines across all tasks.
func (s *Server) claimRun(taskID, owner string) (*run, runClaimResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return nil, runClaimShuttingDown
	}
	if _, exists := s.runs[taskID]; exists {
		return nil, runClaimTaskAlreadyActive
	}
	if s.maxConcurrentRunsPerOwner >= 0 && s.runsByOwner[owner] >= s.maxConcurrentRunsPerOwner {
		return nil, runClaimOwnerAtLimit
	}
	rn := &run{
		id:         taskID,
		owner:      owner,
		cancel:     func() {},
		cancelDone: make(chan struct{}),
		bus:        newTaskBus(s.log),
		done:       make(chan struct{}),
		released:   make(chan struct{}),
	}
	s.runs[taskID] = rn
	s.runsByOwner[owner]++
	return rn, runClaimed
}

func (s *Server) runFor(taskID string) *run {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runs[taskID]
}

func (s *Server) releaseRun(rn *run) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runs[rn.id] == rn {
		delete(s.runs, rn.id)
		close(rn.released)
		if s.runsByOwner[rn.owner] <= 1 {
			delete(s.runsByOwner, rn.owner)
		} else {
			s.runsByOwner[rn.owner]--
		}
	}
}

// finishRun unregisters the run and closes done. Called after finalize,
// so the terminal event is already published when done closes and a
// draining subscriber still sees it.
func (s *Server) finishRun(rn *run) {
	if rn.halted.Load() {
		<-rn.cancelDone
	}
	s.releaseRun(rn)
	close(rn.done)
}

// setRunCancel installs the run's live cancel and reports whether
// CancelTask had already halted the run, both under the lock wakeRun
// reads the cancel under (see startRun).
func (s *Server) setRunCancel(rn *run, cancel context.CancelFunc) (halted bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rn.cancel = cancel
	return rn.halted.Load()
}

// Shutdown stops claiming runs, cancels every live run's context, and
// waits until each has been released (a started run releases after its
// final state is stored and published) or ctx ends. A handler that ignores its context holds Shutdown only until
// ctx's deadline. The HTTP handler keeps answering reads; a send or
// resume after Shutdown is refused.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	released := make([]chan struct{}, 0, len(s.runs))
	for _, rn := range s.runs {
		released = append(released, rn.released)
	}
	s.mu.Unlock()
	s.stopLife()
	for _, r := range released {
		select {
		case <-r:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// wakeRun cancels a run's context. The cancel func is read under the
// lock setRunCancel writes it under.
func (s *Server) wakeRun(rn *run) {
	s.mu.Lock()
	cancel := rn.cancel
	s.mu.Unlock()
	cancel()
}

// startRun launches the handler goroutine for a claimed run. The run
// context is cut from the request's CANCELLATION but not from the
// request itself: returnImmediately and stream disconnects must not
// abort work the client was told is running. TaskTimeout bounds it, and
// Shutdown ends it.
//
// A CancelTask can land between the claim and this call (a streaming
// client knows the id once the snapshot is sent): it halts the run and
// wakes the claim's placeholder cancel, which is a no-op. setRunCancel
// installs the live cancel and reads the halted flag under the lock
// wakeRun reads the cancel under, so either wakeRun calls the live
// cancel or this call sees the halt. A run halted before it starts
// never invokes the handler: finalize settles it as CANCELED against
// the store, and finishRun releases it.
func (s *Server) startRun(r *http.Request, t *taskRun, rn *run, h Handler) {
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), s.timeout)
	stopLife := context.AfterFunc(s.life, cancel)
	runCtx = WithRequest(runCtx, r)
	haltedBeforeStart := s.setRunCancel(rn, cancel)
	if haltedBeforeStart {
		cancel()
	}
	t.attach(rn, runCtx)
	returned := make(chan struct{})
	if _, inProcess := s.store.(*MemoryStore); !inProcess && !haltedBeforeStart {
		go s.watchRemoteCancel(runCtx, returned, t, rn)
	}
	go func() {
		defer func() {
			stopLife()
			cancel()
			s.finishRun(rn)
		}()
		// finalize reads and writes through the Store, publishes, and
		// trims through the RetentionTrimmer: host-supplied code on this
		// goroutine, outside invoke's recover, with no per-request net
		// behind it. A panic there must end this run (logged; the
		// deferred finishRun still releases it), not the process and
		// every other owner's runs with it. The task keeps its last
		// stored state.
		defer func() {
			if p := recover(); p != nil {
				s.log.Error("a2a: run finalize panicked",
					"taskId", rn.id, "panic", textsafe.Recovered(p))
			}
		}()
		var err error
		if !haltedBeforeStart {
			err = s.invoke(runCtx, t, h)
		}
		close(returned)
		s.finalize(runCtx, t, err)
	}()
}

// watchRemoteCancel polls a shared store while a run is live. A
// CancelTask served by another replica writes only the store, so without
// this the handler ran on until its next write or TaskTimeout, and this
// replica's subscribers never saw CANCELED. A terminal state this run
// did not write halts the run the way a local CancelTask does, tells the
// local subscribers, and cuts the handler's context. finalize then
// adopts the stored record. Push configs are left alone: the replica
// that wrote the state already delivered to them.
//
// The watch ends when the handler returns: finalize's own write is not
// held under t.mu across the store call, so a later read could take it
// for a remote one and announce the terminal state twice.
func (s *Server) watchRemoteCancel(ctx context.Context, returned <-chan struct{}, t *taskRun, rn *run) {
	tick := time.NewTicker(s.pollEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-returned:
			return
		case <-tick.C:
		}
		rctx, cancel := context.WithTimeout(ctx, storeOpTimeout)
		rec, err := s.pollGetTask(rctx, t.owner, t.taskID())
		cancel()
		if errors.Is(err, errStorePanicked) {
			return
		}
		gone := errors.Is(err, ErrNotFound)
		if !gone && (err != nil || !rec.Task.Status.State.Terminal()) {
			continue
		}
		t.mu.Lock()
		select {
		case <-returned:
			t.mu.Unlock()
			return
		default:
		}
		if gone {
			// Retention deletes only terminal rows, so a missing row
			// this run has not settled was settled on another replica
			// and trimmed before a poll caught it. There is no record
			// to announce: local subscribers fall through to the store
			// poll, which ends on the missing row. finalize's write
			// is an UPDATE, so it cannot bring the row back.
			halt := !t.rec.Task.Status.State.Terminal() && rn.halted.CompareAndSwap(false, true)
			if halt {
				close(rn.cancelDone)
			}
			t.mu.Unlock()
			if halt {
				s.wakeRun(rn)
			}
			return
		}
		remote := rec.Version > t.rec.Version && !t.rec.Task.Status.State.Terminal() &&
			rn.halted.CompareAndSwap(false, true)
		if remote {
			close(rn.cancelDone)
			rn.bus.publish(busEvent{ev: StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{
				TaskID:    rec.Task.ID,
				ContextID: rec.Task.ContextID,
				Status:    rec.Task.Status,
			}}, version: rec.Version})
		}
		t.mu.Unlock()
		if remote {
			s.wakeRun(rn)
		}
		return
	}
}

// publish fans one event out to the task's live subscribers and its
// push configs. Called only AFTER the event's task state is persisted,
// so rec.Version is the version that write produced; subscribers use
// it to skip events a snapshot already reflects, and push delivery
// uses it to drop an event that lost the race to a newer one.
func (s *Server) publish(owner string, rec *TaskRecord, ev StreamResponse) {
	version := rec.Version
	if rn := s.runFor(rec.Task.ID); rn != nil {
		rn.bus.publish(busEvent{ev: ev, version: version})
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeOpTimeout)
	cfgs, err := s.store.ListPushConfigs(ctx, owner, rec.Task.ID)
	cancel()
	if err != nil {
		s.log.Error("a2a: list push configs", "taskId", rec.Task.ID, "err", err)
		return
	}
	s.push.deliver(cfgs, ev, version)
}

// ---- SendMessage / SendStreamingMessage -------------------------------

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string, streaming bool) {
	var p SendMessageRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if aerr := s.validateInbound(&p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	// Refuse streaming before prepareSend creates or claims a task. A writer
	// without Flush cannot stream, and preparing first left a submitted task
	// plus a run registration behind after the -32004 response.
	if streaming {
		if _, ok := w.(http.Flusher); !ok {
			s.writeResult(w, req.ID, nil, ErrUnsupportedOperation("streaming requires a flushing ResponseWriter"))
			return
		}
		s.streamSend(w, r, req.ID, owner, &p)
		return
	}
	t, rn, h, aerr := s.prepareSend(r, owner, &p)
	if aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	// Non-streaming: answer with the task. returnImmediately hands the
	// SUBMITTED/WORKING snapshot back at once and keeps running in the
	// background; otherwise the response IS the final task.
	if p.Configuration != nil && p.Configuration.ReturnImmediately && h != nil {
		s.startRun(r, t, rn, h)
		task := t.snapshot()
		applyHistoryLength(task, historyLengthOf(&p))
		s.writeResult(w, req.ID, SendMessageResponse{Task: task}, nil)
		return
	}
	if h != nil {
		s.startRun(r, t, rn, h)
		// Wait for the run, but not past the client: a caller that hangs
		// up must not pin this goroutine for the rest of TaskTimeout. The
		// run itself carries on regardless (its context is cut from the
		// request's cancellation), so the client was told nothing false.
		select {
		case <-rn.done:
		case <-r.Context().Done():
			return
		}
	}
	task := t.snapshot()
	applyHistoryLength(task, historyLengthOf(&p))
	s.writeResult(w, req.ID, SendMessageResponse{Task: task}, nil)
}

func historyLengthOf(p *SendMessageRequest) *int {
	if p.Configuration == nil {
		return nil
	}
	return p.Configuration.HistoryLength
}

func pushConfigOf(p *SendMessageRequest) *PushNotificationConfig {
	if p.Configuration == nil {
		return nil
	}
	return p.Configuration.TaskPushNotificationConfig
}

// validateInbound enforces the message shape and the push config (when
// one rides along in configuration) BEFORE anything is created, so a
// refused request leaves no half-built task behind.
func (s *Server) validateInbound(p *SendMessageRequest) *Error {
	m := p.Message
	if m == nil {
		return Errorf(CodeInvalidParams, "message is required")
	}
	if m.Role != RoleUser {
		return Errorf(CodeInvalidParams, "message.role must be ROLE_USER, got %q", m.Role)
	}
	if len(m.Parts) == 0 {
		return Errorf(CodeInvalidParams, "message.parts must not be empty")
	}
	for i := range m.Parts {
		if err := m.Parts[i].Validate(); err != nil {
			return Errorf(CodeInvalidParams, "message.parts[%d]: %v", i, err)
		}
	}
	if cfg := pushConfigOf(p); cfg != nil {
		if s.push.disable {
			return Errorf(CodePushNotificationNotSupported, "push notifications are disabled")
		}
		if cfg.URL == "" {
			return Errorf(CodeInvalidParams, "taskPushNotificationConfig.url is required")
		}
		if err := validatePushURL(cfg.URL, hasCredentials(*cfg), s.push.allowPrivate); err != nil {
			return Errorf(CodeInvalidParams, "%v", err)
		}
	}
	return nil
}

// prepareSend resolves a SendMessage to a runnable task. It returns the
// run context (h and rn non-nil exactly together), or a task that is
// already answered (h nil: rejected at routing), or a JSON-RPC error —
// in which case nothing is left running.
func (s *Server) prepareSend(r *http.Request, owner string, p *SendMessageRequest) (t *taskRun, rn *run, h Handler, aerr *Error) {
	if p.Message.TaskID == "" {
		return s.prepareNewTask(r, owner, p)
	}
	return s.prepareResume(r, owner, p)
}

func (s *Server) prepareNewTask(r *http.Request, owner string, p *SendMessageRequest) (*taskRun, *run, Handler, *Error) {
	msg := p.Message
	if msg.MessageID == "" {
		msg.MessageID = s.newID()
	}
	taskID := s.newID()
	if msg.ContextID == "" {
		msg.ContextID = s.newID()
	}
	msg.TaskID = taskID

	skillID, rerr := s.router(r.Context(), msg, s.skills)
	h := Handler(nil)
	if rerr == nil {
		if sk, ok := s.byID[skillID]; ok {
			h = sk.Handler
		} else {
			rerr = fmt.Errorf("no skill named %q", skillID)
		}
	}

	task := Task{
		ID:        taskID,
		ContextID: msg.ContextID,
		Status:    TaskStatus{State: TaskStateSubmitted, Timestamp: s.stamp()},
		History:   []Message{*msg},
	}
	if rerr == nil {
		task.Metadata = map[string]any{"gofastr.skill": skillID}
	}
	rec := &TaskRecord{
		Task:      task,
		Owner:     owner,
		SkillID:   skillID,
		Scope:     s.callerScope(r).key,
		Tenant:    s.callerScope(r).tenant,
		CreatedAt: s.now(),
		UpdatedAt: s.now(),
	}
	var rn *run
	if rerr == nil {
		var result runClaimResult
		rn, result = s.claimRun(taskID, owner)
		if result == runClaimOwnerAtLimit {
			return nil, nil, nil, ErrUnsupportedOperation("maximum concurrent task runs reached")
		}
		if result == runClaimShuttingDown {
			return nil, nil, nil, ErrUnsupportedOperation("server is shutting down")
		}
		if result != runClaimed {
			// A freshly minted UUID cannot collide; treat as unrecoverable.
			return nil, nil, nil, Errorf(CodeInternalError, "internal error")
		}
	}
	// Store the push config BEFORE the task row, and compensate with a
	// delete if the task insert then fails. The opposite order left a
	// half-built send behind when the config insert failed: an
	// unreachable SUBMITTED task with no run that would never settle
	// (validateInbound's no-half-built posture, but for the post-create
	// failure path). A push config for a task id that never materialized
	// is inert by comparison, and DeletePushConfig removes even that.
	if cfg := pushConfigOf(p); cfg != nil {
		cfg.TaskID = taskID // the task id did not exist when the client built the request
		if aerr := s.storePushConfig(r.Context(), owner, cfg); aerr != nil {
			if rn != nil {
				s.releaseRun(rn)
			}
			return nil, nil, nil, aerr
		}
	}
	if err := s.store.CreateTask(r.Context(), rec); err != nil {
		s.log.Error("a2a: create task", "taskId", taskID, "err", err)
		if cfg := pushConfigOf(p); cfg != nil {
			if derr := s.store.DeletePushConfig(r.Context(), owner, taskID, cfg.ID); derr != nil {
				// The config row is inert without its task, so this is
				// a log line, not a second error to report.
				s.log.Error("a2a: delete push config after failed task create", "taskId", taskID, "err", derr)
			}
		}
		if rn != nil {
			s.releaseRun(rn)
		}
		return nil, nil, nil, Errorf(CodeInternalError, "internal error")
	}
	t := newTaskRun(s, rec, msg, r, owner)
	if rerr != nil {
		// A skill miss is the agent's decision, not a protocol error:
		// the task exists and is REJECTED with the router's message.
		if err := t.Reject(TextPart(rerr.Error())); err != nil {
			s.log.Error("a2a: reject unrouted task", "taskId", taskID, "err", err)
			return nil, nil, nil, Errorf(CodeInternalError, "internal error")
		}
		return t, nil, nil, nil
	}
	return t, rn, h, nil
}

func (s *Server) prepareResume(r *http.Request, owner string, p *SendMessageRequest) (*taskRun, *run, Handler, *Error) {
	msg := p.Message
	if msg.MessageID == "" {
		msg.MessageID = s.newID()
	}
	taskID := msg.TaskID
	rec, err := s.getTask(r.Context(), owner, s.callerScope(r), taskID)
	if errors.Is(err, ErrNotFound) {
		return nil, nil, nil, ErrTaskNotFound(taskID)
	}
	if err != nil {
		s.log.Error("a2a: get task for resume", "taskId", taskID, "err", err)
		return nil, nil, nil, Errorf(CodeInternalError, "internal error")
	}
	state := rec.Task.Status.State
	if state.Terminal() {
		return nil, nil, nil, ErrUnsupportedOperation(fmt.Sprintf("task %s is %s", taskID, state))
	}
	if !state.Interrupted() {
		// SUBMITTED or WORKING: the task has a run (here or on another
		// replica). Conservative reading: refuse rather than queue.
		return nil, nil, nil, ErrUnsupportedOperation(fmt.Sprintf("task %s is running", taskID))
	}
	if cfg := pushConfigOf(p); cfg != nil && cfg.TaskID != "" && cfg.TaskID != taskID {
		return nil, nil, nil, Errorf(CodeInvalidParams, "taskPushNotificationConfig.taskId %q does not match addressed task %q", cfg.TaskID, taskID)
	}
	// Claim before the WORKING write: the claim is what a second
	// message to the same interrupted task hits.
	rn, claimResult := s.claimRun(taskID, owner)
	if claimResult == runClaimTaskAlreadyActive {
		return nil, nil, nil, ErrUnsupportedOperation(fmt.Sprintf("task %s is running", taskID))
	}
	if claimResult == runClaimOwnerAtLimit {
		return nil, nil, nil, ErrUnsupportedOperation("maximum concurrent task runs reached")
	}
	if claimResult == runClaimShuttingDown {
		return nil, nil, nil, ErrUnsupportedOperation("server is shutting down")
	}
	if cfg := pushConfigOf(p); cfg != nil {
		cfg.TaskID = taskID
		if aerr := s.storePushConfig(r.Context(), owner, cfg); aerr != nil {
			s.releaseRun(rn)
			return nil, nil, nil, aerr
		}
	}
	t := newTaskRun(s, rec, msg, r, owner)
	if err := t.resumeWorking(msg); err != nil {
		s.releaseRun(rn)
		if errors.Is(err, errConcurrentChange) {
			return nil, nil, nil, Errorf(CodeUnsupportedOperation, "task %s changed concurrently, retry", taskID)
		}
		// Any other failure here is a backend error: report the generic
		// internal error (errors.go's contract), never the driver text.
		s.log.Error("a2a: resume task", "taskId", taskID, "err", err)
		return nil, nil, nil, Errorf(CodeInternalError, "internal error")
	}
	sk, ok := s.byID[rec.SkillID]
	if !ok || sk.Handler == nil {
		// The skill this task was routed to is no longer registered.
		if err := t.Reject(TextPart(fmt.Sprintf("no skill named %q", rec.SkillID))); err != nil {
			s.log.Error("a2a: reject resume without skill", "taskId", taskID, "err", err)
		}
		s.finishRun(rn)
		return t, nil, nil, nil
	}
	return t, rn, sk.Handler, nil
}

func (s *Server) storePushConfig(ctx context.Context, owner string, cfg *PushNotificationConfig) *Error {
	if cfg.ID == "" {
		cfg.ID = s.newID()
	}
	rec := &PushConfigRecord{Config: *cfg, Owner: owner, CreatedAt: s.now()}
	if err := s.store.CreatePushConfig(ctx, rec); err != nil {
		if errors.Is(err, ErrConflict) {
			return Errorf(CodeInvalidParams, "push config %q already exists for task %q", cfg.ID, cfg.TaskID)
		}
		s.log.Error("a2a: create push config", "taskId", cfg.TaskID, "err", err)
		return Errorf(CodeInternalError, "internal error")
	}
	// Bound the configs this task retains: every event fans a delivery
	// out per config, so the set is capped (Config.MaxPushConfigsPerTask)
	// by deleting the OLDEST past the bound. The create itself always
	// succeeds — the caller never has to learn about the bound.
	s.trimPushConfigs(ctx, owner, cfg.TaskID)
	return nil
}

// trimTerminalTasks bounds the TERMINAL task rows one tier retains
// (Config.TerminalTaskRetention per owner, tenant and scope). Called
// after every persist that leaves a task terminal, with that task's
// tier: one tier's writes never trim another's rows. A store that does
// not implement RetentionTrimmer keeps its own policy. Failure is a log
// line, never a task-write failure: retention is an internal bound
// behind the write it follows.
func (s *Server) trimTerminalTasks(owner string, tier RetentionTier) {
	if s.maxTerminalTasks < 0 {
		return
	}
	rt, ok := s.store.(RetentionTrimmer)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeOpTimeout)
	defer cancel()
	if err := rt.TrimTerminalTasks(ctx, owner, tier, s.maxTerminalTasks); err != nil {
		s.log.Error("a2a: trim terminal tasks", "owner", owner, "err", err)
	}
}

// pausedTrimBatch is how many paused rows past the bound one trim reads
// per state. Every pause trims, so the excess is normally one row.
const pausedTrimBatch = 16

// trimPausedTasks bounds the PAUSED task rows one tier retains
// (Config.PausedTaskRetention per owner, tenant and scope). Called after
// every persist that leaves a task paused, with that task's tier. Past
// the bound the tier's oldest are canceled through the store, so it
// works on any Store, and the canceled rows then fall to terminal
// retention. A handler still running on a canceled task gets a version
// conflict on its next write, as after CancelTask. Failure is a log
// line, like the other trims.
func (s *Server) trimPausedTasks(owner string, tier RetentionTier) {
	if s.maxPausedTasks < 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeOpTimeout)
	defer cancel()
	// A store that filters by scope and tenant pages the tier itself; one
	// that does not may return every tier, so it is read whole and the
	// tier picked out below (a page cut across tiers could miss the
	// tier's own oldest rows).
	_, scopeFilters := s.store.(ScopeFilter)
	_, tenantFilters := s.store.(TenantFilter)
	var paused []*TaskRecord
	for _, st := range []TaskState{TaskStateInputRequired, TaskStateAuthRequired} {
		q := ListQuery{Status: st, Scoped: true, Scope: tier.Scope, Tenanted: true, Tenant: tier.Tenant}
		if scopeFilters && tenantFilters {
			q.Limit = s.maxPausedTasks + pausedTrimBatch
		}
		recs, _, err := s.store.ListTasks(ctx, owner, q)
		if err != nil {
			s.log.Error("a2a: list paused tasks", "owner", owner, "err", err)
			return
		}
		paused = append(paused, recs...)
	}
	// Never count or cancel another tier's row, whatever the store's
	// filtering did.
	paused = slices.DeleteFunc(paused, func(rec *TaskRecord) bool { return !tier.holds(rec) })
	// A task that moved between the two states while they were listed
	// shows up in both; count it once, at its newest state.
	sortRecsNewestFirst(paused)
	seen := make(map[string]bool, len(paused))
	paused = slices.DeleteFunc(paused, func(rec *TaskRecord) bool {
		dup := seen[rec.Task.ID]
		seen[rec.Task.ID] = true
		return dup
	})
	if len(paused) <= s.maxPausedTasks {
		return
	}
	canceled := false
	for _, rec := range paused[s.maxPausedTasks:] {
		cand := rec.Clone()
		cand.Task.Status = TaskStatus{State: TaskStateCanceled, Timestamp: s.stamp()}
		if err := s.store.UpdateTask(ctx, cand); err != nil {
			// A conflict is a resume or cancel that got there first.
			if !errors.Is(err, ErrConflict) {
				s.log.Error("a2a: cancel paused task", "taskId", rec.Task.ID, "err", err)
			}
			continue
		}
		s.publish(owner, cand, StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{
			TaskID:    cand.Task.ID,
			ContextID: cand.Task.ContextID,
			Status:    cand.Task.Status,
		}})
		canceled = true
	}
	if canceled {
		s.trimTerminalTasks(owner, tier)
	}
}

// trimPushConfigs bounds the push-notification configs retained for
// (owner, taskID) (Config.MaxPushConfigsPerTask), after each create.
func (s *Server) trimPushConfigs(ctx context.Context, owner, taskID string) {
	if s.maxPushConfigs < 0 {
		return
	}
	rt, ok := s.store.(RetentionTrimmer)
	if !ok {
		return
	}
	c, cancel := context.WithTimeout(ctx, storeOpTimeout)
	defer cancel()
	if err := rt.TrimPushConfigs(c, owner, taskID, s.maxPushConfigs); err != nil {
		s.log.Error("a2a: trim push configs", "taskId", taskID, "err", err)
	}
}

// snapshot returns a deep copy of the run's current task.
func (t *taskRun) snapshot() *Task {
	return t.Task()
}

// snapshotRecord returns a deep copy of the run's current record: the
// task together with the version its last write left it at.
func (t *taskRun) snapshotRecord() *TaskRecord {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rec.Clone()
}

// ---- SSE ---------------------------------------------------------------

// sseStream writes one task-exchange SSE response. Events are framed by
// mcp.StreamSSE, which delivers a payload with embedded newlines as
// spec-correct multi-line data and so can never be split into two
// events by one; each write is flushed immediately and carries a
// per-write deadline so a client that stops reading cannot pin this

// goroutine.
type sseStream struct {
	w    *errWriter
	fl   http.Flusher
	id   json.RawMessage
	srv  *Server
	rc   *http.ResponseController
	sent bool
}

// errWriter records the first write error; an io.Writer API cannot
// return one, so every later write short-circuits instead of touching
// a dead connection.
type errWriter struct {
	w   http.ResponseWriter
	err error
}

func (ew *errWriter) Write(p []byte) (int, error) {
	if ew.err != nil {
		return 0, ew.err
	}
	n, err := ew.w.Write(p)
	if err != nil {
		ew.err = err
	}
	return n, err
}

func (s *Server) newSSEStream(w http.ResponseWriter, id json.RawMessage) *sseStream {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	return &sseStream{
		w:   &errWriter{w: w},
		fl:  w.(http.Flusher),
		id:  id,
		srv: s,
		rc:  http.NewResponseController(w),
	}
}

// send marshals one JSON-RPC response whose result is the
// StreamResponse and writes it as a single SSE event.
func (st *sseStream) send(ev StreamResponse) error {
	result, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(rpcResponse{JSONRPC: "2.0", ID: st.id, Result: result})
	if err != nil {
		return err
	}
	return st.write("data", payload)
}

// write emits one raw SSE line ("data: <payload>" or ": comment") plus
// the dispatch blank line, then flushes.
func (st *sseStream) write(field string, payload []byte) error {
	if st.w.err != nil {
		return st.w.err
	}
	_ = st.rc.SetWriteDeadline(time.Now().Add(sseWriteDeadline))
	mcp.StreamSSE(st.w, "", string(payload))
	st.fl.Flush()
	st.clearDeadline()
	return st.w.err
}

// clearDeadline drops the write deadline once a write is flushed. On
// HTTP/2 the deadline is a stream timer that resets the stream when it
// fires, write pending or not, so one left armed would kill the stream
// in the idle gap before the next keep-alive.
func (st *sseStream) clearDeadline() {
	_ = st.rc.SetWriteDeadline(time.Time{})
}

func (st *sseStream) keepAlive() error {
	if st.w.err != nil {
		return st.w.err
	}
	_ = st.rc.SetWriteDeadline(time.Now().Add(sseWriteDeadline))
	_, err := fmt.Fprintf(st.w, ": keep-alive\n\n")
	if err != nil {
		st.w.err = err
		return err
	}
	st.fl.Flush()
	st.clearDeadline()
	return st.w.err
}

// streamSend answers SendStreamingMessage: the initial task snapshot,
// then every event until the task settles. handleSend has already
// checked that the writer flushes.
//
// The seat is admitted BEFORE prepareSend creates or claims anything:
// refusing it afterwards left the task SUBMITTED with its run claimed
// and never started, so the task never settled and the owner's run
// quota leaked one slot per refusal. Once prepareSend has claimed a
// run, every path out of here starts it, for the same reason. The seat
// is freed when this returns (when the stream ends, or at once when
// prepareSend refuses the message).
func (s *Server) streamSend(w http.ResponseWriter, r *http.Request, id json.RawMessage, owner string, p *SendMessageRequest) {
	seat, ok := s.seats.admitSeat(owner, s.maxStreamSeats, s.streamSeatOverflow)
	if !ok {
		s.writeTransport(w, http.StatusTooManyRequests,
			Errorf(CodeInvalidRequest, "too many concurrent streams for this owner"))
		return
	}
	defer s.seats.releaseSeat(seat)
	t, rn, h, aerr := s.prepareSend(r, owner, p)
	if aerr != nil {
		s.writeResult(w, id, nil, aerr)
		return
	}
	st := s.newSSEStream(w, id)
	// Subscribe before the snapshot and before the run starts. Writing
	// the snapshot can block on a slow client, and a CancelTask in that
	// window publishes CANCELED once; the run then finds the task already
	// canceled and publishes nothing, so a stream that subscribed after
	// the write ended on SUBMITTED. No version floor: until the run
	// starts nothing advances the snapshot's record (CancelTask writes
	// the store, not the run), so every event on the bus is news.
	var ch chan busEvent
	if h != nil {
		ch = rn.bus.subscribe()
		defer rn.bus.unsubscribe(ch)
	}
	snap := t.snapshotRecord()
	task := cloneTask(snap.Task)
	applyHistoryLength(&task, historyLengthOf(p))
	if err := st.send(StreamResponse{Task: &task}); err != nil {
		// The client is gone before the first event. Run the task
		// anyway, exactly as for a client that leaves right after the
		// snapshot: the run settles it and releases its registration.
		if h != nil {
			s.startRun(r, t, rn, h)
		}
		return
	}
	if h == nil {
		return // routing reject: the snapshot is already terminal
	}
	s.startRun(r, t, rn, h)
	seen, unsettled := s.forwardEvents(st, rn, ch, r, owner, seat, 0)
	if !unsettled {
		return
	}
	// The run ended without its final event reaching this stream. A
	// CancelTask on a resumed task can land between the run's claim and
	// the subscribe above: it publishes CANCELED to a bus nobody has
	// joined, and the halted run adopts the stored state without
	// announcing it again. Follow the store from what was sent, as
	// SubscribeToTask does, so the stream ends on the settled state.
	snap.Version = max(seen, snap.Version)
	s.pollEvents(st, r, owner, snap, seat)
}

// forwardEvents relays bus events to the stream until the task settles,
// the client goes away, or the run ends. An event whose write left the
// task at a version at or below floor is already in the snapshot the
// stream opened with, and is skipped rather than sent twice.
//
// unsettled reports a run that ended with the stream still open: its
// handler returned without a final event, so the task moved on without
// it (another replica resumed it). seen is the last version sent.
func (s *Server) forwardEvents(st *sseStream, rn *run, ch chan busEvent, r *http.Request, owner string, seat *streamSeat, floor int64) (seen int64, unsettled bool) {
	ctx := r.Context()
	keepAlive := time.NewTicker(s.keepAlive)
	defer keepAlive.Stop()
	seen = floor
	// relay sends one event and reports whether the stream is over.
	relay := func(be busEvent) bool {
		if be.version <= floor {
			return false
		}
		if !s.callerHeld(r, owner) {
			return true
		}
		if err := st.send(be.ev); err != nil {
			return true
		}
		seen = max(seen, be.version)
		return endsStream(be.ev)
	}
	for {
		select {
		case be, ok := <-ch:
			if !ok {
				// Dropped for being slow; the stream is done.
				return
			}
			if relay(be) {
				return
			}
		case <-seat.done:
			// Evicted by a newer stream under SeatOverflowEvictOldest;
			// end this stream so its seat is released.
			return
		case <-ctx.Done():
			return
		case <-rn.done:
			// The run settled; drain anything buffered after its final
			// event, then close.
			for {
				select {
				case be, ok := <-ch:
					if !ok || relay(be) {
						return
					}
				default:
					return seen, true
				}
			}
		case <-keepAlive.C:
			if !s.callerHeld(r, owner) {
				return
			}
			if err := st.keepAlive(); err != nil {
				return
			}
		}
	}
}

// callerHeld reports whether a fresh request with r's credentials would
// still resolve to owner, in the same tenant. A stream resolves its caller once, at connect,
// and then outlives that answer: re-establish the principal
// (handler.RecheckPrincipal) and re-run Owner on the refreshed context, so a
// revoked session or a deleted user ends the stream at the next event or
// keep-alive instead of when the client leaves.
//
// A panicking Owner counts as a refusal: the stream ends, the process
// does not.
func (s *Server) callerHeld(r *http.Request, owner string) (held bool) {
	defer func() {
		if recover() != nil {
			held = false
		}
	}()
	fresh, ok := handler.RecheckPrincipal(r.Context())
	if !ok {
		return false
	}
	fr := r.WithContext(fresh)
	got, ok := s.owner(fr)
	if !ok || got != owner {
		return false
	}
	// A member removed from the stream's tenant, or moved to another,
	// stops receiving that tenant's task (tenant.ResolveMiddleware
	// re-resolves it on the recheck).
	if s.tenant == nil {
		return true
	}
	at, ok := r.Context().Value(connectTenantKey{}).(string)
	return ok && s.tenant(fr) == at
}

// connectTenantKey carries the tenant Config.Tenant resolved when the
// request arrived.
type connectTenantKey struct{}

// ---- GetTask / ListTasks / CancelTask / SubscribeToTask ---------------

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string, sc taskScope) {
	var p GetTaskRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if p.ID == "" {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "id is required"))
		return
	}
	// The request's context, not Background: a client that gave up stops
	// the read, and a Store that scopes by the request (its tenant) sees it.
	rec, err := s.getTask(r.Context(), owner, sc, p.ID)
	if errors.Is(err, ErrNotFound) {
		s.writeResult(w, req.ID, nil, ErrTaskNotFound(p.ID))
		return
	}
	if err != nil {
		s.internalErr(w, req.ID, "get task", err)
		return
	}
	task := cloneTask(rec.Task)
	applyHistoryLength(&task, p.HistoryLength)
	s.writeResult(w, req.ID, task, nil)
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string, sc taskScope) {
	var p ListTasksRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if p.Status != "" && !p.Status.Valid() {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "unknown status %q", p.Status))
		return
	}
	offset := 0
	if p.PageToken != "" {
		n, err := decodePageToken(p.PageToken)
		if err != nil {
			s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "invalid pageToken"))
			return
		}
		offset = n
	}
	size := s.clampPageSize(p.PageSize)
	var after time.Time
	if p.StatusTimestampAfter != nil {
		after = p.StatusTimestampAfter.Time
	}
	q := ListQuery{ContextID: p.ContextID, Status: p.Status, After: after,
		Scoped: sc.scoped, Scope: sc.key, Tenanted: sc.tenanted, Tenant: sc.tenant,
		Limit: size, Offset: offset}
	var recs []*TaskRecord
	var total, advance int
	var err error
	_, scopeFilters := s.store.(ScopeFilter)
	_, tenantFilters := s.store.(TenantFilter)
	if (sc.scoped && !scopeFilters) || (sc.tenanted && !tenantFilters) {
		recs, total, err = s.walkScopedTasks(r.Context(), owner, q, sc)
		advance = len(recs)
	} else {
		recs, total, err = s.store.ListTasks(r.Context(), owner, q)
		advance = len(recs)
		// A store that claims ScopeFilter or TenantFilter and still
		// returns another tier's or tenant's task must not hand it over.
		recs = slices.DeleteFunc(recs, func(rec *TaskRecord) bool { return !sc.sees(rec) })
	}
	if err != nil {
		s.internalErr(w, req.ID, "list tasks", err)
		return
	}
	includeArtifacts := p.IncludeArtifacts != nil && *p.IncludeArtifacts
	tasks := make([]Task, 0, len(recs))
	for _, rec := range recs {
		task := cloneTask(rec.Task)
		if !includeArtifacts {
			task.Artifacts = nil
		}
		applyHistoryLength(&task, p.HistoryLength)
		tasks = append(tasks, task)
	}
	resp := ListTasksResponse{Tasks: tasks, PageSize: size, TotalSize: total}
	if offset+advance < total {
		resp.NextPageToken = encodePageToken(offset + advance)
	}
	s.writeResult(w, req.ID, resp, nil)
}

// walkScopedTasks lists a scoped caller through a Store that does not
// declare ScopeFilter, and a caller with a tenant through one that does
// not declare TenantFilter. Such a store's page and total may count other
// tiers' tasks, so a narrow token would read how many tasks a stronger
// credential ran, and a page of dropped rows would hand back its own
// offset. The walk reads every task of the owner that matches q's other
// filters and counts the caller's alone: q.Offset and q.Limit index the
// caller's tasks, and total is how many there are.
func (s *Server) walkScopedTasks(ctx context.Context, owner string, q ListQuery, sc taskScope) ([]*TaskRecord, int, error) {
	offset, size := q.Offset, q.Limit
	q.Limit = max(size, walkScopedBatch)
	var page []*TaskRecord
	seen, read := 0, 0
	for {
		q.Offset = read
		recs, total, err := s.store.ListTasks(ctx, owner, q)
		if err != nil {
			return nil, 0, err
		}
		for _, rec := range recs {
			if !sc.sees(rec) {
				continue
			}
			if seen >= offset && len(page) < size {
				page = append(page, rec)
			}
			seen++
		}
		read += len(recs)
		if len(recs) == 0 || read >= total {
			return page, seen, nil
		}
	}
}

// walkScopedBatch is the store page walkScopedTasks reads per call.
const walkScopedBatch = 200

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string, sc taskScope) {
	var p CancelTaskRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if p.ID == "" {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "id is required"))
		return
	}
	// The local run is halted, then the CANCELED write lands, then the
	// run is woken. Waking it first let the handler's next write land on
	// the unchanged task, so a handler that completed on wake won the
	// race: the client got -32002 and the task stayed COMPLETED. Halting
	// refuses the run's writes before they reach the store, so at most
	// the one write already in flight can beat ours. A run that settles
	// while halted finalizes as CANCELED; a re-read that finds that is
	// the cancellation we asked for, not -32002.
	//
	// A halted run's teardown waits for this call, so every store call
	// below is bounded by storeOpTimeout: a wedged store fails the
	// cancel instead of pinning the run.
	const attempts = 8
	var (
		halted     *run // the local run this call found or left halted
		haltsOwner bool // this call set the flag, so it closes cancelDone
	)
	defer func() {
		if halted != nil {
			if haltsOwner {
				close(halted.cancelDone)
			}
			s.wakeRun(halted)
		}
	}()
	// The request's values (its tenant, its trace) without its
	// cancellation: the run is halted before the write, so a cancel the
	// client abandons halfway must still land.
	base := context.WithoutCancel(r.Context())
	for range attempts {
		ctx, cancel := context.WithTimeout(base, storeOpTimeout)
		rec, err := s.getTask(ctx, owner, sc, p.ID)
		cancel()
		if errors.Is(err, ErrNotFound) {
			s.writeResult(w, req.ID, nil, ErrTaskNotFound(p.ID))
			return
		}
		if err != nil {
			s.internalErr(w, req.ID, "get task for cancel", err)
			return
		}
		state := rec.Task.Status.State
		if state == TaskStateCanceled && halted != nil {
			// Another replica's cancel won the write after this one
			// halted the run, so neither the watch nor finalize will
			// announce it: tell the local subscribers here. A stream ends
			// at its first terminal event, so a second one is never read.
			halted.bus.publish(busEvent{ev: StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{
				TaskID:    rec.Task.ID,
				ContextID: rec.Task.ContextID,
				Status:    rec.Task.Status,
			}}, version: rec.Version})
			task := cloneTask(rec.Task)
			s.writeResult(w, req.ID, SendMessageResponse{Task: &task}, nil)
			return
		}
		if state.Terminal() {
			s.writeResult(w, req.ID, nil, ErrTaskNotCancelable(p.ID, state))
			return
		}
		// Owner verified by the scoped read above: halt the local run
		// so its writes stop before ours goes in. The deferred wake
		// cancels its context on every exit from here.
		if rn := s.runFor(p.ID); rn != nil && halted == nil {
			haltsOwner = rn.halted.CompareAndSwap(false, true)
			halted = rn
		}
		cand := rec.Clone()
		cand.Task.Status = TaskStatus{State: TaskStateCanceled, Timestamp: s.stamp()}
		ctx, cancel = context.WithTimeout(base, storeOpTimeout)
		err = s.store.UpdateTask(ctx, cand)
		cancel()
		if err != nil {
			if errors.Is(err, ErrConflict) {
				continue
			}
			s.internalErr(w, req.ID, "cancel task", err)
			return
		}
		ev := StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{
			TaskID:    cand.Task.ID,
			ContextID: cand.Task.ContextID,
			Status:    cand.Task.Status,
		}}
		s.publish(owner, cand, ev)
		task := cloneTask(cand.Task)
		s.writeResult(w, req.ID, SendMessageResponse{Task: &task}, nil)
		// The cancel write left the task terminal: same retention hook
		// as the run's own finalize.
		s.trimTerminalTasks(owner, TierOf(cand))
		return
	}
	s.writeResult(w, req.ID, nil, Errorf(CodeInternalError, "internal error"))
}

func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string) {
	var p SubscribeToTaskRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if p.ID == "" {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "id is required"))
		return
	}
	// Attach to the in-process run BEFORE reading the snapshot. The bus
	// channel is buffered, so an event the handler publishes between this
	// subscribe and the snapshot send is queued and forwarded after the
	// snapshot. Subscribing after the snapshot (the previous order) lost
	// every event published in that gap: a client that released the
	// handler on seeing the snapshot saw the stream end with no artifact
	// or completion at all on a slow runner. An event queued in that gap
	// may already be in the snapshot, and forwarding it again is not
	// harmless: an artifact append is a delta the client applies on top
	// of the snapshot, so a repeat doubles its parts. Every bus event
	// carries the task version its write produced, and forwardEvents
	// skips those at or below the snapshot's version.
	//
	// The run can also change under the read: the one subscribed to
	// settles and a resume starts the next before the snapshot is read.
	// Forwarding the settled run's bus then ends the stream on a running
	// task, so the subscribe and read repeat until the run holds still,
	// and past a few tries the store poll serves the task instead.
	var (
		rn  *run
		ch  chan busEvent
		rec *TaskRecord
		err error
		sc  = s.callerScope(r)
	)
	for try := 0; ; try++ {
		if rn = s.runFor(p.ID); rn != nil {
			ch = rn.bus.subscribe()
		}
		// The request context bounds the read: a hung store must not pin
		// this handler after the client has gone.
		rec, err = s.getTask(r.Context(), owner, sc, p.ID)
		if err != nil || s.runFor(p.ID) == rn {
			break
		}
		if rn != nil {
			rn.bus.unsubscribe(ch)
			rn, ch = nil, nil
		}
		if try == 2 {
			break
		}
	}
	// A local run that paused the task, under a stored version it did
	// not write, is still unwinding its handler after another replica
	// resumed the task. Its bus hears nothing of that run and its
	// watcher only of terminal states, so following it hid the resumed
	// run's own pause until the local handler returned. Follow the store
	// instead.
	if rn != nil && err == nil {
		if at := rn.pausedAt.Load(); at != 0 && rec.Version > at {
			rn.bus.unsubscribe(ch)
			rn, ch = nil, nil
		}
	}
	if rn != nil {
		defer rn.bus.unsubscribe(ch)
	}
	if errors.Is(err, ErrNotFound) {
		s.writeResult(w, req.ID, nil, ErrTaskNotFound(p.ID))
		return
	}
	if err != nil {
		s.internalErr(w, req.ID, "get task for subscribe", err)
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		s.writeResult(w, req.ID, nil, ErrUnsupportedOperation("streaming requires a flushing ResponseWriter"))
		return
	}
	// Seat admission: the per-owner cap on concurrent streams. Refusal
	// happens before any byte of the stream is written; the seat is
	// freed when this handler returns (i.e. when forwardEvents or
	// pollEvents returns).
	seat, ok := s.seats.admitSeat(owner, s.maxStreamSeats, s.streamSeatOverflow)
	if !ok {
		s.writeTransport(w, http.StatusTooManyRequests,
			Errorf(CodeInvalidRequest, "too many concurrent streams for this owner"))
		return
	}
	defer s.seats.releaseSeat(seat)
	st := s.newSSEStream(w, req.ID)
	// The a2a-go SDK sends the current task as one event and closes
	// when the task is already settled; do the same.
	snap := cloneTask(rec.Task)
	if err := st.send(StreamResponse{Task: &snap}); err != nil {
		return
	}
	state := rec.Task.Status.State
	if state.Terminal() || state.Interrupted() {
		return
	}
	if rn != nil {
		seen, unsettled := s.forwardEvents(st, rn, ch, r, owner, seat, rec.Version)
		if !unsettled {
			return
		}
		// The local run ended without settling the task, so another
		// replica carries it on: follow the store from what was sent.
		from := *rec
		from.Version = seen
		rec = &from
	}
	// Multi-replica fallback: the task is non-terminal and has no run
	// in this process, so it is running on (or stalled from) another
	// replica sharing the store. Poll the store and emit a snapshot
	// whenever the version changes, until the task settles. Events
	// between polls are coalesced into the next snapshot — a replica
	// boundary is exactly where per-event fidelity would require a
	// shared bus, which a SQL store cannot give.
	s.pollEvents(st, r, owner, rec, seat)
}

// pollEvents serves the polling fallback. Its store reads run under the
// request context, cut further when the seat is evicted, so a hung
// store read ends with the stream instead of pinning the seat after
// the client has gone.
func (s *Server) pollEvents(st *sseStream, r *http.Request, owner string, rec *TaskRecord, seat *streamSeat) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		select {
		case <-seat.done:
			cancel()
		case <-ctx.Done():
		}
	}()
	last := rec.Version
	poll := time.NewTicker(s.pollEvery)
	defer poll.Stop()
	keepAlive := time.NewTicker(s.keepAlive)
	defer keepAlive.Stop()
	for {
		select {
		case <-seat.done:
			// Evicted by a newer stream under SeatOverflowEvictOldest;
			// end this stream so its seat is released.
			return
		case <-ctx.Done():
			return
		case <-keepAlive.C:
			if !s.callerHeld(r, owner) {
				return
			}
			if err := st.keepAlive(); err != nil {
				return
			}
		case <-poll.C:
			cur, err := s.pollGetTask(ctx, owner, rec.Task.ID)
			if ctx.Err() != nil {
				return // the client left or the seat was evicted
			}
			if errors.Is(err, ErrNotFound) {
				return
			}
			if errors.Is(err, errStorePanicked) {
				// A panicking store cannot serve this stream again;
				// end it here rather than re-panicking once per poll
				// interval. The response is already streaming, so the
				// attributed log is the refusal.
				s.log.Error("a2a: poll read panicked", "taskId", rec.Task.ID, "err", err)
				return
			}
			if err != nil {
				s.log.Error("a2a: poll read", "taskId", rec.Task.ID, "err", err)
				continue
			}
			if cur.Version == last {
				continue
			}
			last = cur.Version
			if !s.callerHeld(r, owner) {
				return
			}
			snap := cloneTask(cur.Task)
			if err := st.send(StreamResponse{Task: &snap}); err != nil {
				return
			}
			if state := cur.Task.Status.State; state.Terminal() || state.Interrupted() {
				return
			}
		}
	}
}

// errStorePanicked is what pollGetTask returns in place of a panic.
// The recovered value is not surfaced: it can be anything, including
// driver detail the counterparty must not see.
var errStorePanicked = errors.New("a2a: store GetTask panicked")

// pollGetTask reads one task snapshot for the polling fallback stream
// under a recover guard. The Store is host-supplied code running on a
// long-lived goroutine with no per-request net, so a panicking
// implementation must become an error this stream ends on — never an
// unrecovered panic that takes down the process.
// taskScope is the credential tier a caller holds: unscoped, or the
// canonical key of the scopes its credential is narrowed to, plus the
// tenant it acts in when the server has a Tenant hook.
type taskScope struct {
	scoped   bool
	key      string
	tenanted bool
	tenant   string
}

// sees reports whether a caller of this tier may reach rec: an unscoped
// caller reaches every task of its owner, a scoped one only the tasks
// made under the same scopes; with a Tenant hook, only the tasks made in
// the caller's current tenant.
func (sc taskScope) sees(rec *TaskRecord) bool {
	if sc.tenanted && rec.Tenant != sc.tenant {
		return false
	}
	return !sc.scoped || rec.Scope == sc.key
}

// ScopeKey is the canonical TaskRecord.Scope of a scoped credential: its
// scopes sorted and deduplicated, as a JSON array. It is never "", the
// unscoped key, even for an empty scope set.
func ScopeKey(scopes []string) string {
	sorted := slices.Compact(slices.Sorted(slices.Values(scopes)))
	if sorted == nil {
		sorted = []string{}
	}
	b, err := json.Marshal(sorted)
	if err != nil {
		// A []string always marshals; refuse every task rather than none.
		return "[\"\"]"
	}
	return string(b)
}

func (s *Server) callerScope(r *http.Request) taskScope {
	var sc taskScope
	if s.tenant != nil {
		sc.tenanted, sc.tenant = true, s.tenant(r)
	}
	if s.scopes == nil {
		return sc
	}
	scopes, scoped := s.scopes(r)
	if !scoped {
		return sc
	}
	sc.scoped, sc.key = true, ScopeKey(scopes)
	return sc
}

// getTask is Store.GetTask for a caller of tier sc: a task made under
// scopes the caller does not hold reads as absent.
func (s *Server) getTask(ctx context.Context, owner string, sc taskScope, id string) (*TaskRecord, error) {
	rec, err := s.store.GetTask(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	if !sc.sees(rec) {
		return nil, ErrNotFound
	}
	return rec, nil
}

func (s *Server) pollGetTask(ctx context.Context, owner, taskID string) (rec *TaskRecord, err error) {
	defer func() {
		if recover() != nil {
			rec = nil
			err = errStorePanicked
		}
	}()
	return s.store.GetTask(ctx, owner, taskID)
}

// ---- push config methods ----------------------------------------------

func (s *Server) handleCreatePushConfig(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string, sc taskScope) {
	var cfg PushNotificationConfig
	if aerr := decodeParams(req.Params, &cfg); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if cfg.TaskID == "" {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "taskId is required"))
		return
	}
	if cfg.URL == "" {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "url is required"))
		return
	}
	if s.push.disable {
		s.writeResult(w, req.ID, nil, Errorf(CodePushNotificationNotSupported, "push notifications are disabled"))
		return
	}
	if _, err := s.getTask(r.Context(), owner, sc, cfg.TaskID); err != nil {
		if errors.Is(err, ErrNotFound) {
			s.writeResult(w, req.ID, nil, ErrTaskNotFound(cfg.TaskID))
			return
		}
		s.internalErr(w, req.ID, "get task for push config", err)
		return
	}
	if err := validatePushURL(cfg.URL, hasCredentials(cfg), s.push.allowPrivate); err != nil {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "%v", err))
		return
	}
	// The request's values without its cancellation: the create and the
	// trim that bounds the task's configs after it run as one step.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), storeOpTimeout)
	defer cancel()
	if aerr := s.storePushConfig(ctx, owner, &cfg); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	s.writeResult(w, req.ID, cfg, nil)
}

// requireOwnedTask answers ErrTaskNotFound when the task is absent,
// another owner's, made under scopes the caller does not hold, or made in
// another tenant; the push-config handlers all need exactly that. A
// push config belongs to its task, so the task's check is the config's.
func (s *Server) requireOwnedTask(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string, sc taskScope, taskID string) bool {
	_, err := s.getTask(r.Context(), owner, sc, taskID)
	if err == nil {
		return true
	}
	if errors.Is(err, ErrNotFound) {
		s.writeResult(w, req.ID, nil, ErrTaskNotFound(taskID))
		return false
	}
	s.internalErr(w, req.ID, "get task", err)
	return false
}

func (s *Server) handleGetPushConfig(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string, sc taskScope) {
	var p GetTaskPushNotificationConfigRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if !s.requireOwnedTask(w, r, req, owner, sc, p.TaskID) {
		return
	}
	rec, err := s.store.GetPushConfig(r.Context(), owner, p.TaskID, p.ID)
	if errors.Is(err, ErrNotFound) {
		// The spec has no dedicated code for a missing config id; the
		// task was verified above, so the params are what is wrong.
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "push config not found"))
		return
	}
	if err != nil {
		s.internalErr(w, req.ID, "get push config", err)
		return
	}
	s.writeResult(w, req.ID, rec.Config, nil)
}

func (s *Server) handleListPushConfigs(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string, sc taskScope) {
	var p ListTaskPushNotificationConfigsRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if !s.requireOwnedTask(w, r, req, owner, sc, p.TaskID) {
		return
	}
	offset := 0
	if p.PageToken != "" {
		n, err := decodePageToken(p.PageToken)
		if err != nil {
			s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "invalid pageToken"))
			return
		}
		offset = n
	}
	recs, err := s.store.ListPushConfigs(r.Context(), owner, p.TaskID)
	if err != nil {
		s.internalErr(w, req.ID, "list push configs", err)
		return
	}
	total := len(recs)
	if offset > total {
		offset = total
	}
	page := recs[offset:]
	size := s.clampPageSize(p.PageSize)
	if size < len(page) {
		page = page[:size]
	}
	configs := make([]PushNotificationConfig, 0, len(page))
	for _, rec := range page {
		configs = append(configs, rec.Config)
	}
	resp := ListTaskPushNotificationConfigsResponse{Configs: configs}
	if offset+len(page) < total {
		resp.NextPageToken = encodePageToken(offset + len(page))
	}
	s.writeResult(w, req.ID, resp, nil)
}

func (s *Server) handleDeletePushConfig(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string, sc taskScope) {
	var p DeleteTaskPushNotificationConfigRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if !s.requireOwnedTask(w, r, req, owner, sc, p.TaskID) {
		return
	}
	err := s.store.DeletePushConfig(r.Context(), owner, p.TaskID, p.ID)
	if errors.Is(err, ErrNotFound) {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "push config not found"))
		return
	}
	if err != nil {
		s.internalErr(w, req.ID, "delete push config", err)
		return
	}
	s.writeResult(w, req.ID, struct{}{}, nil)
}

// ---- GetExtendedAgentCard ---------------------------------------------

func (s *Server) handleExtendedCard(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string) {
	if s.extended == nil {
		s.writeResult(w, req.ID, nil, Errorf(CodeExtendedAgentCardNotConfigured, "extended agent card not configured"))
		return
	}
	var p GetExtendedAgentCardRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	card, err := s.extended(r.Context(), owner)
	if err != nil {
		var ae *Error
		if errors.As(err, &ae) {
			s.writeResult(w, req.ID, nil, ae)
			return
		}
		s.internalErr(w, req.ID, "extended card", err)
		return
	}
	s.writeResult(w, req.ID, card, nil)
}

// ---- DefaultRouter -----------------------------------------------------

// DefaultRouter picks the skill for a message:
//
//  1. msg.Metadata["skill"], when a non-empty string;
//  2. else the first data part whose Data is a JSON object with a
//     string "skill" key;
//  3. else the only registered skill, when exactly one is;
//  4. else an error.
//
// A skill id that is not registered is an error at step 1 and 2 alike,
// rather than a guess. No error lists the registered ids: they include
// skills a gate withholds from this caller, and the agent card already
// lists the ones it publishes.
func DefaultRouter(_ context.Context, msg *Message, skills []Skill) (string, error) {
	if msg != nil && msg.Metadata != nil {
		if v, ok := msg.Metadata["skill"]; ok {
			if name, isStr := v.(string); isStr && name != "" {
				if skillKnown(skills, name) {
					return name, nil
				}
				return "", fmt.Errorf("no skill named %q", name)
			}
		}
	}
	if msg != nil {
		for i := range msg.Parts {
			data := msg.Parts[i].Data
			if data == nil {
				continue
			}
			obj, ok := (*data).(map[string]any)
			if !ok {
				continue
			}
			if name, ok := obj["skill"].(string); ok && name != "" {
				if skillKnown(skills, name) {
					return name, nil
				}
				return "", fmt.Errorf("no skill named %q", name)
			}
		}
	}
	if len(skills) == 1 {
		return skills[0].ID, nil
	}
	return "", errors.New("the message names no skill")
}

func skillKnown(skills []Skill, id string) bool {
	for _, sk := range skills {
		if sk.ID == id {
			return true
		}
	}
	return false
}
