package main

import (
	"os"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/localrun"
)

// run is one running gofastr app as the screens and the MCP tools show
// it: its local run record plus how an agent connects to it.
type run struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Kind    string     `json:"kind"`
	PID     int        `json:"pid"`
	URL     string     `json:"url"`
	Service string     `json:"service,omitempty"`
	Dir     string     `json:"dir,omitempty"`
	Exe     string     `json:"exe,omitempty"`
	Started time.Time  `json:"started"`
	Self    bool       `json:"self,omitempty"`
	MCP     *mcpServer `json:"mcp"`
}

// mcpServer is one entry of an agent's mcpServers config (.mcp.json):
// a stdio command or an HTTP URL.
type mcpServer struct {
	Type    string   `json:"type"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	URL     string   `json:"url,omitempty"`
}

// listRuns reads the registry: every live run, newest first.
func listRuns() ([]run, error) {
	recs, err := localrun.List()
	if err != nil {
		return nil, err
	}
	out := make([]run, 0, len(recs))
	for _, r := range recs {
		out = append(out, runOf(r))
	}
	return out, nil
}

// findRun is the live run with id, if any.
func findRun(id string) (run, bool, error) {
	runs, err := listRuns()
	if err != nil {
		return run{}, false, err
	}
	for _, r := range runs {
		if r.ID == id {
			return r, true, nil
		}
	}
	return run{}, false, nil
}

func runOf(r localrun.Record) run {
	return run{
		ID: r.ID, Name: r.Name, Kind: kindOf(r), PID: r.PID, URL: r.URL,
		Service: r.Service, Dir: r.Dir, Exe: r.Exe, Started: r.Started,
		Self: r.PID == os.Getpid(),
		MCP:  mcpOf(r),
	}
}

func kindOf(r localrun.Record) string {
	switch {
	case r.Service != "" && r.Dev:
		return "service dev run"
	case r.Service != "":
		return "service"
	case r.Dev:
		return "dev"
	}
	return "app"
}

// mcpOf is how an agent reaches r: its stdio command when it has one
// (a service, whose HTTP /mcp sits behind the browser sign-in), else
// its HTTP MCP URL, else nothing.
func mcpOf(r localrun.Record) *mcpServer {
	if len(r.MCPCommand) > 0 {
		return &mcpServer{Type: "stdio", Command: r.MCPCommand[0], Args: r.MCPCommand[1:]}
	}
	if u := r.MCPURL(); u != "" {
		return &mcpServer{Type: "http", URL: u}
	}
	return nil
}

// configKey is a name for r in an agent's mcpServers map: lower-case
// letters, digits and dashes.
func configKey(name string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(name) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	if k := strings.Trim(b.String(), "-"); k != "" {
		return k
	}
	return "gofastr-app"
}
