package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Client is a typed HTTP client targeting the gofastr server's CRUD routes.
// Pass any *http.Client (httptest, retryable wrapper, etc.) — Client never
// closes it.
//
// Token, when set, is sent as "Authorization: Bearer <Token>" on every
// request. Pair it with the server's API-token middleware
// (auth.TokenMiddleware) and a scoped token minted via the app's
// /auth/tokens endpoints; leave empty for cookie/session or public APIs.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Token   string
}

// NewClient constructs a Client with the default http.Client when one is
// not supplied. BaseURL should NOT include a trailing slash.
func NewClient(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{BaseURL: baseURL, HTTP: httpClient}
}

// APIError is returned for non-2xx responses. Status is the HTTP code;
// Body holds the raw response so callers can decode application-level error
// fields if they want.
type APIError struct {
	Status int
	Body   []byte
}

func (e *APIError) Error() string {
	return fmt.Sprintf("api: %d: %s", e.Status, string(e.Body))
}

// doJSON marshals body (when non-nil), sends method+path, and decodes the
// 2xx response into out. Non-2xx returns an *APIError; transport errors
// pass through.
func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reqBody)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return &APIError{Status: resp.StatusCode, Body: bodyBytes}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// doSingleJSON decodes the {"data": {...}} envelope used by single-record
// CRUD responses into out.
func (c *Client) doSingleJSON(ctx context.Context, method, path string, body, out any) error {
	var envelope map[string]json.RawMessage
	if err := c.doJSON(ctx, method, path, body, &envelope); err != nil {
		return err
	}
	return json.Unmarshal(envelope["data"], out)
}

// Do is the raw escape hatch under the typed methods: it sends method+path
// with an optional JSON body and decodes the 2xx response into out, applying
// the same base URL, auth header, and error handling. Reach for it when the
// typed surface doesn't fit — custom endpoints, or presence-faithful bodies
// built as map[string]any (a typed Input's json:",omitempty" drops explicit
// zero values; a map keeps them).
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	return c.doJSON(ctx, method, path, body, out)
}

// BatchResult is one entry in a _batch response, in input order. Exactly one
// of Data, Error, or Skipped is populated. When a later item failed, earlier
// successes still carry Data — but Committed=false on the envelope means
// nothing was persisted (the whole batch runs in one transaction).
type BatchResult struct {
	Index   int                 `json:"index"`
	Data    map[string]any      `json:"data,omitempty"`
	Error   string              `json:"error,omitempty"`
	Fields  map[string][]string `json:"fields,omitempty"`
	Skipped bool                `json:"skipped,omitempty"`
}

// BatchResponse is the envelope every _batch endpoint returns.
type BatchResponse struct {
	Committed bool          `json:"committed"`
	Results   []BatchResult `json:"results"`
}

// doBatch sends a _batch request. The server answers 200 (committed) or 400
// (rolled back) with the same envelope, so a 400 with a decodable body is a
// result, not an error — callers inspect Committed and per-item Error fields.
func (c *Client) doBatch(ctx context.Context, method, path string, body any) (BatchResponse, error) {
	var out BatchResponse
	err := c.doJSON(ctx, method, path, body, &out)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusBadRequest {
			if jsonErr := json.Unmarshal(apiErr.Body, &out); jsonErr == nil && len(out.Results) > 0 {
				return out, nil
			}
		}
		return BatchResponse{}, err
	}
	return out, nil
}

// watchSSE opens a text/event-stream GET and hands each event:/data: frame
// to fn until ctx cancels, the stream ends (returns nil), or fn errors
// (returned as-is). Comment lines (leading ':') are ignored.
//
// The stream is long-lived: use an *http.Client without a Timeout (the
// default), or the transport will kill the subscription mid-stream.
func (c *Client) watchSSE(ctx context.Context, path string, fn func(event string, data []byte) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return &APIError{Status: resp.StatusCode, Body: bodyBytes}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var event string
	var data []byte
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			if len(data) > 0 {
				if err := fn(event, data); err != nil {
					return err
				}
			}
			event, data = "", nil
		case strings.HasPrefix(line, ":"):
			// comment / heartbeat
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:"))...)
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return scanner.Err()
}

type Plans struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Slug     string `json:"slug,omitempty"`
	Price    string `json:"price,omitempty"`
	Interval string `json:"interval,omitempty"`
	Active   bool   `json:"active,omitempty"`
}

type PlansInput struct {
	Name     string `json:"name,omitempty"`
	Slug     string `json:"slug,omitempty"`
	Price    string `json:"price,omitempty"`
	Interval string `json:"interval,omitempty"`
	Active   bool   `json:"active,omitempty"`
}

type PlansPatch struct {
	Name     *string `json:"name,omitempty"`
	Slug     *string `json:"slug,omitempty"`
	Price    *string `json:"price,omitempty"`
	Interval *string `json:"interval,omitempty"`
	Active   *bool   `json:"active,omitempty"`
}

type PlansListResponse struct {
	Data       []Plans `json:"data"`
	Total      int     `json:"total"`
	Page       int     `json:"page"`
	PerPage    int     `json:"perPage"`
	TotalPages int     `json:"totalPages"`
}

// ListPlans fetches a page of plans. Pass nil for params to use server defaults.
func (c *Client) ListPlans(ctx context.Context, params url.Values) (PlansListResponse, error) {
	var out PlansListResponse
	path := "/plans"
	if params != nil {
		path += "?" + params.Encode()
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return PlansListResponse{}, err
	}
	return out, nil
}

// GetPlans fetches a single record by id. Returns *APIError with 404 when missing.
func (c *Client) GetPlans(ctx context.Context, id string) (Plans, error) {
	var out Plans
	if err := c.doSingleJSON(ctx, http.MethodGet, "/plans/"+url.PathEscape(id), nil, &out); err != nil {
		return Plans{}, err
	}
	return out, nil
}

// CreatePlans posts a new record and returns the server-canonical row.
func (c *Client) CreatePlans(ctx context.Context, body PlansInput) (Plans, error) {
	var out Plans
	if err := c.doSingleJSON(ctx, http.MethodPost, "/plans", body, &out); err != nil {
		return Plans{}, err
	}
	return out, nil
}

// UpdatePlans updates the record at id with the partial body.
func (c *Client) UpdatePlans(ctx context.Context, id string, body PlansInput) (Plans, error) {
	var out Plans
	if err := c.doSingleJSON(ctx, http.MethodPut, "/plans/"+url.PathEscape(id), body, &out); err != nil {
		return Plans{}, err
	}
	return out, nil
}

// PatchPlans updates exactly the fields whose pointers in body are non-nil.
// A nil field is omitted (the server leaves it untouched); a non-nil pointer
// sets the field — including to a zero value (false, 0, ""), which a value
// payload cannot express. Pass an empty PlansPatch to no-op.
func (c *Client) PatchPlans(ctx context.Context, id string, body PlansPatch) (Plans, error) {
	var out Plans
	if err := c.doSingleJSON(ctx, http.MethodPatch, "/plans/"+url.PathEscape(id), body, &out); err != nil {
		return Plans{}, err
	}
	return out, nil
}

// DeletePlans removes the record at id.
func (c *Client) DeletePlans(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/plans/"+url.PathEscape(id), nil, nil)
}

type PlansBatchPatch struct {
	ID       string  `json:"id"`
	Name     *string `json:"name,omitempty"`
	Slug     *string `json:"slug,omitempty"`
	Price    *string `json:"price,omitempty"`
	Interval *string `json:"interval,omitempty"`
	Active   *bool   `json:"active,omitempty"`
}

// BatchCreatePlans creates up to 100 records atomically (one transaction).
// Inspect Committed and the per-item Results — a 400 rollback is returned as
// a BatchResponse, not an error.
func (c *Client) BatchCreatePlans(ctx context.Context, items []PlansInput) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPost, "/plans/_batch", map[string]any{"items": items})
}

// BatchUpdatePlans patches up to 100 records atomically. Each item names its
// target via ID; nil pointer fields are left untouched.
func (c *Client) BatchUpdatePlans(ctx context.Context, items []PlansBatchPatch) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPatch, "/plans/_batch", map[string]any{"items": items})
}

// BatchDeletePlans deletes the given ids atomically.
func (c *Client) BatchDeletePlans(ctx context.Context, ids []string) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodDelete, "/plans/_batch", map[string]any{"ids": ids})
}

// WatchPlans subscribes to the entity's live event feed (entity.created /
// entity.updated / entity.deleted) and blocks, invoking fn per event, until
// ctx cancels, the stream ends, or fn returns an error. data is the full
// event JSON. Requires an authenticated client unless the entity is Public.
func (c *Client) WatchPlans(ctx context.Context, fn func(event string, data []byte) error) error {
	return c.watchSSE(ctx, "/plans/_events", fn)
}

type Customers struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Email   string `json:"email,omitempty"`
	Company string `json:"company,omitempty"`
	Status  string `json:"status,omitempty"`
	Mrr     string `json:"mrr,omitempty"`
	UserId  string `json:"userId,omitempty"`
}

type CustomersInput struct {
	Name    string `json:"name,omitempty"`
	Email   string `json:"email,omitempty"`
	Company string `json:"company,omitempty"`
	Status  string `json:"status,omitempty"`
	Mrr     string `json:"mrr,omitempty"`
	UserId  string `json:"userId,omitempty"`
}

type CustomersPatch struct {
	Name    *string `json:"name,omitempty"`
	Email   *string `json:"email,omitempty"`
	Company *string `json:"company,omitempty"`
	Status  *string `json:"status,omitempty"`
	Mrr     *string `json:"mrr,omitempty"`
	UserId  *string `json:"userId,omitempty"`
}

type CustomersListResponse struct {
	Data       []Customers `json:"data"`
	Total      int         `json:"total"`
	Page       int         `json:"page"`
	PerPage    int         `json:"perPage"`
	TotalPages int         `json:"totalPages"`
}

// ListCustomers fetches a page of customers. Pass nil for params to use server defaults.
func (c *Client) ListCustomers(ctx context.Context, params url.Values) (CustomersListResponse, error) {
	var out CustomersListResponse
	path := "/customers"
	if params != nil {
		path += "?" + params.Encode()
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return CustomersListResponse{}, err
	}
	return out, nil
}

// GetCustomers fetches a single record by id. Returns *APIError with 404 when missing.
func (c *Client) GetCustomers(ctx context.Context, id string) (Customers, error) {
	var out Customers
	if err := c.doSingleJSON(ctx, http.MethodGet, "/customers/"+url.PathEscape(id), nil, &out); err != nil {
		return Customers{}, err
	}
	return out, nil
}

// CreateCustomers posts a new record and returns the server-canonical row.
func (c *Client) CreateCustomers(ctx context.Context, body CustomersInput) (Customers, error) {
	var out Customers
	if err := c.doSingleJSON(ctx, http.MethodPost, "/customers", body, &out); err != nil {
		return Customers{}, err
	}
	return out, nil
}

// UpdateCustomers updates the record at id with the partial body.
func (c *Client) UpdateCustomers(ctx context.Context, id string, body CustomersInput) (Customers, error) {
	var out Customers
	if err := c.doSingleJSON(ctx, http.MethodPut, "/customers/"+url.PathEscape(id), body, &out); err != nil {
		return Customers{}, err
	}
	return out, nil
}

// PatchCustomers updates exactly the fields whose pointers in body are non-nil.
// A nil field is omitted (the server leaves it untouched); a non-nil pointer
// sets the field — including to a zero value (false, 0, ""), which a value
// payload cannot express. Pass an empty CustomersPatch to no-op.
func (c *Client) PatchCustomers(ctx context.Context, id string, body CustomersPatch) (Customers, error) {
	var out Customers
	if err := c.doSingleJSON(ctx, http.MethodPatch, "/customers/"+url.PathEscape(id), body, &out); err != nil {
		return Customers{}, err
	}
	return out, nil
}

// DeleteCustomers removes the record at id.
func (c *Client) DeleteCustomers(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/customers/"+url.PathEscape(id), nil, nil)
}

type CustomersBatchPatch struct {
	ID      string  `json:"id"`
	Name    *string `json:"name,omitempty"`
	Email   *string `json:"email,omitempty"`
	Company *string `json:"company,omitempty"`
	Status  *string `json:"status,omitempty"`
	Mrr     *string `json:"mrr,omitempty"`
	UserId  *string `json:"userId,omitempty"`
}

// BatchCreateCustomers creates up to 100 records atomically (one transaction).
// Inspect Committed and the per-item Results — a 400 rollback is returned as
// a BatchResponse, not an error.
func (c *Client) BatchCreateCustomers(ctx context.Context, items []CustomersInput) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPost, "/customers/_batch", map[string]any{"items": items})
}

// BatchUpdateCustomers patches up to 100 records atomically. Each item names its
// target via ID; nil pointer fields are left untouched.
func (c *Client) BatchUpdateCustomers(ctx context.Context, items []CustomersBatchPatch) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPatch, "/customers/_batch", map[string]any{"items": items})
}

// BatchDeleteCustomers deletes the given ids atomically.
func (c *Client) BatchDeleteCustomers(ctx context.Context, ids []string) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodDelete, "/customers/_batch", map[string]any{"ids": ids})
}

// WatchCustomers subscribes to the entity's live event feed (entity.created /
// entity.updated / entity.deleted) and blocks, invoking fn per event, until
// ctx cancels, the stream ends, or fn returns an error. data is the full
// event JSON. Requires an authenticated client unless the entity is Public.
func (c *Client) WatchCustomers(ctx context.Context, fn func(event string, data []byte) error) error {
	return c.watchSSE(ctx, "/customers/_events", fn)
}

type Subscriptions struct {
	ID         string `json:"id"`
	CustomerId string `json:"customerId,omitempty"`
	PlanId     string `json:"planId,omitempty"`
	Status     string `json:"status,omitempty"`
	Mrr        string `json:"mrr,omitempty"`
	StartedOn  string `json:"startedOn,omitempty"`
	RenewsOn   string `json:"renewsOn,omitempty"`
	UserId     string `json:"userId,omitempty"`
}

type SubscriptionsInput struct {
	CustomerId string `json:"customerId,omitempty"`
	PlanId     string `json:"planId,omitempty"`
	Status     string `json:"status,omitempty"`
	Mrr        string `json:"mrr,omitempty"`
	StartedOn  string `json:"startedOn,omitempty"`
	RenewsOn   string `json:"renewsOn,omitempty"`
	UserId     string `json:"userId,omitempty"`
}

type SubscriptionsPatch struct {
	CustomerId *string `json:"customerId,omitempty"`
	PlanId     *string `json:"planId,omitempty"`
	Status     *string `json:"status,omitempty"`
	Mrr        *string `json:"mrr,omitempty"`
	StartedOn  *string `json:"startedOn,omitempty"`
	RenewsOn   *string `json:"renewsOn,omitempty"`
	UserId     *string `json:"userId,omitempty"`
}

type SubscriptionsListResponse struct {
	Data       []Subscriptions `json:"data"`
	Total      int             `json:"total"`
	Page       int             `json:"page"`
	PerPage    int             `json:"perPage"`
	TotalPages int             `json:"totalPages"`
}

// ListSubscriptions fetches a page of subscriptions. Pass nil for params to use server defaults.
func (c *Client) ListSubscriptions(ctx context.Context, params url.Values) (SubscriptionsListResponse, error) {
	var out SubscriptionsListResponse
	path := "/subscriptions"
	if params != nil {
		path += "?" + params.Encode()
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return SubscriptionsListResponse{}, err
	}
	return out, nil
}

// GetSubscriptions fetches a single record by id. Returns *APIError with 404 when missing.
func (c *Client) GetSubscriptions(ctx context.Context, id string) (Subscriptions, error) {
	var out Subscriptions
	if err := c.doSingleJSON(ctx, http.MethodGet, "/subscriptions/"+url.PathEscape(id), nil, &out); err != nil {
		return Subscriptions{}, err
	}
	return out, nil
}

// CreateSubscriptions posts a new record and returns the server-canonical row.
func (c *Client) CreateSubscriptions(ctx context.Context, body SubscriptionsInput) (Subscriptions, error) {
	var out Subscriptions
	if err := c.doSingleJSON(ctx, http.MethodPost, "/subscriptions", body, &out); err != nil {
		return Subscriptions{}, err
	}
	return out, nil
}

// UpdateSubscriptions updates the record at id with the partial body.
func (c *Client) UpdateSubscriptions(ctx context.Context, id string, body SubscriptionsInput) (Subscriptions, error) {
	var out Subscriptions
	if err := c.doSingleJSON(ctx, http.MethodPut, "/subscriptions/"+url.PathEscape(id), body, &out); err != nil {
		return Subscriptions{}, err
	}
	return out, nil
}

// PatchSubscriptions updates exactly the fields whose pointers in body are non-nil.
// A nil field is omitted (the server leaves it untouched); a non-nil pointer
// sets the field — including to a zero value (false, 0, ""), which a value
// payload cannot express. Pass an empty SubscriptionsPatch to no-op.
func (c *Client) PatchSubscriptions(ctx context.Context, id string, body SubscriptionsPatch) (Subscriptions, error) {
	var out Subscriptions
	if err := c.doSingleJSON(ctx, http.MethodPatch, "/subscriptions/"+url.PathEscape(id), body, &out); err != nil {
		return Subscriptions{}, err
	}
	return out, nil
}

// DeleteSubscriptions removes the record at id.
func (c *Client) DeleteSubscriptions(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/subscriptions/"+url.PathEscape(id), nil, nil)
}

type SubscriptionsBatchPatch struct {
	ID         string  `json:"id"`
	CustomerId *string `json:"customerId,omitempty"`
	PlanId     *string `json:"planId,omitempty"`
	Status     *string `json:"status,omitempty"`
	Mrr        *string `json:"mrr,omitempty"`
	StartedOn  *string `json:"startedOn,omitempty"`
	RenewsOn   *string `json:"renewsOn,omitempty"`
	UserId     *string `json:"userId,omitempty"`
}

// BatchCreateSubscriptions creates up to 100 records atomically (one transaction).
// Inspect Committed and the per-item Results — a 400 rollback is returned as
// a BatchResponse, not an error.
func (c *Client) BatchCreateSubscriptions(ctx context.Context, items []SubscriptionsInput) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPost, "/subscriptions/_batch", map[string]any{"items": items})
}

// BatchUpdateSubscriptions patches up to 100 records atomically. Each item names its
// target via ID; nil pointer fields are left untouched.
func (c *Client) BatchUpdateSubscriptions(ctx context.Context, items []SubscriptionsBatchPatch) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPatch, "/subscriptions/_batch", map[string]any{"items": items})
}

// BatchDeleteSubscriptions deletes the given ids atomically.
func (c *Client) BatchDeleteSubscriptions(ctx context.Context, ids []string) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodDelete, "/subscriptions/_batch", map[string]any{"ids": ids})
}

// WatchSubscriptions subscribes to the entity's live event feed (entity.created /
// entity.updated / entity.deleted) and blocks, invoking fn per event, until
// ctx cancels, the stream ends, or fn returns an error. data is the full
// event JSON. Requires an authenticated client unless the entity is Public.
func (c *Client) WatchSubscriptions(ctx context.Context, fn func(event string, data []byte) error) error {
	return c.watchSSE(ctx, "/subscriptions/_events", fn)
}

type Invoices struct {
	ID         string `json:"id"`
	CustomerId string `json:"customerId,omitempty"`
	Number     string `json:"number,omitempty"`
	Amount     string `json:"amount,omitempty"`
	Status     string `json:"status,omitempty"`
	IssuedOn   string `json:"issuedOn,omitempty"`
	DueOn      string `json:"dueOn,omitempty"`
	PaidOn     string `json:"paidOn,omitempty"`
	UserId     string `json:"userId,omitempty"`
}

type InvoicesInput struct {
	CustomerId string `json:"customerId,omitempty"`
	Number     string `json:"number,omitempty"`
	Amount     string `json:"amount,omitempty"`
	Status     string `json:"status,omitempty"`
	IssuedOn   string `json:"issuedOn,omitempty"`
	DueOn      string `json:"dueOn,omitempty"`
	PaidOn     string `json:"paidOn,omitempty"`
	UserId     string `json:"userId,omitempty"`
}

type InvoicesPatch struct {
	CustomerId *string `json:"customerId,omitempty"`
	Number     *string `json:"number,omitempty"`
	Amount     *string `json:"amount,omitempty"`
	Status     *string `json:"status,omitempty"`
	IssuedOn   *string `json:"issuedOn,omitempty"`
	DueOn      *string `json:"dueOn,omitempty"`
	PaidOn     *string `json:"paidOn,omitempty"`
	UserId     *string `json:"userId,omitempty"`
}

type InvoicesListResponse struct {
	Data       []Invoices `json:"data"`
	Total      int        `json:"total"`
	Page       int        `json:"page"`
	PerPage    int        `json:"perPage"`
	TotalPages int        `json:"totalPages"`
}

// ListInvoices fetches a page of invoices. Pass nil for params to use server defaults.
func (c *Client) ListInvoices(ctx context.Context, params url.Values) (InvoicesListResponse, error) {
	var out InvoicesListResponse
	path := "/invoices"
	if params != nil {
		path += "?" + params.Encode()
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return InvoicesListResponse{}, err
	}
	return out, nil
}

// GetInvoices fetches a single record by id. Returns *APIError with 404 when missing.
func (c *Client) GetInvoices(ctx context.Context, id string) (Invoices, error) {
	var out Invoices
	if err := c.doSingleJSON(ctx, http.MethodGet, "/invoices/"+url.PathEscape(id), nil, &out); err != nil {
		return Invoices{}, err
	}
	return out, nil
}

// CreateInvoices posts a new record and returns the server-canonical row.
func (c *Client) CreateInvoices(ctx context.Context, body InvoicesInput) (Invoices, error) {
	var out Invoices
	if err := c.doSingleJSON(ctx, http.MethodPost, "/invoices", body, &out); err != nil {
		return Invoices{}, err
	}
	return out, nil
}

// UpdateInvoices updates the record at id with the partial body.
func (c *Client) UpdateInvoices(ctx context.Context, id string, body InvoicesInput) (Invoices, error) {
	var out Invoices
	if err := c.doSingleJSON(ctx, http.MethodPut, "/invoices/"+url.PathEscape(id), body, &out); err != nil {
		return Invoices{}, err
	}
	return out, nil
}

// PatchInvoices updates exactly the fields whose pointers in body are non-nil.
// A nil field is omitted (the server leaves it untouched); a non-nil pointer
// sets the field — including to a zero value (false, 0, ""), which a value
// payload cannot express. Pass an empty InvoicesPatch to no-op.
func (c *Client) PatchInvoices(ctx context.Context, id string, body InvoicesPatch) (Invoices, error) {
	var out Invoices
	if err := c.doSingleJSON(ctx, http.MethodPatch, "/invoices/"+url.PathEscape(id), body, &out); err != nil {
		return Invoices{}, err
	}
	return out, nil
}

// DeleteInvoices removes the record at id.
func (c *Client) DeleteInvoices(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/invoices/"+url.PathEscape(id), nil, nil)
}

type InvoicesBatchPatch struct {
	ID         string  `json:"id"`
	CustomerId *string `json:"customerId,omitempty"`
	Number     *string `json:"number,omitempty"`
	Amount     *string `json:"amount,omitempty"`
	Status     *string `json:"status,omitempty"`
	IssuedOn   *string `json:"issuedOn,omitempty"`
	DueOn      *string `json:"dueOn,omitempty"`
	PaidOn     *string `json:"paidOn,omitempty"`
	UserId     *string `json:"userId,omitempty"`
}

// BatchCreateInvoices creates up to 100 records atomically (one transaction).
// Inspect Committed and the per-item Results — a 400 rollback is returned as
// a BatchResponse, not an error.
func (c *Client) BatchCreateInvoices(ctx context.Context, items []InvoicesInput) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPost, "/invoices/_batch", map[string]any{"items": items})
}

// BatchUpdateInvoices patches up to 100 records atomically. Each item names its
// target via ID; nil pointer fields are left untouched.
func (c *Client) BatchUpdateInvoices(ctx context.Context, items []InvoicesBatchPatch) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPatch, "/invoices/_batch", map[string]any{"items": items})
}

// BatchDeleteInvoices deletes the given ids atomically.
func (c *Client) BatchDeleteInvoices(ctx context.Context, ids []string) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodDelete, "/invoices/_batch", map[string]any{"ids": ids})
}

// WatchInvoices subscribes to the entity's live event feed (entity.created /
// entity.updated / entity.deleted) and blocks, invoking fn per event, until
// ctx cancels, the stream ends, or fn returns an error. data is the full
// event JSON. Requires an authenticated client unless the entity is Public.
func (c *Client) WatchInvoices(ctx context.Context, fn func(event string, data []byte) error) error {
	return c.watchSSE(ctx, "/invoices/_events", fn)
}

type Payments struct {
	ID         string `json:"id"`
	InvoiceId  string `json:"invoiceId,omitempty"`
	CustomerId string `json:"customerId,omitempty"`
	Amount     string `json:"amount,omitempty"`
	Method     string `json:"method,omitempty"`
	Status     string `json:"status,omitempty"`
	UserId     string `json:"userId,omitempty"`
}

type PaymentsInput struct {
	InvoiceId  string `json:"invoiceId,omitempty"`
	CustomerId string `json:"customerId,omitempty"`
	Amount     string `json:"amount,omitempty"`
	Method     string `json:"method,omitempty"`
	Status     string `json:"status,omitempty"`
	UserId     string `json:"userId,omitempty"`
}

type PaymentsPatch struct {
	InvoiceId  *string `json:"invoiceId,omitempty"`
	CustomerId *string `json:"customerId,omitempty"`
	Amount     *string `json:"amount,omitempty"`
	Method     *string `json:"method,omitempty"`
	Status     *string `json:"status,omitempty"`
	UserId     *string `json:"userId,omitempty"`
}

type PaymentsListResponse struct {
	Data       []Payments `json:"data"`
	Total      int        `json:"total"`
	Page       int        `json:"page"`
	PerPage    int        `json:"perPage"`
	TotalPages int        `json:"totalPages"`
}

// ListPayments fetches a page of payments. Pass nil for params to use server defaults.
func (c *Client) ListPayments(ctx context.Context, params url.Values) (PaymentsListResponse, error) {
	var out PaymentsListResponse
	path := "/payments"
	if params != nil {
		path += "?" + params.Encode()
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return PaymentsListResponse{}, err
	}
	return out, nil
}

// GetPayments fetches a single record by id. Returns *APIError with 404 when missing.
func (c *Client) GetPayments(ctx context.Context, id string) (Payments, error) {
	var out Payments
	if err := c.doSingleJSON(ctx, http.MethodGet, "/payments/"+url.PathEscape(id), nil, &out); err != nil {
		return Payments{}, err
	}
	return out, nil
}

// CreatePayments posts a new record and returns the server-canonical row.
func (c *Client) CreatePayments(ctx context.Context, body PaymentsInput) (Payments, error) {
	var out Payments
	if err := c.doSingleJSON(ctx, http.MethodPost, "/payments", body, &out); err != nil {
		return Payments{}, err
	}
	return out, nil
}

// UpdatePayments updates the record at id with the partial body.
func (c *Client) UpdatePayments(ctx context.Context, id string, body PaymentsInput) (Payments, error) {
	var out Payments
	if err := c.doSingleJSON(ctx, http.MethodPut, "/payments/"+url.PathEscape(id), body, &out); err != nil {
		return Payments{}, err
	}
	return out, nil
}

// PatchPayments updates exactly the fields whose pointers in body are non-nil.
// A nil field is omitted (the server leaves it untouched); a non-nil pointer
// sets the field — including to a zero value (false, 0, ""), which a value
// payload cannot express. Pass an empty PaymentsPatch to no-op.
func (c *Client) PatchPayments(ctx context.Context, id string, body PaymentsPatch) (Payments, error) {
	var out Payments
	if err := c.doSingleJSON(ctx, http.MethodPatch, "/payments/"+url.PathEscape(id), body, &out); err != nil {
		return Payments{}, err
	}
	return out, nil
}

// DeletePayments removes the record at id.
func (c *Client) DeletePayments(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/payments/"+url.PathEscape(id), nil, nil)
}

type PaymentsBatchPatch struct {
	ID         string  `json:"id"`
	InvoiceId  *string `json:"invoiceId,omitempty"`
	CustomerId *string `json:"customerId,omitempty"`
	Amount     *string `json:"amount,omitempty"`
	Method     *string `json:"method,omitempty"`
	Status     *string `json:"status,omitempty"`
	UserId     *string `json:"userId,omitempty"`
}

// BatchCreatePayments creates up to 100 records atomically (one transaction).
// Inspect Committed and the per-item Results — a 400 rollback is returned as
// a BatchResponse, not an error.
func (c *Client) BatchCreatePayments(ctx context.Context, items []PaymentsInput) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPost, "/payments/_batch", map[string]any{"items": items})
}

// BatchUpdatePayments patches up to 100 records atomically. Each item names its
// target via ID; nil pointer fields are left untouched.
func (c *Client) BatchUpdatePayments(ctx context.Context, items []PaymentsBatchPatch) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPatch, "/payments/_batch", map[string]any{"items": items})
}

// BatchDeletePayments deletes the given ids atomically.
func (c *Client) BatchDeletePayments(ctx context.Context, ids []string) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodDelete, "/payments/_batch", map[string]any{"ids": ids})
}

// WatchPayments subscribes to the entity's live event feed (entity.created /
// entity.updated / entity.deleted) and blocks, invoking fn per event, until
// ctx cancels, the stream ends, or fn returns an error. data is the full
// event JSON. Requires an authenticated client unless the entity is Public.
func (c *Client) WatchPayments(ctx context.Context, fn func(event string, data []byte) error) error {
	return c.watchSSE(ctx, "/payments/_events", fn)
}
