# Widget client correctness review — November 7, 2026

Independently reproduced at `953ccc08256e825bdc17254c463a5695a27092b6`.
One accepted conditional P3 finding; no production changes.

## C15 — Registered widgets named toString or valueOf never mount

Root: `core-ui/runtime/src/widgets.js:270` tests
`if (NS._widgets[cfg.name]) return` against an ordinary object. The actual
namespace initializes `_widgets: {}` at `frag/kernel.js:630`, so inherited
Object.prototype members are mistaken for mounted widgets. Most sibling
reads use the own-property helper documented at widgets.js:78–80.

Contract: the data-cui-open row at `core-ui/ARCHITECTURE.md:403` says a click
opens a registered widget surface. Public `widget.Mount` accepts these names
and serves their catalog, chrome and stylesheet without a name restriction.
This is a conditional naming compatibility issue, not a security finding.

The coordinator authored a fresh public-API Go probe, then executed the full,
unmodified production widgets.js in an independently authored Node DOM adapter.
The adapter uses the exact catalog and chrome fixtures emitted by the Go probe.

| Name | Catalog | Chrome/style HTTP | Fetches | Mounted | Lifecycle events |
| --- | --- | --- | --- | --- | --- |
| notes | present | 200 / 200 | 1 | yes | open, close |
| toString | present | 200 / 200 | 1 | no | none |
| valueOf | present | 200 / 200 | 1 | no | none |

The ordinary-name control proves mounting and closing in the same adapter;
the two affected names fetch chrome successfully but never insert a root or
emit an open event. Only this mount-guard root is accepted. The reviewer's
additional dismissal claim has not independently been established and is
not included.

Fresh validation: the public Go observation test, the Node observation,
and explicitly named TestComposedRuntimeIsSymbolComplete and
TestKernelMarkupMatchesGo runtime controls all pass. This is not a claim
that the broad runtime browser suite passes; its timeout remains recorded
in the findings ledger.

## Reproduction and limits

Retained executable files and emitted fixtures: `/tmp/gofastr-coordinator-widget/`.
From the repository root, create that directory and restore the two sources
below, then map the virtual Go test path to the retained source:

```sh
mkdir -p /tmp/gofastr-coordinator-widget
printf '{"Replace":{"%s/core-ui/widget/zz_coordinator_name_observation_test.go":"/tmp/gofastr-coordinator-widget/widget_observation_test.go"}}\n' "$PWD" > /tmp/gofastr-coordinator-widget/overlay.json
go test -p 2 -count=1 -overlay /tmp/gofastr-coordinator-widget/overlay.json -run '^TestCoordinatorWidgetNameObservation$' -v ./core-ui/widget/
node /tmp/gofastr-coordinator-widget/observation.cjs "$PWD"
```

These are observation assertions: a successful run pins the current defect,
not corrected behavior. The JavaScript uses a deterministic DOM/fetch adapter;
no browser rendering, layout, focus, native dialog or physical network claim
is made. The server half uses real router handlers through httptest. No
production dependencies or checkout probe files are added.

### widget_observation_test.go

```go
package widget_test

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/router"
)

func TestCoordinatorWidgetNameObservation(t *testing.T) {
	routes := router.New()
	widget.MountRuntime(routes)
	names := []string{"notes", "toString", "valueOf"}
	for _, name := range names {
		widget.Mount(routes, &widget.Definition{Name: name, Position: widget.Center})
	}
	directory := "/tmp/gofastr-coordinator-widget"
	request := func(target string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		routes.ServeHTTP(response, httptest.NewRequest("GET", target, nil))
		if response.Code != 200 {
			t.Fatalf("%s: %d %s", target, response.Code, response.Body.String())
		}
		return response
	}
	catalog := request("/__gofastr/widgets").Body.Bytes()
	var entries []struct {
		Config struct {
			Name string `json:"name"`
		} `json:"cfg"`
	}
	if err := json.Unmarshal(catalog, &entries); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		found := false
		for _, entry := range entries {
			if entry.Config.Name == name {
				found = true
			}
		}
		if !found {
			t.Fatalf("catalog omitted %s", name)
		}
		chrome := request("/core-ui/widget/" + name + "/chrome").Body.Bytes()
		if !strings.Contains(string(chrome), `data-cui-widget="`+name+`"`) {
			t.Fatalf("chrome marker missing for %s", name)
		}
		request("/core-ui/widget/" + name + "/style.css")
		if err := os.WriteFile(filepath.Join(directory, name+".html"), chrome, 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: catalog present; chrome and style both 200", name)
	}
	if err := os.WriteFile(filepath.Join(directory, "catalog.json"), catalog, 0600); err != nil {
		t.Fatal(err)
	}
}
```

### observation.cjs

```javascript
'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = process.argv[2] || process.cwd();
const directory = __dirname;
const catalog = JSON.parse(fs.readFileSync(path.join(directory, 'catalog.json'), 'utf8'));
const production = fs.readFileSync(path.join(root, 'core-ui/runtime/src/widgets.js'), 'utf8');

class Element {
  constructor(tag) { this.tagName = tag.toUpperCase(); this.attributes = new Map(); this.style = { removeProperty() {} }; }
  setAttribute(name, value) { this.attributes.set(name, String(value)); }
  getAttribute(name) { return this.attributes.get(name) ?? null; }
  removeAttribute(name) { this.attributes.delete(name); }
  querySelector() { return null; }
  querySelectorAll() { return []; }
  addEventListener() {}
  set innerHTML(html) {
    const match = html.match(/<([a-z]+)\b[^>]*data-cui-widget="([^"]+)"/);
    assert.ok(match, 'adapter requires real widget-root markup');
    const child = new Element(match[1]);
    child.setAttribute('data-cui-widget', match[2]);
    this.firstElementChild = child;
  }
}

(async () => {
  for (const name of ['notes', 'toString', 'valueOf']) {
    const children = [];
    const events = [];
    const fetches = [];
    const body = {
      appendChild(element) { element.parentNode = body; element.isConnected = true; children.push(element); },
      removeChild(element) { children.splice(children.indexOf(element), 1); element.parentNode = null; element.isConnected = false; },
    };
    const document = {
      body, head: { appendChild() {} }, documentElement: { hasAttribute() { return false; } },
      querySelector(selector) { return children.find(element => selector === `[data-cui-widget="${element.getAttribute('data-cui-widget')}"]`) || null; },
      querySelectorAll() { return []; },
      createElement(tag) { return new Element(tag); },
      dispatchEvent(event) { events.push(event.type); },
      addEventListener() {},
    };
    const runtime = {
      _widgets: {}, _modalStack: [], _popoverStack: [], _widgetCatalog: Object.fromEntries(catalog.map(entry => [entry.cfg.name, entry])),
      _navEpoch: 0, scanAndLoadCSS() {}, _originOK() { return true; },
      doc: { reattach() {}, appendBody(element) { body.appendChild(element); } },
      fetch: async target => {
        assert.equal(target, `/core-ui/widget/${name}/chrome`);
        fetches.push(target);
        return { ok: true, redirected: false, text: async () => fs.readFileSync(path.join(directory, name+'.html'), 'utf8') };
      },
    };
    const context = vm.createContext({
      window: { __gofastr: runtime, addEventListener() {} }, document,
      location: { href: 'http://fixture.invalid/page' }, CSS: { escape(value) { return value; } },
      CustomEvent: class { constructor(type, options) { this.type = type; this.detail = options.detail; } },
      AbortController,
    });
    vm.runInContext(production, context);
    await runtime.openWidget(name);
    const mounted = Object.hasOwn(runtime._widgets, name);
    assert.equal(fetches.length, 1);
    assert.equal(mounted, name === 'notes');
    assert.equal(children.length, mounted ? 1 : 0);
    assert.deepEqual(events, mounted ? ['fui:widget-open'] : []);
    runtime.closeWidget(name);
    assert.equal(children.length, 0);
    console.log(`${name}: real catalog+chrome; fetches=1, mounted=${mounted}, lifecycle=${events.join(',') || '(none)'}`);
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
```
