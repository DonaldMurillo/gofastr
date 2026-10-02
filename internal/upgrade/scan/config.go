package scan

import (
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

// configMatchers walks gofastr.yml (and every *.gofastr.yml beside it)
// through the YAML parser. A missing file is not an error: there is
// nothing to match.
func (e *engine) configMatchers() {
	if len(e.configNotes) == 0 {
		return
	}
	entries, err := fs.ReadDir(e.appFS, ".")
	if err != nil {
		return
	}
	names := []string{"gofastr.yml"}
	for _, ent := range entries {
		if !ent.IsDir() && ent.Name() != "gofastr.yml" && strings.HasSuffix(ent.Name(), ".gofastr.yml") {
			names = append(names, ent.Name())
		}
	}
	sort.Strings(names[1:])
	for _, name := range names {
		data, err := e.appRoot.ReadFile(name)
		if err != nil {
			continue
		}
		doc, err := coreyaml.Parse(string(data))
		if err != nil {
			continue
		}
		for _, n := range e.configNotes {
			for _, cm := range n.Find.Config {
				e.configWalk(n, cm, doc, strings.Split(cm.Key, "."), nil, name)
			}
		}
	}
}

// configWalk descends one key segment; "*" matches any one map key or
// list item. The matched node's line is the hit; a Value regexp must
// match the scalar's source text.
func (e *engine) configWalk(n *upgrade.Note, cm upgrade.ConfigMatch, node *coreyaml.Node, segs, walked []string, file string) {
	if node == nil {
		return
	}
	if len(segs) == 0 {
		if cm.Value != nil {
			if node.Kind != coreyaml.Scalar || !cm.Value.MatchString(scalarText(node.Value)) {
				return
			}
		}
		e.add(n, Hit{File: file, Line: node.Line, Why: "config " + strings.Join(walked, ".")})
		return
	}
	seg, rest := segs[0], segs[1:]
	switch node.Kind {
	case coreyaml.Map:
		if seg == "*" {
			for _, k := range slices.Sorted(maps.Keys(node.Map)) {
				e.configWalk(n, cm, node.Map[k], rest, append(walked, k), file)
			}
			return
		}
		if child, ok := node.Map[seg]; ok {
			e.configWalk(n, cm, child, rest, append(walked, seg), file)
		}
	case coreyaml.List:
		if seg != "*" {
			return
		}
		for i, item := range node.List {
			e.configWalk(n, cm, item, rest, append(walked, strconv.Itoa(i)), file)
		}
	}
}

// scalarText renders a scalar the way it was written; the parser keeps
// strings verbatim and other kinds as their source spelling.
func scalarText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
