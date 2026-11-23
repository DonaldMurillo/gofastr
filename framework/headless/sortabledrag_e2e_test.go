package headless

// Browser coverage for headless-sortabledrag: a real mouse or touch
// pointer from the browser's input domain presses a row's grip, moves
// and releases. The row stays put while the pointer moves and a class
// marks where it would land; the release moves it once and commits
// through headless-sortablelist; a release off every list, Escape and
// a cancelled pointer move nothing and commit nothing; a touch on the
// row outside the grip starts no drag.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

const sortableDragLoaded = `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-sortabledrag'])`

// sortableLayout gives the unstyled lists what the kit sheet gives
// them: an empty list keeps a row's height, so a pointer can land on
// it, and the grip alone takes touch-action none.
const sortableLayout = `<style>[data-hui-sortable]{min-height:44px;margin:0 0 16px;padding:0}[data-hui-sortable-grip]{display:inline-block;padding:8px;touch-action:none}[data-hui-sortable-item]{padding:4px 0}</style>`

// gripOf is the JS point at the centre of srcKey's grip.
func gripOf(srcKey string) string {
	return fmt.Sprintf(`(function(){var r=document.querySelector('[data-hui-sort-key=%q] [data-hui-sortable-grip]').getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height/2};})()`, srcKey)
}

// lowerHalfOf is the JS point in the lower half of destKey's row.
func lowerHalfOf(destKey string) string {
	return fmt.Sprintf(`(function(){var r=document.querySelector('[data-hui-sort-key=%q]').getBoundingClientRect();return {x:r.left+r.width/2,y:r.bottom-3};})()`, destKey)
}

// upperHalfOf is the JS point in the upper half of destKey's row.
func upperHalfOf(destKey string) string {
	return fmt.Sprintf(`(function(){var r=document.querySelector('[data-hui-sort-key=%q]').getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+3};})()`, destKey)
}

// middleOfColumn is the JS point at the centre of the container's list.
func middleOfColumn(container string) string {
	return fmt.Sprintf(`(function(){var r=document.querySelector('[data-hui-sortable-container=%q]').getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height/2};})()`, container)
}

type point struct{ X, Y float64 }

// pointerPress presses a real pointer ("mouse" or "touch") at from and
// moves it to to in steps, leaving it held.
func pointerPress(kind, from, to string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		if err := chromedp.Poll(sortableDragLoaded, nil, chromedp.WithPollingTimeout(5*time.Second)).Do(ctx); err != nil {
			return fmt.Errorf("headless-sortabledrag never loaded: %w", err)
		}
		var a, b point
		if err := chromedp.Evaluate(from, &a).Do(ctx); err != nil {
			return err
		}
		if err := chromedp.Evaluate(to, &b).Do(ctx); err != nil {
			return err
		}
		if err := pointerAt(ctx, kind, "down", a); err != nil {
			return err
		}
		const steps = 6
		for i := 1; i <= steps; i++ {
			p := point{a.X + (b.X-a.X)*float64(i)/steps, a.Y + (b.Y-a.Y)*float64(i)/steps}
			if err := pointerAt(ctx, kind, "move", p); err != nil {
				return err
			}
		}
		return nil
	})
}

// pointerRelease lifts the held pointer at the JS point at.
func pointerRelease(kind, at string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		var p point
		if err := chromedp.Evaluate(at, &p).Do(ctx); err != nil {
			return err
		}
		return pointerAt(ctx, kind, "up", p)
	})
}

func pointerAt(ctx context.Context, kind, phase string, p point) error {
	if kind == "touch" {
		typ := map[string]input.TouchType{"down": input.TouchStart, "move": input.TouchMove, "up": input.TouchEnd}[phase]
		pts := []*input.TouchPoint{{X: p.X, Y: p.Y}}
		if phase == "up" {
			pts = []*input.TouchPoint{}
		}
		return input.DispatchTouchEvent(typ, pts).Do(ctx)
	}
	typ := map[string]input.MouseType{"down": input.MousePressed, "move": input.MouseMoved, "up": input.MouseReleased}[phase]
	ev := input.DispatchMouseEvent(typ, p.X, p.Y).WithButton(input.Left).WithButtons(1)
	if phase == "up" {
		ev = ev.WithButtons(0)
	}
	if phase != "move" {
		ev = ev.WithClickCount(1)
	}
	return ev.Do(ctx)
}

// pointerDrag drags srcKey's grip to the JS point to and releases it there.
func pointerDrag(kind, srcKey, to string) chromedp.Action {
	return chromedp.Tasks{pointerPress(kind, gripOf(srcKey), to), pointerRelease(kind, to)}
}

// A mouse and a touch drag each leave the row in place while the
// pointer moves, mark the landing place on the row under it, and on
// release move the row once and commit the new order.
func TestE2E_SortablePointerDragMovesOnRelease(t *testing.T) {
	for _, kind := range []string{"mouse", "touch"} {
		t.Run(kind, func(t *testing.T) {
			b, commits := sortableServer(t, sortableLayout+sortablePage(
				SortableItem{Key: "a", Label: "Alpha"},
				SortableItem{Key: "b", Label: "Beta"},
				SortableItem{Key: "c", Label: "Gamma"},
			), false)
			ctx := behaviorPage(t, b)
			var mid, marks, dragging string
			if err := chromedp.Run(ctx,
				pointerPress(kind, gripOf("a"), lowerHalfOf("b")),
				chromedp.Evaluate(sortOrder, &mid),
				chromedp.Evaluate(`Array.from(document.querySelectorAll('.is-drop-before,.is-drop-after,.is-drop-target')).map(function(e){return (e.getAttribute('data-hui-sort-key')||'list')+':'+e.className;}).join(' ')`, &marks),
				chromedp.Evaluate(`document.querySelector('[data-hui-sort-key="a"]').className`, &dragging),
			); err != nil {
				t.Fatal(err)
			}
			if mid != "a,b,c" {
				t.Fatalf("the row moved while the pointer did: %q, want a,b,c until release", mid)
			}
			if marks != "b:is-drop-after" {
				t.Fatalf("the landing mark reads %q, want b:is-drop-after", marks)
			}
			if dragging != "is-dragging" {
				t.Fatalf("the dragged row's class reads %q, want is-dragging", dragging)
			}
			var after, left string
			if err := chromedp.Run(ctx,
				pointerRelease(kind, lowerHalfOf("b")),
				chromedp.Evaluate(sortOrder, &after),
				chromedp.Evaluate(`document.querySelectorAll('.is-drop-before,.is-drop-after,.is-drop-target,.is-dragging').length + ''`, &left),
			); err != nil {
				t.Fatal(err)
			}
			if after != "b,a,c" {
				t.Fatalf("the release left the order %q, want b,a,c", after)
			}
			if left != "0" {
				t.Fatalf("%s drag classes survived the release", left)
			}
			if !waitLive(ctx, "Order saved.") {
				t.Fatal("the drop never announced the save")
			}
			if len(*commits) != 1 || (*commits)[0] != "b,a,c" {
				t.Fatalf("the drop committed %q, want exactly [b,a,c]", *commits)
			}
		})
	}
}

// The upper half of a row lands the dragged row before it.
func TestE2E_SortablePointerDragUpperHalfLandsBefore(t *testing.T) {
	b, commits := sortableServer(t, sortableLayout+sortablePage(
		SortableItem{Key: "a", Label: "Alpha"},
		SortableItem{Key: "b", Label: "Beta"},
		SortableItem{Key: "c", Label: "Gamma"},
	), false)
	ctx := behaviorPage(t, b)
	if err := chromedp.Run(ctx, pointerDrag("mouse", "c", upperHalfOf("b"))); err != nil {
		t.Fatal(err)
	}
	if got := evalString(t, ctx, sortOrder); got != "a,c,b" {
		t.Fatalf("a drop on b's upper half left %q, want a,c,b", got)
	}
	for range 10 {
		if len(*commits) > 0 {
			break
		}
		chromedp.Run(ctx, chromedp.Sleep(100*time.Millisecond))
	}
	if len(*commits) != 1 || (*commits)[0] != "a,c,b" {
		t.Fatalf("the drop committed %q, want exactly [a,c,b]", *commits)
	}
}

// A drag that ends without a landing place moves nothing and commits
// nothing: a release off every list, Escape while held, and a pointer
// whose capture is lost (the browser's pointercancel).
func TestE2E_SortablePointerDragCancels(t *testing.T) {
	for _, c := range []struct {
		name string
		end  chromedp.Action
	}{
		{"released off the list", pointerRelease("mouse", `({x: 5, y: window.innerHeight - 5})`)},
		{"escape", chromedp.Tasks{
			chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}))`, nil),
			pointerRelease("mouse", lowerHalfOf("b")),
		}},
		{"capture lost", chromedp.Tasks{
			chromedp.Evaluate(`(function(){var g=document.querySelector('[data-hui-sort-key="a"] [data-hui-sortable-grip]');g.dispatchEvent(new PointerEvent('lostpointercapture',{bubbles:true}));})()`, nil),
			pointerRelease("mouse", lowerHalfOf("b")),
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			b, commits := sortableServer(t, sortableLayout+sortablePage(
				SortableItem{Key: "a", Label: "Alpha"},
				SortableItem{Key: "b", Label: "Beta"},
			), false)
			ctx := behaviorPage(t, b)
			var order, left string
			if err := chromedp.Run(ctx,
				pointerPress("mouse", gripOf("a"), lowerHalfOf("b")),
				c.end,
				chromedp.Sleep(300*time.Millisecond),
				chromedp.Evaluate(sortOrder, &order),
				chromedp.Evaluate(`document.querySelectorAll('.is-drop-before,.is-drop-after,.is-drop-target,.is-dragging').length + ''`, &left),
			); err != nil {
				t.Fatal(err)
			}
			if order != "a,b" {
				t.Fatalf("a cancelled drag left the order %q, want a,b", order)
			}
			if left != "0" {
				t.Fatalf("%s drag classes survived the cancel", left)
			}
			if len(*commits) != 0 {
				t.Fatalf("a cancelled drag POSTed %q", *commits)
			}
		})
	}
}

// A touch that starts on the row outside the grip is not a drag: the
// page keeps the gesture (it scrolls), and nothing moves or commits.
func TestE2E_SortableTouchOutsideTheGripIsNoDrag(t *testing.T) {
	b, commits := sortableServer(t, sortableLayout+sortablePage(
		SortableItem{Key: "a", Label: "Alpha"},
		SortableItem{Key: "b", Label: "Beta"},
	), false)
	ctx := behaviorPage(t, b)
	// The finger runs straight down: a sideways swipe is the browser's
	// history gesture, which would leave the page.
	label := `(function(){var r=document.querySelector('[data-hui-sort-key="a"] span:not([data-hui-sortable-grip])').getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height/2};})()`
	below := `(function(){var l=document.querySelector('[data-hui-sort-key="a"] span:not([data-hui-sortable-grip])').getBoundingClientRect(),r=document.querySelector('[data-hui-sort-key="b"]').getBoundingClientRect();return {x:l.left+l.width/2,y:r.bottom-3};})()`
	var order, dragging string
	if err := chromedp.Run(ctx,
		pointerPress("touch", label, below),
		chromedp.Evaluate(`document.querySelectorAll('.is-dragging').length + ''`, &dragging),
		pointerRelease("touch", below),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(sortOrder, &order),
	); err != nil {
		t.Fatal(err)
	}
	if dragging != "0" {
		t.Fatal("a touch on the row's label started a drag")
	}
	if order != "a,b" || len(*commits) != 0 {
		t.Fatalf("a touch outside the grip moved the list to %q and POSTed %q", order, *commits)
	}
}

// A row held over itself marks no landing place, and released there
// moves and commits nothing.
func TestE2E_SortablePointerDropOnItselfIsNoMove(t *testing.T) {
	b, commits := sortableServer(t, sortableLayout+sortablePage(
		SortableItem{Key: "a", Label: "Alpha"},
		SortableItem{Key: "b", Label: "Beta"},
	), false)
	ctx := behaviorPage(t, b)
	var marks string
	if err := chromedp.Run(ctx,
		pointerPress("mouse", gripOf("a"), lowerHalfOf("a")),
		chromedp.Evaluate(`document.querySelectorAll('.is-drop-before,.is-drop-after,.is-drop-target').length + ''`, &marks),
		pointerRelease("mouse", lowerHalfOf("a")),
		chromedp.Sleep(300*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	if marks != "0" {
		t.Fatalf("a row held over itself drew %s landing marks", marks)
	}
	if got := evalString(t, ctx, sortOrder); got != "a,b" || len(*commits) != 0 {
		t.Fatalf("a drop on the row itself left %q and POSTed %q", got, *commits)
	}
}

// sortableGapLayout spaces the rows 10px apart, so a pointer can rest
// in the gap between two rows rather than on either.
const sortableGapLayout = sortableLayout + `<style>[data-hui-sortable-item]{margin:0 0 10px}</style>`

// gapBelow is the JS point in the 10px gap under key's row.
func gapBelow(key string) string {
	return fmt.Sprintf(`(function(){var r=document.querySelector('[data-hui-sort-key=%q]').getBoundingClientRect();return {x:r.left+r.width/2,y:r.bottom+5};})()`, key)
}

// The gap between rows lands by position: the gap right under the
// dragged row is its own place (no move), and the gap under another row
// lands before the next one.
func TestE2E_SortablePointerDragInAGapLandsByPosition(t *testing.T) {
	for _, c := range []struct {
		name, to, want string
		commit         bool
	}{
		{"the gap under the dragged row", gapBelow("a"), "a,b,c,d", false},
		{"the gap between b and c", gapBelow("b"), "b,a,c,d", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			b, commits := sortableServer(t, sortableGapLayout+sortablePage(
				SortableItem{Key: "a", Label: "Alpha"},
				SortableItem{Key: "b", Label: "Beta"},
				SortableItem{Key: "c", Label: "Gamma"},
				SortableItem{Key: "d", Label: "Delta"},
			), false)
			ctx := behaviorPage(t, b)
			if err := chromedp.Run(ctx, pointerDrag("mouse", "a", c.to), chromedp.Sleep(300*time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			if got := evalString(t, ctx, sortOrder); got != c.want {
				t.Fatalf("a release in %s left %q, want %q", c.name, got, c.want)
			}
			for range 10 {
				if len(*commits) > 0 || !c.commit {
					break
				}
				chromedp.Run(ctx, chromedp.Sleep(100*time.Millisecond))
			}
			if c.commit && (len(*commits) != 1 || (*commits)[0] != c.want) {
				t.Fatalf("the drop committed %q, want exactly [%s]", *commits, c.want)
			}
			if !c.commit && len(*commits) != 0 {
				t.Fatalf("a release in %s POSTed %q", c.name, *commits)
			}
		})
	}
}

// A board the page re-renders mid-drag (a poll, an island swap, a
// conflict refresh) ends the drag: the release moves nothing, commits
// nothing and leaves no second copy of the dragged row, in a single
// list or across a linked board.
func TestE2E_SortablePointerDragEndsWhenTheListIsReRendered(t *testing.T) {
	board := string(SortableList(SortableListProps{
		Label: "To do", Group: "board", Container: "todo", RPCPath: "/commit",
		Items: []SortableItem{{Key: "a", Label: "Alpha"}, {Key: "b", Label: "Beta"}},
	}, nil)) + string(SortableList(SortableListProps{
		Label: "Doing", Group: "board", Container: "doing", RPCPath: "/commit",
		Items: []SortableItem{{Key: "c", Label: "Gamma"}},
	}, nil))
	for _, c := range []struct{ name, body, to, want string }{
		{"one list", sortablePage(SortableItem{Key: "a", Label: "Alpha"}, SortableItem{Key: "b", Label: "Beta"},
			SortableItem{Key: "c", Label: "Gamma"}), lowerHalfOf("b"), "a,b,c"},
		{"a linked board", board, lowerHalfOf("c"), "a,b,c"},
	} {
		t.Run(c.name, func(t *testing.T) {
			b, commits := sortableServer(t, sortableLayout+`<div id="board">`+c.body+`</div>`, false)
			ctx := behaviorPage(t, b)
			var keys string
			if err := chromedp.Run(ctx,
				pointerPress("mouse", gripOf("a"), c.to),
				// The server's fresh rows: the same markup without the drag's classes.
				chromedp.Evaluate(`(function(){var l=document.getElementById('board');l.innerHTML=l.innerHTML;
					var c=['is-dragging','is-drop-before','is-drop-after','is-drop-target'];l.querySelectorAll('.'+c.join(',.')).forEach(function(e){e.classList.remove.apply(e.classList,c);});})()`, nil),
				pointerRelease("mouse", c.to),
				chromedp.Sleep(300*time.Millisecond),
				chromedp.Evaluate(`Array.from(document.querySelectorAll('[data-hui-sort-key]')).map(function(r){return r.getAttribute('data-hui-sort-key')}).join(',')`, &keys),
			); err != nil {
				t.Fatal(err)
			}
			if keys != c.want {
				t.Fatalf("a drag over a re-rendered list left the rows %q, want %s", keys, c.want)
			}
			if len(*commits) != 0 {
				t.Fatalf("a drag over a re-rendered list POSTed %q", *commits)
			}
			// The drag is over: a fresh one on the new rows works.
			var left string
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`document.querySelectorAll('.is-dragging,.is-drop-before,.is-drop-after,.is-drop-target').length + ''`, &left),
				pointerDrag("mouse", "b", lowerHalfOf("c")),
				chromedp.Sleep(300*time.Millisecond),
			); err != nil {
				t.Fatal(err)
			}
			if left != "0" {
				t.Fatalf("%s drag classes survived the re-render", left)
			}
			for range 10 {
				if len(*commits) > 0 {
					break
				}
				chromedp.Run(ctx, chromedp.Sleep(100*time.Millisecond))
			}
			if len(*commits) != 1 {
				t.Fatalf("a drag after the re-render committed %q; the first drag never ended", *commits)
			}
		})
	}
}
