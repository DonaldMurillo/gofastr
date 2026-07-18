package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	client "github.com/DonaldMurillo/gofastr/examples/meridian/entities/client"
)

// global carries the per-invocation context and the authenticated API
// client. parseGlobals builds it after the verb registered its own flags.
type global struct {
	ctx    context.Context
	client *client.Client
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(binaryName+" "+name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

// parseGlobals registers the connection flags, parses args, and builds the
// client. Resolution order: flag > env > stored config. The non-nil exit
// code is 2 (usage) when parsing or resolution fails.
func parseGlobals(fs *flag.FlagSet, args []string) (*global, int) {
	urlF := fs.String("url", "", "server URL (default $"+envPrefix+"_URL, then stored config)")
	tokenF := fs.String("token", "", "API token (default $"+envPrefix+"_TOKEN, then stored config)")
	if err := fs.Parse(args); err != nil {
		return nil, 2
	}
	cfg := loadConfig()
	base := firstNonEmpty(*urlF, os.Getenv(envPrefix+"_URL"), cfg.URL)
	if base == "" {
		fmt.Fprintf(os.Stderr, "no server URL: pass --url, set %s_URL, or run `%s login`\n", envPrefix, binaryName)
		return nil, 2
	}
	token := firstNonEmpty(*tokenF, os.Getenv(envPrefix+"_TOKEN"), cfg.Token)
	c := client.NewClient(strings.TrimRight(base, "/")+apiPrefix, nil)
	c.Token = token
	configureClient(c)
	return &global{ctx: context.Background(), client: c}, 0
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// singleResponse / listResponse mirror the server's envelopes with raw maps,
// so output is presence-faithful (a typed struct's omitempty would hide
// zero-valued fields from display).
type singleResponse struct {
	Data map[string]any `json:"data"`
}

type listResponse struct {
	Data       []map[string]any `json:"data"`
	Total      int              `json:"total"`
	Page       int              `json:"page"`
	PerPage    int              `json:"perPage"`
	TotalPages int              `json:"totalPages"`
	Cursor     string           `json:"cursor"`
	HasMore    bool             `json:"hasMore"`
}

func printJSON(v any) int {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(string(data))
	return 0
}

// printListTable renders rows as an aligned table. keys are the JSON wire
// keys in column order; headers the matching titles.
func printListTable(headers, keys []string, rows []map[string]any) {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(headers, "\t"))
	for _, row := range rows {
		cells := make([]string, len(keys))
		for i, k := range keys {
			if v, ok := row[k]; ok && v != nil {
				cells[i] = fmt.Sprintf("%v", v)
			}
		}
		fmt.Fprintln(w, strings.Join(cells, "\t"))
	}
	w.Flush()
}

// apiFail prints err and maps it to an exit code: 4 for auth failures
// (401/403), 1 for every other API or transport error.
func apiFail(err error) int {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		fmt.Fprintln(os.Stderr, apiErr.Error())
		if apiErr.Status == 401 || apiErr.Status == 403 {
			fmt.Fprintf(os.Stderr, "auth failed — mint a token in the app and run `%s login`\n", binaryName)
			return 4
		}
		return 1
	}
	fmt.Fprintln(os.Stderr, err)
	return 1
}

// takeID pops the leading positional argument: <verb> <id> [flags].
func takeID(verb string, args []string) (string, []string, bool) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintf(os.Stderr, "usage: %s %s <id> [flags]\n", binaryName, verb)
		return "", nil, false
	}
	return args[0], args[1:], true
}

// readJSONArg resolves a --json value: "-" reads stdin, "@path" reads a
// file, anything else is inline JSON.
func readJSONArg(v string) ([]byte, error) {
	switch {
	case v == "-":
		return io.ReadAll(os.Stdin)
	case strings.HasPrefix(v, "@"):
		return os.ReadFile(strings.TrimPrefix(v, "@"))
	default:
		return []byte(v), nil
	}
}

// buildBody assembles a mutation payload: either the --json argument
// verbatim, or a map built from the explicitly-set field flags (via
// fs.Visit, so unset flags stay absent and PATCH semantics hold). The two
// sources are mutually exclusive.
func buildBody(fs *flag.FlagSet, jsonArg string, apply func(flagName string, body map[string]any) error) (map[string]any, int) {
	var visited []string
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "url", "token", "json", "o":
			return
		}
		visited = append(visited, f.Name)
	})
	if jsonArg != "" {
		if len(visited) > 0 {
			fmt.Fprintln(os.Stderr, "--json and field flags are mutually exclusive")
			return nil, 2
		}
		raw, err := readJSONArg(jsonArg)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return nil, 2
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			fmt.Fprintf(os.Stderr, "--json: %v\n", err)
			return nil, 2
		}
		return body, 0
	}
	if len(visited) == 0 {
		fmt.Fprintln(os.Stderr, "nothing to send: pass field flags or --json")
		return nil, 2
	}
	body := map[string]any{}
	for _, name := range visited {
		if err := apply(name, body); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return nil, 2
		}
	}
	return body, 0
}

// readJSONArrayArg reads a --json value that must be a JSON array (batch
// verbs), returned raw so element shapes pass through untouched.
func readJSONArrayArg(v string) (json.RawMessage, int) {
	if v == "" {
		fmt.Fprintln(os.Stderr, "batch verbs need --json with a JSON array (inline, @file, or - for stdin)")
		return nil, 2
	}
	raw, err := readJSONArg(v)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, 2
	}
	var probe []json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		fmt.Fprintf(os.Stderr, "--json: expected a JSON array: %v\n", err)
		return nil, 2
	}
	return json.RawMessage(raw), 0
}

// doBatch sends a _batch request. The server answers a rolled-back batch
// with 400 and the same {committed, results[]} envelope, so that case is
// decoded and returned as a response — printBatch then surfaces it on
// stdout and exit code 1 — rather than treated as a transport error.
func doBatch(g *global, method, path string, body any) (client.BatchResponse, int) {
	var resp client.BatchResponse
	if err := g.client.Do(g.ctx, method, path, body, &resp); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 400 {
			if json.Unmarshal(apiErr.Body, &resp) == nil && len(resp.Results) > 0 {
				return resp, 0
			}
		}
		return resp, apiFail(err)
	}
	return resp, 0
}

// printBatch prints the batch envelope; a rolled-back batch exits 1 so
// scripts can gate on success.
func printBatch(resp client.BatchResponse) int {
	if code := printJSON(resp); code != 0 {
		return code
	}
	if !resp.Committed {
		return 1
	}
	return 0
}

// paramFlags collects repeatable --param key=value pairs — the escape hatch
// for query params the generated flags don't cover (e.g. created_at_gt on
// timestamp-managed columns).
type paramFlags struct {
	pairs [][2]string
}

func (p *paramFlags) String() string { return "" }

func (p *paramFlags) Set(v string) error {
	key, value, ok := strings.Cut(v, "=")
	if !ok || key == "" {
		return fmt.Errorf("--param wants key=value, got %q", v)
	}
	p.pairs = append(p.pairs, [2]string{key, value})
	return nil
}
