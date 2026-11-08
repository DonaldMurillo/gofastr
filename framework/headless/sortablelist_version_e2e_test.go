package headless

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/chromedp/chromedp"
)

// versionBoard is a move handler's store: the documented
// compare-and-bump recipe. A move whose version is not the board's
// answers 409 through handler.WriteError; a matching one applies the
// order, bumps the version and renews it on the 204. The conflict
// endpoint returns a column's rows with the version they were read at.
type versionBoard struct {
	mu      sync.Mutex
	cols    map[string][]string
	version int
	renew   bool
	posts   []string
}

func (board *versionBoard) token() string { return fmt.Sprintf("v%d", board.version) }

// bump is another writer's move, out of the browser's sight: column
// a's rows rotate and the version moves on.
func (board *versionBoard) bump() {
	board.mu.Lock()
	defer board.mu.Unlock()
	rows := board.cols["a"]
	board.cols["a"] = append(slices.Clone(rows[1:]), rows[0])
	board.version++
}

func (board *versionBoard) log() []string {
	board.mu.Lock()
	defer board.mu.Unlock()
	return slices.Clone(board.posts)
}

func (board *versionBoard) order(col string) string {
	board.mu.Lock()
	defer board.mu.Unlock()
	return strings.Join(board.cols[col], ",")
}

func (board *versionBoard) rows(col string) []SortableItem {
	items := make([]SortableItem, 0, len(board.cols[col]))
	for _, key := range board.cols[col] {
		items = append(items, SortableItem{Key: key, Label: strings.ToUpper(key)})
	}
	return items
}

func (board *versionBoard) list(col string, versioned, grouped bool) string {
	props := SortableListProps{Label: "Column " + col, Container: col, RPCPath: "/move", Items: board.rows(col)}
	if versioned {
		props.Version = board.token()
		props.ConflictRPC = "/conflict?container=" + col
	}
	if grouped {
		props.Group = "board"
	}
	return string(SortableList(props, nil))
}

func (board *versionBoard) routes(mux *http.ServeMux) {
	mux.HandleFunc("/move", func(resp http.ResponseWriter, req *http.Request) {
		if err := req.ParseForm(); err != nil {
			http.Error(resp, "bad request", http.StatusBadRequest)
			return
		}
		board.mu.Lock()
		defer board.mu.Unlock()
		sent := req.FormValue("version")
		if sent != "" && sent != board.token() {
			board.posts = append(board.posts, "409 "+sent)
			handler.WriteError(resp, req, handler.Errorf(http.StatusConflict, "%s", "The board changed."))
			return
		}
		dest := req.FormValue("container")
		if moved := req.FormValue("moved"); moved != "" {
			for col, keys := range board.cols {
				if col != dest {
					board.cols[col] = slices.DeleteFunc(keys, func(key string) bool { return key == moved })
				}
			}
		}
		board.cols[dest] = strings.Split(req.FormValue("order"), ",")
		board.version++
		board.posts = append(board.posts, "204 "+sent)
		if board.renew && sent != "" {
			RenewSortableVersion(resp, board.token())
		}
		resp.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/conflict", func(resp http.ResponseWriter, req *http.Request) {
		board.mu.Lock()
		defer board.mu.Unlock()
		col := req.URL.Query().Get("container")
		if board.renew {
			RenewSortableVersion(resp, board.token())
		}
		resp.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(resp, string(SortableItems(SortableListProps{Label: "Column " + col, Items: board.rows(col)}, nil)))
	})
}

// versionMove grabs the row, moves it one place down (or across to the
// next column) and drops it, then waits until the server has seen the
// commit and every list has settled it, the conflict refresh included.
func versionMove(t *testing.T, ctx context.Context, board *versionBoard, key, arrow string) {
	t.Helper()
	before := len(board.log())
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`(function(){
		var item = document.querySelector('[data-hui-sort-key=%q]');
		item.focus();
		[' ', %q, ' '].forEach(function (key) {
			item.dispatchEvent(new KeyboardEvent('keydown', {key: key, bubbles: true, cancelable: true}));
		});
	})()`, key, arrow), nil)); err != nil {
		t.Fatal(err)
	}
	for range 40 {
		if len(board.log()) > before {
			break
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(50*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	if len(board.log()) == before {
		t.Fatalf("moving %s never reached the server", key)
	}
	if !pollTrue(ctx, `Array.from(document.querySelectorAll('[data-hui-sortable]')).every(function (list) { return !list.__huiN; })`) {
		t.Fatalf("moving %s never settled in the page", key)
	}
}

// domOrder reads one column's rows and token as "k1,k2@token".
func domOrder(t *testing.T, ctx context.Context, col string) string {
	t.Helper()
	return evalString(t, ctx, fmt.Sprintf(`(function(){
		var list = document.querySelector('[data-hui-sortable-container=%q]');
		return Array.from(list.querySelectorAll('[data-hui-sort-key]')).map(function (row) { return row.getAttribute('data-hui-sort-key'); }).join(',') + '@' + (list.getAttribute('data-hui-sortable-version') || '');
	})()`, col))
}

// A versioned list saves move after move: each 2xx renews the token,
// so the next commit sends the version the server holds. An
// unversioned list is the control (it never sends one), and a server
// that does not renew shows what the renewal fixes.
func TestSortableVersionRenews(t *testing.T) {
	for _, tc := range []struct {
		name      string
		versioned bool
		renew     bool
		posts     []string
		token     string
	}{
		{name: "versioned, renewed", versioned: true, renew: true, posts: []string{"204 v1", "204 v2", "204 v3"}, token: "v4"},
		{name: "unversioned", renew: true, posts: []string{"204 ", "204 ", "204 "}, token: ""},
		{name: "versioned, never renewed", versioned: true, posts: []string{"204 v1", "409 v1", "409 v1"}, token: "v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			board := &versionBoard{cols: map[string][]string{"a": {"a", "b", "c"}}, version: 1, renew: tc.renew}
			server := startBehaviorServer(t, board.list("a", tc.versioned, false), board.routes)
			ctx := behaviorPage(t, server)
			if !pollTrue(ctx, sortableLoaded) {
				t.Fatal("the sortable marker never loaded headless-sortablelist")
			}
			versionMove(t, ctx, board, "a", "ArrowDown")
			versionMove(t, ctx, board, "a", "ArrowDown")
			versionMove(t, ctx, board, "b", "ArrowDown")
			if got := board.log(); !slices.Equal(got, tc.posts) {
				t.Fatalf("the server answered %q, want %q", got, tc.posts)
			}
			if got, want := domOrder(t, ctx, "a"), board.order("a")+"@"+tc.token; got != want {
				t.Fatalf("the list shows %q, want %q (the server's rows and token)", got, want)
			}
		})
	}
}

// A versioned list that conflicts with another writer's move takes the
// server's rows AND their version from the conflict refresh, so the
// move after it saves. Linked columns share the board's token: a
// saved move renews every column still holding the token it sent, and
// a column whose token another writer's move made stale keeps it,
// conflicts on its own next move, and recovers the same way.
func TestSortableConflictRenewsToken(t *testing.T) {
	board := &versionBoard{cols: map[string][]string{"a": {"a1", "a2", "a3"}, "b": {"b1", "b2"}}, version: 1, renew: true}
	server := startBehaviorServer(t, board.list("a", true, true)+board.list("b", true, true), board.routes)
	ctx := behaviorPage(t, server)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("the sortable marker never loaded headless-sortablelist")
	}

	versionMove(t, ctx, board, "a1", "ArrowDown")
	if got := domOrder(t, ctx, "b"); got != "b1,b2@v2" {
		t.Fatalf("after a saved move in column a, column b shows %q, want b1,b2@v2", got)
	}
	versionMove(t, ctx, board, "a1", "ArrowRight")
	versionMove(t, ctx, board, "b1", "ArrowDown")
	if got, want := board.log(), []string{"204 v1", "204 v2", "204 v3"}; !slices.Equal(got, want) {
		t.Fatalf("the linked moves were answered %q, want %q", got, want)
	}

	board.bump()
	versionMove(t, ctx, board, "a2", "ArrowDown")
	if got, want := domOrder(t, ctx, "a"), board.order("a")+"@v5"; got != want {
		t.Fatalf("after the conflict refresh column a shows %q, want %q", got, want)
	}
	if got := domOrder(t, ctx, "b"); !strings.HasSuffix(got, "@v4") {
		t.Fatalf("column b was not refreshed, so it must keep its token; it shows %q", got)
	}
	versionMove(t, ctx, board, "a3", "ArrowDown")
	versionMove(t, ctx, board, "b2", "ArrowDown")
	versionMove(t, ctx, board, "b2", "ArrowDown")
	want := []string{"204 v1", "204 v2", "204 v3", "409 v4", "204 v5", "409 v4", "204 v6"}
	if got := board.log(); !slices.Equal(got, want) {
		t.Fatalf("the server answered %q, want %q", got, want)
	}
	for _, col := range []string{"a", "b"} {
		if got, want := domOrder(t, ctx, col), board.order(col)+"@"+board.token(); got != want {
			t.Fatalf("column %s shows %q, want %q", col, got, want)
		}
	}
}
