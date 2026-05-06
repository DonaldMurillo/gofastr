# Agentic UI Protocols & AI-Agent-to-Frontend Communication Patterns

> Research document for **gofastr** — mapping the emerging landscape of protocols and patterns where AI agents produce UI, not just data. The goal: design a framework for a world where the AI *is* the backend, MCP tool calls *are* routes, and the frontend is a streaming sink for agent events.

---

## Table of Contents

1. [AG-UI Protocol (Agent-UI Protocol)](#1-ag-ui-protocol-agent-ui-protocol)
2. [A2UI (AI-to-UI)](#2-a2ui-ai-to-ui)
3. [MCP (Model Context Protocol) Apps](#3-mcp-model-context-protocol-apps)
4. [Vercel AI SDK / AI SDK Patterns](#4-vercel-ai-sdk--ai-sdk-patterns)
5. [LangGraph / LangChain Agent UIs](#5-langgraph--langchain-agent-uis)
6. [OpenAI Structured Outputs / Function Calling as UI](#6-openai-structured-outputs--function-calling-as-ui)
7. [Emerging Patterns to Synthesize](#7-emerging-patterns-to-synthesize)
8. [What This Means for a Framework](#8-what-this-means-for-a-framework)

---

## 1. AG-UI Protocol (Agent-UI Protocol)

### 1.1 What Is It?

AG-UI (Agent-UI Protocol) is an emerging open protocol that defines how autonomous AI agents communicate with frontend applications in real-time. It treats the frontend not as a consumer of API responses, but as a **streaming sink for agent events** — a fundamentally different mental model from traditional request/response.

The core insight: agents don't just "return data." They *think*, they *use tools*, they *produce intermediate results*, they *collaborate*, and they *arrive at conclusions*. AG-UI makes all of these phases visible to the frontend as a typed event stream.

### 1.2 How It Works

AG-UI defines a **standardized event protocol** layered on top of Server-Sent Events (SSE) or WebSocket connections. An agent run emits a sequence of typed events:

```
Agent Run Start
  → RunStarted event
  → StepStarted event
    → TextMessageStart
    → TextMessageContent (streaming delta)
    → TextMessageEnd
    → ToolCallStart
    → ToolCallArgs (streaming JSON delta)
    → ToolCallEnd
    → StateSnapshot (agent's internal state at this point)
    → StepFinished event
  → StepStarted event
    → (more events...)
  → RunFinished event
```

Each event is a small, typed JSON object. The frontend subscribes to this event stream and renders UI incrementally as events arrive.

### 1.3 Event-Driven Architecture for Agent-to-UI Communication

The protocol defines a taxonomy of event types:

| Event Type | Purpose | UI Implication |
|------------|---------|----------------|
| `RunStarted` | Agent begins processing | Show loading state, disable inputs |
| `StepStarted` | Agent begins a reasoning step | Show step indicator |
| `TextMessageStart/Content/End` | Streaming text output | Render markdown progressively |
| `ToolCallStart/Args/End` | Agent invokes a tool | Render tool execution UI (spinner, status) |
| `ToolCallResult` | Tool returns a result | Render tool result (could be a table, chart, code block) |
| `StateSnapshot` | Agent's internal state | Update UI state (forms, selections, context) |
| `StepFinished` | Step complete | Transition step indicator |
| `RunFinished` | Agent processing complete | Re-enable inputs, show final state |
| `Error` | Something went wrong | Show error UI with retry option |
| `Handoff` | Agent delegates to another agent | Show agent transition UI |

This is **fundamentally different from REST or GraphQL**:
- **REST**: Request → Response. One round trip. The UI waits.
- **GraphQL**: Request → Response. One round trip. The UI waits.
- **AG-UI**: Subscribe → Stream of events. The UI renders progressively as the agent works.

### 1.4 Tool Call Rendering

One of AG-UI's most powerful patterns: **agent tool calls become UI elements**.

When an agent calls a tool, the frontend receives a `ToolCallStart` event with the tool name and arguments (streamed as partial JSON). The frontend can render this as:

```
🔍 Searching database for "users with unpaid invoices"...
```

When the tool returns, the frontend receives `ToolCallResult` with the structured output. This might render as:

```
✅ Found 23 users with unpaid invoices
┌──────────┬────────────────────┬──────────┐
│ Name     │ Email              │ Amount   │
├──────────┼────────────────────┼──────────┤
│ Alice K. │ alice@example.com  │ $1,200   │
│ Bob M.   │ bob@example.com    │ $890     │
│ ...      │ ...                │ ...      │
└──────────┴────────────────────┴──────────┘
```

The frontend doesn't need to know *which* tools the agent will call. It needs a **tool rendering registry** — a map from tool names to React/Vue/Svelte components:

```typescript
const toolRenderers = {
  search_database: DatabaseSearchRenderer,
  generate_chart: ChartRenderer,
  send_email: EmailComposerRenderer,
  create_record: FormRenderer,
  // Generic fallback for unknown tools
  default: GenericToolRenderer,
}
```

### 1.5 State Management Across Agent Turns

AG-UI introduces the concept of **agent state snapshots** — the agent periodically emits its internal state so the frontend can:

1. **Persist the conversation** — Resume later from any state snapshot
2. **Enable undo/redo** — Roll back to a previous agent state
3. **Support human-in-the-loop** — Pause agent execution, let user modify state, resume
4. **Restore after disconnection** — Reconnect and pick up where the agent left off

State snapshots follow a schema:

```json
{
  "type": "StateSnapshot",
  "thread_id": "thread_abc123",
  "run_id": "run_def456",
  "step_index": 3,
  "state": {
    "conversation": [...],
    "tool_results": [...],
    "user_preferences": {...},
    "pending_actions": [...]
  },
  "timestamp": "2026-05-05T10:30:00Z"
}
```

This means the frontend maintains a **causally-ordered event log** (similar to event sourcing), not just a current state. Any state can be reconstructed by replaying events.

### 1.6 Streaming Intermediate Results

AG-UI's streaming model allows the frontend to show:

- **Thinking**: The agent's reasoning process (if exposed)
- **Tool calls in progress**: "Searching..." with a spinner
- **Partial tool results**: First 10 rows appear while the query is still running
- **Multi-step reasoning**: Step 1 of N, Step 2 of N, etc.
- **Collaboration**: Agent A hands off to Agent B (visible transition)

This is a **progressive disclosure** model. The user sees work happening in real-time rather than staring at a loading spinner.

### 1.7 How This Differs from Traditional Request/Response

| Dimension | Traditional (REST/GraphQL) | AG-UI |
|-----------|---------------------------|-------|
| **Communication model** | Request → Response | Subscribe → Event stream |
| **Temporal model** | Synchronous (mostly) | Asynchronous, multi-step |
| **State location** | Server-side session | Event log, replayable |
| **UI update model** | Client re-fetches | Server pushes events |
| **Error model** | HTTP status codes | Typed error events with recovery |
| **Composability** | API endpoints | Agent tools + handoffs |
| **Interactivity** | User → Server → UI | User ↔ Agent ↔ Tools → UI |
| **Latency perception** | Loading spinner | Progressive rendering |

### 1.8 Implications for GoFastr

If GoFastr adopted AG-UI patterns:
- The framework would need a **streaming event emitter** as a first-class primitive
- Route handlers could return not just `Response` but `EventStream`
- The Go backend would maintain an **event log** per agent run (recoverable, replayable)
- The template engine would need to understand **streaming partial renders**
- MCP tool calls would emit AG-UI events directly

---

## 2. A2UI (AI-to-UI)

### 2.1 Core Concept

A2UI (AI-to-UI) describes a family of patterns where AI models output **structured UI descriptions** rather than (or in addition to) natural language. The AI becomes a UI *author*, and the frontend becomes a *renderer* for AI-produced descriptions.

The key shift: instead of a human designer creating a mockup → developer implementing it → data flowing into it, the **AI generates the UI description at runtime** based on the data, the user, and the context.

### 2.2 How AI Generates UI Descriptions That Get Rendered

The pattern works like this:

```
User Query
  → AI Agent processes query
  → Agent decides what UI to show
  → Agent outputs a UI description (JSON)
  → Frontend renders the description as interactive components
```

The UI description is typically a JSON structure that describes a component tree:

```json
{
  "type": "Page",
  "title": "Customer Analysis",
  "sections": [
    {
      "type": "MetricCard",
      "props": {
        "label": "Total Revenue",
        "value": "$1.2M",
        "trend": "+12%",
        "trendDirection": "up"
      }
    },
    {
      "type": "DataTable",
      "props": {
        "columns": ["Name", "Email", "Revenue", "Status"],
        "rows": [
          ["Alice K.", "alice@co.io", "$45,000", "Active"],
          ["Bob M.", "bob@co.io", "$38,000", "Active"]
        ],
        "sortable": true,
        "filterable": true
      }
    },
    {
      "type": "Chart",
      "props": {
        "chartType": "line",
        "title": "Revenue Over Time",
        "data": [...]
      }
    }
  ]
}
```

The frontend has a **component registry** that maps `type` strings to actual React/Vue/Svelte components. The AI never writes JSX — it writes JSON that *describes* JSX.

### 2.3 Structured Output as UI (JSON Schemas That Define Interfaces)

This pattern leverages LLM **structured output** capabilities (OpenAI's structured outputs, Anthropic's tool use, etc.). The AI is constrained to output valid JSON conforming to a schema:

```json
{
  "$schema": "https://ui-schema.dev/v1",
  "type": "object",
  "properties": {
    "layout": {
      "type": "string",
      "enum": ["single-column", "two-column", "dashboard", "list", "detail"]
    },
    "components": {
      "type": "array",
      "items": {
        "oneOf": [
          {"$ref": "#/$defs/MetricCard"},
          {"$ref": "#/$defs/DataTable"},
          {"$ref": "#/$defs/Chart"},
          {"$ref": "#/$defs/Form"},
          {"$ref": "#/$defs/Markdown"},
          {"$ref": "#/$defs/Alert"}
        ]
      }
    }
  },
  "$defs": {
    "MetricCard": {
      "type": "object",
      "properties": {
        "type": {"const": "MetricCard"},
        "props": {
          "type": "object",
          "properties": {
            "label": {"type": "string"},
            "value": {"type": "string"},
            "trend": {"type": "string"},
            "trendDirection": {"enum": ["up", "down", "flat"]}
          },
          "required": ["label", "value"]
        }
      }
    },
    "DataTable": { ... },
    "Chart": { ... },
    "Form": { ... }
  }
}
```

The JSON Schema IS the UI contract. The AI can only produce valid UI descriptions that the frontend knows how to render.

### 2.4 Component Generation from AI Output

The rendering pipeline:

```
AI Output (JSON)
  → JSON Schema validation
  → Component tree construction
  → Component registry lookup
  → React/Vue/Svelte render
  → Interactive UI
```

Implementation in a frontend framework:

```typescript
// Component registry
const registry: Record<string, React.ComponentType<any>> = {
  MetricCard: MetricCardComponent,
  DataTable: DataTableComponent,
  Chart: ChartComponent,
  Form: FormComponent,
  Markdown: MarkdownComponent,
  Alert: AlertComponent,
}

// Dynamic renderer
function A2UIRenderer({ description }: { description: UIDescription }) {
  return (
    <div className={layoutClasses[description.layout]}>
      {description.components.map((comp, i) => {
        const Component = registry[comp.type]
        if (!Component) return <FallbackRenderer key={i} data={comp} />
        return <Component key={i} {...comp.props} />
      })}
    </div>
  )
}
```

### 2.5 Dynamic UI Assembly from Agent Responses

The truly powerful pattern: the UI is **assembled at runtime** by the agent. Different users asking different questions get different UIs:

- "Show me sales data" → Dashboard with charts and tables
- "Help me create a new user" → Form with validation
- "What went wrong with this deployment?" → Timeline with error markers and logs
- "Compare these two products" → Side-by-side comparison view

The same agent, the same frontend, but completely different UI compositions. The agent is the "backend developer" — it decides what to show.

### 2.6 Security Considerations for A2UI

This pattern raises critical security questions:

1. **Injection prevention**: The AI must be constrained to only produce known component types. No arbitrary HTML or script injection.
2. **Data exposure**: The AI might inadvertently include sensitive data in UI descriptions. Need output filtering.
3. **Schema validation**: Every AI output must be validated against the component schema before rendering.
4. **Sandboxed rendering**: Components should be rendered in a sandbox that prevents them from accessing the DOM directly.
5. **Audit trail**: Every AI-generated UI should be logged for security review.

---

## 3. MCP (Model Context Protocol) Apps

### 3.1 MCP as an Application Architecture, Not Just a Tool Protocol

The Model Context Protocol (Anthropic, open-sourced 2024) was designed as a way for AI assistants to connect to external tools and data sources. But it's evolving into something much bigger: **a complete application architecture.**

In a traditional web app:
```
Browser → HTTP → Web Server → Business Logic → Database
```

In an MCP app:
```
AI Agent → MCP Protocol → MCP Server (the "app") → Business Logic → Database
```

The MCP server IS the application. The AI agent is the "browser." MCP tools are the "routes." MCP resources are the "data."

### 3.2 MCP Servers as Backend Services

An MCP server can serve as a complete backend:

```
my-mcp-app/
├── server.ts          # MCP server entry point
├── tools/
│   ├── users.ts       # User CRUD tools
│   ├── orders.ts      # Order management tools
│   ├── reports.ts     # Report generation tools
│   └── search.ts      # Search tools
├── resources/
│   ├── schema.ts      # Database schema as MCP resources
│   └── docs.ts        # API documentation as MCP resources
├── prompts/
│   ├── onboarding.ts  # Onboarding prompt templates
│   └── reporting.ts   # Report generation prompts
└── db/
    └── queries.ts     # Database layer
```

The MCP server exposes:
- **Tools**: Callable actions (like API endpoints)
- **Resources**: Readable data (like GET endpoints)
- **Prompts**: Reusable prompt templates (like view templates)

### 3.3 MCP Tools as Composable Building Blocks

MCP tools are the new "routes." But unlike HTTP routes, they're:

1. **Self-describing**: Each tool has a name, description, and JSON Schema for its inputs/outputs
2. **Composable**: An AI agent can chain tools together (call tool A, use result to call tool B)
3. **Discoverable**: The agent can list available tools at runtime
4. **Type-safe**: Input/output schemas are validated automatically

```typescript
// Traditional: multiple HTTP endpoints
POST /api/users          → Create user
GET  /api/users/:id      → Get user
PUT  /api/users/:id      → Update user
POST /api/emails/welcome → Send welcome email

// MCP: tools the agent can compose
Tool: create_user(name, email, role) → { user_id, ... }
Tool: get_user(user_id) → { name, email, role, ... }
Tool: update_user(user_id, updates) → { ... }
Tool: send_welcome_email(user_id) → { sent: true }

// The AI agent composes these naturally:
// "Create a user and send them a welcome email"
// → Calls create_user → Gets user_id → Calls send_welcome_email(user_id)
```

### 3.4 Building Full Applications as MCP Tool Compositions

A complete application can be expressed as a set of MCP tools:

```typescript
// E-commerce app as MCP tools
const ecommerceTools = {
  // Product catalog
  search_products: { /* search by query, category, price */ },
  get_product: { /* product details */ },
  compare_products: { /* side-by-side comparison */ },

  // Shopping cart
  add_to_cart: { /* add item to cart */ },
  view_cart: { /* see cart contents */ },
  update_cart_item: { /* change quantity */ },
  remove_from_cart: { /* remove item */ },

  // Checkout
  start_checkout: { /* begin checkout flow */ },
  add_shipping_address: { /* shipping details */ },
  select_shipping_method: { /* shipping option */ },
  add_payment: { /* payment method (tokenized) */ },
  place_order: { /* complete purchase */ },

  // Order management
  get_order: { /* order details */ },
  list_orders: { /* order history */ },
  cancel_order: { /* cancel if possible */ },
  track_order: { /* shipping tracking */ },
}
```

An AI agent can handle **any user request** by composing these tools:

- "I want to buy running shoes" → `search_products("running shoes")` → show results → `add_to_cart` → `view_cart` → `start_checkout` → ...
- "Where's my last order?" → `list_orders(limit: 1)` → `track_order(order_id)` → show tracking
- "Cancel order #1234" → `get_order(1234)` → verify status → `cancel_order(1234)`

**No UI code needed.** The AI agent is the interface.

### 3.5 Stateful MCP Interactions (Sessions, Context, Memory)

MCP is evolving to support stateful interactions:

**Sessions**: An MCP server can maintain session state across multiple tool calls:
```typescript
// Session-based cart
add_to_cart(session_id, product_id, qty)  // Server remembers cart per session
view_cart(session_id)                      // Returns session's cart
checkout(session_id)                       // Uses session's cart + shipping + payment
```

**Context**: The MCP protocol supports context passing — the AI agent can pass conversation history, user preferences, and previous tool results as context to new tool calls:
```typescript
get_recommended_products(context: {
  user_id: "usr_123",
  browsing_history: [...],
  cart_contents: [...],
  preferences: { category: "running", price_max: 150 }
})
```

**Memory**: MCP servers can maintain long-term memory:
- User preferences (persisted across sessions)
- Interaction history (what the user has done before)
- Learned patterns (the user always buys on Tuesdays)

### 3.6 MCP Resources as Dynamic Data Sources

MCP resources provide read-only access to data, similar to GET endpoints but with a URI-based addressing model:

```
resource://users/123           → User profile
resource://products/search?q=x  → Search results
resource://docs/api             → API documentation
resource://schema/orders        → Database schema for orders
resource://config/features      → Feature flags
```

Resources are **discoverable** — an AI agent can list all available resources and their schemas, then decide what to read. This is like HATEOAS (Hypermedia as the Engine of Application State) but for AI agents instead of browsers.

### 3.7 Implications for GoFastr

GoFastr already plans a built-in MCP server. But the MCP-as-app pattern suggests:

1. **Every GoFastr app is automatically an MCP server** — routes are exposed as MCP tools, models as MCP resources
2. **The GoFastr MCP server is dual-purpose** — it serves both AI coding agents (development) and AI runtime agents (production)
3. **Route handlers and MCP tools converge** — the same Go function handles both HTTP requests and MCP tool calls
4. **MCP becomes the API layer** — instead of hand-crafting REST/GraphQL APIs, you define MCP tools and get both an AI interface AND a programmatic API

---

## 4. Vercel AI SDK / AI SDK Patterns

### 4.1 Overview

The Vercel AI SDK (now just "AI SDK") is the most production-ready implementation of agent-to-frontend streaming patterns. It provides React hooks and server-side utilities for streaming AI responses directly into UI components.

### 4.2 Streaming UI from Server-Side AI Calls

The core pattern:

```
Server (Route Handler):
  → Call LLM API
  → Stream response as Server-Sent Events (SSE)
  → Include tool calls in the stream

Client (React Component):
  → useChat() hook subscribes to SSE stream
  → Renders streaming text progressively
  → Renders tool calls as React components
  → Handles loading, error, and complete states
```

Server-side (Next.js route handler):
```typescript
// app/api/chat/route.ts
import { streamText } from 'ai'
import { openai } from '@ai-sdk/openai'

export async function POST(req: Request) {
  const { messages } = await req.json()

  const result = streamText({
    model: openai('gpt-4o'),
    messages,
    tools: {
      get_weather: {
        description: 'Get weather for a location',
        parameters: z.object({
          location: z.string(),
        }),
        execute: async ({ location }) => {
          return { temperature: 72, condition: 'sunny' }
        },
      },
    },
  })

  return result.toDataStreamResponse()
}
```

Client-side:
```tsx
// app/page.tsx
'use client'
import { useChat } from 'ai/react'

export default function ChatPage() {
  const { messages, input, handleInputChange, handleSubmit, isLoading } = useChat()

  return (
    <div>
      {messages.map(message => (
        <div key={message.id}>
          <p>{message.role}: {message.content}</p>
          {/* Tool invocations render as React components */}
          {message.toolInvocations?.map(tool => (
            <ToolRenderer key={tool.toolCallId} tool={tool} />
          ))}
        </div>
      ))}

      <form onSubmit={handleSubmit}>
        <input value={input} onChange={handleInputChange} />
        <button type="submit" disabled={isLoading}>Send</button>
      </form>
    </div>
  )
}
```

### 4.3 The Core Hooks

| Hook | Purpose | Pattern |
|------|---------|---------|
| `useChat` | Chat-style back-and-forth | Messages array, streaming responses, tool calls |
| `useCompletion` | Single prompt → completion | One-shot text generation |
| `useObject` | Stream a structured JSON object | Progressive JSON building, schema-validated |
| `useAssistant` | OpenAI Assistant API | Thread-based, with tool use |

**`useObject` is particularly interesting** — it streams a JSON object progressively, validating each chunk against a Zod schema:

```typescript
// Stream a structured object to the client
const { object, isLoading } = useObject({
  api: '/api/generate',
  schema: z.object({
    characters: z.array(z.object({
      name: z.string(),
      class: z.string(),
      level: z.number(),
      items: z.array(z.string()),
    })),
  }),
})

// object builds up progressively:
// {} → { characters: [] } → { characters: [{ name: "Aria" }] } → ...
```

### 4.4 Tool Calls That Render React Components

The AI SDK's most powerful feature: **tool calls rendered as React components.**

When the LLM decides to call a tool, the SDK streams the tool call to the client. The client can render a React component for that tool call:

```tsx
function ToolRenderer({ tool }: { tool: ToolInvocation }) {
  switch (tool.toolName) {
    case 'get_weather':
      // Tool is executing
      if (tool.state === 'call') {
        return <WeatherLoader location={tool.args.location} />
      }
      // Tool has completed
      if (tool.state === 'result') {
        return <WeatherCard weather={tool.result} />
      }
      break

    case 'search_products':
      if (tool.state === 'call') {
        return <SearchSpinner query={tool.args.query} />
      }
      if (tool.state === 'result') {
        return <ProductGrid products={tool.result} />
      }
      break

    default:
      return <GenericToolCall tool={tool} />
  }
}
```

This means the AI **determines what components appear on screen**. The frontend is a rendering substrate for AI-driven UI composition.

### 4.5 Partial Streaming and Progressive Rendering

The AI SDK streams partial results at multiple levels:

1. **Text**: Characters arrive incrementally (typewriter effect)
2. **Tool arguments**: JSON arguments stream in partial chunks
3. **Tool results**: Results appear when the tool completes
4. **Structured objects**: JSON objects build up field by field
5. **Reasoning**: Some models expose thinking tokens (chain-of-thought)

The UI **never waits for a complete response**. Every chunk updates the UI immediately.

### 4.6 How This Changes the Frontend Paradigm

| Traditional Frontend | AI SDK Frontend |
|---------------------|-----------------|
| Routes defined at build time | UI determined at runtime by AI |
| Components placed by developer | Components placed by AI tool calls |
| Data fetched then rendered | Data streamed and rendered simultaneously |
| Loading states are generic spinners | Loading states are AI-visible work (thinking, tool calls) |
| Error handling is try/catch | Error handling is event-based (stream errors, tool failures) |
| State is client-side | State is conversation + tool results |
| Navigation is URL-based | Navigation is conversation-based |
| Forms submit to endpoints | Forms submit to AI agent as messages |

### 4.7 What GoFastr Can Learn

The AI SDK patterns translate to Go:

- **SSE as a first-class response type** — GoFastr handlers can return `EventStream` that emits typed events
- **Tool registry** — Define Go functions as tools with JSON Schema, auto-expose to AI agents
- **Streaming partials** — The template engine can render partial HTML chunks and stream them
- **Progressive rendering** — HTMX + SSE means GoFastr can stream UI updates without JavaScript

---

## 5. LangGraph / LangChain Agent UIs

### 5.1 Agent State Machines with UI Rendering

LangGraph models AI agents as **state machines** (directed graphs) where:
- **Nodes** are agent steps (reasoning, tool use, human review)
- **Edges** are transitions (conditional on state)
- **State** is a typed dictionary that persists across steps

```python
from langgraph.graph import StateGraph, END

class AgentState(TypedDict):
    messages: list[BaseMessage]
    user_query: str
    search_results: list[dict] | None
    draft_response: str | None
    requires_approval: bool
    approved: bool | None

# Define the graph
graph = StateGraph(AgentState)

# Nodes
graph.add_node("understand_query", understand_query)
graph.add_node("search", search_tool)
graph.add_node("draft_response", draft_response)
graph.add_node("human_review", human_review)  # Pause for human input
graph.add_node("send_response", send_response)

# Edges (transitions)
graph.add_edge("understand_query", "search")
graph.add_edge("search", "draft_response")
graph.add_conditional_edges("draft_response", should_review, {
    True: "human_review",
    False: "send_response",
})
graph.add_edge("human_review", "send_response")
graph.add_edge("send_response", END)
```

Each node can emit UI events. The frontend renders a **step-by-step visualization** of the agent's progress through the graph.

### 5.2 Human-in-the-Loop Patterns

LangGraph's most impactful pattern for UI: **interrupt points** where the agent pauses and waits for human input.

```python
@node
def draft_email(state: AgentState) -> AgentState:
    draft = llm.generate(f"Draft an email about: {state['topic']}")
    return {
        **state,
        "draft_email": draft,
        "requires_approval": True,  # This triggers an interrupt
    }
```

The frontend renders:
```
🤖 Agent has drafted an email. Please review:

┌─────────────────────────────────────────────────────────┐
│  Subject: Project Update                                │
│                                                         │
│  Hi team,                                               │
│                                                         │
│  I wanted to share an update on the project...          │
│                                                         │
└─────────────────────────────────────────────────────────┘

[Approve and Send]  [Edit]  [Reject]
```

The agent pauses execution until the human approves, edits, or rejects. This is **collaborative AI**, not autonomous AI.

### 5.3 Agent Checkpoints and UI Restoration

LangGraph supports **checkpointing** — saving the complete state of an agent run at any point:

```python
# Save checkpoint
config = {"configurable": {"thread_id": "thread_123"}}
checkpoint = graph.get_state(config)

# Later: restore from checkpoint
graph.update_state(config, {"approved": True})
graph.invoke(None, config)  # Resume from checkpoint
```

For the frontend, this means:
- **Persistent conversations**: Close the browser, come back tomorrow, agent state is intact
- **Undo/redo**: Roll back to any checkpoint and branch from there
- **Multi-device**: Start on desktop, continue on mobile
- **Audit trail**: Every agent decision is recorded and reviewable

### 5.4 Multi-Agent UI Orchestration

LangGraph supports **multi-agent systems** where different agents handle different concerns:

```python
# Supervisor agent delegates to specialists
supervisor = Agent(
    name="Supervisor",
    tools=[
        DelegateTo("ResearchAgent"),   # Searches and gathers info
        DelegateTo("WriterAgent"),     # Writes content
        DelegateTo("ReviewerAgent"),   # Reviews and critiques
    ]
)
```

The UI for multi-agent systems needs:
- **Agent identity**: Show which agent is currently active
- **Handoff visualization**: Show when one agent delegates to another
- **Parallel execution**: Show multiple agents working simultaneously
- **Conflict resolution**: When agents disagree, show the options to the user

```
┌─────────────────────────────────────────────────────┐
│  🤖 Research Agent                                   │
│  ✅ Found 15 relevant papers                        │
│  ✅ Extracted key findings                           │
│  ↓ Handing off to Writer Agent                       │
├─────────────────────────────────────────────────────┤
│  ✍️ Writer Agent                                     │
│  ⏳ Writing first draft... (65%)                     │
│  ████████████████░░░░░░░░░                           │
├─────────────────────────────────────────────────────┤
│  🔍 Reviewer Agent                                   │
│  ⏳ Waiting for draft...                             │
└─────────────────────────────────────────────────────┘
```

### 5.5 Implications for GoFastr

LangGraph patterns suggest GoFastr should support:
- **Stateful agent execution** as a first-class concept (not just stateless HTTP handlers)
- **Interrupt/resume** for human-in-the-loop flows
- **Checkpoint storage** (PostgreSQL-backed event log per agent run)
- **Multi-agent routing** (which agent handles which request)

---

## 6. OpenAI Structured Outputs / Function Calling as UI

### 6.1 JSON Schema as a UI Description Language

OpenAI's Structured Outputs feature (2024) constrains LLM output to conform to a JSON Schema. This is commonly used for data extraction, but it can be repurposed as a **UI description language**:

```json
{
  "type": "json_schema",
  "json_schema": {
    "name": "dashboard",
    "schema": {
      "type": "object",
      "properties": {
        "title": { "type": "string" },
        "widgets": {
          "type": "array",
          "items": {
            "oneOf": [
              {
                "type": "object",
                "properties": {
                  "widget_type": { "const": "stat" },
                  "label": { "type": "string" },
                  "value": { "type": "string" },
                  "change": { "type": "string" }
                },
                "required": ["widget_type", "label", "value"]
              },
              {
                "type": "object",
                "properties": {
                  "widget_type": { "const": "table" },
                  "headers": { "type": "array", "items": { "type": "string" } },
                  "rows": {
                    "type": "array",
                    "items": { "type": "array", "items": { "type": "string" } }
                  }
                },
                "required": ["widget_type", "headers", "rows"]
              },
              {
                "type": "object",
                "properties": {
                  "widget_type": { "const": "chart" },
                  "chart_type": { "enum": ["line", "bar", "pie"] },
                  "labels": { "type": "array", "items": { "type": "string" } },
                  "datasets": {
                    "type": "array",
                    "items": {
                      "type": "object",
                      "properties": {
                        "label": { "type": "string" },
                        "data": { "type": "array", "items": { "type": "number" } }
                      }
                    }
                  }
                },
                "required": ["widget_type", "chart_type", "labels", "datasets"]
              }
            ]
          }
        }
      },
      "required": ["title", "widgets"]
    }
  }
}
```

The LLM can only produce valid JSON that matches this schema. The frontend renders it. The schema IS the API contract — between the AI and the UI.

### 6.2 Function Calling as Event Dispatch

OpenAI's function calling (and similar features in Anthropic, Google, etc.) can be reinterpreted as an **event dispatch system** for UI:

```
User: "Show me the top customers"
  → LLM decides to call: get_top_customers(limit: 10)
  → Backend executes function
  → LLM receives result
  → LLM produces UI description with the data embedded
  → Frontend renders it
```

The "function" is really a **data fetcher** or **action executor** — similar to a route handler:

```python
# Function definitions (equivalent to route definitions)
functions = [
    {
        "name": "get_customers",
        "description": "Retrieve customer list with optional filters",
        "parameters": {
            "type": "object",
            "properties": {
                "limit": {"type": "integer", "default": 10},
                "sort_by": {"enum": ["revenue", "orders", "name"]},
                "filter": {"type": "string", "description": "Natural language filter"}
            }
        }
    },
    {
        "name": "get_order",
        "description": "Get details for a specific order",
        "parameters": {
            "type": "object",
            "properties": {
                "order_id": {"type": "string"}
            },
            "required": ["order_id"]
        }
    },
    {
        "name": "create_refund",
        "description": "Issue a refund for an order",
        "parameters": {
            "type": "object",
            "properties": {
                "order_id": {"type": "string"},
                "amount": {"type": "number"},
                "reason": {"type": "string"}
            },
            "required": ["order_id", "amount", "reason"]
        }
    },
]
```

These are **routes**. The AI decides which route to call, with what parameters, based on the user's natural language input. No URL routing. No HTTP methods. Just natural language → function call.

### 6.3 Streaming Partial JSON as Progressive UI

When streaming structured output, the JSON builds up incrementally:

```
t=0:    {}
t=50:   {"title": "Cust"}
t=100:  {"title": "Customer Overview"}
t=150:  {"title": "Customer Overview", "widgets": []}
t=200:  {"title": "Customer Overview", "widgets": [{"widget_type": "stat", "label": "To"}
t=250:  {"title": "Customer Overview", "widgets": [{"widget_type": "stat", "label": "Total Revenue", "value": "$1.2M"}
t=300:  {"title": "Customer Overview", "widgets": [{"widget_type": "stat", "label": "Total Revenue", "value": "$1.2M", "change": "+12%"}]
```

The frontend can render each field as it completes. The title appears first, then the first widget, then its props, etc. This is **progressive UI from streaming JSON**.

### 6.4 The "AI Outputs JSON, Framework Renders It" Pattern

This is the fundamental pattern unifying all these approaches:

```
┌─────────────┐     JSON      ┌─────────────────┐     Render     ┌─────────┐
│  AI Agent   │ ────────────→ │  UI Interpreter │ ─────────────→ │  Screen │
│             │               │                 │                │         │
│ Thinks,     │   {           │ Validates       │ React/Vue/     │ Interactive│
│ uses tools, │     type:     │ against schema  │ Go templates/  │ UI       │
│ reasons     │     props:    │ Looks up        │ HTMX/          │          │
│             │   }           │ components      │ Native         │          │
└─────────────┘               └─────────────────┘                └─────────┘
```

The AI agent is the **controller**. The JSON schema is the **view template**. The frontend renderer is the **view engine**. This is MVC, but the M is the AI's internal model, the C is the AI agent, and the V is the JSON schema + renderer.

---

## 7. Emerging Patterns to Synthesize

### 7.1 Agent-as-Backend

**The AI IS the backend, not just a feature.**

```
Traditional:  User → Frontend → Backend (API) → Database
Agentic:      User → AI Agent → Tools → Database
                      ↑
                  The AI agent IS the backend.
                  It decides what tools to call, what data to fetch,
                  and what UI to show.
```

In this model:
- There is no "API layer" in the traditional sense. The AI agent *is* the API.
- "Routes" are tool definitions. The AI decides which tool to "route" to.
- "Controllers" are the AI's reasoning. It decides what to do.
- "Views" are AI-generated UI descriptions.
- "Models" are the tool schemas and the AI's internal state.

**Implication**: The framework's primary job shifts from "handle HTTP requests" to "host AI agents and expose tools."

### 7.2 Tool-as-Route

**MCP tool calls replace traditional HTTP routes.**

| Traditional | Agentic |
|------------|---------|
| `GET /users/:id` | `tool: get_user(user_id)` |
| `POST /users` | `tool: create_user(name, email)` |
| `PUT /users/:id` | `tool: update_user(user_id, updates)` |
| `DELETE /users/:id` | `tool: delete_user(user_id)` |
| `GET /users?search=x` | `tool: search_users(query)` |

The same Go function serves both purposes:

```go
// This function is BOTH an HTTP route handler AND an MCP tool
func GetUser(ctx context.Context, req *GetUserRequest) (*GetUserResponse, error) {
    user, err := db.GetUser(ctx, req.ID)
    if err != nil {
        return nil, fmt.Errorf("get user: %w", err)
    }
    return &GetUserResponse{User: user}, nil
}

// Registered as HTTP route
gofastr.Get("/users/:id", handlers.GetUser)

// Registered as MCP tool (auto-generated from the same function)
mcp.Tool("get_user", "Get a user by ID", handlers.GetUser)
```

The Go struct tags provide everything needed for both:
```go
type GetUserRequest struct {
    ID string `param:"id" json:"user_id" mcp:"required" validate:"uuid"`
}
```

### 7.3 State-as-Conversation

**Session state is conversation history.**

Traditional: session state lives in a server-side store (Redis, cookie, JWT).
Agentic: session state IS the conversation between the user and the AI agent.

```json
{
  "thread_id": "thread_abc123",
  "messages": [
    {"role": "user", "content": "Show me my recent orders"},
    {"role": "assistant", "content": "Here are your recent orders:", "tool_calls": [...]},
    {"role": "tool", "name": "list_orders", "result": [...]},
    {"role": "assistant", "content": "Your last order (#1234) is currently..."},
    {"role": "user", "content": "Cancel that one"},
    {"role": "assistant", "tool_calls": [{"name": "cancel_order", "args": {"order_id": "1234"}}]},
    {"role": "tool", "name": "cancel_order", "result": {"status": "cancelled"}},
    {"role": "assistant", "content": "Order #1234 has been cancelled."}
  ],
  "metadata": {
    "user_id": "usr_123",
    "started_at": "2026-05-05T10:00:00Z",
    "tools_used": ["list_orders", "cancel_order"]
  }
}
```

The conversation IS the session. It captures:
- What the user asked for
- What the agent did
- What tools were called and what they returned
- The current state of the interaction

**Implication**: The framework needs a **conversation store** (not just a session store) that persists the full event log.

### 7.4 UI-as-Stream

**The frontend is a streaming sink for agent events.**

```
Agent Events:
  RunStarted → StepStarted → TextStream → ToolCall → ToolResult →
  StepFinished → StepStarted → TextStream → RunFinished

Frontend renders:
  [Loading...] → [Thinking...] → [Streaming text...] → [Tool spinner...] →
  [Tool result] → [Done step 1] → [Streaming text...] → [Complete]
```

The frontend doesn't "fetch data." It **subscribes to a stream** and renders each event as it arrives. The stream might last milliseconds or hours (for long-running agent tasks).

**Implication**: The framework's response model must be fundamentally stream-oriented. SSE, WebSockets, or chunked transfers are the default, not the exception.

### 7.5 Schema-as-Contract

**JSON Schema replaces API contracts (OpenAPI, GraphQL schemas).**

| Traditional Contract | Agentic Contract |
|---------------------|------------------|
| OpenAPI spec (hundreds of lines of YAML) | JSON Schema per tool (auto-generated from Go structs) |
| GraphQL schema (separate from code) | Struct tags define the schema (single source of truth) |
| REST conventions (HTTP methods, status codes) | Tool semantics (name, description, parameters) |
| Versioning (v1, v2, deprecation) | Schema evolution (additive changes, backward compatible) |

The Go struct IS the contract:

```go
type CreateOrderRequest struct {
    ProductID string `json:"product_id" validate:"required,uuid" mcp:"The product to order"`
    Quantity  int    `json:"quantity" validate:"required,min=1,max=100" mcp:"How many to order"`
    AddressID string `json:"address_id" validate:"required,uuid" mcp:"Shipping address"`
}
```

From this single definition, the framework generates:
- JSON Schema for MCP tool definition
- JSON Schema for structured output validation
- OpenAPI spec for traditional API consumers
- TypeScript types for frontend developers
- HTML form fields with validation for SSR
- Validation rules for request handling

### 7.6 Composable Objects

**Pages are compositions of agent-callable components.**

A "page" in an agentic framework is not a file in a routes directory. It's a **composition of tools** that the AI agent assembles based on the user's request:

```
User: "I need to see my dashboard"
  → Agent composes:
    - tool: get_user_metrics(user_id) → MetricCard components
    - tool: get_recent_orders(user_id, limit: 5) → DataTable component
    - tool: get_activity_feed(user_id) → Timeline component
    - tool: get_recommendations(user_id) → ProductGrid component
  → Agent assembles these into a Dashboard layout
  → Frontend renders the composition
```

The "page" is emergent from the agent's tool calls. There is no `/dashboard` route. There is an agent that knows how to compose dashboard-like views from available tools.

**Implication**: The framework needs a **component registry** (name → renderable component) and a **layout engine** (how to arrange components in space).

---

## 8. What This Means for a Framework

### 8.1 If the Primary Author Is AI and the Primary Consumer Is AI, What Does the Framework Look Like?

The current GoFastr draft is designed for **AI coding agents writing traditional web apps**. But the protocols above suggest a deeper shift: **AI agents running at runtime, composing the app dynamically.**

Two distinct AI roles:

| | AI Coding Agent (current GoFastr focus) | AI Runtime Agent (agentic protocols) |
|--|---------------------------------------|-------------------------------------|
| **When** | Development time | Runtime |
| **What** | Writes Go code | Composes tools, generates UI |
| **Output** | Source files, routes, handlers | Tool calls, UI descriptions, event streams |
| **Consumer** | Go compiler, GoFastr framework | End user via frontend |
| **Protocol** | CLI, MCP (dev tools) | MCP (runtime), AG-UI, SSE |

GoFastr should support **both**. The framework's Go code defines the tools and schemas. The AI runtime agent composes them into user experiences.

The framework becomes:

```
┌─────────────────────────────────────────────────────────────┐
│                       GoFastr App                            │
│                                                              │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │  Tool Layer (defined in Go, invoked by AI or HTTP)      │ │
│  │  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐        │ │
│  │  │ CRUD │ │Search│ │Email │ │Auth  │ │Report│  ...     │ │
│  │  └──┬───┘ └──┬───┘ └──┬───┘ └──┬───┘ └──┬───┘        │ │
│  └─────┼────────┼────────┼────────┼────────┼──────────────┘ │
│        │        │        │        │        │                 │
│  ┌─────┴────────┴────────┴────────┴────────┴──────────────┐ │
│  │  Dual Interface Layer                                   │ │
│  │  ┌─────────────────┐    ┌─────────────────────────┐    │ │
│  │  │  HTTP / HTMX    │    │  MCP Server (runtime)   │    │ │
│  │  │  (traditional)  │    │  (AI agent interface)   │    │ │
│  │  └─────────────────┘    └─────────────────────────┘    │ │
│  └─────────────────────────────────────────────────────────┘ │
│                                                              │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │  Event Stream Layer (SSE / WebSocket)                   │ │
│  │  Emits AG-UI events for AI-driven UI composition        │ │
│  └─────────────────────────────────────────────────────────┘ │
│                                                              │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │  Component Registry (name → renderer)                   │ │
│  │  MetricCard, DataTable, Chart, Form, Timeline, ...      │ │
│  └─────────────────────────────────────────────────────────┘ │
│                                                              │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │  Conversation Store (event-sourced agent state)         │ │
│  │  Thread → Events → State snapshots → Replay             │ │
│  └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

### 8.2 JSON-in → App-out: Can We Define an Entire App as a JSON Document?

Yes. And this is where the "agent-first" vision becomes concrete.

An app definition as JSON:

```json
{
  "name": "ecommerce",
  "version": "1.0.0",
  "database": {
    "driver": "postgresql",
    "models": [
      {
        "name": "User",
        "fields": [
          {"name": "id", "type": "uuid", "primary": true},
          {"name": "email", "type": "string", "unique": true, "validate": "email"},
          {"name": "name", "type": "string", "validate": "required,min=2"},
          {"name": "role", "type": "enum", "values": ["customer", "admin"]},
          {"name": "created_at", "type": "timestamp", "default": "now()"}
        ]
      },
      {
        "name": "Product",
        "fields": [
          {"name": "id", "type": "uuid", "primary": true},
          {"name": "name", "type": "string", "validate": "required"},
          {"name": "price", "type": "decimal", "validate": "min=0"},
          {"name": "category", "type": "string", "index": true},
          {"name": "in_stock", "type": "boolean", "default": true}
        ]
      },
      {
        "name": "Order",
        "fields": [
          {"name": "id", "type": "uuid", "primary": true},
          {"name": "user_id", "type": "uuid", "references": "User.id"},
          {"name": "status", "type": "enum", "values": ["pending", "paid", "shipped", "cancelled"]},
          {"name": "total", "type": "decimal"},
          {"name": "created_at", "type": "timestamp", "default": "now()"}
        ]
      }
    ],
    "relations": [
      {"from": "User", "to": "Order", "type": "has_many"},
      {"from": "Order", "to": "Product", "type": "has_many", "through": "OrderItem"}
    ]
  },
  "tools": [
    {
      "name": "search_products",
      "description": "Search the product catalog",
      "input": {"query": "string", "category": "string?", "max_price": "number?"},
      "output": "Product[]"
    },
    {
      "name": "add_to_cart",
      "description": "Add a product to the shopping cart",
      "input": {"session_id": "uuid", "product_id": "uuid", "quantity": "number"},
      "output": "Cart",
      "requires_auth": true
    },
    {
      "name": "checkout",
      "description": "Complete a purchase",
      "input": {"session_id": "uuid", "shipping_address": "Address", "payment_token": "string"},
      "output": "Order",
      "requires_auth": true
    },
    {
      "name": "get_order_status",
      "description": "Check the status of an order",
      "input": {"order_id": "uuid"},
      "output": "OrderWithTracking",
      "requires_auth": true
    }
  ],
  "ui": {
    "components": {
      "ProductCard": {"fields": ["name", "price", "image", "in_stock"]},
      "CartSummary": {"fields": ["items", "subtotal", "tax", "total"]},
      "OrderTracker": {"fields": ["status", "tracking_number", "estimated_delivery"]}
    },
    "layouts": {
      "dashboard": ["MetricCard", "DataTable", "Chart"],
      "product_page": ["ProductImage", "ProductDetails", "RelatedProducts"],
      "checkout": ["CartSummary", "AddressForm", "PaymentForm", "OrderConfirm"]
    }
  },
  "auth": {
    "providers": ["email_password", "google_oauth", "github_oauth"],
    "mfa": true,
    "session": {"ttl": "24h", "idle_timeout": "2h"}
  }
}
```

From this JSON definition, GoFastr could generate:
- **Database models** (Go structs with ORM tags)
- **Migrations** (SQL CREATE TABLE statements)
- **MCP tools** (auto-exposed from model CRUD + custom tools)
- **HTTP routes** (REST API from tools)
- **JSON Schemas** (from model definitions)
- **Validation rules** (from field constraints)
- **Component schemas** (for AI-generated UI)
- **Auth middleware** (from auth config)
- **Tests** (scaffolded test suite)

The JSON document is the **single source of truth**. The Go code is generated. The AI agent (coding agent) uses the JSON as context for making modifications. The AI agent (runtime agent) uses the MCP tools to serve users.

### 8.3 What Are the "Primitives" of an Agentic Framework?

Drawing from all the protocols above, the primitives are:

| Primitive | What It Is | Traditional Equivalent |
|-----------|-----------|----------------------|
| **Tool** | A callable action with typed input/output | Route / Controller action |
| **Schema** | A JSON Schema describing data shapes | Model / Migration / API contract |
| **Event** | A typed, timestamped occurrence in the system | Log entry / State change |
| **EventStream** | An ordered sequence of events | HTTP response / WebSocket |
| **Thread** | A conversation (ordered events, state) | Session |
| **Component** | A named, renderable UI element with props | View template / React component |
| **Layout** | A spatial arrangement of components | Page template / Layout |
| **Agent** | A stateful reasoning engine that uses tools | Service / Business logic layer |
| **Checkpoint** | A snapshot of agent state at a point in time | Database snapshot |
| **Resource** | A read-only data source addressable by URI | GET endpoint / Database view |

These primitives compose:

```
Schema → Tool → Agent → EventStream → Component → Layout → User
                  ↑                      ↓
              Thread ←── Checkpoint
```

### 8.4 How Does Auth, Security, Validation Work When AI Generates the Interface?

This is the hardest unsolved problem. Some emerging patterns:

**Auth:**
- **Tool-level permissions**: Each MCP tool declares required permissions (`requires_auth: true`, `required_roles: ["admin"]`). The framework enforces before execution.
- **Context-based auth**: The agent passes user context (from the session/thread) with each tool call. The framework validates.
- **Capability tokens**: Short-lived tokens issued per tool call, scoped to the specific action.

```go
// Tool with permission requirements
gofastr.Tool("cancel_order", "Cancel an order",
    func(ctx context.Context, req *CancelOrderRequest) (*CancelOrderResponse, error) {
        // Framework auto-checks: user is authenticated, has "orders:cancel" permission,
        // and the order belongs to the user (or user is admin)
        return services.CancelOrder(ctx, req.OrderID)
    },
    gofastr.RequireAuth(),
    gofastr.RequirePermission("orders:cancel"),
    gofastr.RequireOwnerOrAdmin("order_id"),
)
```

**Security:**
- **Schema validation on every AI output**: Before rendering, validate the AI's UI description against the component schema. Reject anything that doesn't match.
- **Sandboxed rendering**: Components run in a sandbox with no access to globals, cookies, or other components' state.
- **Output filtering**: Scan AI-generated content for sensitive data (PII, credentials) before sending to frontend.
- **Rate limiting per agent**: Limit how many tool calls an agent can make per minute.
- **Audit logging**: Every agent action is logged with full context (who, what, when, why).

**Validation:**
- **Input validation is non-negotiable**: Every tool input is validated against its schema. The AI can't bypass validation.
- **Output validation**: Every tool output is validated against its schema. Corrupt data doesn't reach the agent.
- **Schema evolution**: Schemas are versioned. Breaking changes create new versions. AI agents negotiate schema versions.
- **Runtime type checking**: Even though Go is statically typed, the AI agent produces JSON. The framework validates JSON against Go struct expectations at the boundary.

### 8.5 What's the Rendering Pipeline for Agent-Streamed UI?

The complete pipeline from agent event to pixel:

```
1. Agent emits event (AG-UI protocol)
   ↓
2. Event serialized to JSON
   ↓
3. Event streamed to frontend (SSE / WebSocket)
   ↓
4. Frontend receives and parses event
   ↓
5. Event routed to appropriate handler:
   ├─ TextMessageContent → Append to message body, render markdown
   ├─ ToolCallStart → Show tool execution UI, resolve component from registry
   ├─ ToolCallArgs → Stream partial arguments into component props
   ├─ ToolCallResult → Resolve final component, render with data
   ├─ StateSnapshot → Update application state
   └─ RunFinished → Transition to complete state
   ↓
6. Component renders to DOM (React, Vue, Svelte, or HTMX partials)
   ↓
7. User sees progressive UI update
```

For a **server-rendered approach** (GoFastr's strength with HTMX):

```
1. Agent emits event
   ↓
2. Server-side event handler renders HTML partial
   ↓
3. HTML partial streamed as SSE event
   ↓
4. HTMX receives SSE, swaps HTML into DOM
   ↓
5. No JavaScript framework needed
```

```go
// GoFastr: SSE event → HTML partial
func handleToolCallResult(ctx context.Context, event ToolCallResult) string {
    switch event.ToolName {
    case "search_products":
        return renderTemplate("partials/product_grid.html", event.Result)
    case "get_metrics":
        return renderTemplate("partials/metric_cards.html", event.Result)
    default:
        return renderTemplate("partials/generic_result.html", event.Result)
    }
}
```

This is the **GoFastr sweet spot**: server-rendered HTML partials streamed over SSE, swapped by HTMX. No React. No JavaScript framework. Just HTML streaming from agent events.

### 8.6 Proposed Rendering Modes for GoFastr

GoFastr could support three rendering modes, all powered by the same tool layer:

| Mode | Transport | Rendering | Best For |
|------|-----------|-----------|----------|
| **HTMX + SSE** | Server-Sent Events | HTML partials | Internal tools, dashboards, admin |
| **JSON API** | REST/GraphQL | JSON responses | Mobile apps, third-party integrations |
| **AG-UI Stream** | SSE/WebSocket | Typed events (JSON) | AI-powered chat interfaces, agent UIs |

All three modes hit the same **Tool Layer**. The difference is only in how the response is formatted and transported:

```go
// Same tool, three rendering modes
func SearchProducts(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
    return catalog.Search(ctx, req.Query, req.Filters)
}

// Mode 1: HTTP route → JSON response
gofastr.Get("/api/products", handlers.SearchProducts)

// Mode 2: HTTP route → HTMX partial
gofastr.Get("/products/search", gofastr.RenderPartial("partials/product_grid.html", handlers.SearchProducts))

// Mode 3: MCP tool → AI agent decides when to call
mcp.Tool("search_products", "Search the product catalog", handlers.SearchProducts)
```

### 8.7 The Roadmap: From Traditional to Agentic

Phase 1 — **Traditional with Agent-First DX** (current GoFastr vision):
- Standard web framework (routes, handlers, templates, ORM, auth)
- CLI designed for AI coding agents
- MCP server for development-time agent integration
- Structured errors, `--json` flags, scaffolding

Phase 2 — **Dual Interface** (add runtime AI):
- Every route handler is also an MCP tool
- SSE streaming for agent events
- Component registry for AI-driven UI composition
- Conversation store for agent state persistence
- HTMX partials streamed from agent tool calls

Phase 3 — **Agentic Native** (the full vision):
- JSON-in → App-out: define apps as JSON, GoFastr generates everything
- Runtime AI agent as the primary interface
- Event-sourced state management (threads, checkpoints, replay)
- Multi-agent orchestration
- Schema-as-contract everywhere
- Security model designed for AI-generated UI

---

## Appendix: Key References

| Protocol / Tool | Maintainer | URL | Status |
|----------------|-----------|-----|--------|
| AG-UI Protocol | CopilotKit (emerging) | https://docs.ag-ui.com/ | Early / Evolving |
| MCP (Model Context Protocol) | Anthropic | https://modelcontextprotocol.io/ | Active, v2025-03-26 spec |
| AI SDK | Vercel | https://sdk.vercel.ai/ | Production, actively developed |
| LangGraph | LangChain | https://langchain-ai.github.io/langgraph/ | Production |
| OpenAI Structured Outputs | OpenAI | https://platform.openai.com/docs/guides/structured-outputs | Production |
| Anthropic Tool Use | Anthropic | https://docs.anthropic.com/en/docs/build-with-claude/tool-use | Production |
| HTMX | HTMX.org | https://htmx.org/ | Production, stable |

### Key Insights Across All Protocols

1. **The event stream is the fundamental primitive.** Not the request, not the response — the stream of typed events from agent to frontend.

2. **Tools are the new routes.** MCP tools, function calls, and agent capabilities all converge on the same concept: typed, callable actions with JSON Schema contracts.

3. **The AI is the router.** In traditional apps, the URL determines what runs. In agentic apps, the AI agent determines what tools to call based on user intent.

4. **State is conversation.** The thread/event log IS the session state. Replayable, auditable, persistable.

5. **Schema is the contract.** JSON Schema replaces OpenAPI specs, GraphQL schemas, and handwritten API documentation.

6. **Progressive rendering is the norm.** The UI renders incrementally as the agent works. Loading spinners are replaced by visible work.

7. **Human-in-the-loop is a first-class pattern.** Agents can pause, ask for input, and resume. The framework must support interrupt/resume natively.

8. **Server-rendered HTML can be agentic.** HTMX + SSE means GoFastr can serve AI-driven UI without JavaScript frameworks. This is a massive advantage for the Go ecosystem.

---

*Last updated: 2026-05-05*
*This document is a living research artifact. The protocols described are evolving rapidly.*
