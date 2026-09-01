package main

// Exhaustive audit of every IR-derived string's quoting in the blueprint Go
// emitters (issue #136 tail: "the entity/screen emitters' quoting of
// IR-derived strings was not audited").
//
// Method (see the brief): do NOT eyeball the emitters. Enumerate every field
// of the blueprint IR, drive a hostile value through the REAL declaration
// path (YAML -> loadBlueprint -> validateBlueprint -> renderBlueprintFiles),
// and use go/parser on the emitted bytes as the oracle. The audit table below
// is derived from the IR structs (Blueprint, BlueprintApp, BlueprintScreen,
// BlueprintBlock, BlueprintTransition, BlueprintAction, BlueprintEndpoint,
// BlueprintNamedStub, BlueprintNavItem, BlueprintSeedEntity,
// framework.EntityDeclaration) — every string field, not a sample.
//
// Re-derive the enumeration with:
//
//	grep -n 'type Blueprint' cmd/gofastr/blueprint.go
//	sed -n '33,201p' cmd/gofastr/blueprint.go        # the IR structs
//	grep -n 'Sprintf\|WriteString' cmd/gofastr/blueprint.go cmd/gofastr/generate.go \
//	  cmd/gofastr/generate_typed.go | grep -v '%q\|%d\|%t\|%w'
//
// Anti-vacuity (the repo's #1 defect class is a test that cannot fail):
//
//  1. Every site first runs a BENIGN CONTROL through the same pipeline and
//     must validate + render + leave the marker bytes in the emitted tree.
//     A site whose control never appears is classified notEmitted (dead or
//     runtime-only field) and its hostile runs are skipped, loudly.
//  2. The injection oracle is proven to fire on synthetic broken source
//     (TestAuditOracleDetectsSyntheticInjection).
//  3. The emitter's parse backstop is proven to fire
//     (TestAuditParseBackstopFires).
//
// A hostile run then has exactly two acceptable outcomes: rejected at the
// boundary (load error; the value differs from the passing control only in
// that one scalar, so the error is attributable to the hostile value) or
// emitted inertly (files render, every .go parses, no injected identifier).

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/dotenv"
	"github.com/DonaldMurillo/gofastr/framework"
)

// auditPayloads are the byte sequences that break out of Go literals. The
// marker PWNX must appear as a Go identifier for the injection to count.
var auditPayloads = []string{
	"x`+PWNX()+`y",                  // closes a raw string and calls
	`x"+PWNX()+"y`,                  // closes an interpreted string and calls
	"x\nfunc PWNX() {}\nvar z = \"", // newline: live declaration after a comment or raw literal
	"x\r\nPWNX()",                   // CRLF variant
	"*/\nfunc PWNX() {}\n/*",        // comment terminator
	"\\\"+PWNX()+\"",                // backslash + quote combo
}

const auditControl = "CtrlMark9"

// yamlQ renders s as a YAML double-quoted scalar. Control characters are
// escaped so a hostile value cannot restructure the document itself.
func yamlQ(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(fmt.Sprintf(`\x%02x`, r))
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// auditYAML is a blueprint exercising every IR section at once. Markers
// (__NAME__) are scalar placeholders; each audit site owns exactly one.
const auditYAML = `app:
  name: __APP_NAME__
  description: __APP_DESC__
  module: github.com/audit/app
  base_url: __APP_BASEURL__
  db:
    driver: sqlite
    url: __APP_DBURL__
  static_dir: __APP_STATICDIR__
  api_prefix: __APP_APIPREFIX__
  theme:
    primary: __APP_THEME_PRIMARY__
    font_heading: __APP_FONT__
    dark:
      primary: __APP_DARK_PRIMARY__
  auth:
    enabled: true
    dev_mode: true
    base_path: __AUTH_BASEPATH__
    jwt_secret: __AUTH_JWTSECRET__
  admin:
    enabled: true
    path: __ADMIN_PATH__
    role: __ADMIN_ROLE__
    login_path: __ADMIN_LOGIN__
    seed_email: __ADMIN_EMAIL__
    seed_password: __ADMIN_PASSWORD__
  pwa:
    enabled: true
    name: __PWA_NAME__
    short_name: __PWA_SHORT__
    description: __PWA_DESC__
    start_url: /
    scope: /
    display: standalone
    theme_color: __PWA_THEMECOLOR__
    background_color: __PWA_BGCOLOR__

entities:
  - name: __ENTITY_NAME__
    crud: true
    table: __ENTITY_TABLE__
    search_fields: [title]
    indices:
      - name: __INDEX_NAME__
        columns: [status]
    properties:
      label: __PROP_LABEL__
      icon: __PROP_ICON__
    fields:
      - name: __FIELD_NAME__
        type: string
        required: true
      - name: ctrlmark
        type: string
      - name: status
        type: enum
        values: [__ENUM_VALUE__, published]
        default: published
      - name: meta
        type: json
      - name: author_id
        type: relation
        to: users
      - name: published_on
        type: timestamp
    relations:
      - type: belongs_to
        name: __RELATION_NAME__
        entity: users
        foreign_key: __RELATION_FK__
  - name: users
    crud: true
    fields:
      - name: name
        type: string
        required: true

nav:
  - label: __NAV_LABEL__
    href: __NAV_HREF__
    icon: __NAV_ICON__
    role: __NAV_ROLE__
  - label: nested
    href: /isl
    items:
      - label: __NAV_ITEM_LABEL__
        href: /isl

screens:
  - name: __SCREEN_NAME__
    route: __SCREEN_ROUTE__
    title: __SCREEN1_TITLE__
    description: __SCREEN1_DESC__
    body:
      - type: heading
        level: 1
        text: __HEAD_TEXT__
      - type: paragraph
        text: __PARA_TEXT__
        class: __PARA_CLASS__
      - type: link
        text: link
        href: __LINK_HREF__
      - kind: markdown
        text: __MD_TEXT__
      - kind: callout
        text: __CALLOUT_TEXT__
        props:
          title: __CALLOUT_TITLE__
      - type: text
        text: with action
        actions:
          - name: __ACTION_NAME__
            event: click
            client_js: __ACTION_JS__
      - kind: span
        text: span body
        island: __BLOCK_ISLAND__
      - kind: span
        text: span body 2
        widget: __BLOCK_WIDGET__

  - name: dash
    route: /dash
    title: __DASH_TITLE__
    layout: app
    access:
      auth: true
      role: __ACCESS_ROLE__
    body:
      - kind: page_header
        props:
          title: __PH_TITLE__
          subtitle: __PH_SUBTITLE__
          eyebrow: __PH_EYEBROW__
      - kind: hero
        props:
          title: Hero
          cta_text: __HERO_CTA_TEXT__
          cta_href: /form
          secondary_text: __HERO2_TEXT__
          secondary_href: /tickets
          eyebrow: __HERO_EYEBROW__
      - kind: stat_card
        props:
          label: __STAT_LABEL__
          value: __STAT_VALUE__
      - kind: stat_card
        props:
          label: sourced
          source:
            entity: tickets
            group_by: status
      - kind: bar_chart
        props:
          title: __CHART_TITLE__
          source:
            entity: tickets
            group_by: __CHART_GROUPBY__
      - kind: link_button
        props:
          label: __LB_LABEL__
          href: __LB_HREF__
          variant: secondary
      - kind: pricing
        props:
          plans:
            - name: __PLAN_NAME__
              price: "$9"
              period: mo
              description: __PLAN_DESC__
              cta_text: Go
              cta_href: /form
              features: [__PLAN_FEATURE__, second]
              featured: true
      - kind: section
        props:
          heading: __SECTION_HEADING__
          eyebrow: __SECTION_EYEBROW__
          description: __SECTION_DESC__
          label: __SECTION_LABEL__
          class: __SECTION_CLASS__
          id: __SECTION_ID__
        children:
          - kind: card
            props:
              heading: __CARD_HEADING__
              text: __CARD_TEXT__
      - kind: stack
        props:
          gap: md
        children:
          - type: heading
            level: 2
            text: __CHILD_TEXT__

  - name: tickets
    route: /tickets
    body:
      - kind: entity_list
        text: __LIST_HEADING__
        entity: __BLOCK_ENTITY__
        fields: [__LIST_FIELD__, status]
        limit: 5
        empty_text: __LIST_EMPTY__
        search: __LIST_SEARCH__
        filters: [status]
        create: true

  - name: ticket
    route: /tickets/{id}
    body:
      - kind: entity_detail
        entity: tickets
        transitions:
          - label: __TRANS_LABEL__
            status: __TRANS_STATUS__
            variant: __TRANS_VARIANT__
            stamp: published_on

  - name: form
    route: /form
    body:
      - kind: entity_form
        text: __FORM_TITLE__
        entity: tickets
        mode: __FORM_MODE__
        fields: [title, status, author_id]

  - name: isl
    route: /isl
    body:
      - type: heading
        level: 2
        text: island host

  - name: login
    route: /login
    body:
      - kind: login_form
        text: __LOGIN_TEXT__
        props:
          action: __LOGIN_ACTION__
          next: __LOGIN_NEXT__
          register_href: /form

endpoints:
  - name: tickets_feed
    method: __EP_METHOD__
    path: __EP_PATH__
    entity: tickets
    handler: __EP_HANDLER__
    description: __EP_DESC__

middleware:
  - name: __MW_NAME__
    description: __MW_DESC__

plugins:
  - name: __PLUGIN_NAME__
    description: __PLUGIN_DESC__

helpers:
  - name: __HELPER_NAME__
    description: __HELPER_DESC__

seed:
  - entity: tickets
    rows:
      - title: __SEED_STRING__
        status: published
        meta: [__SEED_LIST__, x]
`

// auditControlValues maps each marker to the benign, schema-valid value its
// control run (and the default fixture) uses. Values that cross a grammar
// (colors, columns, enums) must satisfy it; markers for free-text fields can
// use the generic auditControl.
var auditControlValues = map[string]string{
	"__APP_NAME__":          "Audit App",
	"__APP_DESC__":          "audit description",
	"__APP_BASEURL__":       "https://audit.example.com",
	"__APP_DBURL__":         "file:audit.db",
	"__APP_STATICDIR__":     "static",
	"__APP_APIPREFIX__":     "api",
	"__APP_THEME_PRIMARY__": "#2500aa",
	"__APP_FONT__":          "",
	"__APP_DARK_PRIMARY__":  "#2500dd",
	"__AUTH_BASEPATH__":     "/auth",
	"__AUTH_JWTSECRET__":    "ctrl-secret",
	"__ADMIN_PATH__":        "/admin",
	"__ADMIN_ROLE__":        "admin",
	"__ADMIN_LOGIN__":       "/login",
	"__ADMIN_EMAIL__":       "admin@audit.dev",
	"__ADMIN_PASSWORD__":    "ctrl-pw",
	"__PWA_NAME__":          "Audit",
	"__PWA_SHORT__":         "PwaShortCtrl",
	"__PWA_DESC__":          "pwa desc ctrl",
	"__PWA_THEMECOLOR__":    "#2500bb",
	"__PWA_BGCOLOR__":       "#2500cc",
	"__ENTITY_TABLE__":      "tickets",
	"__INDEX_NAME__":        "idx_ctrl",
	"__PROP_LABEL__":        "Tickets",
	"__PROP_ICON__":         "ticket",
	"__ENUM_VALUE__":        "open",
	"__RELATION_NAME__":     "author",
	"__RELATION_FK__":       "author_id",
	"__NAV_LABEL__":         "Dash",
	"__NAV_ICON__":          "gauge",
	"__NAV_ROLE__":          "admin",
	"__NAV_ITEM_LABEL__":    "Nested",
	"__SCREEN1_TITLE__":     "Home",
	"__SCREEN1_DESC__":      "home desc",
	"__HEAD_TEXT__":         "heading",
	"__PARA_TEXT__":         "paragraph",
	"__PARA_CLASS__":        "ctrl-class",
	"__LINK_HREF__":         "/form",
	"__MD_TEXT__":           "# md",
	"__CALLOUT_TITLE__":     "note",
	"__CALLOUT_TEXT__":      "callout body",
	"__ACTION_NAME__":       "ctrlAction",
	"__ACTION_JS__":         "console.log(1)",
	"__BLOCK_ISLAND__":      "ctrlIsland",
	"__BLOCK_WIDGET__":      "ctrlWidget",
	"__ACCESS_ROLE__":       "admin",
	"__PH_TITLE__":          "Dashboard",
	"__PH_SUBTITLE__":       "subtitlectrl",
	"__PH_EYEBROW__":        "eyebrow",
	"__HERO_CTA_TEXT__":     "Go",
	"__HERO2_TEXT__":        "More",
	"__HERO_EYEBROW__":      "hero eyebrow",
	"__STAT_LABEL__":        "MRR",
	"__STAT_VALUE__":        "$1k",
	"__CHART_TITLE__":       "Chart",
	"__CHART_GROUPBY__":     "ctrlmark",
	"__LB_LABEL__":          "Press",
	"__LB_HREF__":           "/form",
	"__PLAN_NAME__":         "Pro",
	"__PLAN_DESC__":         "plan desc",
	"__PLAN_FEATURE__":      "feat",
	"__SECTION_HEADING__":   "Section",
	"__SECTION_EYEBROW__":   "sec eyebrow",
	"__SECTION_DESC__":      "sec desc",
	"__SECTION_LABEL__":     "sec label",
	"__SECTION_CLASS__":     "sec-class",
	"__SECTION_ID__":        "sec-id",
	"__CARD_HEADING__":      "Card",
	"__CARD_TEXT__":         "card body",
	"__CHILD_TEXT__":        "child heading",
	"__LIST_HEADING__":      "Tickets",
	"__LIST_FIELD__":        "ctrlmark",
	"__LIST_EMPTY__":        "none yet",
	"__LIST_SEARCH__":       "ctrlmark",
	"__TRANS_LABEL__":       "Publish",
	"__TRANS_STATUS__":      "published",
	"__TRANS_VARIANT__":     "primary",
	"__FORM_TITLE__":        "New ticket",
	"__FORM_MODE__":         "create",
	"__LOGIN_TEXT__":        "Sign in",
	"__LOGIN_ACTION__":      "/auth/login",
	"__LOGIN_NEXT__":        "/",
	"__EP_DESC__":           "feeddescctrl",
	"__MW_NAME__":           "requestLogger",
	"__MW_DESC__":           "mwdescctrl",
	"__PLUGIN_NAME__":       "metrics",
	"__PLUGIN_DESC__":       "plugindescctrl",
	"__HELPER_NAME__":       "gravatar",
	"__HELPER_DESC__":       "helperdescctrl",
	"__SEED_STRING__":       "first ticket",
	"__SEED_LIST__":         "tagged",
	"__ENTITY_NAME__":       "tickets",
	"__FIELD_NAME__":        "title",
	"__SCREEN_NAME__":       "home",
	"__SCREEN_ROUTE__":      "/",
	"__DASH_TITLE__":        "DashCtrl",
	"__BLOCK_ENTITY__":      "tickets",
	"__EP_METHOD__":         "GET",
	"__EP_PATH__":           "/feed",
	"__EP_HANDLER__":        "ticketsFeed",
	"__NAV_HREF__":          "/dash",
}

// Every IR string field the emitters read, one site each.
var auditSites = []auditSite{
	// app.*
	{"app.name"},
	{"app.description"},
	{"app.base_url"},
	{"app.db.url"},
	{"app.static_dir"},
	{"app.api_prefix"},
	{"app.theme.primary"},
	{"app.theme.font_heading"},
	{"app.theme.dark.primary"},
	{"app.auth.base_path"},
	{"app.auth.jwt_secret"},
	{"app.admin.path"},
	{"app.admin.role"},
	{"app.admin.login_path"},
	{"app.admin.seed_email"},
	{"app.admin.seed_password"},
	{"app.pwa.name"},
	{"app.pwa.short_name"},
	{"app.pwa.description"},
	{"app.pwa.theme_color"},
	{"app.pwa.background_color"},
	// entities.*
	{"entity.name"},
	{"entity.table"},
	{"entity.index.name"},
	{"entity.properties.label"},
	{"entity.properties.icon"},
	{"field.name"},
	{"field.enum_value"},
	{"relation.name"}, // known non-finding: re-derived here
	{"relation.foreign_key"},
	// screens.*
	{"screen.name"},
	{"screen.route"},
	{"screen.title.access-gated"}, // WithTitle sink: only gated screens mount through it
	{"screen.title"},
	{"screen.description"},
	{"screen.access.role"},
	// blocks: generic
	{"block.heading.text"},
	{"block.text"},
	{"block.class"},
	{"block.href"},
	{"block.markdown.text"},
	{"block.callout.title"},
	{"block.callout.text"},
	{"block.action.name"},
	{"block.action.client_js"},
	{"block.island"},
	{"block.widget"},
	// blocks: catalog props
	{"props.page_header.title"},
	{"props.page_header.subtitle"},
	{"props.page_header.eyebrow"},
	{"props.hero.cta_text"},
	{"props.hero.secondary_text"},
	{"props.hero.eyebrow"},
	{"props.stat_card.label"},
	{"props.stat_card.value"},
	{"props.chart.title"},
	{"props.chart.group_by"},
	{"props.link_button.label"},
	{"props.link_button.href"},
	{"props.pricing.plan.name"},
	{"props.pricing.plan.description"},
	{"props.pricing.plan.feature"},
	{"props.section.heading"},
	{"props.section.eyebrow"},
	{"props.section.description"},
	{"props.section.label"},
	{"props.section.class"},
	{"props.section.id"},
	{"props.card.heading"},
	{"props.card.text"},
	{"block.child.text"},
	// blocks: entity_list / detail / form
	{"block.entity"},
	{"entity_list.heading"},
	{"entity_list.fields[]"},
	{"entity_list.empty_text"},
	{"entity_list.search"},
	{"entity_detail.transition.label"},
	{"entity_detail.transition.status"},
	{"entity_detail.transition.variant"},
	{"entity_form.title"},
	{"entity_form.mode"},
	{"login_form.text"},
	{"login_form.props.action"},
	{"login_form.props.next"},
	// nav.*
	{"nav.label"},
	{"nav.href"},
	{"nav.icon"},
	{"nav.role"},
	{"nav.items[].label"},
	// seed.*
	{"seed.row.string"},
	{"seed.row.list_item"},
	// endpoints / stubs
	{"endpoint.method"},
	{"endpoint.path"},
	{"endpoint.handler"},
	{"endpoint.description"},
	{"middleware.name"},
	{"middleware.description"},
	{"plugin.name"},
	{"plugin.description"},
	{"helper.name"},
	{"helper.description"},
}

// auditSite is one IR field under test; marker is derived from name by
// convention in markerFor below, so the table stays readable.
type auditSite struct {
	name string
}

// siteMarker maps a site name to its YAML marker. Kept explicit next to the
// control values: the marker is the single source of truth for both.
func siteMarker(name string) string {
	pairs := map[string]string{
		"app.name": "__APP_NAME__", "app.description": "__APP_DESC__",
		"app.base_url": "__APP_BASEURL__", "app.db.url": "__APP_DBURL__",
		"app.static_dir": "__APP_STATICDIR__", "app.api_prefix": "__APP_APIPREFIX__",
		"app.theme.primary": "__APP_THEME_PRIMARY__", "app.theme.font_heading": "__APP_FONT__",
		"app.theme.dark.primary": "__APP_DARK_PRIMARY__", "app.auth.base_path": "__AUTH_BASEPATH__",
		"app.auth.jwt_secret": "__AUTH_JWTSECRET__", "app.admin.path": "__ADMIN_PATH__",
		"app.admin.role": "__ADMIN_ROLE__", "app.admin.login_path": "__ADMIN_LOGIN__",
		"app.admin.seed_email": "__ADMIN_EMAIL__", "app.admin.seed_password": "__ADMIN_PASSWORD__",
		"app.pwa.name": "__PWA_NAME__", "app.pwa.short_name": "__PWA_SHORT__",
		"app.pwa.description": "__PWA_DESC__", "app.pwa.theme_color": "__PWA_THEMECOLOR__",
		"app.pwa.background_color": "__PWA_BGCOLOR__",
		"entity.table":             "__ENTITY_TABLE__", "entity.index.name": "__INDEX_NAME__",
		"entity.properties.label": "__PROP_LABEL__", "entity.properties.icon": "__PROP_ICON__",
		"field.enum_value": "__ENUM_VALUE__", "relation.name": "__RELATION_NAME__",
		"relation.foreign_key": "__RELATION_FK__",
		"screen.title":         "__SCREEN1_TITLE__", "screen.description": "__SCREEN1_DESC__",
		"screen.access.role": "__ACCESS_ROLE__",
		"block.heading.text": "__HEAD_TEXT__", "block.text": "__PARA_TEXT__",
		"block.class": "__PARA_CLASS__", "block.href": "__LINK_HREF__",
		"block.markdown.text": "__MD_TEXT__", "block.callout.title": "__CALLOUT_TITLE__",
		"block.callout.text": "__CALLOUT_TEXT__", "block.action.name": "__ACTION_NAME__",
		"block.action.client_js": "__ACTION_JS__", "block.island": "__BLOCK_ISLAND__",
		"block.widget":            "__BLOCK_WIDGET__",
		"props.page_header.title": "__PH_TITLE__", "props.page_header.subtitle": "__PH_SUBTITLE__",
		"props.page_header.eyebrow": "__PH_EYEBROW__", "props.hero.cta_text": "__HERO_CTA_TEXT__",
		"props.hero.secondary_text": "__HERO2_TEXT__", "props.hero.eyebrow": "__HERO_EYEBROW__",
		"props.stat_card.label": "__STAT_LABEL__", "props.stat_card.value": "__STAT_VALUE__",
		"props.chart.title": "__CHART_TITLE__", "props.chart.group_by": "__CHART_GROUPBY__",
		"props.link_button.label": "__LB_LABEL__", "props.link_button.href": "__LB_HREF__",
		"props.pricing.plan.name": "__PLAN_NAME__", "props.pricing.plan.description": "__PLAN_DESC__",
		"props.pricing.plan.feature": "__PLAN_FEATURE__", "props.section.heading": "__SECTION_HEADING__",
		"props.section.eyebrow": "__SECTION_EYEBROW__", "props.section.description": "__SECTION_DESC__",
		"props.section.label": "__SECTION_LABEL__", "props.section.class": "__SECTION_CLASS__",
		"props.section.id": "__SECTION_ID__", "props.card.heading": "__CARD_HEADING__",
		"props.card.text": "__CARD_TEXT__", "block.child.text": "__CHILD_TEXT__",
		"entity_list.heading": "__LIST_HEADING__", "entity_list.fields[]": "__LIST_FIELD__",
		"entity_list.empty_text": "__LIST_EMPTY__", "entity_list.search": "__LIST_SEARCH__",
		"entity_detail.transition.label": "__TRANS_LABEL__", "entity_detail.transition.status": "__TRANS_STATUS__",
		"entity_detail.transition.variant": "__TRANS_VARIANT__", "entity_form.title": "__FORM_TITLE__",
		"entity_form.mode": "__FORM_MODE__", "login_form.text": "__LOGIN_TEXT__",
		"login_form.props.action": "__LOGIN_ACTION__", "login_form.props.next": "__LOGIN_NEXT__",
		"nav.label": "__NAV_LABEL__", "nav.icon": "__NAV_ICON__", "nav.role": "__NAV_ROLE__",
		"nav.items[].label": "__NAV_ITEM_LABEL__",
		"seed.row.string":   "__SEED_STRING__", "seed.row.list_item": "__SEED_LIST__",
		"endpoint.description": "__EP_DESC__",
		"middleware.name":      "__MW_NAME__", "middleware.description": "__MW_DESC__",
		"plugin.name": "__PLUGIN_NAME__", "plugin.description": "__PLUGIN_DESC__",
		"helper.name": "__HELPER_NAME__", "helper.description": "__HELPER_DESC__",
		"entity.name": "__ENTITY_NAME__", "field.name": "__FIELD_NAME__",
		"screen.name": "__SCREEN_NAME__", "screen.route": "__SCREEN_ROUTE__",
		"screen.title.access-gated": "__DASH_TITLE__",
		"block.entity":              "__BLOCK_ENTITY__",
		"endpoint.method":           "__EP_METHOD__", "endpoint.path": "__EP_PATH__",
		"endpoint.handler": "__EP_HANDLER__", "nav.href": "__NAV_HREF__",
	}
	return pairs[name]
}

// buildAuditYAML returns the base blueprint with the given marker replaced by
// value (as a YAML scalar) and every other marker set to its control value.
func buildAuditYAML(marker, value string) string {
	y := auditYAML
	for m, ctrl := range auditControlValues {
		repl := yamlQ(ctrl)
		if m == marker {
			repl = yamlQ(value)
		}
		y = strings.ReplaceAll(y, m, repl)
	}
	return y
}

// loadAudit renders the audit blueprint with marker set to value through the
// real declaration path: temp file -> loadBlueprint (validate) ->
// renderBlueprintFiles.
func loadAudit(t *testing.T, marker, value string) (files []generatedFile, rejected error) {
	t.Helper()
	y := buildAuditYAML(marker, value)
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	if err := os.WriteFile(path, []byte(y), 0o600); err != nil {
		t.Fatalf("write blueprint: %v", err)
	}
	bp, err := loadBlueprint(path)
	if err != nil {
		return nil, err
	}
	fs, rerr := renderBlueprintFiles(bp)
	if rerr != nil {
		return nil, fmt.Errorf("render refused (fail-closed): %w", rerr)
	}
	return fs, nil
}

// auditControlFor returns the benign value a site's control run uses.
func auditControlFor(marker string) string {
	if v, ok := auditControlValues[marker]; ok && v != "" {
		return v
	}
	return auditControl
}

// auditInjectedIdent reports whether src contains the marker as a Go
// identifier (the injection oracle), and whether it parses at all.
func auditInjectedIdent(name, src string) (injected bool, parseErr error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.AllErrors)
	if err != nil {
		return false, err
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && strings.Contains(id.Name, "PWNX") {
			found = true
			return false
		}
		return true
	})
	return found, nil
}

// markerInFiles reports whether the (case-folded) marker appears anywhere in
// the emitted tree, so a control run can prove the site actually emits.
func markerInFiles(files []generatedFile, marker string) bool {
	fold := strings.ToLower(marker)
	for _, f := range files {
		if strings.Contains(strings.ToLower(f.content), fold) {
			return true
		}
	}
	return false
}

func TestAuditBlueprintEmitterQuotingExhaustive(t *testing.T) {
	// No network in the audit: font woff2 fetching is stubbed out.
	prev := fontFetcher
	fontFetcher = func(string) ([]byte, error) { return nil, fmt.Errorf("offline") }
	t.Cleanup(func() { fontFetcher = prev })

	// Phase 1: controls. Prove every site is reachable and non-vacuous.
	emits := map[string]bool{}
	for _, site := range auditSites {
		site := site
		t.Run("control/"+site.name, func(t *testing.T) {
			marker := siteMarker(site.name)
			ctrl := auditControlFor(marker)
			files, err := loadAudit(t, marker, ctrl)
			if err != nil {
				t.Fatalf("CONTROL FAILED for %s: the audit blueprint does not pass the real pipeline with a benign value (%q): %v.\n"+
					`Every hostile result for this site would be vacuous; fix the fixture.`, site.name, ctrl, err)
			}
			emits[site.name] = markerInFiles(files, ctrl)
		})
	}

	// Phase 2: hostile payloads per site.
	for _, site := range auditSites {
		site := site
		if !emits[site.name] {
			// Distinguish "not emitted by these emitters" from a broken
			// control: the control subtest above already failed loudly if the
			// pipeline broke, so reaching here means the value never lands in
			// any generated file. Record it; there is nothing to break out of.
			t.Logf("site %s: control value never appears in the emitted tree (field unused by the Go emitters); hostile runs skipped", site.name)
			continue
		}
		marker := siteMarker(site.name)
		for _, payload := range auditPayloads {
			payload := payload
			label := strings.NewReplacer("\n", "N", "\r", "R", "`", "BT", `"`, "Q", "\\", "S", "/", "SL", "*", "ST").Replace(payload)
			t.Run(site.name+"/"+label, func(t *testing.T) {
				files, err := loadAudit(t, marker, payload)
				if err != nil {
					// Rejected at the boundary (or the emitter refused
					// fail-closed). Acceptable: nothing was generated.
					t.Logf("outcome: %s / %q: rejected or fail-closed: %v", site.name, payload, err)
					return
				}
				t.Logf("outcome: %s / %q: emitted inertly (%d files)", site.name, payload, len(files))
				for _, f := range files {
					if !strings.HasSuffix(f.name, ".go") {
						continue
					}
					if !strings.Contains(f.content, "PWNX") {
						continue
					}
					injected, perr := auditInjectedIdent(f.name, f.content)
					if perr != nil {
						t.Fatalf("SECURITY [injection] %s: emitted %s does not parse: %v", site.name, f.name, perr)
					}
					if injected {
						t.Fatalf("SECURITY [injection] %s: hostile value left its literal and became an identifier in %s", site.name, f.name)
					}
				}
			})
		}
	}
}

// TestAuditEnvRoundTrip pins the .env sink: a hostile secret value must
// survive the generated .env byte-exactly when read back by the loader the
// generated app itself uses, and must not introduce new variable assignments.
func TestAuditEnvRoundTrip(t *testing.T) {
	prev := fontFetcher
	fontFetcher = func(string) ([]byte, error) { return nil, fmt.Errorf("offline") }
	t.Cleanup(func() { fontFetcher = prev })
	for _, secret := range auditPayloads {
		files, err := loadAudit(t, "__AUTH_JWTSECRET__", secret)
		if err != nil {
			continue // rejected upstream; nothing to round-trip
		}
		var env []generatedFile
		for _, f := range files {
			if f.name == ".env" {
				env = append(env, f)
			}
		}
		if len(env) == 0 {
			continue
		}
		dir := t.TempDir()
		path := filepath.Join(dir, ".env")
		if err := os.WriteFile(path, []byte(env[0].content), 0o600); err != nil {
			t.Fatal(err)
		}
		vals, err := dotenv.Load(path)
		if err != nil {
			t.Fatalf("SECURITY [env] hostile jwt_secret produced an unloadable .env: %v", err)
		}
		if got := vals["JWT_SECRET"]; got != secret {
			t.Fatalf("SECURITY [env] jwt_secret did not round-trip: wrote %q, read back %q", secret, got)
		}
		for key := range vals {
			switch key {
			case "JWT_SECRET", "DATABASE_URL", "ADMIN_SEED_PASSWORD":
			default:
				t.Fatalf("SECURITY [env] hostile jwt_secret injected a new assignment %q into .env", key)
			}
		}
	}
}

// TestAuditOracleDetectsSyntheticInjection proves the oracle can fail
// (CLAUDE.md hard rule 11): source where the marker escaped its literal is
// flagged, source where it stayed data is not. If this test fails, every
// "clean" verdict above is worthless.
func TestAuditOracleDetectsSyntheticInjection(t *testing.T) {
	bad := "package main\n\nvar s = `x` + PWNX() + `y`\n"
	injected, perr := auditInjectedIdent("bad.go", bad)
	if perr != nil {
		t.Fatalf("oracle could not parse its own synthetic source: %v", perr)
	}
	if !injected {
		t.Fatal("ORACLE IS VACUOUS: synthetic breakout source was not flagged")
	}
	badInterp := "package main\n\nvar s = \"x\" + PWNX() + \"y\"\n"
	injected, _ = auditInjectedIdent("bad2.go", badInterp)
	if !injected {
		t.Fatal("ORACLE IS VACUOUS: interpreted-string breakout not flagged")
	}
	good := "package main\n\nvar s = \"x`+PWNX()+`y\"\n"
	injected, perr = auditInjectedIdent("good.go", good)
	if perr != nil {
		t.Fatalf("oracle broke on inert source: %v", perr)
	}
	if injected {
		t.Fatal("oracle false-positive: payload inside a quoted literal is data, not code")
	}
}

// TestAuditParseBackstopFires proves the emitter-side backstop
// (assertBlueprintGoParses) actually fires on a broken file rather than
// silently passing everything.
func TestAuditParseBackstopFires(t *testing.T) {
	err := assertBlueprintGoParses([]generatedFile{
		{name: "broken.go", content: "package main\nfunc ( {\n"},
		{name: "fine.go", content: "package main\n"},
	})
	if err == nil {
		t.Fatal("BACKSTOP IS VACUOUS: assertBlueprintGoParses accepted unparseable Go")
	}
	if !strings.Contains(err.Error(), "broken.go") {
		t.Fatalf("backstop error does not name the offending file: %v", err)
	}
}

// TestAuditUnvalidatedEmitterPath drives hostile NAMES through the emitters
// with no validator in front (the loadBlueprintPath(path, false) surface that
// generate --add uses before re-validation, and any struct-level caller).
// Every identifier-shaped field must either keep the output parseable AND
// inert, or make renderBlueprintFiles refuse (fail-closed via
// assertBlueprintGoParses). Silent executable output is the finding.
func TestAuditUnvalidatedEmitterPath(t *testing.T) {
	prev := fontFetcher
	fontFetcher = func(string) ([]byte, error) { return nil, fmt.Errorf("offline") }
	t.Cleanup(func() { fontFetcher = prev })

	hostileNames := []string{
		"x`+PWNX()+`y",
		`x"+PWNX()+"y`,
		"x\nfunc PWNX() {}\nvar z = \"",
	}
	base := func() Blueprint {
		return Blueprint{
			App: BlueprintApp{Name: "app", Module: "m"},
			Entities: []framework.EntityDeclaration{{
				Name:   "tickets",
				Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
			}},
			Screens: []BlueprintScreen{
				{Name: "home", Route: "/", Type: "page"},
			},
		}
	}
	for _, hn := range hostileNames {
		for _, mutate := range []struct {
			what string
			mut  func(*Blueprint, string)
		}{
			{"entity.name", func(b *Blueprint, v string) { b.Entities[0].Name = v }},
			{"field.name", func(b *Blueprint, v string) { b.Entities[0].Fields[0].Name = v }},
			{"relation.name", func(b *Blueprint, v string) {
				b.Entities = append(b.Entities, framework.EntityDeclaration{
					Name:   "targets",
					Fields: []framework.FieldDeclaration{{Name: "label", Type: "string"}},
				})
				b.Entities[0].Relations = []framework.Relation{{
					Type: framework.RelManyToOne, Name: v, Entity: "targets",
				}}
			}},
			{"screen.name", func(b *Blueprint, v string) { b.Screens[0].Name = v }},
			{"endpoint.handler", func(b *Blueprint, v string) {
				b.Endpoints = []BlueprintEndpoint{{Name: "e", Method: "GET", Path: "/x", Handler: v}}
			}},
		} {
			label := strings.NewReplacer("\n", "N", "`", "BT", `"`, "Q").Replace(mutate.what + "/" + hn)
			t.Run(label, func(t *testing.T) {
				bp := base()
				mutate.mut(&bp, hn)
				files, err := renderBlueprintFiles(bp)
				if err != nil {
					return // refused, fail-closed: acceptable
				}
				for _, f := range files {
					if !strings.HasSuffix(f.name, ".go") || !strings.Contains(f.content, "PWNX") {
						continue
					}
					injected, perr := auditInjectedIdent(f.name, f.content)
					if perr != nil {
						t.Fatalf("SECURITY [unvalidated-path] %s: emitted %s does not parse: %v", mutate.what, f.name, perr)
					}
					if injected {
						t.Fatalf("SECURITY [unvalidated-path] %s: hostile name became an identifier in %s", mutate.what, f.name)
					}
				}
			})
		}
	}
}
