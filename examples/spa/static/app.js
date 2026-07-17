const { createApp, ref, onMounted, defineComponent } = Vue
const { createRouter, createWebHistory } = VueRouter

// ── API helpers ─────────────────────────────────────────

async function api(url) {
  const res = await fetch(url)
  if (!res.ok) throw new Error(`API ${res.status}`)
  const json = await res.json()
  return json.data
}

// ── Views ───────────────────────────────────────────────
// Each view has a SINGLE root <div> for Vue <Transition> compatibility.

const Home = defineComponent({
  setup() {
    const articles = ref([])
    const loading = ref(true)

    onMounted(async () => {
      try {
        articles.value = await api('/api/articles')
      } finally {
        loading.value = false
      }
    })

    return { articles, loading }
  },
  template: `
    <div>
      <h1>Welcome to the SPA Demo</h1>
      <p>A single-page app powered by <strong>Vue 3</strong> + <strong>Vue Router</strong> (from jsdelivr CDN) with a GoFastr JSON API backend.</p>
      <h2>Recent Articles</h2>
      <div v-if="loading" class="loading">Loading...</div>
      <router-link v-for="a in articles" :key="a.id" :to="'/articles/' + a.id" class="card">
        <span class="tag">{{ a.category || 'general' }}</span>
        <h3>{{ a.title }}</h3>
        <p class="meta">{{ a.summary }}</p>
      </router-link>
    </div>
  `
})

const ArticleList = defineComponent({
  setup() {
    const articles = ref([])
    const loading = ref(true)

    onMounted(async () => {
      try {
        articles.value = await api('/api/articles')
      } finally {
        loading.value = false
      }
    })

    return { articles, loading }
  },
  template: `
    <div>
      <router-link to="/" class="back">\u2190 Home</router-link>
      <h1>Articles</h1>
      <div v-if="loading" class="loading">Loading...</div>
      <router-link v-for="a in articles" :key="a.id" :to="'/articles/' + a.id" class="card">
        <span class="tag">{{ a.category || 'general' }}</span>
        <h3>{{ a.title }}</h3>
        <p class="meta">{{ a.summary }}</p>
      </router-link>
    </div>
  `
})

const ArticleDetail = defineComponent({
  setup() {
    const article = ref(null)
    const loading = ref(true)
    const error = ref(null)

    onMounted(async () => {
      try {
        const id = window.location.pathname.split('/').pop()
        article.value = await api('/api/articles/' + id)
      } catch (e) {
        error.value = e.message
      } finally {
        loading.value = false
      }
    })

    return { article, loading, error }
  },
  template: `
    <div>
      <router-link to="/articles" class="back">\u2190 Articles</router-link>
      <div v-if="loading" class="loading">Loading...</div>
      <div v-else-if="error" class="error">{{ error }}</div>
      <div v-else>
        <span class="tag">{{ article.category || 'general' }}</span>
        <h1>{{ article.title }}</h1>
        <p>{{ article.summary }}</p>
        <div class="detail-body" style="margin-top:1.5rem">{{ article.body }}</div>
      </div>
    </div>
  `
})

const ProjectList = defineComponent({
  setup() {
    const projects = ref([])
    const loading = ref(true)

    onMounted(async () => {
      try {
        projects.value = await api('/api/projects')
      } finally {
        loading.value = false
      }
    })

    return { projects, loading }
  },
  template: `
    <div>
      <router-link to="/" class="back">\u2190 Home</router-link>
      <h1>Projects</h1>
      <div v-if="loading" class="loading">Loading...</div>
      <div v-for="p in projects" :key="p.id" class="card" style="cursor:default">
        <h3>{{ p.name }}</h3>
        <p class="meta">{{ p.description }}</p>
        <a v-if="p.url" :href="p.url" target="_blank" class="project-link">{{ p.url }}</a>
      </div>
    </div>
  `
})

const About = {
  template: `
    <div>
      <router-link to="/" class="back">\u2190 Home</router-link>
      <h1>About</h1>
      <p>This is a single-page application demo built with <strong>GoFastr</strong>.</p>
      <h2>How it works</h2>
      <ul>
        <li>The Go server provides a JSON API via GoFastr entities (auto-CRUD)</li>
        <li>Static files (HTML, CSS, JS) are served with <strong>SPA mode</strong> enabled</li>
        <li><strong>Vue 3 + Vue Router</strong> loaded from jsdelivr CDN \u2014 no build step</li>
        <li>Client-side routing uses <strong>History API</strong> (real URLs, not hash fragments)</li>
        <li>Any unmatched path falls through to <code>index.html</code> \u2014 the SPA handles it</li>
      </ul>
      <h2>API Endpoints</h2>
      <ul>
        <li><code>GET /api/articles</code> \u2014 list articles</li>
        <li><code>GET /api/articles/:id</code> \u2014 get article</li>
        <li><code>GET /api/projects</code> \u2014 list projects</li>
        <li><code>GET /api/site</code> \u2014 site metadata</li>
      </ul>
      <h2>Tech Stack</h2>
      <ul>
        <li><strong>Backend:</strong> GoFastr (Go)</li>
        <li><strong>Frontend:</strong> Vue 3 + Vue Router 4 (CDN)</li>
        <li><strong>Routing:</strong> Vue Router with <code>createWebHistory</code></li>
        <li><strong>Build:</strong> None \u2014 zero build step</li>
      </ul>
    </div>
  `
}

// ── Router ──────────────────────────────────────────────

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: Home },
    { path: '/articles', component: ArticleList },
    { path: '/articles/:id', component: ArticleDetail },
    { path: '/projects', component: ProjectList },
    { path: '/about', component: About },
  ],
})

// ── App ─────────────────────────────────────────────────

const app = createApp({
  template: `
    <nav>
      <span class="logo">GoFastr SPA</span>
      <div class="links">
        <router-link to="/">Home</router-link>
        <router-link to="/articles">Articles</router-link>
        <router-link to="/projects">Projects</router-link>
        <router-link to="/about">About</router-link>
      </div>
    </nav>
    <main class="page">
      <router-view v-slot="{ Component }">
        <transition name="fade" mode="out-in">
          <component :is="Component" />
        </transition>
      </router-view>
    </main>
    <footer>Built with GoFastr \u2014 Vue 3 SPA via CDN, zero build step</footer>
  `
})

app.use(router)
app.mount('#app')
