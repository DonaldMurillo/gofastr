//gofastr:allow-file(GOFASTR1801) a gofastr.yml fixture: its theme map (background, text) is spec data, not a CSS rule
package clitest

// TestAppSpecYAML is the app spec the end-to-end app tests generate from:
// every construct the generator emits, in one spec.
func TestAppSpecYAML() string {
	return `
app:
  name: Demo
  module: example.com/demo
  static_dir: public
  pwa:
    enabled: true
    short_name: Demo
    theme_color: "#F2AA4C"
  llm_md: true
  theme:
    background: "#101820"
    primary: "#F2AA4C"
    # Dark ink on the amber fill (9.2:1): the light primary the E2E app
    # ships, whose default white ink the Validate pair guard refuses.
    primary-fg: "#101820"
    text: "#F7F4EA"
  db:
    driver: sqlite
    url: file:demo.db
entities:
  - name: users
    fields:
      - name: email
        type: string
        required: true
        unique: true
  - name: posts
    crud: true
    mcp: true
    public: true
    cursor_field: id
    cursor_fields: [created_at, id]
    indices:
      - name: idx_posts_status
        columns: [status]
        unique: false
    properties:
      label: Posts
      icon: newspaper
    fields:
      - name: title
        type: string
        required: true
        max: 120
      - name: status
        type: enum
        values: [draft, published]
      - name: author_id
        type: relation
        to: users
    relations:
      - type: belongs_to
        name: author
        entity: users
        foreign_key: author_id
screens:
  - name: home
    route: /
    title: Home
    description: Demo homepage
    body:
      - type: heading
        level: 1
        text: Demo
      - type: paragraph
        text: Generated from YAML.
      - type: link
        text: Docs
        href: /docs/
        class: docs-link
      - kind: section
        props:
          label: Details
          class: details-section
        children:
          - kind: heading
            props:
              level: 3
              text: Details
          - kind: paragraph
            props:
              text: Everything is generated deterministically.
      - kind: div
        island: live_status
        props:
          class: live-status
        children:
          - kind: paragraph
            props:
              text: Island content
      - kind: button
        widget: save_button
        props:
          id: save-action
          text: Save
          class: primary-action
        actions:
          - name: save_click
            event: click
            client_js: "document.body.setAttribute('data-appspec-clicked', 'yes'); G.updateText('[data-action-result]', 'Saved by browser');"
      - kind: paragraph
        props:
          text: Waiting
          data-action-result: true
      - kind: entity_list
        text: Latest posts
        entity: posts
        fields: [title, status]
        limit: 5
        empty_text: No posts yet.
endpoints:
  - name: publish_post
    entity: posts
    method: POST
    path: "{id}/publish"
    handler: entities/posts_endpoints.go#PublishPost
    public: true
middleware:
  - request_logger
plugins:
  - name: analytics
helpers:
  - name: normalize_slug
`
}
