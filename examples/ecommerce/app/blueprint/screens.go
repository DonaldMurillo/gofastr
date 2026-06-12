package blueprint

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	kilnrender "github.com/DonaldMurillo/gofastr/kiln/noderender"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

type blueprintNodeComponent struct{ node world.Node }

func (c blueprintNodeComponent) Render() render.HTML { return kilnrender.RenderNode(c.node) }

type HomeScreen struct{}

func (s *HomeScreen) ScreenTitle() string        { return "ShopFront — Home" }
func (s *HomeScreen) ScreenDescription() string  { return "E-commerce storefront homepage" }
func (s *HomeScreen) ScreenType() app.ScreenType { return app.ScreenPage }
func (s *HomeScreen) ComponentID() string        { return "screen-home" }
func (s *HomeScreen) Actions() {
	component.On("entity_list_home_products_2", func(ctx *component.ComponentContext) { _ = ctx }, component.WithClientJS("(async () => {\n  const entity = \"products\";\n  const fields = [\"name\",\"price\",\"status\"];\n  const root = document.querySelector('[data-entity-list=\"' + entity + '\"]');\n  const body = root && root.querySelector('[data-entity-list-body]');\n  if (!body) return;\n  const esc = (value) => String(value ?? '').replace(/[&<>\"']/g, (ch) => ({'&':'&amp;','<':'&lt;','>':'&gt;','\"':'&quot;',\"'\":'&#39;'}[ch]));\n  const table = (rowsHTML) => '<table><thead><tr>' + fields.map((field) => '<th>' + esc(field) + '</th>').join('') + '</tr></thead><tbody>' + rowsHTML + '</tbody></table>';\n  body.innerHTML = table('<tr><td colspan=\"' + fields.length + '\">Loading...</td></tr>');\n  try {\n    const res = await fetch('/' + entity + '?limit=' + 8, { headers: { 'Accept': 'application/json' } });\n    if (!res.ok) throw new Error('HTTP ' + res.status);\n    const payload = await res.json();\n    const rows = Array.isArray(payload.data) ? payload.data : [];\n    if (!rows.length) {\n      body.innerHTML = table('<tr><td colspan=\"' + fields.length + '\">No products available yet.</td></tr>');\n      return;\n    }\n    body.innerHTML = table(rows.map((row) => '<tr>' + fields.map((field) => '<td>' + esc(row[field]) + '</td>').join('') + '</tr>').join(''));\n  } catch (err) {\n    body.innerHTML = table('<tr><td colspan=\"' + fields.length + '\">Failed to load ' + esc(entity) + '</td></tr>');\n  }\n})();"))
}

func (s *HomeScreen) Render() render.HTML {
	return render.Tag("div", map[string]string{"data-component": s.ComponentID()},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("ShopFront")),
		render.Tag("p", nil, render.Text("Welcome to our store. Browse our products and categories.")),
		kilnrender.RenderNode(world.Node{Kind: "section", Props: map[string]any{"class": "gofastr-entity-list", "data-entity-list": "products"}, Children: []world.Node{world.Node{Kind: "heading", Props: map[string]any{"level": 2, "text": "Featured Products"}}, world.Node{Kind: "button", Props: map[string]any{"aria-label": "Refresh products", "data-action": "entity_list_home_products_2", "data-entity-list-refresh": "products", "data-param-empty-text": "No products available yet.", "data-param-entity": "products", "data-param-limit": 8, "text": "Refresh", "type": "button"}}, world.Node{Kind: "div", Props: map[string]any{"data-entity-list-body": true, "text": "No products available yet."}}}}),
	)
}

type ProductsScreen struct{}

func (s *ProductsScreen) ScreenTitle() string        { return "All Products" }
func (s *ProductsScreen) ScreenDescription() string  { return "Browse our full product catalog" }
func (s *ProductsScreen) ScreenType() app.ScreenType { return app.ScreenPage }
func (s *ProductsScreen) ComponentID() string        { return "screen-products" }
func (s *ProductsScreen) Actions() {
	component.On("entity_list_products_products_1", func(ctx *component.ComponentContext) { _ = ctx }, component.WithClientJS("(async () => {\n  const entity = \"products\";\n  const fields = [\"name\",\"price\",\"status\",\"stock\"];\n  const root = document.querySelector('[data-entity-list=\"' + entity + '\"]');\n  const body = root && root.querySelector('[data-entity-list-body]');\n  if (!body) return;\n  const esc = (value) => String(value ?? '').replace(/[&<>\"']/g, (ch) => ({'&':'&amp;','<':'&lt;','>':'&gt;','\"':'&quot;',\"'\":'&#39;'}[ch]));\n  const table = (rowsHTML) => '<table><thead><tr>' + fields.map((field) => '<th>' + esc(field) + '</th>').join('') + '</tr></thead><tbody>' + rowsHTML + '</tbody></table>';\n  body.innerHTML = table('<tr><td colspan=\"' + fields.length + '\">Loading...</td></tr>');\n  try {\n    const res = await fetch('/' + entity + '?limit=' + 20, { headers: { 'Accept': 'application/json' } });\n    if (!res.ok) throw new Error('HTTP ' + res.status);\n    const payload = await res.json();\n    const rows = Array.isArray(payload.data) ? payload.data : [];\n    if (!rows.length) {\n      body.innerHTML = table('<tr><td colspan=\"' + fields.length + '\">No products found.</td></tr>');\n      return;\n    }\n    body.innerHTML = table(rows.map((row) => '<tr>' + fields.map((field) => '<td>' + esc(row[field]) + '</td>').join('') + '</tr>').join(''));\n  } catch (err) {\n    body.innerHTML = table('<tr><td colspan=\"' + fields.length + '\">Failed to load ' + esc(entity) + '</td></tr>');\n  }\n})();"))
}

func (s *ProductsScreen) Render() render.HTML {
	return render.Tag("div", map[string]string{"data-component": s.ComponentID()},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Products")),
		kilnrender.RenderNode(world.Node{Kind: "section", Props: map[string]any{"class": "gofastr-entity-list", "data-entity-list": "products"}, Children: []world.Node{world.Node{Kind: "heading", Props: map[string]any{"level": 2, "text": "Product Catalog"}}, world.Node{Kind: "button", Props: map[string]any{"aria-label": "Refresh products", "data-action": "entity_list_products_products_1", "data-entity-list-refresh": "products", "data-param-empty-text": "No products found.", "data-param-entity": "products", "data-param-limit": 20, "text": "Refresh", "type": "button"}}, world.Node{Kind: "div", Props: map[string]any{"data-entity-list-body": true, "text": "No products found."}}}}),
	)
}

type CategoriesScreen struct{}

func (s *CategoriesScreen) ScreenTitle() string        { return "Categories" }
func (s *CategoriesScreen) ScreenDescription() string  { return "Browse product categories" }
func (s *CategoriesScreen) ScreenType() app.ScreenType { return app.ScreenPage }
func (s *CategoriesScreen) ComponentID() string        { return "screen-categories" }
func (s *CategoriesScreen) Actions() {
	component.On("entity_list_categories_categories_1", func(ctx *component.ComponentContext) { _ = ctx }, component.WithClientJS("(async () => {\n  const entity = \"categories\";\n  const fields = [\"name\",\"description\",\"active\"];\n  const root = document.querySelector('[data-entity-list=\"' + entity + '\"]');\n  const body = root && root.querySelector('[data-entity-list-body]');\n  if (!body) return;\n  const esc = (value) => String(value ?? '').replace(/[&<>\"']/g, (ch) => ({'&':'&amp;','<':'&lt;','>':'&gt;','\"':'&quot;',\"'\":'&#39;'}[ch]));\n  const table = (rowsHTML) => '<table><thead><tr>' + fields.map((field) => '<th>' + esc(field) + '</th>').join('') + '</tr></thead><tbody>' + rowsHTML + '</tbody></table>';\n  body.innerHTML = table('<tr><td colspan=\"' + fields.length + '\">Loading...</td></tr>');\n  try {\n    const res = await fetch('/' + entity + '?limit=' + 50, { headers: { 'Accept': 'application/json' } });\n    if (!res.ok) throw new Error('HTTP ' + res.status);\n    const payload = await res.json();\n    const rows = Array.isArray(payload.data) ? payload.data : [];\n    if (!rows.length) {\n      body.innerHTML = table('<tr><td colspan=\"' + fields.length + '\">No categories yet.</td></tr>');\n      return;\n    }\n    body.innerHTML = table(rows.map((row) => '<tr>' + fields.map((field) => '<td>' + esc(row[field]) + '</td>').join('') + '</tr>').join(''));\n  } catch (err) {\n    body.innerHTML = table('<tr><td colspan=\"' + fields.length + '\">Failed to load ' + esc(entity) + '</td></tr>');\n  }\n})();"))
}

func (s *CategoriesScreen) Render() render.HTML {
	return render.Tag("div", map[string]string{"data-component": s.ComponentID()},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Categories")),
		kilnrender.RenderNode(world.Node{Kind: "section", Props: map[string]any{"class": "gofastr-entity-list", "data-entity-list": "categories"}, Children: []world.Node{world.Node{Kind: "heading", Props: map[string]any{"level": 2, "text": "All Categories"}}, world.Node{Kind: "button", Props: map[string]any{"aria-label": "Refresh categories", "data-action": "entity_list_categories_categories_1", "data-entity-list-refresh": "categories", "data-param-empty-text": "No categories yet.", "data-param-entity": "categories", "data-param-limit": 50, "text": "Refresh", "type": "button"}}, world.Node{Kind: "div", Props: map[string]any{"data-entity-list-body": true, "text": "No categories yet."}}}}),
	)
}

type OrdersScreen struct{}

func (s *OrdersScreen) ScreenTitle() string        { return "Orders" }
func (s *OrdersScreen) ScreenDescription() string  { return "View and manage orders" }
func (s *OrdersScreen) ScreenType() app.ScreenType { return app.ScreenPage }
func (s *OrdersScreen) ComponentID() string        { return "screen-orders" }
func (s *OrdersScreen) Actions() {
	component.On("entity_list_orders_orders_1", func(ctx *component.ComponentContext) { _ = ctx }, component.WithClientJS("(async () => {\n  const entity = \"orders\";\n  const fields = [\"order_number\",\"customer_name\",\"status\",\"total\"];\n  const root = document.querySelector('[data-entity-list=\"' + entity + '\"]');\n  const body = root && root.querySelector('[data-entity-list-body]');\n  if (!body) return;\n  const esc = (value) => String(value ?? '').replace(/[&<>\"']/g, (ch) => ({'&':'&amp;','<':'&lt;','>':'&gt;','\"':'&quot;',\"'\":'&#39;'}[ch]));\n  const table = (rowsHTML) => '<table><thead><tr>' + fields.map((field) => '<th>' + esc(field) + '</th>').join('') + '</tr></thead><tbody>' + rowsHTML + '</tbody></table>';\n  body.innerHTML = table('<tr><td colspan=\"' + fields.length + '\">Loading...</td></tr>');\n  try {\n    const res = await fetch('/' + entity + '?limit=' + 20, { headers: { 'Accept': 'application/json' } });\n    if (!res.ok) throw new Error('HTTP ' + res.status);\n    const payload = await res.json();\n    const rows = Array.isArray(payload.data) ? payload.data : [];\n    if (!rows.length) {\n      body.innerHTML = table('<tr><td colspan=\"' + fields.length + '\">No orders yet.</td></tr>');\n      return;\n    }\n    body.innerHTML = table(rows.map((row) => '<tr>' + fields.map((field) => '<td>' + esc(row[field]) + '</td>').join('') + '</tr>').join(''));\n  } catch (err) {\n    body.innerHTML = table('<tr><td colspan=\"' + fields.length + '\">Failed to load ' + esc(entity) + '</td></tr>');\n  }\n})();"))
}

func (s *OrdersScreen) Render() render.HTML {
	return render.Tag("div", map[string]string{"data-component": s.ComponentID()},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Orders")),
		kilnrender.RenderNode(world.Node{Kind: "section", Props: map[string]any{"class": "gofastr-entity-list", "data-entity-list": "orders"}, Children: []world.Node{world.Node{Kind: "heading", Props: map[string]any{"level": 2, "text": "Recent Orders"}}, world.Node{Kind: "button", Props: map[string]any{"aria-label": "Refresh orders", "data-action": "entity_list_orders_orders_1", "data-entity-list-refresh": "orders", "data-param-empty-text": "No orders yet.", "data-param-entity": "orders", "data-param-limit": 20, "text": "Refresh", "type": "button"}}, world.Node{Kind: "div", Props: map[string]any{"data-entity-list-body": true, "text": "No orders yet."}}}}),
	)
}

type ReviewsScreen struct{}

func (s *ReviewsScreen) ScreenTitle() string        { return "Reviews" }
func (s *ReviewsScreen) ScreenDescription() string  { return "Customer reviews and ratings" }
func (s *ReviewsScreen) ScreenType() app.ScreenType { return app.ScreenPage }
func (s *ReviewsScreen) ComponentID() string        { return "screen-reviews" }
func (s *ReviewsScreen) Actions() {
	component.On("entity_list_reviews_reviews_1", func(ctx *component.ComponentContext) { _ = ctx }, component.WithClientJS("(async () => {\n  const entity = \"reviews\";\n  const fields = [\"author_name\",\"rating\",\"title\"];\n  const root = document.querySelector('[data-entity-list=\"' + entity + '\"]');\n  const body = root && root.querySelector('[data-entity-list-body]');\n  if (!body) return;\n  const esc = (value) => String(value ?? '').replace(/[&<>\"']/g, (ch) => ({'&':'&amp;','<':'&lt;','>':'&gt;','\"':'&quot;',\"'\":'&#39;'}[ch]));\n  const table = (rowsHTML) => '<table><thead><tr>' + fields.map((field) => '<th>' + esc(field) + '</th>').join('') + '</tr></thead><tbody>' + rowsHTML + '</tbody></table>';\n  body.innerHTML = table('<tr><td colspan=\"' + fields.length + '\">Loading...</td></tr>');\n  try {\n    const res = await fetch('/' + entity + '?limit=' + 20, { headers: { 'Accept': 'application/json' } });\n    if (!res.ok) throw new Error('HTTP ' + res.status);\n    const payload = await res.json();\n    const rows = Array.isArray(payload.data) ? payload.data : [];\n    if (!rows.length) {\n      body.innerHTML = table('<tr><td colspan=\"' + fields.length + '\">No reviews yet.</td></tr>');\n      return;\n    }\n    body.innerHTML = table(rows.map((row) => '<tr>' + fields.map((field) => '<td>' + esc(row[field]) + '</td>').join('') + '</tr>').join(''));\n  } catch (err) {\n    body.innerHTML = table('<tr><td colspan=\"' + fields.length + '\">Failed to load ' + esc(entity) + '</td></tr>');\n  }\n})();"))
}

func (s *ReviewsScreen) Render() render.HTML {
	return render.Tag("div", map[string]string{"data-component": s.ComponentID()},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Customer Reviews")),
		kilnrender.RenderNode(world.Node{Kind: "section", Props: map[string]any{"class": "gofastr-entity-list", "data-entity-list": "reviews"}, Children: []world.Node{world.Node{Kind: "heading", Props: map[string]any{"level": 2, "text": "Latest Reviews"}}, world.Node{Kind: "button", Props: map[string]any{"aria-label": "Refresh reviews", "data-action": "entity_list_reviews_reviews_1", "data-entity-list-refresh": "reviews", "data-param-empty-text": "No reviews yet.", "data-param-entity": "reviews", "data-param-limit": 20, "text": "Refresh", "type": "button"}}, world.Node{Kind: "div", Props: map[string]any{"data-entity-list-body": true, "text": "No reviews yet."}}}}),
	)
}

type ProductNewScreen struct{}

func (s *ProductNewScreen) ScreenTitle() string        { return "Add Product" }
func (s *ProductNewScreen) ScreenDescription() string  { return "Create a new product listing" }
func (s *ProductNewScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ProductNewScreen) Render() render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Add New Product")),
		kilnrender.RenderNode(world.Node{Kind: "form", Props: map[string]any{"class": "gofastr-entity-form", "data-action": "entity_form_productNew_products", "data-entity-form": "products", "data-entity-mode": "create", "data-form-action": "/products"}, Children: []world.Node{world.Node{Kind: "heading", Props: map[string]any{"level": 2, "text": "New Product"}}, world.Node{Kind: "div", Props: map[string]any{"class": "form-field"}, Children: []world.Node{world.Node{Kind: "label", Props: map[string]any{"for": "field-name", "text": "Name"}}, world.Node{Kind: "input", Props: map[string]any{"id": "field-name", "name": "name", "required": true, "type": "text"}}}}, world.Node{Kind: "div", Props: map[string]any{"class": "form-field"}, Children: []world.Node{world.Node{Kind: "label", Props: map[string]any{"for": "field-slug", "text": "Slug"}}, world.Node{Kind: "input", Props: map[string]any{"id": "field-slug", "name": "slug", "required": true, "type": "text"}}}}, world.Node{Kind: "div", Props: map[string]any{"class": "form-field"}, Children: []world.Node{world.Node{Kind: "label", Props: map[string]any{"for": "field-sku", "text": "Sku"}}, world.Node{Kind: "input", Props: map[string]any{"id": "field-sku", "name": "sku", "required": false, "type": "text"}}}}, world.Node{Kind: "div", Props: map[string]any{"class": "form-field", "text": "Description"}, Children: []world.Node{world.Node{Kind: "textarea", Props: map[string]any{"id": "field-description", "name": "description", "required": false}}}}, world.Node{Kind: "div", Props: map[string]any{"class": "form-field"}, Children: []world.Node{world.Node{Kind: "label", Props: map[string]any{"for": "field-price", "text": "Price"}}, world.Node{Kind: "input", Props: map[string]any{"id": "field-price", "name": "price", "required": true, "type": "number"}}}}, world.Node{Kind: "div", Props: map[string]any{"class": "form-field"}, Children: []world.Node{world.Node{Kind: "label", Props: map[string]any{"for": "field-stock", "text": "Stock"}}, world.Node{Kind: "input", Props: map[string]any{"id": "field-stock", "name": "stock", "required": true, "type": "number"}}}}, world.Node{Kind: "div", Props: map[string]any{"class": "form-field", "text": "Status"}, Children: []world.Node{world.Node{Kind: "select", Props: map[string]any{"id": "field-status", "name": "status", "required": false}}}}, world.Node{Kind: "div", Props: map[string]any{"class": "form-field form-field-checkbox"}, Children: []world.Node{world.Node{Kind: "input", Props: map[string]any{"id": "field-featured", "name": "featured", "type": "checkbox"}}, world.Node{Kind: "label", Props: map[string]any{"for": "field-featured", "text": "Featured"}}}}, world.Node{Kind: "button", Props: map[string]any{"data-action": "entity_form_productNew_products_submit", "data-form-submit": "products", "text": "Create", "type": "submit"}}}}),
	)
}

type ProductDetailScreen struct{}

func (s *ProductDetailScreen) ScreenTitle() string        { return "Product Details" }
func (s *ProductDetailScreen) ScreenDescription() string  { return "View product details" }
func (s *ProductDetailScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ProductDetailScreen) Render() render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Product Details")),
		kilnrender.RenderNode(world.Node{Kind: "section", Props: map[string]any{"class": "gofastr-entity-detail", "data-action": "entity_detail_productDetail_products", "data-entity-detail": "products"}, Children: []world.Node{world.Node{Kind: "heading", Props: map[string]any{"level": 2, "text": "Product"}}, world.Node{Kind: "div", Props: map[string]any{"class": "detail-field", "data-field": "name", "data-field-label": "Name"}, Children: []world.Node{world.Node{Kind: "span", Props: map[string]any{"class": "detail-label", "text": "Name"}}, world.Node{Kind: "span", Props: map[string]any{"class": "detail-value", "data-field-value": "name", "text": "—"}}}}, world.Node{Kind: "div", Props: map[string]any{"class": "detail-field", "data-field": "slug", "data-field-label": "Slug"}, Children: []world.Node{world.Node{Kind: "span", Props: map[string]any{"class": "detail-label", "text": "Slug"}}, world.Node{Kind: "span", Props: map[string]any{"class": "detail-value", "data-field-value": "slug", "text": "—"}}}}, world.Node{Kind: "div", Props: map[string]any{"class": "detail-field", "data-field": "sku", "data-field-label": "Sku"}, Children: []world.Node{world.Node{Kind: "span", Props: map[string]any{"class": "detail-label", "text": "Sku"}}, world.Node{Kind: "span", Props: map[string]any{"class": "detail-value", "data-field-value": "sku", "text": "—"}}}}, world.Node{Kind: "div", Props: map[string]any{"class": "detail-field", "data-field": "description", "data-field-label": "Description"}, Children: []world.Node{world.Node{Kind: "span", Props: map[string]any{"class": "detail-label", "text": "Description"}}, world.Node{Kind: "span", Props: map[string]any{"class": "detail-value", "data-field-value": "description", "text": "—"}}}}, world.Node{Kind: "div", Props: map[string]any{"class": "detail-field", "data-field": "price", "data-field-label": "Price"}, Children: []world.Node{world.Node{Kind: "span", Props: map[string]any{"class": "detail-label", "text": "Price"}}, world.Node{Kind: "span", Props: map[string]any{"class": "detail-value", "data-field-value": "price", "text": "—"}}}}, world.Node{Kind: "div", Props: map[string]any{"class": "detail-field", "data-field": "stock", "data-field-label": "Stock"}, Children: []world.Node{world.Node{Kind: "span", Props: map[string]any{"class": "detail-label", "text": "Stock"}}, world.Node{Kind: "span", Props: map[string]any{"class": "detail-value", "data-field-value": "stock", "text": "—"}}}}, world.Node{Kind: "div", Props: map[string]any{"class": "detail-field", "data-field": "status", "data-field-label": "Status"}, Children: []world.Node{world.Node{Kind: "span", Props: map[string]any{"class": "detail-label", "text": "Status"}}, world.Node{Kind: "span", Props: map[string]any{"class": "detail-value", "data-field-value": "status", "text": "—"}}}}, world.Node{Kind: "div", Props: map[string]any{"class": "detail-field", "data-field": "featured", "data-field-label": "Featured"}, Children: []world.Node{world.Node{Kind: "span", Props: map[string]any{"class": "detail-label", "text": "Featured"}}, world.Node{Kind: "span", Props: map[string]any{"class": "detail-value", "data-field-value": "featured", "text": "—"}}}}}}),
	)
}

type OrderDetailScreen struct{}

func (s *OrderDetailScreen) ScreenTitle() string        { return "Order Details" }
func (s *OrderDetailScreen) ScreenDescription() string  { return "View order details" }
func (s *OrderDetailScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *OrderDetailScreen) Render() render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Order Details")),
		kilnrender.RenderNode(world.Node{Kind: "section", Props: map[string]any{"class": "gofastr-entity-detail", "data-action": "entity_detail_orderDetail_orders", "data-entity-detail": "orders"}, Children: []world.Node{world.Node{Kind: "heading", Props: map[string]any{"level": 2, "text": "Order"}}, world.Node{Kind: "div", Props: map[string]any{"class": "detail-field", "data-field": "order_number", "data-field-label": "Order Number"}, Children: []world.Node{world.Node{Kind: "span", Props: map[string]any{"class": "detail-label", "text": "Order Number"}}, world.Node{Kind: "span", Props: map[string]any{"class": "detail-value", "data-field-value": "order_number", "text": "—"}}}}, world.Node{Kind: "div", Props: map[string]any{"class": "detail-field", "data-field": "status", "data-field-label": "Status"}, Children: []world.Node{world.Node{Kind: "span", Props: map[string]any{"class": "detail-label", "text": "Status"}}, world.Node{Kind: "span", Props: map[string]any{"class": "detail-value", "data-field-value": "status", "text": "—"}}}}, world.Node{Kind: "div", Props: map[string]any{"class": "detail-field", "data-field": "customer_name", "data-field-label": "Customer Name"}, Children: []world.Node{world.Node{Kind: "span", Props: map[string]any{"class": "detail-label", "text": "Customer Name"}}, world.Node{Kind: "span", Props: map[string]any{"class": "detail-value", "data-field-value": "customer_name", "text": "—"}}}}, world.Node{Kind: "div", Props: map[string]any{"class": "detail-field", "data-field": "customer_email", "data-field-label": "Customer Email"}, Children: []world.Node{world.Node{Kind: "span", Props: map[string]any{"class": "detail-label", "text": "Customer Email"}}, world.Node{Kind: "span", Props: map[string]any{"class": "detail-value", "data-field-value": "customer_email", "text": "—"}}}}, world.Node{Kind: "div", Props: map[string]any{"class": "detail-field", "data-field": "total", "data-field-label": "Total"}, Children: []world.Node{world.Node{Kind: "span", Props: map[string]any{"class": "detail-label", "text": "Total"}}, world.Node{Kind: "span", Props: map[string]any{"class": "detail-value", "data-field-value": "total", "text": "—"}}}}, world.Node{Kind: "div", Props: map[string]any{"class": "detail-field", "data-field": "notes", "data-field-label": "Notes"}, Children: []world.Node{world.Node{Kind: "span", Props: map[string]any{"class": "detail-label", "text": "Notes"}}, world.Node{Kind: "span", Props: map[string]any{"class": "detail-value", "data-field-value": "notes", "text": "—"}}}}}}),
	)
}
