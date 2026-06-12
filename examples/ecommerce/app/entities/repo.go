package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/framework"
)

// CategoriesRepo is the typed repository for categories rows.
type CategoriesRepo struct {
	handler *framework.CrudHandler
}

// NewCategoriesRepo wires a typed repo against the App's "categories" entity. Panics if the
// entity hasn't been registered yet.
func NewCategoriesRepo(app *framework.App) *CategoriesRepo {
	entity, err := app.Registry.Get("categories")
	if err != nil {
		panic("entities: categories not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("categories")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &CategoriesRepo{handler: h}
}

// Handler returns the underlying CrudHandler — useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *CategoriesRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *CategoriesRepo) WithTx(tx *sql.Tx) *CategoriesRepo {
	h := *r.handler
	h.DB = tx
	return &CategoriesRepo{handler: &h}
}

// Create persists row and back-fills server-generated fields onto it.
func (r *CategoriesRepo) Create(ctx context.Context, row *Categories) error {
	body, err := framework.MarshalEntity(row)
	if err != nil {
		return err
	}
	out, err := r.handler.CreateOne(ctx, body)
	if err != nil {
		return err
	}
	return framework.UnmarshalEntity(out, row)
}

// Get fetches a row by id with optional eager-loaded includes.
func (r *CategoriesRepo) Get(ctx context.Context, id string, includes ...string) (*Categories, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Categories
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *CategoriesRepo) Update(ctx context.Context, id string, row *Categories) error {
	body, err := framework.MarshalEntity(row)
	if err != nil {
		return err
	}
	delete(body, "id") // id is taken from the path arg, not the body
	out, err := r.handler.UpdateOne(ctx, id, body)
	if err != nil {
		return err
	}
	return framework.UnmarshalEntity(out, row)
}

// Delete removes the row by id (or soft-deletes if SoftDelete is enabled on
// the entity).
func (r *CategoriesRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *CategoriesRepo) Query() *framework.TypedQuery[Categories] {
	return framework.NewTypedQuery[Categories](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *CategoriesRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(CategoriesID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *CategoriesRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *CategoriesRepo) FirstOrCreate(ctx context.Context, row *Categories, match framework.Condition) (*Categories, error) {
	existing, err := r.Query().Where(match).First(ctx)
	if err == nil {
		return existing, nil
	}
	if !framework.IsNotFound(err) {
		return nil, err
	}
	if err := r.Create(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

// BatchCreate inserts every row in one transaction; on any per-item error
// the entire batch rolls back. Returned slice is in input order.
func (r *CategoriesRepo) BatchCreate(ctx context.Context, rows []*Categories) ([]*Categories, error) {
	bodies := make([]map[string]any, len(rows))
	for i, row := range rows {
		b, err := framework.MarshalEntity(row)
		if err != nil {
			return nil, err
		}
		bodies[i] = b
	}
	results, err := r.handler.BatchCreateMany(ctx, bodies)
	if err != nil {
		return nil, err
	}
	for i, res := range results {
		if err := framework.UnmarshalEntity(res, rows[i]); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// BatchUpdate updates every row by its id in one transaction.
func (r *CategoriesRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Categories) ([]*Categories, error) {
	bodies := make([]map[string]any, len(rows))
	for i, row := range rows {
		b, err := framework.MarshalEntity(row)
		if err != nil {
			return nil, err
		}
		delete(b, "id")
		bodies[i] = b
	}
	results, err := r.handler.BatchUpdateMany(ctx, ids, bodies)
	if err != nil {
		return nil, err
	}
	for i, res := range results {
		if err := framework.UnmarshalEntity(res, rows[i]); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// BatchDelete deletes every id atomically.
func (r *CategoriesRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// OrderItemsRepo is the typed repository for order_items rows.
type OrderItemsRepo struct {
	handler *framework.CrudHandler
}

// NewOrderItemsRepo wires a typed repo against the App's "order_items" entity. Panics if the
// entity hasn't been registered yet.
func NewOrderItemsRepo(app *framework.App) *OrderItemsRepo {
	entity, err := app.Registry.Get("order_items")
	if err != nil {
		panic("entities: order_items not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("order_items")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &OrderItemsRepo{handler: h}
}

// Handler returns the underlying CrudHandler — useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *OrderItemsRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *OrderItemsRepo) WithTx(tx *sql.Tx) *OrderItemsRepo {
	h := *r.handler
	h.DB = tx
	return &OrderItemsRepo{handler: &h}
}

// Create persists row and back-fills server-generated fields onto it.
func (r *OrderItemsRepo) Create(ctx context.Context, row *OrderItems) error {
	body, err := framework.MarshalEntity(row)
	if err != nil {
		return err
	}
	out, err := r.handler.CreateOne(ctx, body)
	if err != nil {
		return err
	}
	return framework.UnmarshalEntity(out, row)
}

// Get fetches a row by id with optional eager-loaded includes.
func (r *OrderItemsRepo) Get(ctx context.Context, id string, includes ...string) (*OrderItems, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row OrderItems
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *OrderItemsRepo) Update(ctx context.Context, id string, row *OrderItems) error {
	body, err := framework.MarshalEntity(row)
	if err != nil {
		return err
	}
	delete(body, "id") // id is taken from the path arg, not the body
	out, err := r.handler.UpdateOne(ctx, id, body)
	if err != nil {
		return err
	}
	return framework.UnmarshalEntity(out, row)
}

// Delete removes the row by id (or soft-deletes if SoftDelete is enabled on
// the entity).
func (r *OrderItemsRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *OrderItemsRepo) Query() *framework.TypedQuery[OrderItems] {
	return framework.NewTypedQuery[OrderItems](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *OrderItemsRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(OrderItemsID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *OrderItemsRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *OrderItemsRepo) FirstOrCreate(ctx context.Context, row *OrderItems, match framework.Condition) (*OrderItems, error) {
	existing, err := r.Query().Where(match).First(ctx)
	if err == nil {
		return existing, nil
	}
	if !framework.IsNotFound(err) {
		return nil, err
	}
	if err := r.Create(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

// BatchCreate inserts every row in one transaction; on any per-item error
// the entire batch rolls back. Returned slice is in input order.
func (r *OrderItemsRepo) BatchCreate(ctx context.Context, rows []*OrderItems) ([]*OrderItems, error) {
	bodies := make([]map[string]any, len(rows))
	for i, row := range rows {
		b, err := framework.MarshalEntity(row)
		if err != nil {
			return nil, err
		}
		bodies[i] = b
	}
	results, err := r.handler.BatchCreateMany(ctx, bodies)
	if err != nil {
		return nil, err
	}
	for i, res := range results {
		if err := framework.UnmarshalEntity(res, rows[i]); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// BatchUpdate updates every row by its id in one transaction.
func (r *OrderItemsRepo) BatchUpdate(ctx context.Context, ids []string, rows []*OrderItems) ([]*OrderItems, error) {
	bodies := make([]map[string]any, len(rows))
	for i, row := range rows {
		b, err := framework.MarshalEntity(row)
		if err != nil {
			return nil, err
		}
		delete(b, "id")
		bodies[i] = b
	}
	results, err := r.handler.BatchUpdateMany(ctx, ids, bodies)
	if err != nil {
		return nil, err
	}
	for i, res := range results {
		if err := framework.UnmarshalEntity(res, rows[i]); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// BatchDelete deletes every id atomically.
func (r *OrderItemsRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// OrdersRepo is the typed repository for orders rows.
type OrdersRepo struct {
	handler *framework.CrudHandler
}

// NewOrdersRepo wires a typed repo against the App's "orders" entity. Panics if the
// entity hasn't been registered yet.
func NewOrdersRepo(app *framework.App) *OrdersRepo {
	entity, err := app.Registry.Get("orders")
	if err != nil {
		panic("entities: orders not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("orders")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &OrdersRepo{handler: h}
}

// Handler returns the underlying CrudHandler — useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *OrdersRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *OrdersRepo) WithTx(tx *sql.Tx) *OrdersRepo {
	h := *r.handler
	h.DB = tx
	return &OrdersRepo{handler: &h}
}

// Create persists row and back-fills server-generated fields onto it.
func (r *OrdersRepo) Create(ctx context.Context, row *Orders) error {
	body, err := framework.MarshalEntity(row)
	if err != nil {
		return err
	}
	out, err := r.handler.CreateOne(ctx, body)
	if err != nil {
		return err
	}
	return framework.UnmarshalEntity(out, row)
}

// Get fetches a row by id with optional eager-loaded includes.
func (r *OrdersRepo) Get(ctx context.Context, id string, includes ...string) (*Orders, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Orders
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *OrdersRepo) Update(ctx context.Context, id string, row *Orders) error {
	body, err := framework.MarshalEntity(row)
	if err != nil {
		return err
	}
	delete(body, "id") // id is taken from the path arg, not the body
	out, err := r.handler.UpdateOne(ctx, id, body)
	if err != nil {
		return err
	}
	return framework.UnmarshalEntity(out, row)
}

// Delete removes the row by id (or soft-deletes if SoftDelete is enabled on
// the entity).
func (r *OrdersRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *OrdersRepo) Query() *framework.TypedQuery[Orders] {
	return framework.NewTypedQuery[Orders](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *OrdersRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(OrdersID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *OrdersRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *OrdersRepo) FirstOrCreate(ctx context.Context, row *Orders, match framework.Condition) (*Orders, error) {
	existing, err := r.Query().Where(match).First(ctx)
	if err == nil {
		return existing, nil
	}
	if !framework.IsNotFound(err) {
		return nil, err
	}
	if err := r.Create(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

// BatchCreate inserts every row in one transaction; on any per-item error
// the entire batch rolls back. Returned slice is in input order.
func (r *OrdersRepo) BatchCreate(ctx context.Context, rows []*Orders) ([]*Orders, error) {
	bodies := make([]map[string]any, len(rows))
	for i, row := range rows {
		b, err := framework.MarshalEntity(row)
		if err != nil {
			return nil, err
		}
		bodies[i] = b
	}
	results, err := r.handler.BatchCreateMany(ctx, bodies)
	if err != nil {
		return nil, err
	}
	for i, res := range results {
		if err := framework.UnmarshalEntity(res, rows[i]); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// BatchUpdate updates every row by its id in one transaction.
func (r *OrdersRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Orders) ([]*Orders, error) {
	bodies := make([]map[string]any, len(rows))
	for i, row := range rows {
		b, err := framework.MarshalEntity(row)
		if err != nil {
			return nil, err
		}
		delete(b, "id")
		bodies[i] = b
	}
	results, err := r.handler.BatchUpdateMany(ctx, ids, bodies)
	if err != nil {
		return nil, err
	}
	for i, res := range results {
		if err := framework.UnmarshalEntity(res, rows[i]); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// BatchDelete deletes every id atomically.
func (r *OrdersRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// ProductsRepo is the typed repository for products rows.
type ProductsRepo struct {
	handler *framework.CrudHandler
}

// NewProductsRepo wires a typed repo against the App's "products" entity. Panics if the
// entity hasn't been registered yet.
func NewProductsRepo(app *framework.App) *ProductsRepo {
	entity, err := app.Registry.Get("products")
	if err != nil {
		panic("entities: products not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("products")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &ProductsRepo{handler: h}
}

// Handler returns the underlying CrudHandler — useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *ProductsRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *ProductsRepo) WithTx(tx *sql.Tx) *ProductsRepo {
	h := *r.handler
	h.DB = tx
	return &ProductsRepo{handler: &h}
}

// Create persists row and back-fills server-generated fields onto it.
func (r *ProductsRepo) Create(ctx context.Context, row *Products) error {
	body, err := framework.MarshalEntity(row)
	if err != nil {
		return err
	}
	out, err := r.handler.CreateOne(ctx, body)
	if err != nil {
		return err
	}
	return framework.UnmarshalEntity(out, row)
}

// Get fetches a row by id with optional eager-loaded includes.
func (r *ProductsRepo) Get(ctx context.Context, id string, includes ...string) (*Products, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Products
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *ProductsRepo) Update(ctx context.Context, id string, row *Products) error {
	body, err := framework.MarshalEntity(row)
	if err != nil {
		return err
	}
	delete(body, "id") // id is taken from the path arg, not the body
	out, err := r.handler.UpdateOne(ctx, id, body)
	if err != nil {
		return err
	}
	return framework.UnmarshalEntity(out, row)
}

// Delete removes the row by id (or soft-deletes if SoftDelete is enabled on
// the entity).
func (r *ProductsRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *ProductsRepo) Query() *framework.TypedQuery[Products] {
	return framework.NewTypedQuery[Products](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *ProductsRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(ProductsID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *ProductsRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *ProductsRepo) FirstOrCreate(ctx context.Context, row *Products, match framework.Condition) (*Products, error) {
	existing, err := r.Query().Where(match).First(ctx)
	if err == nil {
		return existing, nil
	}
	if !framework.IsNotFound(err) {
		return nil, err
	}
	if err := r.Create(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

// BatchCreate inserts every row in one transaction; on any per-item error
// the entire batch rolls back. Returned slice is in input order.
func (r *ProductsRepo) BatchCreate(ctx context.Context, rows []*Products) ([]*Products, error) {
	bodies := make([]map[string]any, len(rows))
	for i, row := range rows {
		b, err := framework.MarshalEntity(row)
		if err != nil {
			return nil, err
		}
		bodies[i] = b
	}
	results, err := r.handler.BatchCreateMany(ctx, bodies)
	if err != nil {
		return nil, err
	}
	for i, res := range results {
		if err := framework.UnmarshalEntity(res, rows[i]); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// BatchUpdate updates every row by its id in one transaction.
func (r *ProductsRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Products) ([]*Products, error) {
	bodies := make([]map[string]any, len(rows))
	for i, row := range rows {
		b, err := framework.MarshalEntity(row)
		if err != nil {
			return nil, err
		}
		delete(b, "id")
		bodies[i] = b
	}
	results, err := r.handler.BatchUpdateMany(ctx, ids, bodies)
	if err != nil {
		return nil, err
	}
	for i, res := range results {
		if err := framework.UnmarshalEntity(res, rows[i]); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// BatchDelete deletes every id atomically.
func (r *ProductsRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// ReviewsRepo is the typed repository for reviews rows.
type ReviewsRepo struct {
	handler *framework.CrudHandler
}

// NewReviewsRepo wires a typed repo against the App's "reviews" entity. Panics if the
// entity hasn't been registered yet.
func NewReviewsRepo(app *framework.App) *ReviewsRepo {
	entity, err := app.Registry.Get("reviews")
	if err != nil {
		panic("entities: reviews not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("reviews")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &ReviewsRepo{handler: h}
}

// Handler returns the underlying CrudHandler — useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *ReviewsRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *ReviewsRepo) WithTx(tx *sql.Tx) *ReviewsRepo {
	h := *r.handler
	h.DB = tx
	return &ReviewsRepo{handler: &h}
}

// Create persists row and back-fills server-generated fields onto it.
func (r *ReviewsRepo) Create(ctx context.Context, row *Reviews) error {
	body, err := framework.MarshalEntity(row)
	if err != nil {
		return err
	}
	out, err := r.handler.CreateOne(ctx, body)
	if err != nil {
		return err
	}
	return framework.UnmarshalEntity(out, row)
}

// Get fetches a row by id with optional eager-loaded includes.
func (r *ReviewsRepo) Get(ctx context.Context, id string, includes ...string) (*Reviews, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Reviews
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *ReviewsRepo) Update(ctx context.Context, id string, row *Reviews) error {
	body, err := framework.MarshalEntity(row)
	if err != nil {
		return err
	}
	delete(body, "id") // id is taken from the path arg, not the body
	out, err := r.handler.UpdateOne(ctx, id, body)
	if err != nil {
		return err
	}
	return framework.UnmarshalEntity(out, row)
}

// Delete removes the row by id (or soft-deletes if SoftDelete is enabled on
// the entity).
func (r *ReviewsRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *ReviewsRepo) Query() *framework.TypedQuery[Reviews] {
	return framework.NewTypedQuery[Reviews](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *ReviewsRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(ReviewsID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *ReviewsRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *ReviewsRepo) FirstOrCreate(ctx context.Context, row *Reviews, match framework.Condition) (*Reviews, error) {
	existing, err := r.Query().Where(match).First(ctx)
	if err == nil {
		return existing, nil
	}
	if !framework.IsNotFound(err) {
		return nil, err
	}
	if err := r.Create(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

// BatchCreate inserts every row in one transaction; on any per-item error
// the entire batch rolls back. Returned slice is in input order.
func (r *ReviewsRepo) BatchCreate(ctx context.Context, rows []*Reviews) ([]*Reviews, error) {
	bodies := make([]map[string]any, len(rows))
	for i, row := range rows {
		b, err := framework.MarshalEntity(row)
		if err != nil {
			return nil, err
		}
		bodies[i] = b
	}
	results, err := r.handler.BatchCreateMany(ctx, bodies)
	if err != nil {
		return nil, err
	}
	for i, res := range results {
		if err := framework.UnmarshalEntity(res, rows[i]); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// BatchUpdate updates every row by its id in one transaction.
func (r *ReviewsRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Reviews) ([]*Reviews, error) {
	bodies := make([]map[string]any, len(rows))
	for i, row := range rows {
		b, err := framework.MarshalEntity(row)
		if err != nil {
			return nil, err
		}
		delete(b, "id")
		bodies[i] = b
	}
	results, err := r.handler.BatchUpdateMany(ctx, ids, bodies)
	if err != nil {
		return nil, err
	}
	for i, res := range results {
		if err := framework.UnmarshalEntity(res, rows[i]); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// BatchDelete deletes every id atomically.
func (r *ReviewsRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}
