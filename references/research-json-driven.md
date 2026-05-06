# JSON-Driven & Declarative Framework Architectures

## Research for GoFastr: An AI-First, JSON-Defined Web Framework

> **Thesis:** AI models are exceptional at producing structured JSON. If a framework's primary interface is JSON (not code), then AI can build complete applications in a single shot — and humans can understand, audit, and extend them. Go validates the JSON, compiles the app, and serves it as a single binary.

---

## Table of Contents

1. [Declarative UI Frameworks](#1-declarative-ui-frameworks)
2. [JSON Schema as a Universal Contract](#2-json-schema-as-a-universal-contract)
3. [Config-Driven Backend Frameworks](#3-config-driven-backend-frameworks)
4. [Object/Page Definition Patterns](#4-objectpage-definition-patterns)
5. [Composable Architecture Patterns](#5-composable-architecture-patterns)
6. [The "AI Outputs JSON → Framework Builds App" Thesis](#6-the-ai-outputs-json--framework-builds-app-thesis)
7. [Practical JSON-Driven App Definitions](#7-practical-json-driven-app-definitions)
8. [Hybrid Approach: JSON Definitions + Go Extensions](#8-hybrid-approach-json-definitions--go-extensions)
9. [Concrete Recommendations](#9-concrete-recommendations)

---

## 1. Declarative UI Frameworks

### Flutter's Widget Tree (JSON-Serializable)

Flutter's entire UI is a tree of immutable widget descriptions. Every widget is a declarative configuration object — a `build()` method returns a *description* of the UI, not imperative DOM mutations.

```dart
// Flutter widget tree — this is essentially structured data
Scaffold(
  appBar: AppBar(title: Text('Posts')),
  body: ListView(
    children: posts.map((p) => ListTile(
      title: Text(p.title),
      subtitle: Text(p.author),
    )).toList(),
  ),
  floatingActionButton: FloatingActionButton(
    onPressed: () => nav.push('/posts/new'),
    child: Icon(Icons.add),
  ),
)
```

**Why it matters for GoFastr:**

- The widget tree is a pure data structure — it's trivially serializable to JSON
- Flutter's approach proves that an entire UI can be described as a nested configuration object
- Google's Flutter Dynamic Widgets project already ships JSON → Flutter rendering
- The pattern: **component name + props + children** is universal across all declarative UI

**The universal primitive:**

```json
{
  "type": "Scaffold",
  "props": {
    "appBar": {"type": "AppBar", "props": {"title": "Posts"}},
    "body": {
      "type": "ListView",
      "children": [
        {"type": "PostCard", "props": {"postId": 1}},
        {"type": "PostCard", "props": {"postId": 2}}
      ]
    }
  }
}
```

### SwiftUI's Declarative DSL

SwiftUI proved that Apple — the most opinionated platform company — would bet on declarative UI:

```swift
NavigationView {
  List(posts) { post in
    NavigationLink(destination: PostDetail(post: post)) {
      VStack(alignment: .leading) {
        Text(post.title).font(.headline)
        Text(post.author).font(.subheadline)
      }
    }
  }
  .navigationTitle("Posts")
}
```

**Key insights:**

- SwiftUI's DSL compiles to a *description* that the runtime renders
- The `.font(.headline)` modifier pattern is really just key-value props on a component
- The SwiftUI runtime diffing engine is the "framework" — the DSL is just a convenient syntax
- **If you strip the Swift syntax, it's a tree of typed objects with properties** — exactly what JSON expresses

### Android Jetpack Compose's Declarative Model

Compose took the same bet as SwiftUI but for Android:

```kotlin
@Composable
fun PostList(posts: List<Post>) {
    LazyColumn {
        items(posts) { post ->
            PostCard(
                post = post,
                onClick = { navController.navigate("posts/${post.id}") }
            )
        }
    }
}
```

**Key insights:**

- `@Composable` functions are really factory functions that emit a node tree
- The Compose runtime handles diffing, recomposition, and state management
- The composition is a tree of `ComposeNode` objects — pure data
- Google's Compose for Web and Compose Multiplatform prove the model is platform-agnostic

### React Server Components (Serialized Component Trees)

RSC is the most direct precedent for "JSON describes a UI that a runtime renders":

- Server components produce a **serialized component tree** — a stream of JSON-like objects
- The client runtime deserializes and renders this tree
- The wire format is literally: `{ type: "div", props: { children: [...] } }`
- This is the same format as Flutter widgets, SwiftUI views, and Compose nodes

**RSC wire format (simplified):**

```
0:["$","div",null,{"children":[
  ["$","h1",null,{"children":"Posts"}],
  ["$","ul",null,{"children":[
    ["$","li",null,{"children":[
      ["$","a",null,{"href":"/posts/1","children":"First Post"}]
    ]}]
  ]}]
]}]
```

**Why it matters:** RSC proves that a serialized tree format can drive a production UI framework at Meta scale. The "server produces JSON, client renders" pattern is exactly GoFastr's model.

### HTMX — Declarative Behavior via Attributes

HTMX is the most radical "no-code" web framework: you describe *behavior* in HTML attributes:

```html
<button hx-get="/api/posts" hx-target="#post-list" hx-swap="innerHTML">
  Load Posts
</button>

<form hx-post="/api/posts" hx-target="#result" hx-swap="outerHTML">
  <input name="title" required>
  <textarea name="body"></textarea>
  <button type="submit">Create Post</button>
</form>

<div hx-get="/api/notifications" hx-trigger="every 5s" hx-swap="innerHTML">
</div>
```

**Key insights:**

- HTMX proves you can describe rich interactivity *without JavaScript*
- The attribute pattern (`hx-get`, `hx-post`, `hx-target`, `hx-trigger`) is a declarative DSL embedded in HTML
- **AI can produce HTMX attributes trivially** — they're just key-value pairs
- The pattern: **event + action + target** covers 90% of web interactivity
- GoFastr should adopt this pattern: server-rendered HTML + declarative behavior attributes

### Alpine.js — Declarative DOM Enhancement

Alpine.js adds reactivity to HTML via declarative attributes:

```html
<div x-data="{ posts: [], loading: true }"
     x-init="fetch('/api/posts').then(r => r.json()).then(d => { posts = d; loading = false })">
  <template x-if="loading">
    <p>Loading...</p>
  </template>
  <template x-for="post in posts">
    <div>
      <h2 x-text="post.title"></h2>
      <p x-text="post.body"></p>
    </div>
  </template>
</div>
```

**Key insights:**

- `x-data` declares state, `x-bind`/`x-text`/`x-for` declare rendering
- This is a **declarative reactive system** — the DOM is a function of state
- Alpine's approach works for small-to-medium interactivity without a build step
- Combined with HTMX, you get a full-stack declarative UI with zero JavaScript files

### Astro's Content Collections

Astro pioneered "structured data → typed pages":

```typescript
// src/content/config.ts
import { defineCollection, z } from 'astro:content';

const posts = defineCollection({
  schema: z.object({
    title: z.string(),
    body: z.string(),
    author: z.string(),
    published: z.boolean().default(false),
  }),
});

export const collections = { posts };
```

Content is defined as structured data (Markdown + frontmatter), and Astro generates typed pages. This is essentially **JSON Schema → pages**.

**Why it matters:** Astro proves that structured data definitions can drive page generation with full type safety. The schema IS the contract.

---

## 2. JSON Schema as a Universal Contract

### OpenAPI/Swagger — Define an API in JSON, Get Everything

OpenAPI is the most successful "JSON as source of truth" project in web development:

```yaml
# A single YAML/JSON file produces:
# - Route handlers (skeletons)
# - Request/response validation
# - Interactive docs (Swagger UI)
# - Client SDKs in 20+ languages
# - Mock servers
# - Type definitions
openapi: "3.1.0"
paths:
  /posts:
    get:
      summary: List posts
      parameters:
        - name: limit
          in: query
          schema:
            type: integer
            default: 20
      responses:
        "200":
          description: A list of posts
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: "#/components/schemas/Post"
    post:
      summary: Create a post
      requestBody:
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/PostInput"
      responses:
        "201":
          description: Created
```

**What OpenAPI proves:**

- A single JSON/YAML file can generate: routes, validation, docs, clients, mocks, and types
- The JSON is the **single source of truth** — everything else is derived
- This works at massive scale (AWS, Stripe, GitHub all use OpenAPI)
- **AI is exceptional at producing OpenAPI specs** — it's structured data with well-known patterns

### JSON Forms — Render Forms from JSON Schema

The [JSON Forms](https://jsonforms.io/) project renders complete forms from JSON Schema + a UI schema:

```json
{
  "schema": {
    "type": "object",
    "properties": {
      "title": { "type": "string", "minLength": 1, "maxLength": 200 },
      "body": { "type": "string", "format": "textarea" },
      "published": { "type": "boolean", "default": false },
      "tags": {
        "type": "array",
        "items": { "type": "string" },
        "maxItems": 5
      }
    },
    "required": ["title"]
  },
  "uischema": {
    "type": "VerticalLayout",
    "elements": [
      { "type": "Control", "scope": "#/properties/title" },
      { "type": "Control", "scope": "#/properties/body", "options": { "multi": true } },
      { "type": "Control", "scope": "#/properties/published" },
      { "type": "Control", "scope": "#/properties/tags" }
    ]
  }
}
```

**What this proves:** A form is a pure function of its schema. If you can describe the data shape, you can render the form — including validation, labels, error messages, and layout. Every CRUD app is 80% forms.

### Prisma Schema → Database + Types + Client

Prisma's `.prisma` schema is a declarative definition that produces:

```prisma
model Post {
  id        Int      @id @default(autoincrement())
  title     String
  body      String?
  author    User     @relation(fields: [authorId], references: [id])
  authorId  Int
  published Boolean  @default(false)
  createdAt DateTime @default(now())
  updatedAt DateTime @updatedAt

  @@index([published, createdAt])
}
```

From this single definition, Prisma generates:
- **Database migrations** (SQL schema)
- **TypeScript types** (fully typed client)
- **Query client** (type-safe queries)
- **Referential integrity** constraints

**The key insight:** The schema IS the code. Everything else is derived. This is the pattern GoFastr should follow.

### Tailwind Config → Design System

```javascript
// tailwind.config.js — a JSON-serializable design system
module.exports = {
  theme: {
    colors: {
      primary: { 50: '#eff6ff', 500: '#3b82f6', 900: '#1e3a8a' },
      surface: { DEFAULT: '#ffffff', dark: '#1a1a2e' },
    },
    fontFamily: {
      sans: ['Inter', 'system-ui', 'sans-serif'],
      mono: ['JetBrains Mono', 'monospace'],
    },
    extend: {
      spacing: { 18: '4.5rem' },
    },
  },
}
```

A single config produces: utility classes, design tokens, responsive variants, dark mode, and a consistent visual system. **The config is the design system.**

### How Far Can JSON Schema Go as a "Programming Language"?

JSON Schema can express:

| Concept | JSON Schema Support | Example |
|---------|-------------------|---------|
| Types | ✅ Full | `string`, `number`, `boolean`, `array`, `object` |
| Validation | ✅ Full | `minLength`, `pattern`, `minimum`, `required` |
| Relations | ⚠️ Via extensions | `$ref` for self-references, custom `relation` keyword |
| Logic | ⚠️ Limited | `allOf`, `anyOf`, `oneOf`, `if/then/else` |
| Computation | ❌ None | Cannot express transformations or derived values |
| Control flow | ❌ None | No loops, conditionals, or branching |
| Side effects | ❌ None | Cannot express I/O, network calls, or mutations |
| Permissions | ⚠️ Via extensions | Custom keywords like `x-permission` |
| UI hints | ⚠️ Via extensions | Custom keywords like `x-widget`, `x-label` |

**The boundary:** JSON Schema is excellent for describing *data shapes and constraints*. It cannot express *behavior* or *computation*. This is exactly why GoFastr needs the hybrid approach: **JSON for structure, Go for behavior**.

---

## 3. Config-Driven Backend Frameworks

### Firebase — Define Rules in JSON, Get a Backend

Firebase's `firebase.json` + `firestore.rules` define an entire backend:

```json
// firestore.rules — declarative security
rules_version = '2';
service cloud.firestore {
  match /databases/{database}/documents {
    match /posts/{postId} {
      allow read: if true;
      allow create: if request.auth != null;
      allow update, delete: if request.auth != null
                             && request.auth.uid == resource.data.authorId;
    }
  }
}
```

**What you get from Firebase config:**

- Realtime database with sync
- Authentication (email, Google, GitHub, etc.)
- File storage with security rules
- Hosting with CDN
- Cloud Functions triggered by database events

**The pattern:** Define the data shape + security rules → get a full backend with zero server code.

### Supabase — Define Tables, Get API + Auth + Realtime

Supabase goes further: define tables in SQL (or via their dashboard), and you get:

- **Auto-generated REST API** (PostgREST)
- **Auto-generated GraphQL API**
- **Realtime subscriptions** (websockets)
- **Row Level Security** (RLS policies)
- **Authentication** (JWT-based, multi-provider)
- **File storage** with policies
- **Edge functions** for custom logic

```sql
-- Define a table, get an entire API
CREATE TABLE posts (
  id BIGSERIAL PRIMARY KEY,
  title TEXT NOT NULL,
  body TEXT,
  author_id UUID REFERENCES auth.users(id),
  published BOOLEAN DEFAULT false,
  created_at TIMESTAMPTZ DEFAULT now()
);

-- Define permissions, get security
CREATE POLICY "Public read" ON posts FOR SELECT USING (published = true);
CREATE POLICY "Authenticated create" ON posts FOR INSERT
  WITH CHECK (auth.uid() = author_id);
```

**The pattern:** Data definition → derived API + security + realtime. This is GoFastr's closest analog.

### Encore — Define APIs in Go Types, Get Infrastructure

Encore is particularly relevant because it uses Go as its declaration language:

```go
// Service definition — Encore derives infrastructure from this
package blog

type Post struct {
    ID      int
    Title   string
    Body    string
    Author  string
}

type BlogResponse struct {
    Posts []Post
}

//encore:api public method=GET path=/posts
func ListPosts(ctx context.Context) (*BlogResponse, error) {
    // Implementation
}

//encore:api auth method=POST path=/posts
func CreatePost(ctx context.Context, params *CreateParams) (*Post, error) {
    // Implementation
}
```

From these Go type annotations, Encore generates:
- API gateway routing
- Service discovery
- Distributed tracing
- Infrastructure-as-code (Terraform)
- API documentation
- Client SDKs
- Secret management

**The key insight:** Encore uses Go types as the declaration language. GoFastr should use JSON as the declaration language and Go as the implementation language.

### Hasura — Define Tables, Get GraphQL API

Hasura connects to a PostgreSQL database and instantly generates:

- A full GraphQL API (queries, mutations, subscriptions)
- Permission rules at the row/column level
- Relationships (one-to-many, many-to-many) inferred from foreign keys
- Event triggers (webhooks on data changes)
- Remote schemas (merge multiple GraphQL APIs)

```yaml
# Hasura metadata — defines the entire API surface
version: 3
tables:
  - table: posts
    select_permissions:
      - role: public
        permission:
          filter: { published: { _eq: true } }
      - role: user
        permission:
          filter: {}
    insert_permissions:
      - role: user
        permission:
          check: { author_id: { _eq: "X-Hasura-User-Id" } }
          set:
            author_id: "X-Hasura-User-Id"
```

**The pattern:** Metadata YAML → complete GraphQL API with permissions. The metadata IS the application definition.

### Appwrite — Define Collections, Get Full Backend

Appwrite's model: define collections (like database tables) via their dashboard or API, and get:

- Auto-generated REST API for each collection
- Document-level permissions (read/write for user, team, or public)
- Authentication with 30+ providers
- File storage with transformations
- Realtime subscriptions
- Server-side functions (Node.js, Python, Dart, etc.)

### Forest Admin — Define Models, Get Admin Panel

Forest Admin generates a complete admin panel from model definitions:

```javascript
// models/post.js
module.exports = {
  fields: [
    { field: 'title', type: 'String' },
    { field: 'body', type: 'Text' },
    { field: 'author', type: 'String' },
    { field: 'published', type: 'Boolean' },
  ],
  actions: {
    Publish: { type: 'single', endpoint: '/actions/publish' },
  },
  segments: ['Published', 'Drafts'],
};
```

From this, Forest generates: list views, detail views, search, filters, CSV export, relationships, charts, and custom actions. **Every CRUD app needs an admin panel, and it should be free from model definitions.**

### Summary: Config-Driven Backend Pattern

| Framework | Definition Format | What You Get |
|-----------|------------------|-------------|
| Firebase | JSON rules + config | DB + auth + storage + hosting |
| Supabase | SQL tables + RLS | REST + GraphQL + realtime + auth + storage |
| Encore | Go types + annotations | API gateway + infra + tracing + docs |
| Hasura | YAML metadata + SQL | GraphQL API + permissions + events |
| Appwrite | Collection definitions | REST + auth + storage + realtime + functions |
| Forest Admin | Model definitions | Admin panel + CRUD + search + export |

**The universal pattern:** Define your data model and permissions → derive everything else. GoFastr's JSON schema should follow this pattern.

---

## 4. Object/Page Definition Patterns

### Drupal's Node System — Everything Is a "Node" with Fields

Drupal's architecture is a content management framework built on a single abstraction: **everything is a node**.

```
Node (base)
├── type: "article"
├── fields:
│   ├── title (text)
│   ├── body (text with format)
│   ├── author (entity reference → User)
│   ├── tags (entity reference → Term, multiple)
│   ├── published (boolean)
│   └── featured_image (entity reference → Media)
├── permissions:
│   ├── view: public
│   ├── edit: owner + admin
│   └── delete: admin
└── display modes:
    ├── teaser (list view)
    ├── full (detail view)
    └── RSS (feed view)
```

**Key insights:**

- The "node type" is a schema — it defines what fields exist
- Fields are reusable across types (tags work on articles, pages, products)
- Display modes define *how* a node renders in different contexts
- Views (Drupal's query builder) generate lists, tables, and feeds from node types
- **This is essentially a type system for content** — and it's entirely configurable through a UI (i.e., structured data)

**GoFastr parallel:** A "model" in GoFastr is exactly a Drupal node type — it defines fields, permissions, and display modes.

### WordPress Gutenberg — Blocks That Compose into Pages

Gutenberg represents every piece of content as a **block** — a self-contained unit of structure + presentation:

```json
{
  "blockName": "core/paragraph",
  "attrs": {},
  "innerBlocks": [],
  "innerHTML": "<p>Hello world</p>"
}
```

A page is a tree of blocks:

```json
[
  {"blockName": "core/heading", "attrs": {"level": 1}, "innerHTML": "<h1>My Post</h1>"},
  {"blockName": "core/paragraph", "attrs": {}, "innerHTML": "<p>Intro text...</p>"},
  {"blockName": "core/image", "attrs": {"id": 42, "sizeSlug": "large"}, "innerHTML": "..."},
  {"blockName": "core/columns", "attrs": {"columns": 2}, "innerBlocks": [
    [{"blockName": "core/paragraph", "innerHTML": "<p>Left</p>"}],
    [{"blockName": "core/paragraph", "innerHTML": "<p>Right</p>"}]
  ]}
]
```

**Key insights:**

- Blocks are a **composable UI primitive** — they nest, they have attributes, they render
- The block tree is stored as JSON in the database
- Block types are registered with an `edit` component (for the editor) and a `save` component (for rendering)
- Custom blocks are just: name + attributes schema + render function
- **This is exactly a component system** — blocks = components

**GoFastr parallel:** Pages in GoFastr should be defined as block trees — JSON arrays of components with props.

### Notion's Block Model — Composable Blocks

Notion takes the block model further — every piece of content is a block:

```json
{
  "object": "block",
  "id": "9bc30ad4-9373-46a5-84e7-3370e0b2c424",
  "type": "heading_2",
  "heading_2": {
    "rich_text": [{ "type": "text", "text": { "content": "Getting Started" } }]
  }
}
```

Notion's block types: paragraph, heading_1-3, bulleted_list, numbered_list, to_do, toggle, callout, quote, divider, image, video, file, pdf, bookmark, code, table, database, embed, and more.

**Key insight:** Notion proves that a finite set of block types can compose into virtually any document structure. **GoFastr needs a similar "component palette" — a fixed set of building blocks that compose into pages.**

### Salesforce Lightning — Declarative Components

Salesforce Lightning components declare their attributes and the platform renders them:

```xml
<aura:component>
  <aura:attribute name="posts" type="Post[]" />
  <aura:attribute name="loading" type="Boolean" default="false" />

  <lightning:card iconName="standard:record" title="Posts">
    <div class="slds-m-around_medium">
      <aura:if isTrue="{!v.loading}">
        <lightning:spinner />
        <lightning:datatable
          data="{!v.posts}"
          columns="{!v.columns}"
          onrowaction="{!c.handleRowAction}"
        />
      </aura:if>
    </div>
  </lightning:card>
</aura:component>
```

**Key insight:** Enterprise apps are built from a standard component library + declarative wiring. The components handle rendering; you just declare what data flows where.

### Storybook CSF — Component Descriptions as Data

Storybook's Component Story Format (CSF) describes components as structured data:

```javascript
// Button.stories.js — a component description
export default {
  title: 'Components/Button',
  component: Button,
  argTypes: {
    variant: { control: 'select', options: ['primary', 'secondary', 'danger'] },
    size: { control: 'select', options: ['sm', 'md', 'lg'] },
    disabled: { control: 'boolean' },
  },
};

export const Primary = { args: { variant: 'primary', children: 'Click me' } };
export const Disabled = { args: { variant: 'primary', disabled: true, children: 'Disabled' } };
```

CSF is essentially: **component name + props schema + example instances**. This is a machine-readable component contract.

---

## 5. Composable Architecture Patterns

### Entity Component System (ECS) — Compose Entities from Components

From game development, ECS is the most powerful composable architecture:

- **Entity:** An ID (a "thing" — no behavior, no data)
- **Component:** A bag of data (Position, Velocity, Health, Renderable)
- **System:** A function that operates on entities with specific components

```json
{
  "entities": [
    {
      "id": "player-1",
      "components": {
        "Position": {"x": 100, "y": 200},
        "Velocity": {"dx": 0, "dy": 0},
        "Renderable": {"sprite": "player.png"},
        "Health": {"current": 100, "max": 100},
        "PlayerControlled": {}
      }
    },
    {
      "id": "enemy-1",
      "components": {
        "Position": {"x": 300, "y": 400},
        "Velocity": {"dx": -1, "dy": 0},
        "Renderable": {"sprite": "enemy.png"},
        "Health": {"current": 50, "max": 50},
        "AI": {"behavior": "chase_player"}
      }
    }
  ]
}
```

**Why ECS matters for GoFastr:**

- **Composition over inheritance** — entities are defined by what components they have
- Components are pure data — trivially serializable to JSON
- Systems are pure logic — they query for entities with specific components
- **This is exactly how GoFastr should define models:** a model is an entity, fields are components, behaviors are systems
- The query pattern "give me all entities with Position AND Velocity" maps to "give me all models with title AND author"

### Plugin Systems — Everything Is a "Capability"

Modern frameworks treat every feature as a plugin/capability:

**VS Code's extension API:**
```json
{
  "contributes": {
    "commands": [{"command": "myExt.hello", "title": "Say Hello"}],
    "languages": [{"id": "myLang", "extensions": [".myl"]}],
    "themes": [{"label": "My Theme", "uiTheme": "vs-dark"}]
  }
}
```

**Webpack's plugin system:**
```javascript
class MyPlugin {
  apply(compiler) {
    compiler.hooks.emit.tap('MyPlugin', (compilation) => {
      // Modify the build
    });
  }
}
```

**The pattern:** A plugin declares its capabilities via structured data. The host system discovers and activates them. GoFastr should treat every feature (auth, storage, email, payments) as a capability declared in JSON.

### Pipeline/Middleware Chains as Compositions

Express.js, Koa, and Go's http.Handler all use the middleware pattern:

```json
{
  "middleware": [
    {"type": "logging"},
    {"type": "cors", "options": {"origins": ["*"]}},
    {"type": "auth", "options": {"required": false}},
    {"type": "rate-limit", "options": {"max": 100, "window": "1m"}},
    {"type": "route-handler"}
  ]
}
```

**Key insight:** Middleware chains are a **declarative composition pattern** — you define an ordered list of processing steps. This maps directly to JSON arrays.

### Micro-Frontends as Composable Pieces

Micro-frontend architectures compose independent frontend apps:

```json
{
  "layout": {
    "header": {"app": "shell", "component": "NavBar"},
    "sidebar": {"app": "catalog", "component": "CategoryTree"},
    "main": {"app": "dynamic", "route": true},
    "footer": {"app": "shell", "component": "Footer"}
  },
  "apps": {
    "shell": {"url": "/shell.js", "version": "1.2.0"},
    "catalog": {"url": "/catalog.js", "version": "3.0.1"},
    "dynamic": {"url": "/dynamic.js", "version": "2.1.0"}
  }
}
```

**GoFastr parallel:** A page layout is a composition of slots, each filled by a component. This is exactly how GoFastr pages should be defined.

### Web Components — Custom Elements That Compose

Web Components are the browser-native component model:

```json
{
  "tag": "post-card",
  "attributes": [
    {"name": "post-id", "type": "number", "required": true},
    {"name": "show-author", "type": "boolean", "default": true}
  ],
  "slots": ["default", "actions"],
  "events": [
    {"name": "post-click", "detail": "postId"},
    {"name": "post-delete", "detail": "postId"}
  ],
  "template": "<div class='post-card'><slot></slot></div>",
  "styles": ".post-card { border: 1px solid #ddd; padding: 1rem; }"
}
```

**Key insight:** Web Components prove that components can be fully described by: tag name, attributes, slots, events, template, and styles. All of these are serializable to JSON.

---

## 6. The "AI Outputs JSON → Framework Builds App" Thesis

### What Would a JSON App Definition Look Like?

A complete application definition in JSON would need to specify:

1. **App metadata** — name, database, auth providers
2. **Data models** — types, fields, relations, validation, permissions
3. **API endpoints** — routes, methods, request/response schemas
4. **Pages** — routes, layouts, components, data bindings
5. **Components** — reusable UI building blocks
6. **Workflows** — server-side logic chains (hooks, jobs, events)
7. **Configuration** — environment variables, feature flags, deployment

### Can We Define Everything in JSON?

| Concern | JSON-Definable? | How | Limits |
|---------|-----------------|-----|--------|
| Data models | ✅ Fully | Field types, relations, validation rules | Complex validation rules need code |
| CRUD API | ✅ Fully | Auto-generated from models | Custom business logic needs code |
| Pages/routes | ✅ Fully | Route tree + component composition | Complex client-side state needs code |
| Auth rules | ✅ Mostly | Role/permission matrices | Dynamic permissions need code |
| Forms | ✅ Fully | JSON Schema → form renderer | Custom widgets need code |
| Admin panel | ✅ Fully | Auto-generated from models | Custom admin actions need code |
| Business logic | ⚠️ Partially | Hooks, triggers, workflows | Complex logic needs code |
| Custom UI | ⚠️ Partially | Component composition | Custom components need code |
| Integrations | ⚠️ Partially | Declared as capabilities | Integration logic needs code |
| Scheduled jobs | ✅ Mostly | Cron + query + action | Complex job logic needs code |

**The 80/20:** JSON can define ~80% of a typical CRUD application. The remaining 20% (custom business logic, custom UI components, integrations) needs code.

### What Are the Minimal Primitives?

Drawing from all the frameworks above, GoFastr needs these primitives:

**Data primitives:**
- `string`, `text`, `integer`, `float`, `boolean`, `date`, `datetime`, `json`, `enum`
- `relation` (one-to-one, one-to-many, many-to-many)
- `file`, `image`

**Model primitives:**
- `fields` (with types, validation, defaults)
- `permissions` (read, create, update, delete — per role)
- `indexes`
- `hooks` (before/after create, update, delete)
- `computed` fields

**API primitives:**
- `GET /resource` (list)
- `GET /resource/:id` (detail)
- `POST /resource` (create)
- `PUT /resource/:id` (update)
- `DELETE /resource/:id` (delete)
- Custom endpoints with request/response schemas

**UI primitives:**
- `layout` (header, sidebar, main, footer slots)
- `page` (route + sections)
- `component` (type + props + children)
- `form` (schema + fields + actions)
- `list` (query + columns + actions)
- `detail` (query + sections)

**Behavior primitives:**
- `navigation` (links, redirects)
- `events` (on-click, on-submit, on-load)
- `validation` (client + server)
- `notifications` (toast, email)

### How Does This Compare to Code-First Frameworks?

| Aspect | Code-First (Rails, Django, Express) | JSON-First (GoFastr) |
|--------|--------------------------------------|---------------------|
| **Learning curve** | Must learn language + framework | Must learn the JSON schema |
| **Expressiveness** | Unlimited (it's code) | Bounded by schema capabilities |
| **Type safety** | Language-dependent | Schema validation (strict) |
| **Debuggability** | Stack traces, breakpoints | Schema errors, validation errors |
| **AI compatibility** | AI must know syntax, idioms | AI outputs structured data (trivial) |
| **Iteration speed** | Write code → restart → test | Edit JSON → hot reload → test |
| **Ecosystem** | Full language ecosystem | Framework primitives + extensions |
| **Auditability** | Must read code | JSON is self-documenting |
| **Extensibility** | Unlimited | Extension points defined by framework |
| **Deployment** | Language-specific | Single Go binary |

### What Are the Trade-offs?

**Advantages of JSON-first:**
1. **AI-native** — LLMs are dramatically better at producing valid JSON than idiomatic Go/Ruby/Python
2. **Auditable** — You can read the entire app definition in one file
3. **Validatable** — JSON Schema catches errors at definition time, not runtime
4. **Serializable** — Can be stored, versioned, diffed, and merged like any text file
5. **Portable** — The same JSON could theoretically target different backends
6. **Fast iteration** — Change the JSON, see the result immediately
7. **Secure by default** — The framework handles injection, XSS, CSRF, etc.
8. **Consistent** — Every app follows the same structure

**Disadvantages of JSON-first:**
1. **Limited expressiveness** — Complex business logic can't be expressed in JSON
2. **Abstraction leak** — You'll eventually need to understand what the framework generates
3. **Debugging difficulty** — When things go wrong, you're debugging JSON → Go → runtime
4. **Lock-in** — You're tied to the framework's capabilities
5. **Schema complexity** — A rich enough JSON schema becomes a DSL (which is a programming language)
6. **No composability of abstractions** — You can't define new JSON primitives from JSON
7. **Versioning** — Schema changes across framework versions could break app definitions

### How Does Go Fit?

Go is the ideal runtime for a JSON-first framework:

1. **Go validates the JSON** — `encoding/json` + JSON Schema validation gives strict type checking at build time
2. **Go generates the code** — `text/template` produces type-safe Go code from JSON definitions
3. **Go compiles the app** — `go build` produces a single static binary with no dependencies
4. **Go serves the app** — `net/http` is production-grade (used by Stripe, Twitch, Uber)
5. **Go's type system enforces correctness** — Generated Go code catches errors at compile time
6. **Go's concurrency handles scale** — Goroutines make real-time features, background jobs, etc. easy
7. **Go's standard library has everything** — HTTP, HTML templates, SQL drivers, crypto, encoding

**The compilation pipeline:**

```
app.json → (Go validator) → validated schema
        → (Go generator) → models.go, handlers.go, pages.go, migrations/
        → (Go compiler) → single binary
        → (Go server) → HTTP server serving the app
```

---

## 7. Practical JSON-Driven App Definitions

### Example 1: Blog Application

```json
{
  "$schema": "https://gofastr.dev/schema/v1",
  "app": {
    "name": "myblog",
    "version": "1.0.0",
    "database": {
      "driver": "postgresql",
      "url": "${DATABASE_URL}"
    },
    "auth": {
      "providers": ["email", "github"],
      "sessions": {
        "maxAge": "7d",
        "cookieName": "session"
      }
    },
    "server": {
      "port": "${PORT:8080}",
      "host": "0.0.0.0"
    }
  },
  "models": [
    {
      "name": "User",
      "fields": [
        {"name": "email", "type": "string", "unique": true, "required": true},
        {"name": "name", "type": "string", "required": true},
        {"name": "avatar", "type": "image", "optional": true},
        {"name": "role", "type": "enum", "values": ["user", "admin"], "default": "user"},
        {"name": "bio", "type": "text", "optional": true}
      ],
      "permissions": {
        "read": "public",
        "create": "system",
        "update": "owner",
        "delete": "admin"
      }
    },
    {
      "name": "Post",
      "fields": [
        {"name": "title", "type": "string", "required": true, "maxLength": 200},
        {"name": "slug", "type": "string", "unique": true, "computed": "slugify(title)"},
        {"name": "body", "type": "text", "required": true},
        {"name": "excerpt", "type": "text", "computed": "truncate(body, 200)"},
        {"name": "author", "type": "relation", "to": "User", "inverse": "posts"},
        {"name": "tags", "type": "relation", "to": "Tag", "many": true, "inverse": "posts"},
        {"name": "published", "type": "boolean", "default": false},
        {"name": "publishedAt", "type": "datetime", "optional": true},
        {"name": "coverImage", "type": "image", "optional": true}
      ],
      "indexes": [
        {"fields": ["slug"], "unique": true},
        {"fields": ["published", "publishedAt"]}
      ],
      "permissions": {
        "read": "public",
        "create": "authenticated",
        "update": "owner",
        "delete": ["owner", "admin"]
      },
      "hooks": {
        "beforeCreate": "validate-slug",
        "afterCreate": "notify-subscribers",
        "beforeUpdate": "validate-slug"
      }
    },
    {
      "name": "Tag",
      "fields": [
        {"name": "name", "type": "string", "required": true, "unique": true},
        {"name": "slug", "type": "string", "unique": true, "computed": "slugify(name)"}
      ],
      "permissions": {
        "read": "public",
        "create": "admin",
        "update": "admin",
        "delete": "admin"
      }
    },
    {
      "name": "Comment",
      "fields": [
        {"name": "body", "type": "text", "required": true, "maxLength": 2000},
        {"name": "author", "type": "relation", "to": "User"},
        {"name": "post", "type": "relation", "to": "Post", "inverse": "comments"},
        {"name": "approved", "type": "boolean", "default": false}
      ],
      "permissions": {
        "read": "public",
        "create": "authenticated",
        "update": ["owner", "admin"],
        "delete": ["owner", "admin"]
      }
    }
  ],
  "api": {
    "prefix": "/api",
    "endpoints": [
      {
        "method": "GET",
        "path": "/posts",
        "query": "Post.where(published=true).include(author, tags).order(publishedAt DESC).limit(?limit).offset(?offset)",
        "params": [
          {"name": "limit", "type": "integer", "default": 20, "max": 100},
          {"name": "offset", "type": "integer", "default": 0, "min": 0},
          {"name": "tag", "type": "string", "optional": true}
        ],
        "response": {"type": "array", "items": "Post"}
      },
      {
        "method": "GET",
        "path": "/posts/:slug",
        "query": "Post.where(slug=slug, published=true).include(author, tags, comments.where(approved=true))",
        "response": "Post"
      },
      {
        "method": "POST",
        "path": "/posts",
        "auth": true,
        "body": "PostInput",
        "response": "Post",
        "status": 201
      }
    ]
  },
  "pages": [
    {
      "route": "/",
      "layout": "main",
      "title": "My Blog",
      "sections": [
        {
          "component": "hero",
          "props": {"heading": "My Blog", "subheading": "Thoughts on code and life"}
        },
        {
          "component": "post-list",
          "query": "Post.where(published=true).include(author).order(publishedAt DESC).limit(10)",
          "props": {"showExcerpt": true, "showAuthor": true}
        }
      ]
    },
    {
      "route": "/posts/:slug",
      "layout": "main",
      "title": "{{post.title}}",
      "sections": [
        {
          "component": "post-detail",
          "query": "Post.where(slug=slug, published=true).include(author, tags, comments.where(approved=true))"
        },
        {
          "component": "comment-section",
          "query": "Comment.where(postId=post.id, approved=true).include(author)",
          "props": {"allowCreate": "authenticated"}
        }
      ]
    },
    {
      "route": "/admin",
      "layout": "admin",
      "auth": {"role": "admin"},
      "sections": [
        {
          "component": "admin-dashboard",
          "queries": {
            "posts": "Post.count()",
            "comments": "Comment.count()",
            "users": "User.count()"
          }
        }
      ]
    }
  ],
  "components": [
    {
      "name": "post-list",
      "props": [
        {"name": "posts", "type": "Post[]", "required": true},
        {"name": "showExcerpt", "type": "boolean", "default": false},
        {"name": "showAuthor", "type": "boolean", "default": true}
      ],
      "template": "post-list.html"
    },
    {
      "name": "post-detail",
      "props": [{"name": "post", "type": "Post", "required": true}],
      "template": "post-detail.html"
    },
    {
      "name": "comment-section",
      "props": [
        {"name": "comments", "type": "Comment[]"},
        {"name": "allowCreate", "type": "boolean", "default": false}
      ],
      "template": "comment-section.html"
    },
    {
      "name": "hero",
      "props": [
        {"name": "heading", "type": "string", "required": true},
        {"name": "subheading", "type": "string"}
      ],
      "template": "hero.html"
    }
  ],
  "workflows": [
    {
      "name": "notify-subscribers",
      "trigger": "afterCreate:Post",
      "steps": [
        {"type": "query", "name": "subscribers", "query": "User.where(subscribed=true)"},
        {"type": "email", "to": "subscribers.email", "template": "new-post", "subject": "New post: {{post.title}}"}
      ]
    },
    {
      "name": "validate-slug",
      "trigger": "beforeCreate:Post,beforeUpdate:Post",
      "steps": [
        {"type": "compute", "field": "slug", "value": "slugify(title)"},
        {"type": "validate", "condition": "slug is unique", "error": "A post with this title already exists"}
      ]
    }
  ],
  "assets": {
    "stylesheets": ["styles.css"],
    "scripts": [],
    "images": ["logo.svg"]
  }
}
```

### Example 2: Project Management App

```json
{
  "$schema": "https://gofastr.dev/schema/v1",
  "app": {
    "name": "projectr",
    "database": {"driver": "postgresql", "url": "${DATABASE_URL}"},
    "auth": {
      "providers": ["email", "google", "github"],
      "teams": true
    }
  },
  "models": [
    {
      "name": "Project",
      "fields": [
        {"name": "name", "type": "string", "required": true},
        {"name": "description", "type": "text"},
        {"name": "owner", "type": "relation", "to": "User"},
        {"name": "members", "type": "relation", "to": "User", "many": true},
        {"name": "status", "type": "enum", "values": ["active", "archived"], "default": "active"},
        {"name": "color", "type": "string", "pattern": "^#[0-9a-f]{6}$"}
      ],
      "permissions": {
        "read": "member",
        "create": "authenticated",
        "update": "member",
        "delete": "owner"
      }
    },
    {
      "name": "Task",
      "fields": [
        {"name": "title", "type": "string", "required": true},
        {"name": "description", "type": "text"},
        {"name": "project", "type": "relation", "to": "Project", "inverse": "tasks"},
        {"name": "assignee", "type": "relation", "to": "User", "optional": true},
        {"name": "status", "type": "enum", "values": ["todo", "in_progress", "review", "done"], "default": "todo"},
        {"name": "priority", "type": "enum", "values": ["low", "medium", "high", "urgent"], "default": "medium"},
        {"name": "dueDate", "type": "date", "optional": true},
        {"name": "labels", "type": "relation", "to": "Label", "many": true},
        {"name": "parent", "type": "relation", "to": "Task", "optional": true, "inverse": "subtasks"}
      ],
      "permissions": {
        "read": "project_member",
        "create": "project_member",
        "update": "project_member",
        "delete": ["creator", "project_owner"]
      },
      "hooks": {
        "afterUpdate": ["notify-assignee", "update-project-stats"]
      }
    },
    {
      "name": "Label",
      "fields": [
        {"name": "name", "type": "string", "required": true},
        {"name": "color", "type": "string", "required": true},
        {"name": "project", "type": "relation", "to": "Project"}
      ]
    },
    {
      "name": "Comment",
      "fields": [
        {"name": "body", "type": "text", "required": true},
        {"name": "author", "type": "relation", "to": "User"},
        {"name": "task", "type": "relation", "to": "Task", "inverse": "comments"}
      ]
    }
  ],
  "pages": [
    {
      "route": "/",
      "layout": "app",
      "auth": true,
      "redirect": "/projects",
      "sections": []
    },
    {
      "route": "/projects",
      "layout": "app",
      "auth": true,
      "sections": [
        {
          "component": "project-list",
          "query": "Project.where(members includes currentUser).include(owner, members).order(name)"
        }
      ]
    },
    {
      "route": "/projects/:id",
      "layout": "app",
      "auth": true,
      "sections": [
        {
          "component": "project-header",
          "query": "Project.find(id).include(owner, members)"
        },
        {
          "component": "task-board",
          "query": "Task.where(projectId=id).include(assignee, labels).group(status)",
          "props": {
            "columns": ["todo", "in_progress", "review", "done"],
            "draggable": true
          }
        }
      ]
    }
  ]
}
```

### What the Go Framework Does with This JSON

The Go compilation pipeline transforms `app.json` into a running application:

#### Phase 1: Validation
```
app.json → JSON Schema validation → type checking → referential integrity check
```
- Validate against GoFastr JSON Schema
- Check all relations reference existing models
- Check all field types are valid
- Check all permissions reference valid roles
- Check all component references exist

#### Phase 2: Code Generation
```
validated schema → Go code generation
```

Generated files:
```
generated/
├── models/
│   ├── user.go          // Go structs with tags for JSON/SQL
│   ├── post.go
│   ├── tag.go
│   └── comment.go
├── database/
│   ├── connection.go    // Database connection pool
│   ├── migrations/
│   │   ├── 001_create_users.up.sql
│   │   ├── 001_create_users.down.sql
│   │   ├── 002_create_posts.up.sql
│   │   └── ...
│   └── queries/
│       ├── user_queries.go    // Type-safe query functions
│       ├── post_queries.go
│       └── ...
├── api/
│   ├── router.go        // HTTP router with all endpoints
│   ├── handlers/
│   │   ├── posts.go     // CRUD handlers
│   │   ├── auth.go      // Login/register handlers
│   │   └── ...
│   ├── middleware/
│   │   ├── auth.go      // JWT/session middleware
│   │   ├── cors.go
│   │   └── logging.go
│   └── validation/
│       └── schemas.go   // Request validation
├── pages/
│   ├── renderer.go      // HTML template renderer
│   ├── templates/
│   │   ├── layouts/
│   │   │   ├── main.html
│   │   │   └── admin.html
│   │   ├── components/
│   │   │   ├── post-list.html
│   │   │   ├── post-detail.html
│   │   │   └── ...
│   │   └── pages/
│   │       ├── index.html
│   │       ├── post-detail.html
│   │       └── admin.html
│   └── routes.go        // Page routes
├── auth/
│   ├── providers/
│   │   ├── email.go     // Email/password auth
│   │   └── github.go    // OAuth
│   ├── sessions.go
│   └── middleware.go
└── workflows/
    ├── hooks.go         // Hook dispatcher
    └── notify.go        // Email notification workflow
```

#### Phase 3: Compilation
```
generated/ + extensions/ → go build → single binary
```

#### Phase 4: Runtime
```
single binary → serves HTTP (pages + API + static assets)
```

---

## 8. Hybrid Approach: JSON Definitions + Go Extensions

### The Core Principle

**JSON defines the WHAT. Go defines the HOW.**

- **JSON (declarative):** Models, fields, permissions, routes, pages, components, queries
- **Go (imperative):** Custom business logic, custom components, integrations, complex validation

### Extension Points

The framework should provide these Go extension points:

#### 1. Model Hooks (Lifecycle)

```go
// extensions/hooks.go
package extensions

import (
    "context"
    "myblog/generated/models"
)

func BeforeCreatePost(ctx context.Context, post *models.Post) error {
    // Custom validation or mutation before a post is created
    if containsSpam(post.Body) {
        return fmt.Errorf("post appears to be spam")
    }
    post.Slug = slugify(post.Title)
    return nil
}

func AfterCreatePost(ctx context.Context, post *models.Post) error {
    // Send notifications, update search index, etc.
    go notifySubscribers(post)
    go indexForSearch(post)
    return nil
}
```

JSON registration:
```json
{
  "models": [{
    "name": "Post",
    "hooks": {
      "beforeCreate": "BeforeCreatePost",
      "afterCreate": "AfterCreatePost"
    }
  }]
}
```

#### 2. Custom API Endpoints

```go
// extensions/endpoints.go
package extensions

import (
    "net/http"
    "myblog/generated"
)

func SearchPosts(w http.ResponseWriter, r *http.Request) {
    query := r.URL.Query().Get("q")
    results, err := generated.DB.SearchPosts(r.Context(), query)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    generated.RenderJSON(w, results)
}
```

JSON registration:
```json
{
  "api": {
    "custom": [
      {"method": "GET", "path": "/search", "handler": "SearchPosts"}
    ]
  }
}
```

#### 3. Custom Components

```go
// extensions/components.go
package extensions

import (
    "net/http"
)

func ChartComponent(w http.ResponseWriter, props map[string]any) {
    // Render a custom chart component
    data := props["data"].([]any)
    chartType := props["chartType"].(string)
    // ... render SVG chart
}
```

JSON registration:
```json
{
  "components": [
    {
      "name": "chart",
      "type": "custom",
      "handler": "ChartComponent",
      "props": [
        {"name": "data", "type": "array", "required": true},
        {"name": "chartType", "type": "enum", "values": ["bar", "line", "pie"]}
      ]
    }
  ]
}
```

#### 4. Middleware

```go
// extensions/middleware.go
package extensions

import "net/http"

func RateLimitMiddleware(next http.Handler) http.Handler {
    limiter := NewRateLimiter(100, time.Minute)
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if !limiter.Allow(r.RemoteAddr) {
            http.Error(w, "rate limited", 429)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

JSON registration:
```json
{
  "server": {
    "middleware": [
      {"handler": "RateLimitMiddleware", "position": "before_auth"}
    ]
  }
}
```

#### 5. Workflow Steps

```go
// extensions/workflows.go
package extensions

import "context"

func SendSlackNotification(ctx context.Context, payload map[string]any) error {
    webhook := os.Getenv("SLACK_WEBHOOK")
    message := payload["message"].(string)
    return postToSlack(webhook, message)
}
```

JSON registration:
```json
{
  "workflows": [
    {
      "name": "notify-slack",
      "trigger": "afterCreate:Post",
      "steps": [
        {"type": "custom", "handler": "SendSlackNotification", "payload": {"message": "New post: {{post.title}}"}}
      ]
    }
  ]
}
```

#### 6. Permission Evaluators

```go
// extensions/permissions.go
package extensions

import "context"

func CanEditProject(ctx context.Context, user *models.User, project *models.Project) bool {
    // Complex permission logic that can't be expressed in JSON
    if user.Role == "admin" {
        return true
    }
    if user.ID == project.OwnerID {
        return true
    }
    return isProjectMember(user.ID, project.ID)
}
```

JSON registration:
```json
{
  "models": [{
    "name": "Project",
    "permissions": {
      "update": {"evaluator": "CanEditProject"}
    }
  }]
}
```

### The 90/10 Principle

**AI generates 90% of the app as JSON:**
- All models, fields, relations, indexes
- All CRUD API endpoints
- All pages with standard components
- All standard permissions (public, authenticated, owner, admin)
- All standard workflows (email notifications, validation)
- All forms and validation rules

**Humans write 10% in Go:**
- Complex business logic (pricing calculations, recommendation engines)
- Custom components (charts, maps, interactive editors)
- Third-party integrations (Stripe, Slack, SendGrid)
- Complex permissions (team-based, attribute-based)
- Performance optimizations (caching, query tuning)

### The Compilation Model

```
┌─────────────────────────────────────────────────────────┐
│                     app.json                             │
│  (AI-generated, human-reviewed, schema-validated)        │
└───────────────────────┬─────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────────┐
│                  Go Validator                            │
│  - JSON Schema validation                               │
│  - Referential integrity                                │
│  - Type checking                                        │
│  - Permission consistency                               │
└───────────────────────┬─────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────────┐
│                  Go Code Generator                       │
│  - models/*.go         (Go structs)                      │
│  - database/*.go       (migrations + queries)            │
│  - api/*.go            (handlers + router + middleware)  │
│  - pages/*.go          (renderer + templates)            │
│  - auth/*.go           (providers + sessions)            │
└───────────────────────┬─────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────────┐
│              extensions/*.go                             │
│  (Human-written Go code that hooks into generated code)  │
│  - hooks.go                                            │
│  - endpoints.go                                        │
│  - components.go                                       │
│  - middleware.go                                       │
│  - workflows.go                                        │
│  - permissions.go                                      │
└───────────────────────┬─────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────────┐
│                  go build                                │
│  - Compiles generated + extension code                   │
│  - Links into a single static binary                    │
│  - Zero runtime dependencies                            │
└───────────────────────┬─────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────────┐
│              ./myblog (single binary)                    │
│  - Serves HTTP pages (server-rendered HTML)              │
│  - Serves JSON API (REST)                               │
│  - Serves static assets                                 │
│  - Handles auth sessions                                │
│  - Connects to PostgreSQL                               │
│  - Runs background workflows                            │
└─────────────────────────────────────────────────────────┘
```

---

## 9. Concrete Recommendations

### Recommended JSON Schema Structure

The GoFastr JSON schema should be organized into these top-level sections:

```json
{
  "$schema": "https://gofastr.dev/schema/v1",
  "app": {},
  "models": [],
  "api": {},
  "pages": [],
  "components": [],
  "workflows": [],
  "assets": {}
}
```

### Section-by-Section Recommendations

#### `app` — Application Configuration

```json
{
  "app": {
    "name": "string (required, kebab-case)",
    "version": "string (semver)",
    "description": "string",
    "database": {
      "driver": "enum: postgresql | sqlite | mysql",
      "url": "string (env var reference)"
    },
    "auth": {
      "providers": ["enum: email | google | github | apple | magic_link"],
      "sessions": {
        "maxAge": "duration string",
        "cookieName": "string",
        "secure": "boolean"
      },
      "teams": "boolean",
      "registration": {
        "open": "boolean",
        "allowedDomains": ["string"]
      }
    },
    "server": {
      "port": "string (env var reference with default)",
      "host": "string",
      "cors": {
        "origins": ["string"],
        "methods": ["string"],
        "headers": ["string"]
      }
    },
    "middleware": [
      {"handler": "string (Go function name)", "position": "enum: before_auth | after_auth | before_routes"}
    ]
  }
}
```

#### `models` — Data Models

```json
{
  "models": [
    {
      "name": "PascalCase string",
      "tableName": "string (optional, defaults to snake_case of name)",
      "fields": [
        {
          "name": "camelCase string",
          "type": "enum: string | text | integer | float | boolean | date | datetime | json | enum | uuid | image | file",
          "required": "boolean (default: false)",
          "unique": "boolean (default: false)",
          "default": "any (type-appropriate default value)",
          "optional": "boolean (default: false)",
          "maxLength": "integer",
          "minLength": "integer",
          "min": "number",
          "max": "number",
          "pattern": "string (regex)",
          "values": "string[] (for enum type)",
          "to": "string (model name, for relation type)",
          "many": "boolean (for relation type)",
          "inverse": "string (field name on related model)",
          "computed": "string (expression for computed fields)"
        }
      ],
      "indexes": [
        {
          "fields": ["string"],
          "unique": "boolean",
          "name": "string (optional)"
        }
      ],
      "permissions": {
        "read": "PermissionRule",
        "create": "PermissionRule",
        "update": "PermissionRule",
        "delete": "PermissionRule"
      },
      "hooks": {
        "beforeCreate": "string | string[] (Go function names)",
        "afterCreate": "string | string[]",
        "beforeUpdate": "string | string[]",
        "afterUpdate": "string | string[]",
        "beforeDelete": "string | string[]",
        "afterDelete": "string | string[]"
      },
      "display": {
        "label": "string (field to use as display name)",
        "icon": "string (icon name)",
        "color": "string (field to use for color)"
      }
    }
  ]
}
```

**PermissionRule** is one of:
- `"public"` — anyone can access
- `"authenticated"` — must be logged in
- `"owner"` — must be the record owner (requires an `owner` or `author` relation to User)
- `"admin"` — must have admin role
- `"system"` — only system-level operations (no direct API access)
- `"none"` — no access
- `"string"` — Go function name (custom evaluator)
- `["string"]` — any of the listed rules must match

#### `api` — API Configuration

```json
{
  "api": {
    "prefix": "string (default: /api)",
    "versioning": "enum: none | url | header",
    "autoCRUD": "boolean (default: true)",
    "pagination": {
      "defaultLimit": "integer (default: 20)",
      "maxLimit": "integer (default: 100)"
    },
    "endpoints": [
      {
        "method": "enum: GET | POST | PUT | PATCH | DELETE",
        "path": "string (with :params)",
        "auth": "boolean | PermissionRule",
        "body": "string (model name or input type)",
        "query": "string (GoFastr query expression)",
        "params": [
          {"name": "string", "type": "FieldType", "required": "boolean", "default": "any"}
        ],
        "response": "string (model name or type)",
        "status": "integer (default: 200)",
        "cache": {"ttl": "duration", "key": "string"}
      }
    ],
    "custom": [
      {"method": "string", "path": "string", "handler": "string (Go function)"}
    ]
  }
}
```

#### `pages` — Page Definitions

```json
{
  "pages": [
    {
      "route": "string (URL pattern with :params)",
      "layout": "string (layout name)",
      "auth": "boolean | PermissionRule",
      "redirect": "string (redirect URL if auth check fails)",
      "title": "string (supports {{template}} syntax)",
      "description": "string (meta description)",
      "sections": [
        {
          "component": "string (component name)",
          "query": "string (GoFastr query expression)",
          "props": {"key": "value | template expression"},
          "slot": "string (named slot in layout)"
        }
      ]
    }
  ]
}
```

#### `components` — Reusable Components

```json
{
  "components": [
    {
      "name": "string (kebab-case)",
      "type": "enum: standard | custom | layout",
      "handler": "string (Go function, for custom type)",
      "template": "string (HTML template file path, for standard type)",
      "props": [
        {"name": "string", "type": "FieldType", "required": "boolean", "default": "any"}
      ],
      "slots": ["string (named slots)"],
      "events": [
        {"name": "string", "detail": "string (type)"}
      ]
    }
  ]
}
```

#### `workflows` — Server-Side Logic

```json
{
  "workflows": [
    {
      "name": "string",
      "trigger": "string (hook:Model or cron expression)",
      "condition": "string (optional, expression that must be true)",
      "steps": [
        {
          "type": "enum: query | email | custom | log | transform | validate | http",
          "handler": "string (Go function, for custom type)",
          "query": "string (GoFastr query, for query type)",
          "to": "string (email address or template, for email type)",
          "template": "string (email template, for email type)",
          "subject": "string",
          "url": "string (for http type)",
          "method": "string (for http type)",
          "body": "object (for http type)",
          "field": "string (for transform/validate type)",
          "value": "string (for transform type)",
          "condition": "string (for validate type)",
          "error": "string (error message for validate type)",
          "payload": "object (for custom type)"
        }
      ]
    }
  ]
}
```

### Recommended Standard Components

GoFastr should ship these standard components (no custom code needed):

| Component | Description | Props |
|-----------|-------------|-------|
| `text` | Plain text/heading | `content`, `level`, `align` |
| `rich-text` | Markdown/rich content | `content` |
| `image` | Responsive image | `src`, `alt`, `width`, `height`, `fit` |
| `link` | Anchor/button link | `href`, `text`, `variant`, `external` |
| `button` | Action button | `text`, `action`, `variant`, `icon` |
| `form` | Auto-generated form | `model`, `fields`, `action`, `method` |
| `list` | Data list/table | `data`, `columns`, `sortable`, `paginated` |
| `detail` | Record detail view | `data`, `sections` |
| `card` | Content card | `title`, `body`, `image`, `actions` |
| `grid` | Responsive grid layout | `columns`, `gap`, `children` |
| `columns` | Multi-column layout | `count`, `gap`, `children` |
| `tabs` | Tab navigation | `items`, `active` |
| `modal` | Modal dialog | `title`, `body`, `actions`, `trigger` |
| `search` | Search input with results | `query`, `placeholder`, `resultComponent` |
| `pagination` | Page navigation | `current`, `total`, `perPage` |
| `breadcrumb` | Navigation breadcrumb | `items` |
| `sidebar` | Sidebar layout | `content`, `sidebar`, `width` |
| `nav` | Navigation bar | `items`, `logo`, `auth` |
| `footer` | Page footer | `content`, `links` |
| `hero` | Hero section | `heading`, `subheading`, `cta`, `image` |
| `empty-state` | Empty state message | `title`, `description`, `action` |
| `error` | Error display | `code`, `message` |
| `loading` | Loading indicator | `type` |
| `avatar` | User avatar | `src`, `name`, `size` |
| `badge` | Status badge | `text`, `variant` |
| `chart` | Simple chart | `data`, `type`, `xAxis`, `yAxis` |
| `stat` | Statistic display | `value`, `label`, `change` |
| `admin-dashboard` | Auto-generated admin | `models`, `stats` |
| `admin-list` | Admin list view | `model`, `filters`, `actions` |
| `admin-detail` | Admin detail/edit view | `model`, `fields` |

### Recommended Go Extension Points

| Extension Point | Go Interface | When to Use |
|----------------|-------------|-------------|
| `BeforeCreate(M)` | `func(ctx, *M) error` | Validate, mutate, or reject before creation |
| `AfterCreate(M)` | `func(ctx, *M) error` | Side effects after creation (notifications, indexing) |
| `BeforeUpdate(M)` | `func(ctx, *M, old *M) error` | Validate transitions, enforce invariants |
| `AfterUpdate(M)` | `func(ctx, *M, old *M) error` | Side effects on change (audit logs, webhooks) |
| `BeforeDelete(M)` | `func(ctx, *M) error` | Prevent deletion, cascade checks |
| `AfterDelete(M)` | `func(ctx, *M) error` | Cleanup (files, relations, notifications) |
| `CustomHandler` | `func(w, r) error` | Custom API endpoint logic |
| `CustomComponent` | `func(w, props)` | Custom HTML rendering |
| `Middleware` | `func(next) Handler` | Request/response pipeline modification |
| `PermissionEvaluator` | `func(ctx, user, resource) bool` | Complex authorization logic |
| `WorkflowStep` | `func(ctx, payload) error` | Custom workflow step (integrations, etc.) |
| `QueryModifier` | `func(ctx, query) Query` | Dynamic query modification (multi-tenancy, etc.) |
| `TemplateFunction` | `func(args ...) any` | Custom template helpers |
| `ScheduledJob` | `func(ctx) error` | Cron-based background jobs |

### Recommended Query Language

The GoFastr query language should be simple, type-safe, and parseable:

```
// Basic queries
Model.find(id)
Model.where(field=value)
Model.where(field > value)
Model.where(field in [1, 2, 3])
Model.where(field contains "search")

// Relations
Model.include(relation)
Model.include(relation.where(field=value))

// Ordering and pagination
Model.order(field ASC)
Model.limit(20).offset(40)

// Aggregation
Model.count()
Model.count().group(field)
Model.sum(field)

// Template variables
Model.where(authorId = currentUser.id)
Model.where(slug = params.slug)

// Combining
Post.where(published=true).include(author, tags).order(publishedAt DESC).limit(10)
```

### Development Workflow

```
1. Describe the app in JSON (AI-assisted)
   $ gofastr init "blog with posts, comments, tags, and admin panel"
   → generates app.json

2. Validate the schema
   $ gofastr validate
   → checks JSON against schema, verifies relations, permissions

3. Generate Go code
   $ gofastr generate
   → generates models, API, pages, migrations

4. Add custom Go extensions (optional)
   $ code extensions/hooks.go
   → write custom business logic

5. Run in development mode
   $ gofastr dev
   → runs with hot reload, auto-migration

6. Build for production
   $ gofastr build
   → compiles to single binary

7. Deploy
   $ ./myblog
   → serves on PORT, connects to DATABASE_URL
```

### AI Integration

The JSON-first approach makes AI integration trivial:

```bash
# AI generates the entire app
$ gofastr ai "Create a project management app with tasks, projects, teams, comments, and a kanban board"
→ generates app.json

# AI adds a feature
$ gofastr ai "Add due dates and priority levels to tasks"
→ modifies app.json (adds fields, updates pages)

# AI creates a custom report
$ gofastr ai "Add an admin page showing task completion rates by assignee"
→ adds page + API endpoint to app.json

# AI migrates the schema
$ gofastr ai "Rename the 'body' field to 'content' on the Post model"
→ updates app.json, generates migration
```

---

## Appendix: Precedent Analysis Matrix

| Framework | Definition Format | Auto-Generates | Extension Model | AI-Friendliness |
|-----------|------------------|----------------|-----------------|-----------------|
| Firebase | JSON rules | DB + auth + hosting | Cloud Functions | ⭐⭐⭐⭐ |
| Supabase | SQL + dashboard | REST + GraphQL + auth | Edge Functions | ⭐⭐⭐ |
| Hasura | YAML metadata | GraphQL + permissions | Custom resolvers | ⭐⭐⭐⭐ |
| Encore | Go annotations | Infra + API gateway | Go code | ⭐⭐ |
| Prisma | .prisma schema | DB + types + client | Go/TS resolvers | ⭐⭐⭐ |
| Drupal | UI config | Pages + forms + views | PHP modules | ⭐⭐ |
| WordPress | PHP + blocks | Pages + admin | PHP plugins | ⭐⭐ |
| Notion | Block JSON | Documents + views | API integrations | ⭐⭐⭐⭐⭐ |
| OpenAPI | YAML/JSON | Routes + docs + clients | Code generation | ⭐⭐⭐⭐⭐ |
| Forest Admin | Model JS | Admin panel | Smart actions | ⭐⭐⭐⭐ |
| HTMX | HTML attributes | Interactive UI | Custom endpoints | ⭐⭐⭐⭐⭐ |
| **GoFastr (proposed)** | **JSON** | **DB + API + pages + auth + admin** | **Go code** | **⭐⭐⭐⭐⭐** |

---

## Key Takeaways

1. **JSON as the primary interface is proven** — Firebase, Hasura, OpenAPI, and Notion all succeed with structured data as the source of truth.

2. **The "define models, derive everything" pattern works** — Supabase, Hasura, and Encore prove that data model definitions can generate complete backends.

3. **Component trees are data** — Flutter, React RSC, and Web Components prove that UI can be serialized as structured data.

4. **Permissions are declarative** — Firebase rules, Hasura permissions, and Supabase RLS prove that authorization can be defined as structured rules.

5. **The 80/20 rule applies** — JSON handles 80% of CRUD apps. The remaining 20% needs a code escape hatch. Go is ideal for this.

6. **AI + JSON is a superpower** — LLMs produce valid JSON far more reliably than idiomatic code in any programming language. JSON-first frameworks are inherently AI-optimized.

7. **Go is the perfect runtime** — Static binary compilation, excellent HTTP server, strong type system, fast execution, and zero runtime dependencies.

8. **The hybrid model wins** — JSON for structure, Go for behavior. Both compile into a single binary. AI generates the JSON, humans write the Go.

---

*This research was compiled for the GoFastr project — an AI-first, JSON-defined web framework written in Go.*
