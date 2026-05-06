# Modern JS Fullstack Framework Research

> Comprehensive analysis of 7 modern JavaScript fullstack frameworks for informing the design of a Go-based competing framework.
> Date: 2026-05-05

---

## Table of Contents

1. [Next.js 15](#nextjs-15)
2. [Nuxt 4](#nuxt-4)
3. [SvelteKit 2](#sveltekit-2)
4. [Remix](#remix)
5. [Astro](#astro)
6. [SolidStart](#solidstart)
7. [TanStack Start](#tanstack-start)
8. [Feature Comparison Matrix](#feature-comparison-matrix)
9. [Killer Features for a New Framework](#killer-features-for-a-new-framework)

---

## Next.js 15

### 1. Core Architecture

**App Router (`/app` directory):**
- File-system based routing using `app/` directory hierarchy
- Each route segment maps to a folder; `page.tsx` defines the UI, `layout.tsx` wraps it
- **React Server Components (RSC)** as the default — components render on the server and stream a serialized RSC payload to the client
- The rendering pipeline: Server Component tree → RSC wire format (serialized React elements) → streamed to client → Client-side React reconciles the virtual DOM from the payload
- **Nested layouts** persist across navigations within their subtree; only the leaf `page.tsx` re-renders
- **Special files per route segment:**
  - `page.tsx` — the route UI (unique to a URL)
  - `layout.tsx` — shared layout (wraps children, persists across navigation)
  - `loading.tsx` — React Suspense boundary (shows instant loading UI)
  - `error.tsx` — error boundary for the segment
  - `not-found.tsx` — 404 UI for the segment
  - `template.tsx` — re-mounting layout (like layout but remounts on navigation)
  - `default.tsx` — fallback for parallel routes

**Parallel & Intercepting Routes:**
- `@folder` convention creates named "slots" in layouts that render simultaneously
- `(.)folder`, `(..)folder`, `(...)`folder` conventions for intercepting routes (e.g., modals that have their own URL)

### 2. Rendering Strategies

| Strategy | How it works |
|---|---|
| **SSR** | Default for App Router pages. Server renders the RSC tree, streams HTML + RSC payload. Client hydrates. |
| **SSG** | Set `dynamic = 'force-static'` or export `generateStaticParams()`. Pages pre-rendered at build time. |
| **ISR** | `revalidate` option in `fetch()` or route segment config. Background regeneration without full rebuild. |
| **Streaming SSR** | Built-in via React Suspense. `loading.tsx` files create streaming boundaries. The HTML is sent in chunks as data resolves. |
| **Partial Prerendering (PPR)** | Experimental in Next.js 15. Static shell wraps dynamic holes. Combines SSG speed with SSR freshness in a single HTTP response. |
| **Client-side navigation** | Soft navigations using the Next.js `<Link>` component. Only fetches the RSC payload for the new route, not full HTML. |

### 3. Data Fetching Patterns

**Server Components (async):**
```tsx
// app/posts/page.tsx — this runs on the server only
export default async function PostsPage() {
  const posts = await db.posts.findMany();
  return <PostList posts={posts} />;
}
```
- No `getServerSideProps` or `getStaticProps` anymore — just `await` directly in the component
- React caches `fetch()` requests within a single render pass (deduplication)

**Server Actions:**
```tsx
// Mutations via "use server" directive
async function createPost(formData: FormData) {
  'use server';
  await db.posts.create({ title: formData.get('title') });
  revalidatePath('/posts'); // fine-grained cache invalidation
}
```
- Functions marked with `'use server'` become RPC endpoints
- Can be called from client components via form `action` prop or imperative function call
- Built on top of POST requests with multipart form data or JSON encoding
- `revalidatePath()` and `revalidateTag()` for granular cache busting

**Cache Control:**
```tsx
// Per-route segment config
export const dynamic = 'force-dynamic'; // always SSR
export const revalidate = 60; // ISR every 60s
export const fetchCache = 'force-no-store'; // opt out of fetch caching

// Per-request in fetch()
const data = await fetch(url, { next: { revalidate: 3600, tags: ['posts'] } });
```

### 4. File Conventions and DX

```
app/
  layout.tsx           # Root layout (required — must have <html>, <body>)
  page.tsx             # Home page /
  loading.tsx          # Loading UI for /
  error.tsx            # Error boundary for /
  not-found.tsx        # 404 for /
  globals.css
  posts/
    page.tsx           # /posts
    [id]/
      page.tsx         # /posts/:id
      loading.tsx
    layout.tsx         # Shared layout for /posts/*
  api/
    route.ts           # API route handler (GET, POST, etc.)
```

- **Co-location:** Components, utilities, and tests can live alongside route files
- **Private folders:** `_folder` is excluded from routing (for co-located utils)
- **Route groups:** `(folder)` organizes routes without affecting the URL path
- **API routes:** `route.ts` files export named HTTP method handlers (`GET`, `POST`, etc.)
- **Dynamic segments:** `[param]` for dynamic, `[...slug]` for catch-all, `[[...slug]]` for optional catch-all

### 5. Middleware/Plugin System

**Middleware (`middleware.ts`):**
```tsx
import { NextResponse } from 'next/server';
import type { NextRequest } from 'next/server';

export function middleware(request: NextRequest) {
  // Runs on the Edge Runtime before the route is matched
  // Can redirect, rewrite, set headers, set cookies
  const token = request.cookies.get('auth-token');
  if (!token && request.nextUrl.pathname.startsWith('/dashboard')) {
    return NextResponse.redirect(new URL('/login', request.url));
  }
  return NextResponse.next();
}

export const config = {
  matcher: ['/dashboard/:path*', '/admin/:path*'],
};
```
- Runs on the Edge Runtime (V8 isolates, not full Node.js)
- Executes before caching and route matching
- Can modify request/response (redirect, rewrite, set headers, cookies)
- **Limitations:** No access to Node.js APIs, limited runtime, cannot execute database queries directly

**Next.js Plugins:** No formal plugin system. Customization via:
- `next.config.js` (webpack/turbopack config, headers, redirects, rewrites)
- Custom server (standalone mode) for advanced use cases

### 6. Type Safety Approach

- **Route params and searchParams** are typed in page components via generic props
- **Server Actions** can be typed with TypeScript — the `'use server'` boundary enforces that only serializable types cross
- **Link component** does not provide compile-time route type-checking by default (unlike TanStack Router)
- Community solutions like `next-routes`, `next-safe-action`, and Zod schemas fill gaps
- **Next.js 15 improvement:** Better typing for `params` (now a Promise for async support)
- `next-safe-action` is the de-facto standard for type-safe server actions with Zod validation

### 7. Build and Deployment Model

- **Builder:** Turbopack (Rust-based, default in Next.js 15 for dev), Webpack (production)
- **Output modes:**
  - `standalone` — self-contained output for Docker/VM deployment
  - `export` — fully static HTML export (no server)
  - Default — Node.js server bundle
- **Vercel:** First-class hosting with Edge Functions, ISR, image optimization
- **Self-hosted:** `next start` runs a Node.js server; or Docker with `standalone` output
- **Edge Runtime:** Middleware and some API routes can run on V8 isolates (Cloudflare Workers, Vercel Edge)
- **Incremental compilation:** Turbopack provides sub-second HMR even in large apps

### 8. Unique Innovations

- **React Server Components integration** — deepest implementation of RSC, co-developed with React team
- **Partial Prerendering (PPR)** — combines static shell + dynamic holes in a single HTTP response
- **Turbopack** — Rust-based bundler with incremental computation; claims 700x faster HMR than Webpack
- **Server Actions** — form-level mutations that work without JavaScript (progressive enhancement)
- **Streaming SSR with Suspense** — granular loading states via `loading.tsx` boundaries
- **On-demand ISR** — revalidation via API route calls without rebuilds

---

## Nuxt 4

### 1. Core Architecture

**Vue-based fullstack framework with Nitro server engine:**
- File-system routing via the `pages/` directory
- Each `.vue` file in `pages/` becomes a route; nested directories create nested routes
- **Layouts** via `layouts/default.vue` — similar to Next.js layouts but opt-in per page
- **Nuxt App** has a dual-engine architecture:
  - **Vue engine** — handles client-side rendering, hydration, and reactivity
  - **Nitro engine** — handles server-side rendering, API routes, and server logic

**Nitro Server Engine:**
- Standalone server runtime decoupled from Vue
- Provides: API routes (`server/api/`), middleware (`server/middleware/`), plugins, utilities
- Generates an output target for virtually any deployment platform
- Auto-detects and bundles only server-side code; tree-shakes client code from server bundle

**Rendering Pipeline:**
1. Request hits Nitro server
2. Server middleware executes (auth, redirects, etc.)
3. Route is matched to a page component
4. Vue SSR renders the component tree to HTML
5. HTML is streamed back to client (if streaming enabled)
6. Client hydrates with Vue 3's progressive hydration

### 2. Rendering Strategies

| Strategy | How it works |
|---|---|
| **SSR** | Default. Nitro renders Vue components server-side. |
| **SSG** | `nuxt generate` — pre-renders all routes at build time. Hybrid rendering allows per-route config. |
| **ISR / Hybrid** | `routeRules` config per route: `swr: 60` (stale-while-revalidate), `prerender: true`, `ssr: false` |
| **SPA mode** | Set `ssr: false` in `nuxt.config.ts` for client-only rendering |
| **Edge SSR** | Nitro can target Cloudflare Workers, Vercel Edge, Deno Deploy |
| **Streaming** | Experimental — `experimental.renderJsonPayloads` and streaming SSR via `render:streaming` |

**Hybrid Rendering (route rules):**
```ts
export default defineNuxtConfig({
  routeRules: {
    '/': { prerender: true },        // SSG
    '/blog/**': { swr: 3600 },       // ISR (stale-while-revalidate)
    '/admin/**': { ssr: false },      // SPA
    '/api/**': { cors: true },        // CORS headers
  }
});
```

### 3. Data Fetching Patterns

**Composables:**
```vue
<script setup>
// SSR-friendly data fetching
const { data: posts, pending, error, refresh } = await useFetch('/api/posts');

// Direct API call with useAsyncData
const { data } = await useAsyncData('user', () => $fetch('/api/user'));

// SSR-only data (no client refetch)
const { data } = await useAsyncData('config', () => $fetch('/api/config'), {
  server: true,
  lazy: false,
});
</script>
```

- **`useFetch(url)`** — auto-keyed, deduplicated, SSR-aware fetch composable
- **`useAsyncData(key, fn)`** — generic async data loading with caching and deduplication
- Data is serialized from server → client via `__NUXT__` payload injection
- **`$fetch`** — direct HTTP client (ofetch) without composable wrapper; use in server routes or non-reactive contexts

**Server Routes (Nitro):**
```ts
// server/api/posts.ts
export default defineEventHandler(async (event) => {
  const method = getMethod(event);
  if (method === 'GET') {
    return await db.posts.findMany();
  }
  if (method === 'POST') {
    const body = await readBody(event);
    return await db.posts.create({ data: body });
  }
});

// server/api/posts/[id].get.ts — method-specific file naming
export default defineEventHandler(async (event) => {
  const { id } = getRouterParam(event);
  return await db.posts.findUnique({ where: { id } });
});
```

### 4. File Conventions and DX

```
nuxt-app/
  app/
    pages/
      index.vue              # /
      posts/
        index.vue            # /posts
        [id].vue             # /posts/:id
    components/              # Auto-imported Vue components
    composables/             # Auto-imported composables (useXxx)
    layouts/
      default.vue            # Default layout
    plugins/                 # Auto-registered plugins
    middleware/              # Route middleware
  server/
    api/
      posts.ts               # /api/posts
      posts/
        [id].get.ts          # GET /api/posts/:id
    middleware/
      auth.ts                # Server middleware
    routes/
      sitemap.xml.ts         # Custom route handler
  nuxt.config.ts
```

**Auto-imports (killer DX feature):**
- All components in `components/` are auto-imported — no manual `import` statements
- All composables in `composables/` are auto-imported
- Vue, Nuxt, and third-party APIs are auto-imported
- Achieved via `unjs/unimport` — scans directories at build time and generates import declarations
- **Nuxt 4 change:** `app/` directory as the main source directory (cleaner separation from `server/`)

**Convention over configuration:**
- File names determine routes, API endpoints, and middleware registration
- `.server.ts` / `.client.ts` suffixes for environment-specific modules
- Method-specific API routes: `posts.get.ts`, `posts.post.ts`

### 5. Middleware/Plugin System

**Server Middleware (Nitro):**
```ts
// server/middleware/auth.ts
export default defineEventHandler((event) => {
  const url = getRequestURL(event);
  if (url.pathname.startsWith('/admin')) {
    const token = getCookie(event, 'auth-token');
    if (!token) {
      throw createError({ statusCode: 401, message: 'Unauthorized' });
    }
  }
});
```

**Route Middleware (Vue):**
```ts
// middleware/auth.ts
export default defineNuxtRouteMiddleware((to, from) => {
  const auth = useAuth();
  if (!auth.isLoggedIn()) return navigateTo('/login');
});

// In page:
definePageMeta({ middleware: ['auth'] });
```

**Plugins:**
```ts
// plugins/my-plugin.ts — auto-registered
export default defineNuxtPlugin((nuxtApp) => {
  nuxtApp.provide('myUtility', () => 'hello');
});
```

**Nuxt Modules:** Rich ecosystem of plugins that hook into Nuxt's build/runtime:
- `@nuxtjs/tailwindcss`, `@nuxt/image`, `@nuxt/content`, etc.
- Modules can add components, composables, server routes, and build configuration

### 6. Type Safety Approach

- **Auto-generated types:** `nuxt generate` creates `.nuxt/types/` with typed routes, components, and composables
- **Typed API routes:** `defineEventHandler()` infers return types
- **`useFetch<T>()`** generic for typed response data
- **`typedRouter`** — experimental typed router with `useRouter()` and `<NuxtLink>`
- **Nuxt 4 improvements:** Better type generation, stricter typing for definePageMeta
- **Zod integration** for runtime validation via community modules
- Auto-imports generate TypeScript declarations so IDE autocomplete works seamlessly

### 7. Build and Deployment Model

**Build:** Vite (client + dev), Nitro (server). Rollup for production client bundle.
**Nitro Presets** (deployment targets):
```
node-server, vercel, netlify, cloudflare-workers, deno-deploy,
aws-lambda, firebase, edge, bun, service-worker, static
```
- Each preset generates an optimized output for the target platform
- `nuxi build` → `.output/` directory with server bundle + client assets
- `nuxi generate` → fully static site
- **Zero-config deployment:** Auto-detects hosting environment and applies appropriate preset

### 8. Unique Innovations

- **Auto-imports** — zero-boilerplate imports for components, composables, and utilities
- **Nitro engine** — universal server runtime with 15+ deployment presets
- **Hybrid rendering** — per-route rendering strategy via `routeRules` (SSR, SSG, SWR, SPA)
- **Module ecosystem** — `@nuxt/` modules that inject functionality at every level
- **Layer system** — extend Nuxt apps with other Nuxt apps (like a module but for entire app slices)
- **Universal `$fetch`** — same API works on client and server with automatic proxy

---

## SvelteKit 2

### 1. Core Architecture

**File-system routing with `src/routes/`:**
- Each directory in `routes/` is a route segment
- `+page.svelte` — the page component
- `+page.ts` — runs on both server and client (load function)
- `+page.server.ts` — runs on server only (load function with database access)
- `+layout.svelte` — shared layout component
- `+layout.server.ts` — shared server-side data loading
- `+error.svelte` — error UI

**Rendering Pipeline:**
1. Request hits the SvelteKit server
2. Hooks (`handle` in `hooks.server.ts`) run (auth, logging, etc.)
3. Route is matched
4. `load` functions execute (server-first, then shared) in parallel where possible
5. Svelte SSR renders the component tree to HTML
6. HTML + serialized data is sent to the client
7. Client-side Svelte hydrates the components
8. Subsequent navigations call `load` functions client-side only (no full page reload)

**Adapter Pattern:**
- SvelteKit abstracts the deployment target behind "adapters"
- `@sveltejs/adapter-node`, `@sveltejs/adapter-vercel`, `@sveltejs/adapter-cloudflare`, `@sveltejs/adapter-static`
- The adapter determines how the app is built and deployed
- Custom adapters can be written for any platform

### 2. Rendering Strategies

| Strategy | How it works |
|---|---|
| **SSR** | Default. Server renders HTML via Svelte SSR. |
| **SSG** | `@sveltejs/adapter-static` + prerender config. `+page.ts` exports `prerender = true`. |
| **SPA** | SSR disabled for specific pages via `+page.ts`: `export const ssr = false`. |
| **Hybrid** | Per-page config: some pages SSR, some SSG, some SPA. |
| **Streaming** | SvelteKit 2 supports streaming SSR via top-level `await` in components (Svelte 5 runes). |

**Per-page configuration:**
```ts
// +page.ts
export const ssr = true;       // enable SSR (default)
export const prerender = true; // pre-render at build time
export const csr = true;       // enable client-side rendering
```

### 3. Data Fetching Patterns

**Load Functions:**
```ts
// +page.server.ts — server-only load
export async function load({ params, cookies, locals, parent }) {
  const parentData = await parent(); // access parent layout data
  const post = await db.posts.findUnique({ where: { id: params.id } });
  return { post }; // must be serializable (JSON)
}

// +page.ts — universal load (runs on server for initial, client for navigation)
export async function load({ fetch, params }) {
  const res = await fetch(`/api/posts/${params.id}`);
  return await res.json();
}
```

**Form Actions:**
```ts
// +page.server.ts
export const actions = {
  default: async ({ request, cookies, locals }) => {
    const formData = await request.formData();
    const title = formData.get('title');
    await db.posts.create({ data: { title } });
    return { success: true }; // returned as `$form` in the page
  },
  delete: async ({ request }) => {
    // named action: ?/delete
    const formData = await request.formData();
    await db.posts.delete({ where: { id: formData.get('id') } });
  }
};
```
- Progressive enhancement by default — works without JavaScript
- `use:enhance` directive for client-side enhancement (optimistic UI, etc.)
- Form data is sent as `multipart/form-data` POST

**Load function properties:**
- `params` — route parameters
- `url` — current URL
- `fetch` — server-side fetch (preserves cookies)
- `cookies` — server-only: read/write cookies
- `locals` — server-only: shared state from hooks
- `parent` — access parent layout data
- `depends` — register dependencies for revalidation
- `isDataRequest` — whether this is a client-side navigation

### 4. File Conventions and DX

```
src/
  routes/
    +layout.svelte          # Root layout
    +layout.server.ts       # Root server load
    +page.svelte            # / page
    +page.server.ts         # / server load + actions
    +error.svelte           # Error page
    posts/
      +page.svelte          # /posts
      +page.server.ts       # posts server load
      [id]/
        +page.svelte        # /posts/:id
        +page.server.ts
    api/
      posts/
        +server.ts          # API endpoint (GET, POST, etc.)
  lib/
    components/             # $lib/components importable via $lib
    utils/                  # $lib/utils
  params/                   # Custom param matchers
    slug.ts                 # export function match(param) { ... }
  hooks.server.ts           # Server hooks (handle)
  hooks.client.ts           # Client hooks
```

**Key conventions:**
- `+` prefix denotes SvelteKit special files (distinguishes from co-located components)
- `$lib` alias for `src/lib/`
- `$app/stores`, `$app/navigation` — built-in module aliases
- Custom param matchers via `src/params/`

### 5. Middleware/Plugin System

**Hooks (`hooks.server.ts`):**
```ts
export const handle = async ({ event, resolve }) => {
  // event.locals is shared with load functions and actions
  event.locals.user = await getUser(event.cookies);

  // Interceptor pattern — can modify response
  const response = await resolve(event, {
    transformPageChunk: ({ html }) => html.replace('%lang%', 'en'),
    filterSerializedResponseHeaders: (name) => name.startsWith('x-'),
  });

  response.headers.set('x-custom', 'value');
  return response;
};

// Sequence multiple hooks
import { sequence } from '@sveltejs/kit/hooks';
export const handle = sequence(authHook, loggingHook, corsHook);
```

**Client hooks (`hooks.client.ts`):**
```ts
export const handle = async ({ event, resolve }) => {
  // Runs on client-side navigation
  return resolve(event);
};
```

### 6. Type Safety Approach

- **`$types`** — auto-generated types for each route, imported as `import type { PageData, ActionData } from './$types'`
- **Route params** are typed based on the file path (`[id]` → `{ id: string }`)
- **Load function return types** are inferred and available in the page component via `data` prop
- **Action data** is typed via `ActionData` in the page component
- **App.D.ts** — ambient type declarations for `locals`, `page`, `platform`
- **No runtime validation** built-in — Zod/schemas are community patterns
- **Svelte 5 runes** provide better TypeScript inference for reactive state

### 7. Build and Deployment Model

- **Dev:** Vite dev server with HMR
- **Build:** `svelte-kit build` → adapter compiles to target
- **Adapters:**
  - `adapter-node` — Node.js server (Express-like)
  - `adapter-vercel` — Vercel serverless functions
  - `adapter-cloudflare` — Cloudflare Pages/Workers
  - `adapter-static` — fully static HTML (prerender all routes)
  - `adapter-netlify` — Netlify functions
  - Custom adapters via the adapter API
- **Output:** `.svelte-kit/` contains build artifacts; adapter produces final output in `build/`

### 8. Unique Innovations

- **`+` file convention** — clear visual distinction between framework files and user files
- **Form actions** — deeply integrated progressive enhancement (works without JS)
- **Adapter pattern** — clean abstraction over deployment targets
- **Fine-grained reactivity** — Svelte's compile-time reactivity (no virtual DOM) produces minimal JS bundles
- **Load function chaining** — `parent()` enables data composition across nested layouts
- **Scoped CSS by default** — `<style>` blocks are scoped without configuration
- **Svelte 5 runes** — `$state`, `$derived`, `$effect` provide explicit reactivity signals without the Svelte compiler magic

---

## Remix

### 1. Core Architecture

**Loader/Action pattern with nested routing:**
- Each route module exports a `loader` (GET data) and/or `action` (POST/mutation data)
- Routes are defined in `app/routes/` via file-system routing (or config-based)
- **Nested routes** render simultaneously — a URL can match multiple route modules
- Each matched route loads data in parallel
- The page is a composition of all matched route components

**Rendering Pipeline:**
1. Request hits the Remix server
2. All matching route loaders execute **in parallel**
3. If a loader throws/returns a redirect, Remix aborts and sends the redirect
4. All loader data is serialized
5. React SSR renders the nested component tree
6. HTML + serialized data (via `<script>` tags) sent to client
7. Client-side Remix hydrates and takes over navigation
8. Subsequent navigations call only the loaders for changed route segments

**Nested Routing Architecture:**
```
URL: /posts/123/edit
Matches: app/routes/posts.tsx → app/routes/posts/$postId.tsx → app/routes/posts/$postId/edit.tsx
Each renders inside its parent's <Outlet />
```
- Parent routes stay mounted when child routes change
- Only changed segments re-fetch data
- Each level of nesting has its own error boundary and loading state

### 2. Rendering Strategies

| Strategy | How it works |
|---|---|
| **SSR** | Always. Remix is fundamentally an SSR framework. |
| **SSG** | Not built-in. Achieved via third-party solutions or pre-rendering scripts. |
| **ISR** | Not built-in. Requires HTTP cache headers (`Cache-Control`) at the CDN layer. |
| **SPA fallback** | Not a primary mode. Remix can be configured for client-only, but loses its core value. |
| **Streaming** | `defer()` in loaders returns an HTML stream with suspense boundaries. |

**`defer()` for streaming:**
```ts
export async function loader() {
  const criticalData = await getCriticalData();
  const slowData = getSlowData(); // Returns a Promise, not awaited
  return defer({ criticalData, slowData }); // slowData streams in via <Await>
}
```
- Critical data renders server-side; deferred data streams to client and renders via `<Await>` component
- Uses React Suspense under the hood

### 3. Data Fetching Patterns

**Loader (data for GET):**
```ts
// app/routes/posts.tsx
import { json } from '@remix-run/node';
import { useLoaderData } from '@remix-run/react';

export async function loader({ request, params, context }) {
  const url = new URL(request.url);
  const page = url.searchParams.get('page') || '1';
  const posts = await db.posts.findMany({ skip: (page - 1) * 20, take: 20 });
  return json({ posts, page });
}

export default function Posts() {
  const { posts, page } = useLoaderData<typeof loader>();
  return <PostList posts={posts} />;
}
```

**Action (data for POST/mutations):**
```ts
export async function action({ request }) {
  const formData = await request.formData();
  const intent = formData.get('intent');

  switch (intent) {
    case 'create': {
      const title = formData.get('title');
      await db.posts.create({ data: { title } });
      return json({ success: true });
    }
    case 'delete': {
      await db.posts.delete({ where: { id: formData.get('id') } });
      return json({ success: true });
    }
  }
}
```

**Key patterns:**
- Loaders run on the server only — never on the client
- Actions run on the server only
- After an action, Remix automatically re-calls all loaders for the current URL (revalidation)
- **Optimistic UI** via `useFetcher()` — submit forms without navigation
- **Multi-form** pattern: one route, one action, multiple intents via form fields
- **Web standard APIs** — `Request`, `Response`, `FormData`, `Headers` throughout

### 4. File Conventions and DX

```
app/
  root.tsx                  # Root layout (required)
  routes/
    _index.tsx              # / (flat file convention)
    posts.tsx               # /posts
    posts.$id.tsx           # /posts/:id
    posts.$id.edit.tsx      # /posts/:id/edit
    posts_._index.tsx       # /posts (layout route without path)
    _auth.login.tsx         # /login (layout without path)
    _auth.register.tsx      # /register
  entry.client.tsx          # Client hydration entry
  entry.server.tsx          # Server entry (customizable SSR)
```

**Remix v2 flat-file routing conventions:**
- `.` separator for nested routes (e.g., `posts.$id.tsx` → `/posts/:id`)
- `_` prefix for layout routes without a path segment
- `_index.tsx` for the index route of a layout
- Traditional directory-based routing also supported

### 5. Middleware/Plugin System

**No traditional middleware.** Remix's philosophy is explicit over implicit:

**Loader/action wrappers (equivalent to middleware):**
```ts
// Custom wrapper pattern
function withAuth(loaderFn) {
  return async (args) => {
    const user = await getUser(args.request);
    if (!user) throw redirect('/login');
    return loaderFn({ ...args, context: { ...args.context, user } });
  };
}

export const loader = withAuth(async ({ context }) => {
  return json({ user: context.user });
});
```

**`entry.server.tsx`:** Full control over the server-side rendering pipeline
- Can inject global middleware (Express-style) if using Express adapter
- Can customize streaming, caching headers, etc.

**Resource routes:** Any route can omit the default export and become a pure API endpoint (returns JSON, XML, etc.)

### 6. Type Safety Approach

- **`useLoaderData<typeof loader>()`** — full type inference from loader to component
- **`useActionData<typeof action>()`** — full type inference from action to component
- **`MetaFunction<typeof loader>`** — typed meta tags based on loader data
- **Route params** are typed: `Params<{ id: string }>`
- **`context`** in entry server can be typed globally
- No built-in runtime validation — Zod is commonly used for form validation
- **`conform`** library provides type-safe form validation integrating with Remix

### 7. Build and Deployment Model

- **Build:** esbuild for server bundles, PostCSS/Tailwind for CSS
- **Adapters:** Express, Architect (AWS), Cloudflare Workers, Vercel, Netlify, Deno
- **Single build** produces both client and server bundles
- **Static assets** served from `public/`
- **Custom server:** Remix can be used as middleware in an Express/Node.js server
- **SPA mode:** `remix build --s` produces a client-only single-page app (escape hatch)

### 8. Unique Innovations

- **Loader/Action pattern** — clean separation of reads (GET) and writes (POST) at the route level
- **Progressive enhancement** — forms work without JavaScript; Remix enhances them client-side
- **Nested routing with parallel data loading** — multiple route loaders run concurrently
- **Automatic revalidation** — after mutations, all active loaders re-run (like React Query's cache invalidation, but built-in)
- **Web standard APIs throughout** — `Request`, `Response`, `FormData` instead of framework abstractions
- **`useFetcher()`** — optimistic mutations without page navigation
- **`defer()`** — built-in streaming with critical/non-critical data split
- **Error boundaries per route segment** — errors bubble up to the nearest boundary

---

## Astro

### 1. Core Architecture

**Content-focused framework with Island Architecture:**
- Pages are `.astro` files (or `.mdx`) that render to static HTML
- No JavaScript is sent to the client by default ("zero JS by default")
- Interactive components are "islands" — explicitly opted-in via `client:*` directives
- **Multi-framework** — islands can be React, Svelte, Vue, Solid, Preact, etc. components

**Island Architecture:**
```
┌─────────────────────────────────┐
│  Static HTML shell (.astro)     │
│  ┌───────────┐  ┌────────────┐  │
│  │ React     │  │ Svelte     │  │
│  │ island    │  │ island     │  │
│  │ (hydrated)│  │ (hydrated) │  │
│  └───────────┘  └────────────┘  │
│                                 │
│  Static content (no JS)         │
└─────────────────────────────────┘
```
- Each island hydrates independently
- Islands can use different frameworks
- Static HTML between islands ships zero JavaScript

**Rendering Pipeline:**
1. Astro builds the page as static HTML
2. Islands are identified via `client:*` directives
3. Each island's component framework renders to HTML
4. A small Astro runtime hydrates each island on the client
5. Islands hydrate lazily (on visible, on idle, on load, or on interaction)

### 2. Rendering Strategies

| Strategy | How it works |
|---|---|
| **SSG** | Default. All pages pre-rendered at build time. |
| **SSR** | `output: 'server'` in config. Uses adapter for Node.js, Vercel, Cloudflare, etc. |
| **Hybrid** | `output: 'hybrid'` — default static, opt individual pages into SSR via `export const prerender = false`. |
| **Partial hydration** | Core to Astro — only islands hydrate. `client:load`, `client:idle`, `client:visible`, `client:only`. |
| **Streaming** | Not a primary feature (content sites). SSR mode streams HTML. |

**Client directives:**
```astro
<!-- Hydrate immediately -->
<ReactCounter client:load />

<!-- Hydrate once page is idle -->
<ReactCounter client:idle />

<!-- Hydrate when element is visible -->
<ReactCounter client:visible />

<!-- Hydrate when media query matches -->
<ReactCounter client:visible="(max-width: 768px)" />

<!-- Skip SSR, only render on client -->
<ReactCounter client:only="react" />
```

### 3. Data Fetching Patterns

**Static data in Astro components:**
```astro
---
// This "frontmatter" runs at build time only (SSG) or per-request (SSR)
import { getCollection } from 'astro:content';

const posts = await getCollection('blog');
const data = await fetch('https://api.example.com/posts').then(r => r.json());
---

<html>
  {posts.map(post => <article>{post.data.title}</article>)}
</html>
```

**Content Collections:**
```ts
// src/content/config.ts
import { defineCollection, z } from 'astro:content';

const blog = defineCollection({
  type: 'content',     // or 'data' for JSON/YAML
  schema: z.object({
    title: z.string(),
    date: z.date(),
    tags: z.array(z.string()),
    draft: z.boolean().default(false),
  }),
});

export const collections = { blog };
```

```mdx
---
# src/content/blog/my-post.mdx
title: "Hello World"
date: 2024-01-15
tags: ["astro", "web"]
---

# Hello World
This is my blog post.
```

- Type-safe content queries via `getCollection()` and `getEntry()`
- Schema validation at build time
- Supports Markdown, MDX, and data files (JSON, YAML)

**Server endpoints (SSR mode):**
```ts
// src/pages/api/posts.ts
export async function GET({ params, request, locals }) {
  const posts = await db.posts.findMany();
  return new Response(JSON.stringify(posts), {
    headers: { 'Content-Type': 'application/json' },
  });
}

export async function POST({ request }) {
  const body = await request.json();
  await db.posts.create({ data: body });
  return new Response(null, { status: 201 });
}
```

### 4. File Conventions and DX

```
src/
  pages/
    index.astro              # /
    about.astro              # /about
    blog/
      index.astro            # /blog
      [slug].astro           # /blog/:slug
    api/
      posts.ts               # API endpoint
  components/
    ReactCounter.tsx         # Multi-framework component
    SvelteToggle.svelte
    Header.astro             # Astro component (no JS)
  layouts/
    Base.astro               # Reusable layout
  content/
    config.ts                # Collection schemas
    blog/
      post-1.mdx
      post-2.mdx
  styles/
    global.css
```

**Key DX features:**
- `.astro` components use a "frontmatter script" (`---`) for server-side code
- Template is HTML-like with JSX expressions
- No client-side JavaScript by default
- **View Transitions API** built-in for animated page navigations
- **Content layer** (Astro 4+) for loading content from any source (CMS, database, files)

### 5. Middleware/Plugin System

**Middleware (`src/middleware.ts`):**
```ts
import { defineMiddleware } from 'astro:middleware';

export const onRequest = defineMiddleware(async (context, next) => {
  // context.locals is shared with pages and endpoints
  context.locals.user = await getUser(context.cookies);

  const response = await next(); // call the route handler
  response.headers.set('x-custom', 'value');
  return response;
});
```

**Integrations (plugin system):**
```ts
// astro.config.mjs
import { defineConfig } from 'astro/config';
import react from '@astrojs/react';
import svelte from '@astrojs/svelte';
import tailwind from '@astrojs/tailwind';
import mdx from '@astrojs/mdx';

export default defineConfig({
  integrations: [react(), svelte(), tailwind(), mdx()],
  output: 'hybrid',
});
```
- Integrations add UI frameworks, CSS tools, and custom build steps
- **Custom integrations** can hook into the build lifecycle (`astro:config:setup`, `astro:build:start`, etc.)
- Very extensible — integrations can inject components, virtual modules, and build plugins

### 6. Type Safety Approach

- **Content collections** are the gold standard for type-safe content:
  - Zod schemas define types
  - `getCollection()` and `getEntry()` are fully typed
  - Build-time validation catches errors early
- **Page props** typed via `Props` interface in `.astro` files
- **API routes** use standard `Request`/`Response` types
- **`astro:content`** module provides typed imports
- **Astro 4+:** `InferGetStaticPropsType<typeof getStaticPaths>` for typed dynamic routes
- No built-in type-safe routing (URLs are strings)

### 7. Build and Deployment Model

- **Build:** Vite under the hood
- **Output modes:**
  - `static` — fully static HTML files (default)
  - `server` — SSR with adapter
  - `hybrid` — static by default, per-page SSR
- **Adapters:** `@astrojs/node`, `@astrojs/vercel`, `@astrojs/cloudflare`, `@astrojs/netlify`, `@astrojs/deno`
- **Content build:** Markdown/MDX processed at build time; content collections validated
- **Island scripts** are bundled and optimized per-island (minimal JS)
- **Astro DB** — built-in SQLite database for Astro Studio (managed hosting)

### 8. Unique Innovations

- **Island Architecture** — partial hydration at its purest; zero JS by default
- **Multi-framework support** — use React, Svelte, Vue, Solid in the same project
- **Content Collections** — type-safe, schema-validated content with Zod
- **Zero JS by default** — ship no JavaScript unless explicitly requested
- **View Transitions** — built-in animated navigations using the browser API
- **Content Layer** — universal data loading from any source (files, CMS, APIs, databases)
- **`.astro` component syntax** — HTML-first with server-side frontmatter
- **Astro DB** — integrated SQLite database for content-driven sites

---

## SolidStart

### 1. Core Architecture

**Solid primitives + file-based routing:**
- Built on Solid.js — a reactive framework using fine-grained signals (no virtual DOM)
- File-system routing via `src/routes/`
- **Server functions** with `"use server"` directive (similar to Next.js)
- Solid's reactivity is "true reactivity" — updates touch only the exact DOM nodes that change

**Rendering Pipeline:**
1. Request hits the SolidStart server
2. Route is matched via file-system conventions
3. Server functions and route data resolve
4. Solid's `renderToString` (or streaming `renderToStringAsync`) generates HTML
5. HTML is sent to client
6. Client-side Solid "hydrates" but with **no virtual DOM diff**
- Solid uses real DOM nodes; reactivity is tracked via signals and effects
- Hydration is annotation-based (adds comments in HTML to mark boundaries)

### 2. Rendering Strategies

| Strategy | How it works |
|---|---|
| **SSR** | Default. Solid renders to HTML string on the server. |
| **SSG** | `export const prerender = true` on routes. SolidStart builds static HTML. |
| **SPA** | Set `ssr: false` for specific routes or globally. |
| **Streaming** | Built-in via `renderToStringAsync`. Suspense boundaries stream in. |
| **Islands** | Not a primary feature, but can be achieved with lazy loading. |

**Solid's streaming with Suspense:**
```tsx
import { Suspense } from 'solid-js';

export default function Page() {
  const data = createResource(async () => await fetchPosts());
  return (
    <Suspense fallback={<Loading />}>
      <PostList posts={data()} />
    </Suspense>
  );
}
```
- Suspense boundaries create streaming chunks
- Server sends HTML in order; suspended chunks stream when ready

### 3. Data Fetching Patterns

**Server Functions:**
```tsx
// src/api/posts.ts — "use server" module
"use server";

export async function getPosts() {
  return await db.posts.findMany();
}

export async function createPost(title: string) {
  return await db.posts.create({ data: { title } });
}
```

**Route data with `createServerData$`:**
```tsx
// src/routes/posts.tsx
import { createServerData$ } from 'solid-start/server';

export function routeData() {
  return createServerData$(async () => {
    return await db.posts.findMany();
  });
}

export default function Posts() {
  const posts = useRouteData<typeof routeData>();
  return (
    <For each={posts()}>
      {(post) => <div>{post.title}</div>}
    </For>
  );
}
```

**Form actions:**
```tsx
// src/routes/posts.tsx
import { createServerAction$ } from 'solid-start/server';

export default function Posts() {
  const [enrolling, enroll] = createServerAction$(async (formData: FormData) => {
    await db.posts.create({ data: { title: formData.get('title') } });
  });

  return (
    <form onSubmit={(e) => {
      e.preventDefault();
      enroll(new FormData(e.currentTarget));
    }}>
      <input name="title" />
      <button type="submit" disabled={enrolling.pending}>Submit</button>
    </form>
  );
}
```

### 4. File Conventions and DX

```
src/
  routes/
    index.tsx               # /
    about.tsx               # /about
    posts/
      index.tsx             # /posts
      [id].tsx              # /posts/:id
    api/
      posts.ts              # API handler
  entry-client.tsx          # Client entry
  entry-server.tsx          # Server entry
  app.tsx                   # App wrapper
```

- **Vinxi** as the build system (built on Vite)
- Convention-based but less opinionated than Next.js/Remix
- `routeData` export for route-level data loading
- **Solid signals** used throughout — `createSignal`, `createMemo`, `createEffect`
- **`<For>` component** for efficient list rendering (key-based, no virtual DOM)

### 5. Middleware/Plugin System

**Server middleware:**
```ts
// src/middleware.ts (or server entry customization)
import { createHandler } from 'solid-start/entry-server';

// Custom server middleware via the entry-server.tsx
export default createHandler(
  // Custom request interceptor
  ({ forward }) => async (event) => {
    // Pre-processing
    const response = await forward(event);
    // Post-processing
    return response;
  }
);
```

**API routes:**
```ts
// src/api/hello.ts
import { json } from 'solid-start/server';

export function GET() {
  return json({ message: 'Hello' });
}

export function POST({ request }) {
  return json({ received: true });
}
```

### 6. Type Safety Approach

- **Solid is written in TypeScript** — first-class TS support
- **`createSignal<T>()`** — generic type parameter
- **`useRouteData<typeof routeData>()`** — inferred types from route data
- **Server functions** are typed end-to-end (parameters and return types)
- **JSX type system** — Solid uses its own JSX types
- **No built-in Zod integration** — validation is manual or via community libs
- Vinxi provides some type-safe config

### 7. Build and Deployment Model

- **Build:** Vinxi (Vite-based build tool by the Solid team)
- **Output:** Node.js server bundle + client assets
- **Deployment targets:** Node.js (default), Cloudflare Workers, Vercel, Netlify
- **`solid-start` adapters** for different platforms
- Vite plugins ecosystem is available
- **Hot Module Replacement** via Vite

### 8. Unique Innovations

- **Fine-grained reactivity without virtual DOM** — Solid signals update exact DOM nodes; no diffing
- **Compile-time reactivity** — the Solid compiler transforms reactive expressions (though less "magic" than Svelte)
- **Server functions with `"use server"`** — clean RPC boundary
- **No hooks rules** — Solid signals can be used anywhere (not limited to component bodies)
- **`<For>` keyed list rendering** — efficient DOM recycling without virtual DOM
- **Smallest bundle size** among React-like frameworks (~7KB gzip for Solid core)
- **True streaming SSR** — Suspense boundaries create natural streaming points

---

## TanStack Start

### 1. Core Architecture

**Type-safe fullstack framework built on TanStack Router:**
- **TanStack Router** is the foundation — provides fully type-safe routing with search params
- **Server functions** (`createServerFn`) replace loaders/actions with a unified API
- File-based routing via route tree generation
- Built on Vinxi (same as SolidStart)

**Routing Architecture:**
```tsx
// Route definition with full type safety
// src/routes/posts/$postId.tsx
import { createFileRoute } from '@tanstack/react-router';

export const Route = createFileRoute('/posts/$postId')({
  component: PostPage,
  validateSearch: (search) =>
    z.object({ edit: z.boolean().optional() }).parse(search),
});

function PostPage() {
  const { postId } = Route.useParams(); // typed!
  const search = Route.useSearch();     // typed! { edit?: boolean }
  // ...
}
```

**Route tree generation:**
```bash
# TanStack Router generates a route tree from your file structure
src/routes/
  __root.tsx              # Root route (layout)
  index.tsx               # /
  posts/
    index.tsx             # /posts
    $postId.tsx           # /posts/:postId
```
- Route tree is generated as `routeTree.gen.ts`
- Full IDE support with autocomplete for all routes, params, and search params

### 2. Rendering Strategies

| Strategy | How it works |
|---|---|
| **SSR** | Default. Server renders the React component tree. |
| **SSG** | Via `static` option in route config. Pre-render at build time. |
| **SPA** | Fallback mode. Client-only rendering. |
| **Streaming** | Built-in via React Suspense boundaries. |

**Route-level rendering config:**
```tsx
export const Route = createFileRoute('/posts')({
  component: PostsPage,
  // SSR by default, but can configure per-route
});
```

### 3. Data Fetching Patterns

**Server Functions:**
```tsx
import { createServerFn } from '@tanstack/start';

// Define a server function
export const getPosts = createServerFn({ method: 'GET' })
  .handler(async () => {
    return await db.posts.findMany();
  });

export const createPost = createServerFn({ method: 'POST' })
  .validator((data: { title: string }) => data)
  .handler(async ({ data }) => {
    return await db.posts.create({ data });
  });
```

**Route loaders with server functions:**
```tsx
export const Route = createFileRoute('/posts')({
  loader: async ({ context }) => {
    const posts = await getPosts();
    return { posts };
  },
  component: PostsPage,
});

function PostsPage() {
  const { posts } = Route.useLoaderData(); // typed!
}
```

**Key patterns:**
- `createServerFn` creates an RPC endpoint with type-safe input/output
- `.validator()` middleware for input validation (Zod compatible)
- Server functions can be called from anywhere (loaders, components, event handlers)
- **Search params are validated** via `validateSearch` — Zod schemas for URL search params
- **Optimistic updates** via TanStack Query integration

### 4. File Conventions and DX

```
src/
  routes/
    __root.tsx              # Root route (required)
    index.tsx               # /
    posts/
      index.tsx             # /posts
      $postId.tsx           # /posts/:postId (dynamic)
    _auth/
      login.tsx             # /login (layout route)
  routeTree.gen.ts          # Auto-generated route tree (do not edit)
  client.tsx                # Client entry
  server.tsx                # Server entry
  router.tsx                # Router configuration
```

- **Route tree generation** — files are scanned and a typed route tree is generated
- **`__root.tsx`** — the root layout that all routes render inside
- **`_folder`** — layout routes (folder doesn't add to URL)
- **`$param`** — dynamic route parameters
- **Search param validation** integrated into route definitions

### 5. Middleware/Plugin System

**Server function middleware:**
```tsx
const withAuth = (fn) => createServerFn()
  .middleware([authMiddleware])
  .handler(async ({ context, data }) => {
    // context.user is populated by authMiddleware
    return fn(context, data);
  });
```

**Router middleware:**
```tsx
// src/router.tsx
const router = createRouter({
  routeTree,
  context: { user: null },
});

// BeforeLoad hook per route
export const Route = createFileRoute('/admin')({
  beforeLoad: ({ context }) => {
    if (!context.user) throw redirect({ to: '/login' });
  },
});
```

### 6. Type Safety Approach

**The most type-safe framework in this comparison:**
- **100% type-safe routing** — routes, params, and search params are all inferred
- **`Link` component** — `<Link to="/posts/$postId" params={{ postId: '123' }} />` — typos are compile errors
- **`useNavigate()`** — typed navigation: `navigate({ to: '/posts/$postId', params: { postId: '123' } })`
- **Search params** — Zod-validated and typed: `Route.useSearch()` returns the parsed type
- **Loader data** — `Route.useLoaderData()` returns the typed loader return value
- **Server functions** — input validator + handler = end-to-end type safety
- **Route context** — typed across middleware chain
- **Auto-generated route tree** — the `routeTree.gen.ts` file contains full type information

```tsx
// This is a compile error if postId is missing or /postz is a typo:
<Link to="/postz/$postId" params={{ postId: '123' }} />

// Search params are validated:
// URL: /posts/123?edit=true  →  { edit: true }
// URL: /posts/123?edit=yes   →  Zod validation error
```

### 7. Build and Deployment Model

- **Build:** Vinxi (Vite-based)
- **Output:** Node.js server + client bundle
- **Deployment:** Node.js (default), with adapters for Vercel, Cloudflare Workers
- **Still in beta/RC** — not yet 1.0 stable
- Vite plugin ecosystem available
- **Dev server:** Vite with HMR

### 8. Unique Innovations

- **100% type-safe routing** — the gold standard for TypeScript routing
- **`createServerFn`** — unified server function API with middleware chain
- **Search params as a first-class concern** — Zod-validated, typed search params
- **Route context passing** — typed context flows through middleware to loaders to components
- **TanStack Query integration** — server functions integrate with TanStack Query for caching and optimistic updates
- **Code-generated route tree** — eliminates runtime route matching overhead and enables full type safety
- **Before load / after load hooks** — per-route guards and post-processing

---

## Feature Comparison Matrix

| Feature | Next.js 15 | Nuxt 4 | SvelteKit 2 | Remix | Astro | SolidStart | TanStack Start |
|---|:---:|:---:|:---:|:---:|:---:|:---:|:---:|
| **UI Framework** | React | Vue | Svelte | React | Multi-framework | Solid | React |
| **Routing** | File-system (app/) | File-system (pages/) | File-system (routes/) | File-system (routes/) | File-system (pages/) | File-system (routes/) | File-system + code-gen |
| **Default Rendering** | SSR (RSC) | SSR | SSR | SSR | SSG | SSR | SSR |
| **SSG** | ✅ | ✅ | ✅ | ❌ (3rd party) | ✅ (default) | ✅ | ✅ |
| **SSR** | ✅ | ✅ | ✅ | ✅ | ✅ (opt-in) | ✅ | ✅ |
| **ISR / SWR** | ✅ | ✅ (SWR) | ❌ | ❌ (HTTP cache) | ❌ | ❌ | ❌ |
| **Streaming SSR** | ✅ (Suspense) | ✅ (experimental) | ✅ (Svelte 5) | ✅ (defer) | ❌ | ✅ (Suspense) | ✅ (Suspense) |
| **Partial Hydration** | ✅ (RSC) | ❌ | ❌ | ❌ | ✅ (Islands) | ❌ | ❌ |
| **Server Components** | ✅ (RSC) | ❌ | ❌ | ❌ | ✅ (.astro) | ❌ | ❌ |
| **Server Actions** | ✅ | ❌ (server routes) | ✅ (form actions) | ✅ (actions) | ❌ | ✅ | ✅ (server fns) |
| **Progressive Enhancement** | ✅ (forms) | ✅ | ✅ (best) | ✅ (best) | ✅ (by default) | ✅ | ❌ |
| **Nested Layouts** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Parallel Data Loading** | ✅ (RSC) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **File-based API Routes** | ✅ (route.ts) | ✅ (server/api/) | ✅ (+server.ts) | ✅ (resource routes) | ✅ (pages/api/) | ✅ (api/) | ✅ (server fns) |
| **Middleware** | ✅ (Edge) | ✅ (Nitro) | ✅ (hooks) | Manual wrappers | ✅ | ✅ | ✅ (per-fn) |
| **Auto-imports** | ❌ | ✅ (best) | ✅ ($lib) | ❌ | ❌ | ❌ | ❌ |
| **Type-safe Routing** | ❌ | ⚠️ (partial) | ✅ ($types) | ✅ (loader inference) | ❌ | ✅ | ✅ (best) |
| **Type-safe API Layer** | ⚠️ (3rd party) | ⚠️ (3rd party) | ✅ | ✅ | ✅ (content) | ✅ | ✅ (best) |
| **Content Collections** | ❌ | ✅ (@nuxt/content) | ❌ | ❌ | ✅ (built-in) | ❌ | ❌ |
| **Multi-framework** | ❌ | ❌ | ❌ | ❌ | ✅ (unique) | ❌ | ❌ |
| **Bundler** | Turbopack/Webpack | Vite + Nitro | Vite | esbuild | Vite | Vinxi (Vite) | Vinxi (Vite) |
| **Edge Runtime** | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ | ⚠️ |
| **Zero-config Deploy** | ✅ (Vercel) | ✅ (Nitro presets) | ✅ (adapters) | ✅ (adapters) | ✅ (adapters) | ⚠️ | ⚠️ |
| **Stability** | 15.x (mature) | 4.x (mature) | 2.x (mature) | 2.x (mature) | 4.x (mature) | 1.x (stable) | Beta/RC |
| **Bundle Size (framework)** | Large (React) | Medium (Vue) | Small (Svelte) | Large (React) | Tiny (no JS default) | Tiny (Solid) | Large (React) |

---

## Killer Features for a New Framework

Based on this analysis, here are the features a new Go-based fullstack framework should absolutely have, with inspiration sources:

### 1. **File-System Routing with Type-Safe Route Generation**
> *Inspired by: TanStack Start (code-gen), SvelteKit (`+` convention)*

The route tree should be generated from the file system, producing compiled type information. In Go, this could be a code generator that produces route registries, path-to-handler maps, and typed parameter structs — all at compile time with zero runtime cost.

**Why it's killer:** Developers get IDE autocomplete for routes, compile-time error checking for broken links, and zero-cost routing. TanStack Start proves this is possible; Go's code generation makes it even more natural.

### 2. **Server Functions (RPC Boundary)**
> *Inspired by: Next.js (Server Actions), SolidStart, TanStack Start (`createServerFn`)*

A `//go:generate` or annotation-based system that turns Go functions into type-safe RPC endpoints. The client calls a Go function as if it were local — serialization, HTTP transport, and error handling are automatic.

**Why it's killer:** Eliminates the boilerplate of writing API routes, request parsing, and response serialization. The function signature IS the API contract. TypeScript frameworks are converging on this pattern; Go's type system makes it even more powerful.

### 3. **Progressive Enhancement / HTML-First Forms**
> *Inspired by: Remix (actions), SvelteKit (form actions)*

Forms should work without JavaScript. Server-side form handlers process submissions, return HTML, and optionally enhance with client-side JS for optimistic updates and animations.

**Why it's killer:** Accessibility, resilience, and SEO. Remix and SvelteKit prove that "works without JS" and "delightful with JS" are not mutually exclusive.

### 4. **Hybrid Rendering (Per-Route Strategy)**
> *Inspired by: Nuxt (routeRules), Next.js (segment config), Astro (prerender opt-in/out)*

Each route should be individually configurable for rendering strategy:
- **Static** — pre-rendered at build time (SSG)
- **Dynamic** — server-rendered per request (SSR)
- **Cached** — server-rendered with TTL/stale-while-revalidate (ISR/SWR)
- **SPA** — client-only rendering

```go
//go:route /posts cache=60s
//go:route /admin dynamic
//go:route /about static
```

**Why it's killer:** One framework handles every use case. Nuxt's `routeRules` and Astro's `output: 'hybrid'` show this is the right abstraction level.

### 5. **Islands Architecture / Selective Hydration**
> *Inspired by: Astro (islands), Next.js (RSC partial hydration)*

Static HTML by default. Interactive components are explicitly marked as "islands" that hydrate independently. This ships minimal JavaScript and hydrates only what's needed.

**Why it's killer:** Astro proves that most pages are mostly static. Islands let you add interactivity precisely where needed. In Go, the server renders the HTML shell; the client hydrates only the marked islands.

### 6. **Auto-Generated API Layer from Server Functions**
> *Inspired by: TanStack Start (validator + handler), Next.js (Server Actions)*

Server functions should automatically generate:
- Type-safe client SDKs (TypeScript, Go)
- OpenAPI/Swagger documentation
- Input validation (from Go struct tags → JSON schema)

**Why it's killer:** Write the server function once; get a typed client, docs, and validation for free. No framework does this completely today.

### 7. **Middleware Chain with Typed Context**
> *Inspired by: SvelteKit (hooks + event.locals), TanStack Start (middleware chain)*

A composable middleware system where context is typed and flows through the chain:

```go
func AuthMiddleware(ctx *Context, next Handler) *Response {
    user := authenticate(ctx.Request)
    ctx.Set("user", user)  // typed: ctx.User is *User
    return next(ctx)
}
```

**Why it's killer:** SvelteKit's `event.locals` pattern is elegant but untyped. TanStack Start's middleware chain is typed but complex. A Go framework can do both: compile-time type safety + runtime simplicity.

### 8. **Streaming SSR with Granular Suspense**
> *Inspired by: Next.js (loading.tsx), Remix (defer), SolidStart (Suspense)*

The server streams HTML in chunks. Slow data sources don't block fast ones. Loading UI is shown instantly for pending chunks.

**Why it's killer:** Perceived performance. Users see content progressively instead of waiting for everything. Every major framework converges on this because it works.

### 9. **Content Collections with Schema Validation**
> *Inspired by: Astro (content collections)*

A built-in system for managing Markdown/MDX content with:
- Schema validation (struct tags in Go → validation rules)
- Type-safe queries
- Build-time error checking
- Frontmatter parsing

**Why it's killer:** Astro is the only framework that does this well. Content sites are a huge market. Go's struct tags make schema definition natural.

### 10. **Multi-Template Engine Support**
> *Inspired by: Astro (multi-framework), Go's `html/template` ecosystem*

Support multiple template engines as first-class citizens:
- Go `html/template` (default, secure)
- Templ (type-safe Go HTML components)
- Preact/Alpine.js islands for interactivity
- Markdown for content pages

**Why it's killer:** Astro's multi-framework support is its killer feature for content sites. A Go framework should do the same but with Go-native template engines.

### 11. **Universal Build with Adapter Presets**
> *Inspired by: Nuxt (Nitro presets), SvelteKit (adapters)*

One build command, multiple deployment targets:
- Binary (self-contained Go binary serving static + dynamic)
- Docker (minimal Alpine image)
- AWS Lambda / Cloudflare Workers (serverless)
- Static export (CDN-only)
- Kubernetes (health checks, metrics built-in)

**Why it's killer:** Nuxt's Nitro engine proves this works. Go's static compilation makes it even simpler — a single binary is the simplest deployment artifact possible.

### 12. **Co-Location with Clear Boundaries**
> *Inspired by: SvelteKit (`+` prefix), Next.js (`page.tsx` convention)*

Route files, server logic, styles, and tests live in the same directory. Framework files are clearly distinguished (by naming convention or directory structure). Private files are excluded from routing.

**Why it's killer:** SvelteKit's `+` prefix is the cleanest solution. Next.js's `page.tsx` convention is also good. Co-location reduces context switching and keeps related code together.

### 13. **Fine-Grained Cache Invalidation**
> *Inspired by: Next.js (revalidatePath, revalidateTag), Remix (automatic revalidation)*

After a mutation, the framework knows which cached data is stale and revalidates only what's needed. Tag-based and path-based invalidation.

**Why it's killer:** Next.js's `revalidateTag` is powerful but complex. Remix's automatic revalidation is simple but coarse. A Go framework should offer both: simple automatic revalidation with opt-in fine-grained control.

### 14. **Zero-Config Dev Experience**
> *Inspired by: Nuxt (auto-imports, zero config), Next.js (`create-next-app`)*

Running `go run .` or `gofastr dev` should:
- Start a hot-reloading dev server
- Auto-generate route types
- Auto-discover server functions
- Provide a dashboard with routes, functions, and errors
- Auto-reload on file changes

**Why it's killer:** Nuxt's zero-config philosophy wins developers. In Go, `go generate` + file watching can achieve similar DX without the overhead of a Node.js toolchain.

---

## Summary: Architecture Recommendations for a Go Framework

Based on this research, a Go-based fullstack framework should combine:

1. **Astro's islands** — HTML-first with opt-in interactivity
2. **TanStack Start's type safety** — compile-time verified routes and API calls
3. **Remix's loader/action pattern** — simple, progressive-enhancement-friendly data flow
4. **Nuxt's hybrid rendering** — per-route rendering strategy
5. **SvelteKit's file conventions** — clear, co-located route files with `+` prefix distinction
6. **Next.js's streaming + caching** — granular streaming SSR with fine-grained cache control
7. **Go's superpower** — single binary deployment with zero runtime dependencies

The killer differentiator: **compile-time everything**. Go's type system and code generation enable type-safe routing, validated server functions, and optimized builds — all verified before the application runs. No JavaScript framework can match this because they lack a compile step as powerful as Go's.
