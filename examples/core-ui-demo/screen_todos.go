package main

import (
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core-ui/html"
	"github.com/gofastr/gofastr/core/render"
)

// TodosScreen demonstrates an end-to-end interactive feature: a form input,
// list rendering, server-defined client-side actions (add/toggle/delete),
// and a live count derived from state.
//
// Storage lives in client state (G.getState / G.setState) under the
// "todos" key, so the demo works without a database while still flowing
// through the same compile-Go-to-JS action pipeline as the rest of the app.
type TodosScreen struct{}

func (s *TodosScreen) ScreenTitle() string        { return "Todos" }
func (s *TodosScreen) ScreenDescription() string  { return "Forms, lists, and persisted client state" }
func (s *TodosScreen) ScreenType() app.ScreenType { return app.ScreenPage }
func (s *TodosScreen) ComponentID() string        { return "todos" }

func (s *TodosScreen) Render() render.HTML {
	return html.Div(html.DivConfig{Attrs: html.Attrs{"data-component": "todos"}, Class: "todos-screen"},
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Todos")),
		html.Paragraph(html.TextConfig{},
			render.Text("Add a task, toggle it complete, or delete it. State persists in the browser via the runtime's state helpers.")),

		html.Section(html.SectionConfig{Label: "New todo"},
			html.Div(html.DivConfig{Class: "todo-form"},
				render.Tag("input", map[string]string{
					"id":          "todo-input",
					"type":        "text",
					"placeholder": "What needs doing?",
					"class":       "todo-input",
					"aria-label":  "New todo description",
					"onkeydown":   "if(event.key==='Enter'){event.preventDefault();G.trigger('todos','todo-add');}",
				}),
				html.Button(html.ButtonConfig{
					Label: "Add",
					Class: "cta-button",
					Attrs: html.Attrs{"data-action": "todo-add"},
				}),
			),
		),

		html.Section(html.SectionConfig{Label: "Todo list"},
			html.Div(html.DivConfig{Class: "todo-stats"},
				render.Tag("span", map[string]string{"id": "todo-count", "aria-live": "polite"}, render.Text("0 todos")),
				html.Button(html.ButtonConfig{
					Label: "Clear completed",
					Class: "todo-clear",
					Attrs: html.Attrs{"data-action": "todo-clear-completed"},
				}),
			),
			render.Tag("ul", map[string]string{
				"id":         "todo-list",
				"class":      "todo-list",
				"aria-label": "Todos",
			}, render.HTML(`<li class="todo-empty" data-empty="true">No todos yet — add one above.</li>`)),
		),
	)
}

// renderTodosClientJS is the shared snippet every action handler uses to
// re-render the list and stats from client state. Inlining it here keeps
// the action handlers focused on what they change.
const renderTodosClientJS = `
function renderTodos(){
  const todos = G.getState('todos', []);
  const list = document.getElementById('todo-list');
  if (!list) return;
  if (todos.length === 0) {
    list.innerHTML = '<li class="todo-empty" data-empty="true">No todos yet — add one above.</li>';
  } else {
    list.innerHTML = todos.map(function(t, i){
      const checked = t.done ? 'checked' : '';
      const cls = t.done ? 'todo-item todo-done' : 'todo-item';
      const safe = (t.text || '').replace(/[&<>"']/g, function(c){
        return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c];
      });
      return '<li class="'+cls+'" data-todo-index="'+i+'">'+
        '<label class="todo-check"><input type="checkbox" '+checked+' onclick="G.trigger(\'todos\',\'todo-toggle\',{index:'+i+'})"><span>'+safe+'</span></label>'+
        '<button class="todo-delete" aria-label="Delete todo" onclick="G.trigger(\'todos\',\'todo-delete\',{index:'+i+'})">×</button>'+
      '</li>';
    }).join('');
  }
  const remaining = todos.filter(function(t){return !t.done;}).length;
  const total = todos.length;
  const summary = total === 0 ? '0 todos' : (remaining + ' of ' + total + ' remaining');
  G.updateText('#todo-count', summary);
}
renderTodos();
`

func (s *TodosScreen) Actions() {
	component.On("todo-add", func(ctx *component.ComponentContext) {},
		component.WithClientJS(`
const input = document.getElementById('todo-input');
if (!input) return;
const text = (input.value || '').trim();
if (!text) return;
const todos = G.getState('todos', []).slice();
todos.push({text: text, done: false});
G.setState('todos', todos);
input.value = '';
input.focus();
`+renderTodosClientJS))

	component.On("todo-toggle", func(ctx *component.ComponentContext) {},
		component.WithClientJS(`
const idx = (params && typeof params.index === 'number') ? params.index : -1;
const todos = G.getState('todos', []).slice();
if (idx < 0 || idx >= todos.length) return;
todos[idx] = Object.assign({}, todos[idx], {done: !todos[idx].done});
G.setState('todos', todos);
`+renderTodosClientJS))

	component.On("todo-delete", func(ctx *component.ComponentContext) {},
		component.WithClientJS(`
const idx = (params && typeof params.index === 'number') ? params.index : -1;
const todos = G.getState('todos', []).slice();
if (idx < 0 || idx >= todos.length) return;
todos.splice(idx, 1);
G.setState('todos', todos);
`+renderTodosClientJS))

	component.On("todo-clear-completed", func(ctx *component.ComponentContext) {},
		component.WithClientJS(`
const todos = G.getState('todos', []).filter(function(t){ return !t.done; });
G.setState('todos', todos);
`+renderTodosClientJS))
}
