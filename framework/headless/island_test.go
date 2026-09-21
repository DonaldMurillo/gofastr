package headless

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
)

// fixtureIsland is the island the fixtures name. A fixture render is not
// wired to a handler; it only has to satisfy the required-Island
// contract with a plausible endpoint and signal.
var fixtureIsland = Island{Endpoint: "/island/apps", Signal: "apps"}

// The attrs function's shape: the four keys of the RPC contract, the
// query shared between the href and the endpoint, and push-state only
// on a GET that has a URL to write.
func TestIslandAttrsShape(t *testing.T) {
	isle := Island{Endpoint: "/island/apps", Signal: "apps"}
	read := isle.attrs("/apps?page=3&sort=name", "GET")
	want := map[string]string{
		"data-fui-rpc":        "/island/apps?page=3&sort=name",
		"data-fui-rpc-method": "GET",
		"data-fui-rpc-signal": "apps",
		"data-fui-push-state": "/apps?page=3&sort=name",
	}
	for k, v := range want {
		if read[k] != v {
			t.Errorf("%s = %q, want %q", k, read[k], v)
		}
	}
	if len(read) != len(want) {
		t.Errorf("a GET with an href carries more than the contract: %v", read)
	}

	// An endpoint with its own query joins rather than stacks one.
	joined := Island{Endpoint: "/island/apps?keep=1", Signal: "apps"}.
		attrs("/apps?page=2", "GET")
	if joined["data-fui-rpc"] != "/island/apps?keep=1&page=2" {
		t.Errorf("query join = %q", joined["data-fui-rpc"])
	}

	// A fragment never reaches the endpoint: it names a place inside
	// the document the href renders, not a place inside the region
	// the island fetches.
	frag := isle.attrs("/apps?page=3#results", "GET")
	if frag["data-fui-rpc"] != "/island/apps?page=3" {
		t.Errorf("a fragment reached the endpoint: %q", frag["data-fui-rpc"])
	}
	if frag["data-fui-push-state"] != "/apps?page=3#results" {
		t.Errorf("push-state must keep the href as written, fragment and all: %q", frag["data-fui-push-state"])
	}

	// A key present in both keeps both values in order, the
	// endpoint's first — the endpoint names the resource and the href
	// narrows it, and a silent overwrite would drop whichever pair
	// lost the race.
	both := Island{Endpoint: "/island/apps?sort=name", Signal: "apps"}.
		attrs("/apps?sort=age&page=2", "GET")
	if both["data-fui-rpc"] != "/island/apps?page=2&sort=name&sort=age" {
		t.Errorf("a shared key merged to %q, want both values with the endpoint's first", both["data-fui-rpc"])
	}

	// A mutation writes no URL: where the change lands is the server's
	// to say through X-Gofastr-Push-State.
	post := isle.attrs("/apps/blog", "POST")
	if _, ok := post["data-fui-push-state"]; ok {
		t.Error("a POST carried push-state")
	}
	if post["data-fui-rpc"] != "/island/apps" {
		t.Errorf("a mutation with no verb to carry kept the href's query: %q", post["data-fui-rpc"])
	}

	// A form's trigger has no href of its own: no query, no push-state.
	form := isle.attrs("", "GET")
	if _, ok := form["data-fui-push-state"]; ok {
		t.Error("a form trigger without an href carried push-state")
	}
	if form["data-fui-rpc"] != "/island/apps" {
		t.Errorf("form rpc = %q", form["data-fui-rpc"])
	}

	// A lower-case method is canonicalised, not copied: the runtime
	// upper-cases what it reads and the markup should say it once.
	if m := isle.attrs("", "post")["data-fui-rpc-method"]; m != "POST" {
		t.Errorf("method = %q, want POST", m)
	}
}

// An island that looks wired and is not is refused at render, where
// the mistake is a panic naming it, rather than in the browser, where
// it is a region that never updates.
func TestIslandRefusesTheUnwired(t *testing.T) {
	refuse(t, "Endpoint", func() { Island{Signal: "apps"}.attrs("", "GET") })
	refuse(t, "Endpoint", func() { Island{Endpoint: "https://evil.example/x", Signal: "apps"}.attrs("", "GET") })
	refuse(t, "Endpoint", func() { Island{Endpoint: "//evil.example/x", Signal: "apps"}.attrs("", "GET") })
	refuse(t, "Signal", func() { Island{Endpoint: "/island/apps"}.attrs("", "GET") })
}

// The contract lands on the element that keeps the href, so the
// no-script path and the island are one element — and the href is
// still there for no script. The pager's Island is optional, the
// Table posture: a plain render is a list screen's navigation (the
// URL is the truth for a list), and only an embedded pager carries
// the contract beside its hrefs.
func TestPaginationCarriesTheContractOnItsAnchors(t *testing.T) {
	plain := Pagination(PaginationProps{Page: 5, Pages: 5, Path: "/apps", PageParam: "page",
		AriaLabel: "Pages"}, nil)
	has(t, plain, `href="/apps?page=4"`, "the plain pager lost its href")
	hasNot(t, plain, "data-fui", "a list screen's plain pager carried an island contract it was not given")
	hasNot(t, plain, "data-hui-page", "a plain pager rendered a page hook nothing reads")

	island := Pagination(PaginationProps{Page: 5, Pages: 5, Path: "/apps", PageParam: "page",
		AriaLabel: "Pages", Island: fixtureIsland}, nil)
	for _, want := range []string{
		`href="/apps?page=4"`,
		`data-fui-rpc="/island/apps?page=4"`,
		`data-fui-rpc-method="GET"`,
		`data-fui-rpc-signal="apps"`,
		`data-fui-push-state="/apps?page=4"`,
		`data-hui-page="4"`,
	} {
		has(t, island, want, "the page anchor did not carry both destinations")
	}
	// The disabled end carries no contract: it goes nowhere. Attributes
	// render sorted, so a disabled anchor would show the pair.
	has(t, island, `aria-disabled="true"`, "the last page's Next is not disabled")
	hasNot(t, island, `aria-disabled="true" data-fui-`, "the disabled Next carried a contract or a page hook")
}

func TestToolbarSearchIsTheFormThatCarriesTheContract(t *testing.T) {
	got := ToolbarSearch(ToolbarSearchProps{Island: fixtureIsland}, nil,
		Input(InputProps{Type: "search", Name: "q", AriaLabel: "Search apps"}, nil))
	has(t, got, `<form data-fui-rpc="/island/apps" data-fui-rpc-method="GET" data-fui-rpc-signal="apps" method="get">`,
		"the search wrapper is not the GET form carrying the contract")
	hasNot(t, got, "data-fui-push-state", "the search form wrote a URL only the server can name")
}

// A caller cannot forge or override the contract through ExtraAttrs:
// Safe drops every data-fui-* key, so the only way in is the Island.
func TestTheContractCannotBeSmuggled(t *testing.T) {
	smuggled := Pagination(PaginationProps{Page: 2, Pages: 5, Path: "/x", PageParam: "p",
		AriaLabel: "Pages", Island: fixtureIsland,
		ExtraAttrs: map[string]string{"data-fui-rpc": "/evil"}}, nil)
	hasNot(t, smuggled, "/evil", "a request arrived through ExtraAttrs, which is for decoration")
	has(t, smuggled, `data-fui-rpc="/island/apps?p=3"`, "the island's own contract was not rendered")
}

// A component whose whole purpose is an in-page state change refuses
// the link-only render the framework's first hard rule forbids: the
// Island is required, so that render cannot be built. A partially-set
// island is not a link-only render but a broken one, and is refused by
// its own rule — whichever posture the component renders, a pager
// included: its optional Island is optional, not unvalidated.
func TestRequiredIslandsRefuseTheLinkOnlyRender(t *testing.T) {
	refuse(t, "Island", func() {
		ToolbarSearch(ToolbarSearchProps{}, nil, Input(InputProps{Name: "q", AriaLabel: "q"}, nil))
	})
	refuse(t, "Island", func() {
		Tag(TagProps{Label: "env=prod", DismissHref: "/apps?env="}, nil)
	})
	refuse(t, "Signal", func() {
		Pagination(PaginationProps{Page: 2, Pages: 5, Path: "/x", PageParam: "p", AriaLabel: "Pages",
			Island: Island{Endpoint: "/island/apps"}}, nil)
	})
}

// A form is a page of its own without an Island and an island with
// one: the optional seam leaves the plain render alone and, when set,
// puts the POST contract on the form that keeps its action.
func TestOptionalIslandsAreOptional(t *testing.T) {
	plain := Form(FormProps{Action: "/apps"}, nil)
	hasNot(t, plain, "data-fui", "a form with no Island carries framework attributes")
	isled := Form(FormProps{Action: "/apps", Island: fixtureIsland}, nil)
	has(t, isled, `action="/apps"`, "the form lost its action")
	has(t, isled, `data-fui-rpc="/island/apps" data-fui-rpc-method="POST" data-fui-rpc-signal="apps"`,
		"the form did not carry the POST contract")
}

// A Tag's dismiss removes a filter, which is an in-page state change:
// the × keeps its href for no script and carries the GET contract
// beside it, with the href's query shared so the page and the region
// answer one question. A tag with nothing to dismiss carries nothing.
func TestTagCarriesTheContractOnItsDismiss(t *testing.T) {
	got := Tag(TagProps{Label: "env=prod", DismissHref: "/apps?env=", Island: fixtureIsland}, nil)
	has(t, got, `href="/apps?env="`, "the dismiss lost its href")
	has(t, got, `data-fui-rpc="/island/apps?env=" data-fui-rpc-method="GET" data-fui-rpc-signal="apps"`,
		"the dismiss did not carry the GET contract with the href's query")
	has(t, got, `data-fui-push-state="/apps?env="`, "the dismiss did not write the URL")
	fixed := Tag(TagProps{Label: "env=prod", Island: fixtureIsland}, nil)
	hasNot(t, fixed, "data-fui", "a tag with nothing to dismiss carries the contract anyway")
}

// The one "wired and is not" failure Island.check exists to catch: a
// signal the kernel refuses to write. The runtime warns and drops the
// write, so the RPC fires and the region never updates. Refused at
// render, on the Island and on a Button's Action alike; the same rule
// a Bind already enforced.
func TestIslandRefusesAReservedSignal(t *testing.T) {
	for _, name := range []string{"__proto__", "constructor", "prototype"} {
		refuse(t, "reserved", func() {
			Pagination(PaginationProps{Page: 1, Pages: 2, Path: "/x", PageParam: "p", AriaLabel: "Pages",
				Island: Island{Endpoint: "/island/apps", Signal: name}}, nil)
		})
		refuse(t, "reserved", func() {
			Button(ButtonProps{Label: "Go", Type: "button",
				Action: html.Attrs{"data-fui-rpc": "/x", "data-fui-rpc-signal": name}}, nil)
		})
		refuse(t, "reserved", func() {
			Button(ButtonProps{Label: "Go", Type: "button",
				Action: html.Attrs{"data-fui-signal-set": name + ":1"}}, nil)
		})
	}
}

// The Form Request seam names signals too, and an empty one is not a
// name: the submit would succeed and land in a region that never
// updates. Refused beside the reserved names, with the same shape of
// message the seam's other empties get.
func TestFormRequestRefusesAnEmptySignal(t *testing.T) {
	refuse(t, "empty", func() {
		Form(FormProps{Action: "/x",
			Request: html.Attrs{"data-fui-rpc": "/x", "data-fui-rpc-signal": ""}}, nil)
	})
}

// The same-origin guard covers the whole class it names: "//host" is
// protocol-relative, and the URL parser reads a backslash the same
// way, so "/\\host" resolves off-origin and the runtime declines to
// fetch it — a dead control, which is what the guard exists to
// prevent.
func TestEndpointsRefuseTheBackslashSpelling(t *testing.T) {
	refuse(t, "same-origin", func() {
		Pagination(PaginationProps{Page: 1, Pages: 2, Path: "/x", PageParam: "p", AriaLabel: "Pages",
			Island: Island{Endpoint: "/\\evil.com/x", Signal: "apps"}}, nil)
	})
	refuse(t, "same-origin", func() {
		OptimisticAction(OptimisticActionProps{Endpoint: "/\\evil.com/x", IdleLabel: "Follow", SuccessLabel: "Following"}, nil)
	})
}

// ExtraAttrs is sanitised the way the browser reads it. Attribute
// names are case-insensitive, so a request spelled DATA-FUI-RPC is
// data-fui-rpc in the DOM; the runtime's privileged unprefixed keys —
// data-behavior, data-island and their family — carry no data-fui- to
// match; and this package's own data-hui-* hooks are the contract
// between a component and the module that binds it, so a forged one
// binds behaviour to an element never built for it. All three were
// let through by a check on the spelling as written.
func TestSafeRefusesFoldedAndPrivilegedKeys(t *testing.T) {
	got := Badge(BadgeProps{Label: "x", ExtraAttrs: html.Attrs{
		"DATA-FUI-RPC":    "/evil",
		"Data-Island":     "smuggled",
		"data-behavior":   "/evil.js",
		"data-action":     "delete",
		"data-param-id":   "1",
		"data-kiln-tool":  "t",
		"STYLE":           "display:none",
		"data-hui-reveal": "",
		"DATA-HUI-WHEN":   "forged",
		"data-testid":     "kept",
	}}, nil)
	for _, bad := range []string{"/evil", "smuggled", "delete", "data-param", "data-kiln", "display:none", "data-hui-", "forged"} {
		hasNot(t, got, bad, "a refused attribute arrived through ExtraAttrs")
	}
	has(t, got, `data-testid="kept"`, "an ordinary attribute was dropped with the refused ones")
}

// Dismissing an alert is an in-page state change like removing a
// filter: the × keeps its href for no script and carries the GET
// contract beside it, and without an Island the render is refused.
func TestAlertDismissIsAnIsland(t *testing.T) {
	got := Alert(AlertProps{Title: "Deploy failed", DismissHref: "/apps?dismiss=1", Island: fixtureIsland}, nil)
	has(t, got, `href="/apps?dismiss=1"`, "the dismiss lost its href")
	has(t, got, `data-fui-rpc="/island/apps?dismiss=1" data-fui-rpc-method="GET" data-fui-rpc-signal="apps"`,
		"the dismiss did not carry the GET contract with the href's query")
	refuse(t, "Island", func() {
		Alert(AlertProps{Title: "Deploy failed", DismissHref: "/apps?dismiss=1"}, nil)
	})
}

// A Form says its submit is an RPC exactly once. Island and Request
// are two ways of saying it, and the second one to arrive would have
// to win or lose by precedence — a silent choice between two things a
// caller meant. The refusal is the contract; this is the test nobody
// had watched fail.
func TestFormRefusesIslandAndRequestTogether(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("a Form carrying both Island and Request must panic, not pick one")
		}
		msg, _ := r.(string)
		for _, want := range []string{"Island", "Request", "pick one"} {
			if !strings.Contains(msg, want) {
				t.Errorf("panic %q does not say %q", msg, want)
			}
		}
	}()
	Form(FormProps{
		Action:  "/apps",
		Island:  Island{Endpoint: "/island/apps", Signal: "apps"},
		Request: Action{"data-fui-rpc": "/apps"},
	}, nil)
}

// Either one alone is fine: the refusal is about the pair, not about
// the fields.
func TestFormAcceptsIslandOrRequestAlone(t *testing.T) {
	isle := string(Form(FormProps{Action: "/apps", Island: Island{Endpoint: "/island/apps", Signal: "apps"}}, nil))
	if !strings.Contains(isle, "data-fui-rpc") {
		t.Errorf("an Island form carries no rpc wiring:\n%s", isle)
	}
	req := string(Form(FormProps{Action: "/apps", Request: Action{"data-fui-rpc": "/apps"}}, nil))
	if !strings.Contains(req, `data-fui-rpc="/apps"`) {
		t.Errorf("a Request form carries no rpc wiring:\n%s", req)
	}
}
