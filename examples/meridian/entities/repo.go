package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/framework"
)

// CustomersRepo is the typed repository for customers rows.
type CustomersRepo struct {
	handler *framework.CrudHandler
}

// NewCustomersRepo wires a typed repo against the App's "customers" entity. Panics if the
// entity hasn't been registered yet.
func NewCustomersRepo(app *framework.App) *CustomersRepo {
	entity, err := app.Registry.Get("customers")
	if err != nil {
		panic("entities: customers not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("customers")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &CustomersRepo{handler: h}
}

// Handler returns the underlying CrudHandler — useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *CustomersRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *CustomersRepo) WithTx(tx *sql.Tx) *CustomersRepo {
	h := *r.handler
	h.DB = tx
	return &CustomersRepo{handler: &h}
}

// Create persists row and back-fills server-generated fields onto it.
func (r *CustomersRepo) Create(ctx context.Context, row *Customers) error {
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
func (r *CustomersRepo) Get(ctx context.Context, id string, includes ...string) (*Customers, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Customers
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *CustomersRepo) Update(ctx context.Context, id string, row *Customers) error {
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
func (r *CustomersRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *CustomersRepo) Query() *framework.TypedQuery[Customers] {
	return framework.NewTypedQuery[Customers](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *CustomersRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(CustomersID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *CustomersRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *CustomersRepo) FirstOrCreate(ctx context.Context, row *Customers, match framework.Condition) (*Customers, error) {
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
func (r *CustomersRepo) BatchCreate(ctx context.Context, rows []*Customers) ([]*Customers, error) {
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
func (r *CustomersRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Customers) ([]*Customers, error) {
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
func (r *CustomersRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// InvoicesRepo is the typed repository for invoices rows.
type InvoicesRepo struct {
	handler *framework.CrudHandler
}

// NewInvoicesRepo wires a typed repo against the App's "invoices" entity. Panics if the
// entity hasn't been registered yet.
func NewInvoicesRepo(app *framework.App) *InvoicesRepo {
	entity, err := app.Registry.Get("invoices")
	if err != nil {
		panic("entities: invoices not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("invoices")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &InvoicesRepo{handler: h}
}

// Handler returns the underlying CrudHandler — useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *InvoicesRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *InvoicesRepo) WithTx(tx *sql.Tx) *InvoicesRepo {
	h := *r.handler
	h.DB = tx
	return &InvoicesRepo{handler: &h}
}

// Create persists row and back-fills server-generated fields onto it.
func (r *InvoicesRepo) Create(ctx context.Context, row *Invoices) error {
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
func (r *InvoicesRepo) Get(ctx context.Context, id string, includes ...string) (*Invoices, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Invoices
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *InvoicesRepo) Update(ctx context.Context, id string, row *Invoices) error {
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
func (r *InvoicesRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *InvoicesRepo) Query() *framework.TypedQuery[Invoices] {
	return framework.NewTypedQuery[Invoices](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *InvoicesRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(InvoicesID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *InvoicesRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *InvoicesRepo) FirstOrCreate(ctx context.Context, row *Invoices, match framework.Condition) (*Invoices, error) {
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
func (r *InvoicesRepo) BatchCreate(ctx context.Context, rows []*Invoices) ([]*Invoices, error) {
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
func (r *InvoicesRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Invoices) ([]*Invoices, error) {
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
func (r *InvoicesRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// PaymentsRepo is the typed repository for payments rows.
type PaymentsRepo struct {
	handler *framework.CrudHandler
}

// NewPaymentsRepo wires a typed repo against the App's "payments" entity. Panics if the
// entity hasn't been registered yet.
func NewPaymentsRepo(app *framework.App) *PaymentsRepo {
	entity, err := app.Registry.Get("payments")
	if err != nil {
		panic("entities: payments not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("payments")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &PaymentsRepo{handler: h}
}

// Handler returns the underlying CrudHandler — useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *PaymentsRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *PaymentsRepo) WithTx(tx *sql.Tx) *PaymentsRepo {
	h := *r.handler
	h.DB = tx
	return &PaymentsRepo{handler: &h}
}

// Create persists row and back-fills server-generated fields onto it.
func (r *PaymentsRepo) Create(ctx context.Context, row *Payments) error {
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
func (r *PaymentsRepo) Get(ctx context.Context, id string, includes ...string) (*Payments, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Payments
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *PaymentsRepo) Update(ctx context.Context, id string, row *Payments) error {
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
func (r *PaymentsRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *PaymentsRepo) Query() *framework.TypedQuery[Payments] {
	return framework.NewTypedQuery[Payments](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *PaymentsRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(PaymentsID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *PaymentsRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *PaymentsRepo) FirstOrCreate(ctx context.Context, row *Payments, match framework.Condition) (*Payments, error) {
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
func (r *PaymentsRepo) BatchCreate(ctx context.Context, rows []*Payments) ([]*Payments, error) {
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
func (r *PaymentsRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Payments) ([]*Payments, error) {
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
func (r *PaymentsRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// PlansRepo is the typed repository for plans rows.
type PlansRepo struct {
	handler *framework.CrudHandler
}

// NewPlansRepo wires a typed repo against the App's "plans" entity. Panics if the
// entity hasn't been registered yet.
func NewPlansRepo(app *framework.App) *PlansRepo {
	entity, err := app.Registry.Get("plans")
	if err != nil {
		panic("entities: plans not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("plans")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &PlansRepo{handler: h}
}

// Handler returns the underlying CrudHandler — useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *PlansRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *PlansRepo) WithTx(tx *sql.Tx) *PlansRepo {
	h := *r.handler
	h.DB = tx
	return &PlansRepo{handler: &h}
}

// Create persists row and back-fills server-generated fields onto it.
func (r *PlansRepo) Create(ctx context.Context, row *Plans) error {
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
func (r *PlansRepo) Get(ctx context.Context, id string, includes ...string) (*Plans, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Plans
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *PlansRepo) Update(ctx context.Context, id string, row *Plans) error {
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
func (r *PlansRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *PlansRepo) Query() *framework.TypedQuery[Plans] {
	return framework.NewTypedQuery[Plans](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *PlansRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(PlansID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *PlansRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *PlansRepo) FirstOrCreate(ctx context.Context, row *Plans, match framework.Condition) (*Plans, error) {
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
func (r *PlansRepo) BatchCreate(ctx context.Context, rows []*Plans) ([]*Plans, error) {
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
func (r *PlansRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Plans) ([]*Plans, error) {
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
func (r *PlansRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// SubscriptionsRepo is the typed repository for subscriptions rows.
type SubscriptionsRepo struct {
	handler *framework.CrudHandler
}

// NewSubscriptionsRepo wires a typed repo against the App's "subscriptions" entity. Panics if the
// entity hasn't been registered yet.
func NewSubscriptionsRepo(app *framework.App) *SubscriptionsRepo {
	entity, err := app.Registry.Get("subscriptions")
	if err != nil {
		panic("entities: subscriptions not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("subscriptions")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &SubscriptionsRepo{handler: h}
}

// Handler returns the underlying CrudHandler — useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *SubscriptionsRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *SubscriptionsRepo) WithTx(tx *sql.Tx) *SubscriptionsRepo {
	h := *r.handler
	h.DB = tx
	return &SubscriptionsRepo{handler: &h}
}

// Create persists row and back-fills server-generated fields onto it.
func (r *SubscriptionsRepo) Create(ctx context.Context, row *Subscriptions) error {
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
func (r *SubscriptionsRepo) Get(ctx context.Context, id string, includes ...string) (*Subscriptions, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Subscriptions
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *SubscriptionsRepo) Update(ctx context.Context, id string, row *Subscriptions) error {
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
func (r *SubscriptionsRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *SubscriptionsRepo) Query() *framework.TypedQuery[Subscriptions] {
	return framework.NewTypedQuery[Subscriptions](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *SubscriptionsRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(SubscriptionsID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *SubscriptionsRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *SubscriptionsRepo) FirstOrCreate(ctx context.Context, row *Subscriptions, match framework.Condition) (*Subscriptions, error) {
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
func (r *SubscriptionsRepo) BatchCreate(ctx context.Context, rows []*Subscriptions) ([]*Subscriptions, error) {
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
func (r *SubscriptionsRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Subscriptions) ([]*Subscriptions, error) {
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
func (r *SubscriptionsRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}
