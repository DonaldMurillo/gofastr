package clitest

const EndpointsSettings = `endpoints:
  - name: feature_post
    entity: posts
    method: POST
    path: "{id}/feature"
    handler: entities/featured.go#FeaturePost
    description: Pin a post to the front page.
    mcp: true
  - name: search_posts
    entity: posts
    method: GET
    path: search
    handler: entities/featured.go#SearchPosts
    public: true
  - name: clear_cache
    entity: posts
    method: DELETE
    path: "{id}/cache"
    handler: entities/featured.go#ClearCache
  - name: archive_post
    entity: posts
    method: POST
    path: "{id}/archive"
    handler: entities/featured.go#ArchivePost
codegen:
  generators:
    - name: cli
      output: cmd/shop
      config:
        binary: shop
        api_prefix: ""
    - name: sdk
      config:
        targets: [js]
`
