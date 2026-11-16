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
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// A2AConfig configures the Agent2Agent v1.0 task exchange mounted by WithA2A.
type A2AConfig struct {
	// Path is the JSON-RPC endpoint, default "/a2a"; NewApp panics on one
	// that does not start with "/".
	Path string
	// Skills are hand-written skills; Start fails when there is no skill at all.
	Skills []a2a.Skill
	// DisableEntitySkills turns off the skill derived for each entity with MCP tools.
	DisableEntitySkills bool
	// Store persists tasks and push configs; nil means an SQL store on the
	// app's DB, or a per-process memory store without one.
	Store a2a.Store
	// ExtendedCard serves GetExtendedAgentCard; nil answers
	// CodeExtendedAgentCardNotConfigured.
	ExtendedCard func(ctx context.Context, owner string) (map[string]any, error)
	// AllowPrivatePush admits push URLs on internal hosts. Tests and internal
	// deployments only.
	AllowPrivatePush bool
	// AllowedOrigins admits browser Origins other than the request's own host.
	AllowedOrigins []string
	// TaskTimeout caps one skill-handler run; default 5 minutes.
	TaskTimeout time.Duration
}

// WithA2A mounts the Agent2Agent v1.0 task exchange at A2AConfig.Path on the
// app router. The server is built during Start.
func WithA2A(cfg A2AConfig) AppOption {
	return func(a *App) {
		if cfg.Path != "" && !strings.HasPrefix(cfg.Path, "/") {
			panic(fmt.Sprintf("framework: WithA2A path %q must start with /", cfg.Path))
		}
		a.a2aCfg = &cfg
	}
}

// A2A returns the mounted exchange server, or nil without WithA2A or before
// Start mounts it.
func (a *App) A2A() *a2a.Server { return a.a2a }

// A2ASkills returns the exchange's skills for the agent card, sorted by id:
// the hand-written skills and the entity skills an anonymous caller may
// list. It returns nil without WithA2A.
func (a *App) A2ASkills() []uihost.AgentSkill {
	if a.a2aCfg == nil {
		return nil
	}
	anonymous, err := a.MCP.ListToolsFor(context.Background())
	if err != nil {
		anonymous = nil
	}
	skills := a.a2aSkillsFrom(anonymous)
	out := make([]uihost.AgentSkill, 0, len(skills))
	for _, sk := range skills {
		out = append(out, uihost.AgentSkill{
			ID:          sk.ID,
			Name:        sk.Name,
			Description: sk.Description,
			Tags:        sk.Tags,
			Examples:    sk.Examples,
			InputModes:  sk.InputModes,
			OutputModes: sk.OutputModes,
		})
	}
	slices.SortFunc(out, func(x, y uihost.AgentSkill) int { return strings.Compare(x.ID, y.ID) })
	return out
}

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
		Skills:         a.a2aSkills(),
		Store:          store,
		Owner:          a2aOwnerPrincipal,
		Scopes:         a2aCallerScopes,
		Tenant:         requestTenantID,
		ExtendedCard:   cfg.ExtendedCard,
		Push:           a2a.PushOptions{AllowPrivate: cfg.AllowPrivatePush},
		Logger:         a.Logger(),
		TaskTimeout:    cfg.TaskTimeout,
		AllowedOrigins: cfg.AllowedOrigins,
	})
	if err != nil {
		return fmt.Errorf("framework: WithA2A: %w", err)
	}
	a.a2a = srv
	a.a2aPath = path
	a.router.Post(path, srv)
	return nil
}

func a2aOwnerPrincipal(r *http.Request) (string, bool) {
	id, ok := owner.Get(r.Context())
	if !ok || id == nil {
		return "", false
	}
	return fmt.Sprintf("%v", id), true
}

func a2aCallerScopes(r *http.Request) ([]string, bool) {
	held, ok := access.HeldScopes(r.Context())
	if !ok {
		return nil, false
	}
	out := make([]string, len(held))
	for i, p := range held {
		out[i] = string(p)
	}
	return out, true
}

var entityActions = []string{"list", "get", "create", "update", "delete"}

type entitySkillSet struct {
	ns     string
	entity string
	tools  map[string]mcp.Tool
}

func (a *App) entitySkillSets() []entitySkillSet {
	return a.entitySkillSetsFrom(a.MCP.ListTools())
}

func (a *App) entitySkillSetsFrom(tools []mcp.Tool) []entitySkillSet {
	entities := a.Registry.All()
	byKey := map[string]*entitySkillSet{}
	for _, tool := range tools {
		var key, ns, entity, action string
		if parts := strings.Split(tool.Name, "."); len(parts) == 3 {
			if !slices.Contains(entityActions, parts[2]) {
				continue
			}
			ns, entity, action = parts[0], parts[1], parts[2]
			key = ns + "." + entity
		} else if strings.Contains(tool.Name, ".") {
			continue
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

func (a *App) a2aSkills() []a2a.Skill {
	return a.a2aSkillsFrom(a.MCP.ListTools())
}

func (a *App) a2aSkillsFrom(tools []mcp.Tool) []a2a.Skill {
	skills := slices.Clone(a.a2aCfg.Skills)
	if !a.a2aCfg.DisableEntitySkills {
		for _, set := range a.entitySkillSetsFrom(tools) {
			skills = append(skills, a.newEntitySkill(set))
		}
	}
	return skills
}

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
		result, err := a.MCP.CallToolFor(mcp.WithRequest(ctx, t.Request()), tool.Name, args)
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
