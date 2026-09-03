package framework

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/a2a"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// a2a.go wires core/a2a's Agent2Agent task exchange into the App: one
// option mounts it at a path behind the app router (session/bearer auth,
// owner context, recovery, request logging all apply), derives one skill
// per entity that has MCP tools, and exposes the skill list for the
// uihost agent card. The exchange itself — wire methods, task lifecycle,
// store, streaming, push — is core/a2a's; see framework/docs/content/a2a.md.

// A2AConfig configures the Agent2Agent v1.0 task exchange mounted by
// WithA2A.
type A2AConfig struct {
	// Path is the JSON-RPC endpoint path. Default "/a2a". Must start
	// with "/" (NewApp panics otherwise, the same fail-fast as every
	// other invalid option value). A path the app already mounts is a
	// route-conflict panic at Start, the same contract WithMCP documents
	// for /mcp: pick one owner per path.
	Path string
	// Skills are hand-written skills, invoked by name (metadata.skill or
	// a data part carrying "skill"). Combined with the derived entity
	// skills; at least one skill overall or Start fails.
	Skills []a2a.Skill
	// DisableEntitySkills turns OFF the derived per-entity skills. The
	// zero value means ON: every registered entity with MCP tools gets
	// one skill. A Disable* boolean rather than a *bool, matching how
	// the repo's other default-on flags read (Config.NoLLMMD,
	// DisableSignalHandling).
	DisableEntitySkills bool
	// Store persists tasks and push configs. Nil → a2a.NewSQLStore on
	// the app's DB (tables created IF NOT EXISTS), or a2a.NewMemoryStore
	// when the app has no DB. The SQL store is shared across replicas;
	// the memory store is per process.
	Store a2a.Store
	// ExtendedCard, when set, serves GetExtendedAgentCard. Nil → the
	// method answers CodeExtendedAgentCardNotConfigured.
	ExtendedCard func(ctx context.Context, owner string) (map[string]any, error)
	// AllowPrivatePush permits push-notification URLs targeting internal
	// hosts (loopback, RFC1918, CGNAT, link-local). Tests and internal
	// deployments only: a caller-registered URL the server POSTs to is
	// an SSRF vector otherwise.
	AllowPrivatePush bool
	// TaskTimeout is the ceiling on one skill-handler run. Default
	// 5 minutes.
	TaskTimeout time.Duration
}

// WithA2A mounts the Agent2Agent v1.0 task exchange (core/a2a) at
// A2AConfig.Path, default "/a2a". The route goes through the app router
// so the app's middleware chain — session/bearer auth, owner context,
// recovery, request logging — applies to every exchange request, the
// same argument RegisterEntityMCPTools makes for its router parameter.
//
// The server is built during Start, after entities, plugins, and
// batteries have registered their MCP tools (entity skills derive from
// the tool registry). Combine with uihost.WithAgentReady:
// AgentCardConfig{A2AEndpoint: "/a2a", Skills: app.A2ASkills()}.
func WithA2A(cfg A2AConfig) AppOption {
	return func(a *App) {
		if cfg.Path != "" && !strings.HasPrefix(cfg.Path, "/") {
			panic(fmt.Sprintf("framework: WithA2A path %q must start with /", cfg.Path))
		}
		a.a2aCfg = &cfg
	}
}

// A2A returns the mounted Agent2Agent exchange server, or nil when
// WithA2A was never called or Start has not mounted it yet (the server
// is built during Start, once the MCP tool registry is complete).
func (a *App) A2A() *a2a.Server { return a.a2a }

// A2ASkills returns the exchange's skills as uihost.AgentSkill entries
// for the agent card: every hand-written A2AConfig skill plus one
// entity.<name> skill per registered entity with MCP tools, sorted by
// id. Call it after your entity declarations (entity skills are read
// from the MCP tool registry, which those declarations fill). Nil when
// WithA2A was never called. Feed the result straight into
// uihost.AgentCardConfig.Skills.
func (a *App) A2ASkills() []uihost.AgentSkill {
	if a.a2aCfg == nil {
		return nil
	}
	skills := a.a2aSkills()
	out := make([]uihost.AgentSkill, 0, len(skills))
	for _, sk := range skills {
		out = append(out, uihost.AgentSkill{
			ID:          sk.ID,
			Name:        sk.Name,
			Description: sk.Description,
			Tags:        sk.Tags,
			Examples:    sk.Examples,
		})
	}
	slices.SortFunc(out, func(x, y uihost.AgentSkill) int { return strings.Compare(x.ID, y.ID) })
	return out
}

// mountA2A builds and mounts the exchange. Called from Start in the
// same phase that mounts /mcp: entity MCP tools are registered by then
// (app.Entity registers them immediately), so the derived skills see
// the complete tool registry.
func (a *App) mountA2A() error {
	cfg := *a.a2aCfg
	path := cfg.Path
	if path == "" {
		path = "/a2a"
	}
	store := cfg.Store
	if store == nil {
		if a.DB != nil {
			s, err := a2a.NewSQLStore(a.DB)
			if err != nil {
				return fmt.Errorf("framework: WithA2A store: %w", err)
			}
			store = s
		} else {
			store = a2a.NewMemoryStore()
		}
	}
	srv, err := a2a.NewServer(a2a.Config{
		Skills:       a.a2aSkills(),
		Store:        store,
		Owner:        a2aOwnerPrincipal,
		ExtendedCard: cfg.ExtendedCard,
		Push:         a2a.PushOptions{AllowPrivate: cfg.AllowPrivatePush},
		Logger:       a.Logger(),
		TaskTimeout:  cfg.TaskTimeout,
	})
	if err != nil {
		return fmt.Errorf("framework: WithA2A: %w", err)
	}
	a.a2a = srv
	a.a2aPath = path
	// POST only; the server answers GET with 405. Routed through
	// a.router so the app's middleware chain applies to the exchange.
	a.router.Post(path, srv)
	return nil
}

// a2aOwnerPrincipal resolves the A2A caller the way ownerPrincipal does
// (the framework's owner extractor, formatted with %v) but WITHOUT the
// credential-fingerprint fallback. Idempotency keys anonymous traffic by
// its credential because an anonymous response carries no per-user
// data. The task exchange is per-user data: every task row is owned,
// and hard rule 6 (no per-user data without owner scoping) means a
// caller that resolved to no owner must be refused (401), never
// bucketed into a shared "anon" namespace where one caller could read
// and resume another's tasks.
func a2aOwnerPrincipal(r *http.Request) (string, bool) {
	id, ok := owner.Get(r.Context())
	if !ok || id == nil {
		return "", false
	}
	return fmt.Sprintf("%v", id), true
}

// entityActions are the CRUD tool actions crud.RegisterEntityMCPTools
// generates per entity, in canonical display order.
var entityActions = []string{"list", "get", "create", "update", "delete"}

// entitySkillSet groups one entity's MCP tools: the entity name, its
// namespace ("" for the flat tool names), and action → tool.
type entitySkillSet struct {
	ns     string
	entity string
	tools  map[string]mcp.Tool
}

// entitySkillSets walks the MCP tool registry and groups the entity CRUD
// tools by entity: "<entity>_<action>" flat names and
// "<ns>.<entity>.<action>" namespaced ones. The registry, not a fresh
// schema walk, is the source: it is the same walk OpenAPI, the CLI, the
// SDK, and tools/list share, so the skill descriptions cannot drift
// from the tools they invoke. A prefix match alone cannot tell an
// entity tool from a custom tool whose name happens to end in "_list",
// so the entity half must also exist in the registry.
func (a *App) entitySkillSets() []entitySkillSet {
	entities := a.Registry.All()
	byKey := map[string]*entitySkillSet{}
	for _, tool := range a.MCP.ListTools() {
		var key, ns, entity, action string
		if parts := strings.Split(tool.Name, "."); len(parts) == 3 {
			if !slices.Contains(entityActions, parts[2]) {
				continue
			}
			ns, entity, action = parts[0], parts[1], parts[2]
			key = ns + "." + entity
		} else if strings.Contains(tool.Name, ".") {
			continue // not an entity tool shape
		} else {
			var found bool
			for _, act := range entityActions {
				if strings.HasSuffix(tool.Name, "_"+act) {
					entity = strings.TrimSuffix(tool.Name, "_"+act)
					action, key, found = act, entity, true
					break
				}
			}
			if !found {
				continue
			}
		}
		if _, ok := entities[entity]; !ok {
			continue
		}
		set, ok := byKey[key]
		if !ok {
			set = &entitySkillSet{ns: ns, entity: entity, tools: map[string]mcp.Tool{}}
			byKey[key] = set
		}
		set.tools[action] = tool
	}
	keys := slices.Sorted(maps.Keys(byKey))
	out := make([]entitySkillSet, 0, len(keys))
	for _, k := range keys {
		out = append(out, *byKey[k])
	}
	return out
}

// a2aSkills is the one skill-list builder: hand-written skills from the
// WithA2A config plus one skill per entity with MCP tools (unless
// disabled). mountA2A passes it to a2a.Config.Skills and A2ASkills
// converts it for the card, so the exchange and the card advertise one
// list from one source.
func (a *App) a2aSkills() []a2a.Skill {
	skills := slices.Clone(a.a2aCfg.Skills)
	if !a.a2aCfg.DisableEntitySkills {
		for _, set := range a.entitySkillSets() {
			skills = append(skills, a.newEntitySkill(set))
		}
	}
	return skills
}

// newEntitySkill builds the skill for one entity's CRUD tools. The
// handler speaks the data-part contract: one data part whose object
// carries "operation" (one of the entity's tool actions) and
// "arguments" (the tool's params object).
func (a *App) newEntitySkill(set entitySkillSet) a2a.Skill {
	id := "entity." + set.entity
	entityKey := set.entity
	if set.ns != "" {
		id = "entity." + set.ns + "." + set.entity
		entityKey = set.ns + "." + set.entity
	}
	example, err := json.Marshal(map[string]any{
		"skill":     id,
		"operation": "list",
		"arguments": map[string]any{},
	})
	if err != nil {
		example = []byte(`{"skill":"` + id + `","operation":"list","arguments":{}}`)
	}
	return a2a.Skill{
		ID:          id,
		Name:        capitalize(set.entity) + " records",
		Description: entitySkillDescription(set),
		Tags:        []string{"entity", entityKey},
		Examples:    []string{string(example)},
		InputModes:  []string{"application/json"},
		OutputModes: []string{"application/json"},
		Handler:     a.entitySkillHandler(set),
	}
}

// entitySkillDescription lists one line per operation with the tool's
// input-schema property keys, so an integrator reading the card knows
// the data-part contract without a tools/list round trip.
func entitySkillDescription(set entitySkillSet) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CRUD on %s records through the app's MCP tools. Send one data part carrying skill, operation, and arguments:", set.entity)
	for _, action := range entityActions {
		tool, ok := set.tools[action]
		if !ok {
			continue
		}
		props, _ := tool.InputSchema["properties"].(map[string]any)
		keys := slices.Sorted(maps.Keys(props))
		if len(keys) == 0 {
			fmt.Fprintf(&b, "\n%s()", action)
		} else {
			fmt.Fprintf(&b, "\n%s(%s)", action, strings.Join(keys, ", "))
		}
	}
	return b.String()
}

// capitalize upper-cases the first ASCII letter. Entity names are ASCII
// identifiers by construction (they are table/column name fragments).
func capitalize(s string) string {
	if s == "" {
		return s
	}
	c := s[0]
	if c >= 'a' && c <= 'z' {
		return string(c-'a'+'A') + s[1:]
	}
	return s
}

// entitySkillHandler is the skill handler for one entity's tools. The
// tool call re-dispatches with the ORIGINAL request on the context
// (mcp.WithRequest): the entity tool copies the caller's credential
// headers onto its in-process request through the app router, so owner
// scoping applies to A2A exactly as to MCP and REST. CallTool also runs
// the server's call gate, so a tool the framework hides from agents (a
// disabled module's) stays hidden here too.
func (a *App) entitySkillHandler(set entitySkillSet) a2a.Handler {
	available := func() string {
		var acts []string
		for _, action := range entityActions {
			if _, ok := set.tools[action]; ok {
				acts = append(acts, action)
			}
		}
		return strings.Join(acts, ", ")
	}
	return func(ctx context.Context, t a2a.TaskContext) error {
		operation, args, ierr := entityInvocation(t.Message())
		if ierr != nil {
			return t.Reject(a2a.TextPart(ierr.Error() + "; operations: " + available()))
		}
		tool, ok := set.tools[operation]
		if !ok {
			return t.Reject(a2a.TextPart(fmt.Sprintf("unknown operation %q; operations: %s", operation, available())))
		}
		if err := t.Working(); err != nil {
			return err
		}
		result, err := a.MCP.CallTool(mcp.WithRequest(ctx, t.Request()), tool.Name, args)
		if err != nil {
			var ae *a2a.Error
			if errors.As(err, &ae) {
				return ae
			}
			return t.Fail(a2a.TextPart(err.Error()))
		}
		if err := t.Artifact(a2a.Artifact{
			Name:  set.entity + "." + operation,
			Parts: []a2a.Part{a2a.DataPart(result)},
		}, false); err != nil {
			return err
		}
		return t.Complete()
	}
}

// entityInvocation reads the data-part contract from the message: the
// first data part whose object carries a string "operation" (a part
// whose object lacks the key is skipped, a present non-string is a
// client error), with an optional "arguments" object (missing → empty
// arguments; a present non-object is a client error). A nil error
// return means ok.
func entityInvocation(msg *a2a.Message) (operation string, args map[string]any, ierr error) {
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
			raw, present := obj["operation"]
			if !present {
				// Absent key: this part carries no invocation, keep
				// scanning (a failed type assertion on a missing key
				// is not absence — that error shape used to reject a
				// valid operation in a LATER part).
				continue
			}
			op, isStr := raw.(string)
			if !isStr {
				return "", nil, fmt.Errorf(`data part "operation" must be a string, got %T`, raw)
			}
			if op == "" {
				continue
			}
			args = map[string]any{}
			if raw, has := obj["arguments"]; has && raw != nil {
				m, ok := raw.(map[string]any)
				if !ok {
					return "", nil, errors.New(`data part "arguments" must be an object`)
				}
				args = m
			}
			return op, args, nil
		}
	}
	return "", nil, errors.New(`message must carry a data part with "operation" and "arguments"`)
}
