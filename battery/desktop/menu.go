package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// Menu is the native menu bar (or platform equivalent) declared in Go.
// Each item is exactly one of: a Navigate path (client-side nav, no
// reload), a Handler func, or a Role the shell implements natively.

// Menu roles the shell recognizes.
const (
	RoleQuit      = "quit"
	RoleAbout     = "about"
	RoleSeparator = "separator"
	RoleSettings  = "settings"
	RoleShow      = "show"
)

// Menu is the top-level item list.
type Menu struct {
	Items []MenuItem
}

// MenuItem is one menu row. ID is optional; empty IDs are auto-assigned
// "m<N>" in declaration order when the battery takes a copy at New.
type MenuItem struct {
	ID string
	// Title is the displayed label, in the OS language the shell
	// reads. A separator needs none; a role item gets the OS default
	// when it is zero.
	Title i18n.Phrase
	// Key is an accelerator ("cmd+n", "ctrl+shift+t"): known modifiers
	// (cmd/command/meta, ctrl/control, alt/option/opt, shift) then one
	// key, with at least one modifier other than shift. A bare key is
	// refused: the shell would take that keystroke from the page. The
	// shell maps it to the platform spelling.
	Key  string
	Role string
	// Navigate is a same-origin absolute path; exclusive with Handler.
	Navigate string
	// Handler runs on a goroutine with a 30 s context.
	Handler  func(ctx context.Context) error
	Children []MenuItem
}

// validateMenu checks the whole tree and returns a deep copy with IDs
// assigned. Called at New: an invalid menu is a construction error.
func validateMenu(m *Menu) (*Menu, error) {
	if m == nil {
		return nil, nil
	}
	out := &Menu{}
	seen := make(map[string]bool)
	var assign func(items []MenuItem) ([]MenuItem, error)
	assign = func(items []MenuItem) ([]MenuItem, error) {
		cp := make([]MenuItem, 0, len(items))
		for _, it := range items {
			c := it
			c.Children = nil
			if it.Navigate != "" && it.Handler != nil {
				return nil, fmt.Errorf("desktop: menu item %q declares both Navigate and Handler; pick one", it.displayID())
			}
			if it.Navigate != "" && !validNavigatePath(it.Navigate) {
				return nil, fmt.Errorf("desktop: menu item %q: Navigate %q must be a same-origin absolute path (leading /, no scheme, no //, no control characters, no .. segments)", it.displayID(), it.Navigate)
			}
			if it.Role != "" && it.Role != RoleQuit && it.Role != RoleAbout && it.Role != RoleSeparator &&
				it.Role != RoleSettings && it.Role != RoleShow {
				return nil, fmt.Errorf("desktop: menu item %q: unknown role %q (want %q, %q, %q, %q, or %q)",
					it.displayID(), it.Role, RoleQuit, RoleAbout, RoleSeparator, RoleSettings, RoleShow)
			}
			if it.Role == RoleSeparator && (it.Navigate != "" || it.Handler != nil) {
				return nil, fmt.Errorf("desktop: menu item %q: a separator cannot Navigate or Handler", it.displayID())
			}
			if it.Key != "" && !validMenuKey(it.Key) {
				return nil, fmt.Errorf("desktop: menu item %q: Key %q must be modifiers then one key with at least one of cmd/ctrl/alt (\"cmd+n\", \"ctrl+shift+t\"): a bare or shift-only key fires on plain typing, so the page never receives it", it.displayID(), it.Key)
			}
			if len(it.Children) > 0 {
				kids, err := assign(it.Children)
				if err != nil {
					return nil, err
				}
				c.Children = kids
			}
			cp = append(cp, c)
		}
		return cp, nil
	}
	var err error
	if out.Items, err = assign(m.Items); err != nil {
		return nil, err
	}
	// Assign IDs after validation so error messages can name positions.
	var n int
	var walk func(items *[]MenuItem)
	walk = func(items *[]MenuItem) {
		for i := range *items {
			n++
			if (*items)[i].ID == "" {
				(*items)[i].ID = fmt.Sprintf("m%d", n)
			}
			if len((*items)[i].Children) > 0 {
				walk(&(*items)[i].Children)
			}
		}
	}
	walk(&out.Items)
	// Duplicate-ID check on the assigned copy.
	var check func(items []MenuItem) error
	check = func(items []MenuItem) error {
		for _, it := range items {
			if seen[it.ID] {
				return fmt.Errorf("desktop: duplicate menu item id %q", it.ID)
			}
			seen[it.ID] = true
			if err := check(it.Children); err != nil {
				return err
			}
		}
		return nil
	}
	if err := check(out.Items); err != nil {
		return nil, err
	}
	return out, nil
}

// displayID names an item in errors before IDs are assigned.
func (it MenuItem) displayID() string {
	if it.ID != "" {
		return it.ID
	}
	if !it.Title.IsZero() {
		return it.Title.English()
	}
	return "<untitled>"
}

// validNavigatePath enforces the same-origin absolute path grammar
// (deviation 4): starts with "/", no scheme, no protocol-relative //,
// no control characters, no "." or ".." segments. The path part, up
// to the query or fragment, is also read the way a URL parser reads it:
// %2e is a dot, so "/a/%2e%2e?x" holds a ".." segment. A decoded
// segment may not hold a slash, a backslash or a control character
// either: Go's ServeMux decodes a wildcard, so "/files/..%2f..%2fx"
// hands PathValue("name") == "../../x" to a GET /files/{name} route.
func validNavigatePath(p string) bool {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
		return false
	}
	if strings.ContainsAny(p, "\\\r\n\x00\x1b") || strings.IndexFunc(p, unicode.IsControl) >= 0 {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	path := p
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	for _, seg := range strings.Split(path, "/") {
		dec, err := url.PathUnescape(seg)
		if err != nil {
			continue
		}
		if dec == "." || dec == ".." || strings.IndexFunc(dec, refusedDecodedRune) >= 0 {
			return false
		}
	}
	return true
}

// refusedDecodedRune reports a rune a decoded path segment may not
// carry: a separator (/ or \) that splits it into more segments for a
// handler reading the decoded value, or a control character.
func refusedDecodedRune(r rune) bool {
	return r == '/' || r == '\\' || r < 0x20 || r == 0x7f || unicode.IsControl(r)
}

// validMenuKey accepts +-joined ASCII tokens: one or more known
// modifiers, then exactly one key, with at least one of cmd/ctrl/alt
// among the modifiers. Every shell matches menu accelerators before the
// page sees the keystroke (AppKit key equivalents, GTK accel groups,
// the Windows message loop), so a bare or shift-only key would swallow
// that character in every text field. An unknown modifier token is not
// accepted either: the planner would drop it and leave the bare key.
func validMenuKey(k string) bool {
	if len(k) > 64 {
		return false
	}
	parts := strings.Split(k, "+")
	if len(parts) < 2 {
		return false
	}
	command := false
	for i, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
				return false
			}
		}
		mod, isMod := menuKeyModifier(asciicase.Lower(part))
		if i == len(parts)-1 {
			if isMod {
				return false // modifiers with no key
			}
			continue
		}
		if !isMod {
			return false
		}
		command = command || mod
	}
	return command
}

// menuKeyModifier reports whether tok is a modifier keyEquivalent maps,
// and whether it is a command modifier (anything but shift).
func menuKeyModifier(tok string) (command, ok bool) {
	switch tok {
	case "cmd", "command", "meta", "ctrl", "control", "alt", "option", "opt":
		return true, true
	case "shift":
		return false, true
	}
	return false, false
}

// find returns the item with the given id (searching children).
func (m *Menu) find(id string) (MenuItem, bool) {
	if m == nil {
		return MenuItem{}, false
	}
	var found MenuItem
	var ok bool
	var walk func(items []MenuItem)
	walk = func(items []MenuItem) {
		for _, it := range items {
			if it.ID == id {
				found, ok = it, true
				return
			}
			walk(it.Children)
			if ok {
				return
			}
		}
	}
	walk(m.Items)
	return found, ok
}

// menuHandlerTimeout bounds a menu Handler invocation.
const menuHandlerTimeout = 30 * time.Second

// dispatchMenu is WindowConfig.OnMenu. Navigate items eval the runtime
// navigate call in the window; Handler items run on a goroutine with a
// deadline. Role items never arrive here (the shell handles them); an
// unknown id is a Warn.
func (b *Battery) dispatchMenu(id string) { b.dispatchMenuMode(id, true) }

// dispatchTrayMenu keeps Handler completion inside the native tray's bounded
// callback worker. Main-menu and existing platform dispatch remain asynchronous.
func (b *Battery) dispatchTrayMenu(id string) { b.dispatchMenuMode(id, false) }

func (b *Battery) dispatchMenuMode(id string, async bool) {
	item, ok := b.menu.find(id)
	if !ok {
		b.logger.Warn("desktop: menu activation for unknown item id", "id", id)
		return
	}
	switch {
	case item.Navigate != "":
		path, err := json.Marshal(item.Navigate)
		if err != nil {
			b.logger.Error("desktop: menu navigate path marshal failed", "id", id)
			return
		}
		js := "window.__gofastr.navigate(" + string(path) + ")"
		w, wok := b.Window()
		if !wok {
			b.logger.Warn("desktop: menu navigate before the window opened", "id", id)
			return
		}
		if err := w.Eval(b.inAppJS(js)); err != nil {
			b.logger.Error("desktop: menu navigate eval failed", "id", id, "error", err)
		}
	case item.Handler != nil:
		handlerFn := item.Handler
		run := func() {
			// A menu handler is app-supplied code on a bare goroutine:
			// recover so one panic cannot kill the process.
			defer func() {
				if v := recover(); v != nil {
					b.logger.Error("desktop: menu handler panicked", "id", id, "panic", textsafe.Recovered(v))
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), menuHandlerTimeout)
			defer cancel()
			if err := handlerFn(ctx); err != nil {
				b.logger.Error("desktop: menu handler failed", "id", id, "error", err)
			}
		}
		if async {
			go run()
		} else {
			run()
		}
	}
}
