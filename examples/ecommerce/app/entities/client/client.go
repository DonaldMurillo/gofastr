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

type Categories struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Slug        string `json:"slug,omitempty"`
	Description string `json:"description,omitempty"`
	Image       string `json:"image,omitempty"`
	SortOrder   int    `json:"sortOrder,omitempty"`
	Active      bool   `json:"active,omitempty"`
}

type CategoriesInput struct {
	Name        string `json:"name,omitempty"`
	Slug        string `json:"slug,omitempty"`
	Description string `json:"description,omitempty"`
	Image       string `json:"image,omitempty"`
	SortOrder   int    `json:"sortOrder,omitempty"`
	Active      bool   `json:"active,omitempty"`
}

type CategoriesPatch struct {
	Name        *string `json:"name,omitempty"`
	Slug        *string `json:"slug,omitempty"`
	Description *string `json:"description,omitempty"`
	Image       *string `json:"image,omitempty"`
	SortOrder   *int    `json:"sortOrder,omitempty"`
	Active      *bool   `json:"active,omitempty"`
}

type CategoriesListResponse struct {
	Data       []Categories `json:"data"`
	Total      int          `json:"total"`
	Page       int          `json:"page"`
	PerPage    int          `json:"perPage"`
	TotalPages int          `json:"totalPages"`
}

// ListCategories fetches a page of categories. Pass nil for params to use server defaults.
func (c *Client) ListCategories(ctx context.Context, params url.Values) (CategoriesListResponse, error) {
	var out CategoriesListResponse
	path := "/categories"
	if params != nil {
		path += "?" + params.Encode()
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return CategoriesListResponse{}, err
	}
	return out, nil
}

// GetCategories fetches a single record by id. Returns *APIError with 404 when missing.
func (c *Client) GetCategories(ctx context.Context, id string) (Categories, error) {
	var out Categories
	if err := c.doSingleJSON(ctx, http.MethodGet, "/categories/"+url.PathEscape(id), nil, &out); err != nil {
		return Categories{}, err
	}
	return out, nil
}

// CreateCategories posts a new record and returns the server-canonical row.
func (c *Client) CreateCategories(ctx context.Context, body CategoriesInput) (Categories, error) {
	var out Categories
	if err := c.doSingleJSON(ctx, http.MethodPost, "/categories", body, &out); err != nil {
		return Categories{}, err
	}
	return out, nil
}

// UpdateCategories updates the record at id with the partial body.
func (c *Client) UpdateCategories(ctx context.Context, id string, body CategoriesInput) (Categories, error) {
	var out Categories
	if err := c.doSingleJSON(ctx, http.MethodPut, "/categories/"+url.PathEscape(id), body, &out); err != nil {
		return Categories{}, err
	}
	return out, nil
}

// PatchCategories updates exactly the fields whose pointers in body are non-nil.
// A nil field is omitted (the server leaves it untouched); a non-nil pointer
// sets the field — including to a zero value (false, 0, ""), which a value
// payload cannot express. Pass an empty CategoriesPatch to no-op.
func (c *Client) PatchCategories(ctx context.Context, id string, body CategoriesPatch) (Categories, error) {
	var out Categories
	if err := c.doSingleJSON(ctx, http.MethodPatch, "/categories/"+url.PathEscape(id), body, &out); err != nil {
		return Categories{}, err
	}
	return out, nil
}

// DeleteCategories removes the record at id.
func (c *Client) DeleteCategories(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/categories/"+url.PathEscape(id), nil, nil)
}

type CategoriesBatchPatch struct {
	ID          string  `json:"id"`
	Name        *string `json:"name,omitempty"`
	Slug        *string `json:"slug,omitempty"`
	Description *string `json:"description,omitempty"`
	Image       *string `json:"image,omitempty"`
	SortOrder   *int    `json:"sortOrder,omitempty"`
	Active      *bool   `json:"active,omitempty"`
}

// BatchCreateCategories creates up to 100 records atomically (one transaction).
// Inspect Committed and the per-item Results — a 400 rollback is returned as
// a BatchResponse, not an error.
func (c *Client) BatchCreateCategories(ctx context.Context, items []CategoriesInput) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPost, "/categories/_batch", map[string]any{"items": items})
}

// BatchUpdateCategories patches up to 100 records atomically. Each item names its
// target via ID; nil pointer fields are left untouched.
func (c *Client) BatchUpdateCategories(ctx context.Context, items []CategoriesBatchPatch) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPatch, "/categories/_batch", map[string]any{"items": items})
}

// BatchDeleteCategories deletes the given ids atomically.
func (c *Client) BatchDeleteCategories(ctx context.Context, ids []string) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodDelete, "/categories/_batch", map[string]any{"ids": ids})
}

// WatchCategories subscribes to the entity's live event feed (entity.created /
// entity.updated / entity.deleted) and blocks, invoking fn per event, until
// ctx cancels, the stream ends, or fn returns an error. data is the full
// event JSON. Requires an authenticated client unless the entity is Public.
func (c *Client) WatchCategories(ctx context.Context, fn func(event string, data []byte) error) error {
	return c.watchSSE(ctx, "/categories/_events", fn)
}

type Products struct {
	ID             string         `json:"id"`
	Name           string         `json:"name,omitempty"`
	Slug           string         `json:"slug,omitempty"`
	Sku            string         `json:"sku,omitempty"`
	Description    string         `json:"description,omitempty"`
	Price          string         `json:"price,omitempty"`
	CompareAtPrice string         `json:"compareAtPrice,omitempty"`
	Stock          int            `json:"stock,omitempty"`
	CategoryId     string         `json:"categoryId,omitempty"`
	Status         string         `json:"status,omitempty"`
	Featured       bool           `json:"featured,omitempty"`
	Weight         float64        `json:"weight,omitempty"`
	Image          string         `json:"image,omitempty"`
	Tags           map[string]any `json:"tags,omitempty"`
}

type ProductsInput struct {
	Name           string         `json:"name,omitempty"`
	Slug           string         `json:"slug,omitempty"`
	Sku            string         `json:"sku,omitempty"`
	Description    string         `json:"description,omitempty"`
	Price          string         `json:"price,omitempty"`
	CompareAtPrice string         `json:"compareAtPrice,omitempty"`
	Stock          int            `json:"stock,omitempty"`
	CategoryId     string         `json:"categoryId,omitempty"`
	Status         string         `json:"status,omitempty"`
	Featured       bool           `json:"featured,omitempty"`
	Weight         float64        `json:"weight,omitempty"`
	Image          string         `json:"image,omitempty"`
	Tags           map[string]any `json:"tags,omitempty"`
}

type ProductsPatch struct {
	Name           *string         `json:"name,omitempty"`
	Slug           *string         `json:"slug,omitempty"`
	Sku            *string         `json:"sku,omitempty"`
	Description    *string         `json:"description,omitempty"`
	Price          *string         `json:"price,omitempty"`
	CompareAtPrice *string         `json:"compareAtPrice,omitempty"`
	Stock          *int            `json:"stock,omitempty"`
	CategoryId     *string         `json:"categoryId,omitempty"`
	Status         *string         `json:"status,omitempty"`
	Featured       *bool           `json:"featured,omitempty"`
	Weight         *float64        `json:"weight,omitempty"`
	Image          *string         `json:"image,omitempty"`
	Tags           *map[string]any `json:"tags,omitempty"`
}

type ProductsListResponse struct {
	Data       []Products `json:"data"`
	Total      int        `json:"total"`
	Page       int        `json:"page"`
	PerPage    int        `json:"perPage"`
	TotalPages int        `json:"totalPages"`
}

// ListProducts fetches a page of products. Pass nil for params to use server defaults.
func (c *Client) ListProducts(ctx context.Context, params url.Values) (ProductsListResponse, error) {
	var out ProductsListResponse
	path := "/products"
	if params != nil {
		path += "?" + params.Encode()
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return ProductsListResponse{}, err
	}
	return out, nil
}

// GetProducts fetches a single record by id. Returns *APIError with 404 when missing.
func (c *Client) GetProducts(ctx context.Context, id string) (Products, error) {
	var out Products
	if err := c.doSingleJSON(ctx, http.MethodGet, "/products/"+url.PathEscape(id), nil, &out); err != nil {
		return Products{}, err
	}
	return out, nil
}

// CreateProducts posts a new record and returns the server-canonical row.
func (c *Client) CreateProducts(ctx context.Context, body ProductsInput) (Products, error) {
	var out Products
	if err := c.doSingleJSON(ctx, http.MethodPost, "/products", body, &out); err != nil {
		return Products{}, err
	}
	return out, nil
}

// UpdateProducts updates the record at id with the partial body.
func (c *Client) UpdateProducts(ctx context.Context, id string, body ProductsInput) (Products, error) {
	var out Products
	if err := c.doSingleJSON(ctx, http.MethodPut, "/products/"+url.PathEscape(id), body, &out); err != nil {
		return Products{}, err
	}
	return out, nil
}

// PatchProducts updates exactly the fields whose pointers in body are non-nil.
// A nil field is omitted (the server leaves it untouched); a non-nil pointer
// sets the field — including to a zero value (false, 0, ""), which a value
// payload cannot express. Pass an empty ProductsPatch to no-op.
func (c *Client) PatchProducts(ctx context.Context, id string, body ProductsPatch) (Products, error) {
	var out Products
	if err := c.doSingleJSON(ctx, http.MethodPatch, "/products/"+url.PathEscape(id), body, &out); err != nil {
		return Products{}, err
	}
	return out, nil
}

// DeleteProducts removes the record at id.
func (c *Client) DeleteProducts(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/products/"+url.PathEscape(id), nil, nil)
}

type ProductsBatchPatch struct {
	ID             string          `json:"id"`
	Name           *string         `json:"name,omitempty"`
	Slug           *string         `json:"slug,omitempty"`
	Sku            *string         `json:"sku,omitempty"`
	Description    *string         `json:"description,omitempty"`
	Price          *string         `json:"price,omitempty"`
	CompareAtPrice *string         `json:"compareAtPrice,omitempty"`
	Stock          *int            `json:"stock,omitempty"`
	CategoryId     *string         `json:"categoryId,omitempty"`
	Status         *string         `json:"status,omitempty"`
	Featured       *bool           `json:"featured,omitempty"`
	Weight         *float64        `json:"weight,omitempty"`
	Image          *string         `json:"image,omitempty"`
	Tags           *map[string]any `json:"tags,omitempty"`
}

// BatchCreateProducts creates up to 100 records atomically (one transaction).
// Inspect Committed and the per-item Results — a 400 rollback is returned as
// a BatchResponse, not an error.
func (c *Client) BatchCreateProducts(ctx context.Context, items []ProductsInput) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPost, "/products/_batch", map[string]any{"items": items})
}

// BatchUpdateProducts patches up to 100 records atomically. Each item names its
// target via ID; nil pointer fields are left untouched.
func (c *Client) BatchUpdateProducts(ctx context.Context, items []ProductsBatchPatch) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPatch, "/products/_batch", map[string]any{"items": items})
}

// BatchDeleteProducts deletes the given ids atomically.
func (c *Client) BatchDeleteProducts(ctx context.Context, ids []string) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodDelete, "/products/_batch", map[string]any{"ids": ids})
}

// WatchProducts subscribes to the entity's live event feed (entity.created /
// entity.updated / entity.deleted) and blocks, invoking fn per event, until
// ctx cancels, the stream ends, or fn returns an error. data is the full
// event JSON. Requires an authenticated client unless the entity is Public.
func (c *Client) WatchProducts(ctx context.Context, fn func(event string, data []byte) error) error {
	return c.watchSSE(ctx, "/products/_events", fn)
}

type Orders struct {
	ID              string         `json:"id"`
	UserId          string         `json:"userId,omitempty"`
	OrderNumber     string         `json:"orderNumber,omitempty"`
	Status          string         `json:"status,omitempty"`
	CustomerName    string         `json:"customerName,omitempty"`
	CustomerEmail   string         `json:"customerEmail,omitempty"`
	CustomerPhone   string         `json:"customerPhone,omitempty"`
	ShippingAddress map[string]any `json:"shippingAddress,omitempty"`
	BillingAddress  map[string]any `json:"billingAddress,omitempty"`
	Subtotal        string         `json:"subtotal,omitempty"`
	Tax             string         `json:"tax,omitempty"`
	ShippingCost    string         `json:"shippingCost,omitempty"`
	Total           string         `json:"total,omitempty"`
	Notes           string         `json:"notes,omitempty"`
	ShippedAt       string         `json:"shippedAt,omitempty"`
	DeliveredAt     string         `json:"deliveredAt,omitempty"`
}

type OrdersInput struct {
	UserId          string         `json:"userId,omitempty"`
	OrderNumber     string         `json:"orderNumber,omitempty"`
	Status          string         `json:"status,omitempty"`
	CustomerName    string         `json:"customerName,omitempty"`
	CustomerEmail   string         `json:"customerEmail,omitempty"`
	CustomerPhone   string         `json:"customerPhone,omitempty"`
	ShippingAddress map[string]any `json:"shippingAddress,omitempty"`
	BillingAddress  map[string]any `json:"billingAddress,omitempty"`
	Subtotal        string         `json:"subtotal,omitempty"`
	Tax             string         `json:"tax,omitempty"`
	ShippingCost    string         `json:"shippingCost,omitempty"`
	Total           string         `json:"total,omitempty"`
	Notes           string         `json:"notes,omitempty"`
	ShippedAt       string         `json:"shippedAt,omitempty"`
	DeliveredAt     string         `json:"deliveredAt,omitempty"`
}

type OrdersPatch struct {
	UserId          *string         `json:"userId,omitempty"`
	OrderNumber     *string         `json:"orderNumber,omitempty"`
	Status          *string         `json:"status,omitempty"`
	CustomerName    *string         `json:"customerName,omitempty"`
	CustomerEmail   *string         `json:"customerEmail,omitempty"`
	CustomerPhone   *string         `json:"customerPhone,omitempty"`
	ShippingAddress *map[string]any `json:"shippingAddress,omitempty"`
	BillingAddress  *map[string]any `json:"billingAddress,omitempty"`
	Subtotal        *string         `json:"subtotal,omitempty"`
	Tax             *string         `json:"tax,omitempty"`
	ShippingCost    *string         `json:"shippingCost,omitempty"`
	Total           *string         `json:"total,omitempty"`
	Notes           *string         `json:"notes,omitempty"`
	ShippedAt       *string         `json:"shippedAt,omitempty"`
	DeliveredAt     *string         `json:"deliveredAt,omitempty"`
}

type OrdersListResponse struct {
	Data       []Orders `json:"data"`
	Total      int      `json:"total"`
	Page       int      `json:"page"`
	PerPage    int      `json:"perPage"`
	TotalPages int      `json:"totalPages"`
}

// ListOrders fetches a page of orders. Pass nil for params to use server defaults.
func (c *Client) ListOrders(ctx context.Context, params url.Values) (OrdersListResponse, error) {
	var out OrdersListResponse
	path := "/orders"
	if params != nil {
		path += "?" + params.Encode()
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return OrdersListResponse{}, err
	}
	return out, nil
}

// GetOrders fetches a single record by id. Returns *APIError with 404 when missing.
func (c *Client) GetOrders(ctx context.Context, id string) (Orders, error) {
	var out Orders
	if err := c.doSingleJSON(ctx, http.MethodGet, "/orders/"+url.PathEscape(id), nil, &out); err != nil {
		return Orders{}, err
	}
	return out, nil
}

// CreateOrders posts a new record and returns the server-canonical row.
func (c *Client) CreateOrders(ctx context.Context, body OrdersInput) (Orders, error) {
	var out Orders
	if err := c.doSingleJSON(ctx, http.MethodPost, "/orders", body, &out); err != nil {
		return Orders{}, err
	}
	return out, nil
}

// UpdateOrders updates the record at id with the partial body.
func (c *Client) UpdateOrders(ctx context.Context, id string, body OrdersInput) (Orders, error) {
	var out Orders
	if err := c.doSingleJSON(ctx, http.MethodPut, "/orders/"+url.PathEscape(id), body, &out); err != nil {
		return Orders{}, err
	}
	return out, nil
}

// PatchOrders updates exactly the fields whose pointers in body are non-nil.
// A nil field is omitted (the server leaves it untouched); a non-nil pointer
// sets the field — including to a zero value (false, 0, ""), which a value
// payload cannot express. Pass an empty OrdersPatch to no-op.
func (c *Client) PatchOrders(ctx context.Context, id string, body OrdersPatch) (Orders, error) {
	var out Orders
	if err := c.doSingleJSON(ctx, http.MethodPatch, "/orders/"+url.PathEscape(id), body, &out); err != nil {
		return Orders{}, err
	}
	return out, nil
}

// DeleteOrders removes the record at id.
func (c *Client) DeleteOrders(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/orders/"+url.PathEscape(id), nil, nil)
}

type OrdersBatchPatch struct {
	ID              string          `json:"id"`
	UserId          *string         `json:"userId,omitempty"`
	OrderNumber     *string         `json:"orderNumber,omitempty"`
	Status          *string         `json:"status,omitempty"`
	CustomerName    *string         `json:"customerName,omitempty"`
	CustomerEmail   *string         `json:"customerEmail,omitempty"`
	CustomerPhone   *string         `json:"customerPhone,omitempty"`
	ShippingAddress *map[string]any `json:"shippingAddress,omitempty"`
	BillingAddress  *map[string]any `json:"billingAddress,omitempty"`
	Subtotal        *string         `json:"subtotal,omitempty"`
	Tax             *string         `json:"tax,omitempty"`
	ShippingCost    *string         `json:"shippingCost,omitempty"`
	Total           *string         `json:"total,omitempty"`
	Notes           *string         `json:"notes,omitempty"`
	ShippedAt       *string         `json:"shippedAt,omitempty"`
	DeliveredAt     *string         `json:"deliveredAt,omitempty"`
}

// BatchCreateOrders creates up to 100 records atomically (one transaction).
// Inspect Committed and the per-item Results — a 400 rollback is returned as
// a BatchResponse, not an error.
func (c *Client) BatchCreateOrders(ctx context.Context, items []OrdersInput) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPost, "/orders/_batch", map[string]any{"items": items})
}

// BatchUpdateOrders patches up to 100 records atomically. Each item names its
// target via ID; nil pointer fields are left untouched.
func (c *Client) BatchUpdateOrders(ctx context.Context, items []OrdersBatchPatch) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPatch, "/orders/_batch", map[string]any{"items": items})
}

// BatchDeleteOrders deletes the given ids atomically.
func (c *Client) BatchDeleteOrders(ctx context.Context, ids []string) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodDelete, "/orders/_batch", map[string]any{"ids": ids})
}

// WatchOrders subscribes to the entity's live event feed (entity.created /
// entity.updated / entity.deleted) and blocks, invoking fn per event, until
// ctx cancels, the stream ends, or fn returns an error. data is the full
// event JSON. Requires an authenticated client unless the entity is Public.
func (c *Client) WatchOrders(ctx context.Context, fn func(event string, data []byte) error) error {
	return c.watchSSE(ctx, "/orders/_events", fn)
}

type OrderItems struct {
	ID          string `json:"id"`
	UserId      string `json:"userId,omitempty"`
	OrderId     string `json:"orderId,omitempty"`
	ProductId   string `json:"productId,omitempty"`
	ProductName string `json:"productName,omitempty"`
	Quantity    int    `json:"quantity,omitempty"`
	UnitPrice   string `json:"unitPrice,omitempty"`
	TotalPrice  string `json:"totalPrice,omitempty"`
}

type OrderItemsInput struct {
	UserId      string `json:"userId,omitempty"`
	OrderId     string `json:"orderId,omitempty"`
	ProductId   string `json:"productId,omitempty"`
	ProductName string `json:"productName,omitempty"`
	Quantity    int    `json:"quantity,omitempty"`
	UnitPrice   string `json:"unitPrice,omitempty"`
	TotalPrice  string `json:"totalPrice,omitempty"`
}

type OrderItemsPatch struct {
	UserId      *string `json:"userId,omitempty"`
	OrderId     *string `json:"orderId,omitempty"`
	ProductId   *string `json:"productId,omitempty"`
	ProductName *string `json:"productName,omitempty"`
	Quantity    *int    `json:"quantity,omitempty"`
	UnitPrice   *string `json:"unitPrice,omitempty"`
	TotalPrice  *string `json:"totalPrice,omitempty"`
}

type OrderItemsListResponse struct {
	Data       []OrderItems `json:"data"`
	Total      int          `json:"total"`
	Page       int          `json:"page"`
	PerPage    int          `json:"perPage"`
	TotalPages int          `json:"totalPages"`
}

// ListOrderItems fetches a page of order_items. Pass nil for params to use server defaults.
func (c *Client) ListOrderItems(ctx context.Context, params url.Values) (OrderItemsListResponse, error) {
	var out OrderItemsListResponse
	path := "/order_items"
	if params != nil {
		path += "?" + params.Encode()
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return OrderItemsListResponse{}, err
	}
	return out, nil
}

// GetOrderItems fetches a single record by id. Returns *APIError with 404 when missing.
func (c *Client) GetOrderItems(ctx context.Context, id string) (OrderItems, error) {
	var out OrderItems
	if err := c.doSingleJSON(ctx, http.MethodGet, "/order_items/"+url.PathEscape(id), nil, &out); err != nil {
		return OrderItems{}, err
	}
	return out, nil
}

// CreateOrderItems posts a new record and returns the server-canonical row.
func (c *Client) CreateOrderItems(ctx context.Context, body OrderItemsInput) (OrderItems, error) {
	var out OrderItems
	if err := c.doSingleJSON(ctx, http.MethodPost, "/order_items", body, &out); err != nil {
		return OrderItems{}, err
	}
	return out, nil
}

// UpdateOrderItems updates the record at id with the partial body.
func (c *Client) UpdateOrderItems(ctx context.Context, id string, body OrderItemsInput) (OrderItems, error) {
	var out OrderItems
	if err := c.doSingleJSON(ctx, http.MethodPut, "/order_items/"+url.PathEscape(id), body, &out); err != nil {
		return OrderItems{}, err
	}
	return out, nil
}

// PatchOrderItems updates exactly the fields whose pointers in body are non-nil.
// A nil field is omitted (the server leaves it untouched); a non-nil pointer
// sets the field — including to a zero value (false, 0, ""), which a value
// payload cannot express. Pass an empty OrderItemsPatch to no-op.
func (c *Client) PatchOrderItems(ctx context.Context, id string, body OrderItemsPatch) (OrderItems, error) {
	var out OrderItems
	if err := c.doSingleJSON(ctx, http.MethodPatch, "/order_items/"+url.PathEscape(id), body, &out); err != nil {
		return OrderItems{}, err
	}
	return out, nil
}

// DeleteOrderItems removes the record at id.
func (c *Client) DeleteOrderItems(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/order_items/"+url.PathEscape(id), nil, nil)
}

type OrderItemsBatchPatch struct {
	ID          string  `json:"id"`
	UserId      *string `json:"userId,omitempty"`
	OrderId     *string `json:"orderId,omitempty"`
	ProductId   *string `json:"productId,omitempty"`
	ProductName *string `json:"productName,omitempty"`
	Quantity    *int    `json:"quantity,omitempty"`
	UnitPrice   *string `json:"unitPrice,omitempty"`
	TotalPrice  *string `json:"totalPrice,omitempty"`
}

// BatchCreateOrderItems creates up to 100 records atomically (one transaction).
// Inspect Committed and the per-item Results — a 400 rollback is returned as
// a BatchResponse, not an error.
func (c *Client) BatchCreateOrderItems(ctx context.Context, items []OrderItemsInput) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPost, "/order_items/_batch", map[string]any{"items": items})
}

// BatchUpdateOrderItems patches up to 100 records atomically. Each item names its
// target via ID; nil pointer fields are left untouched.
func (c *Client) BatchUpdateOrderItems(ctx context.Context, items []OrderItemsBatchPatch) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPatch, "/order_items/_batch", map[string]any{"items": items})
}

// BatchDeleteOrderItems deletes the given ids atomically.
func (c *Client) BatchDeleteOrderItems(ctx context.Context, ids []string) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodDelete, "/order_items/_batch", map[string]any{"ids": ids})
}

// WatchOrderItems subscribes to the entity's live event feed (entity.created /
// entity.updated / entity.deleted) and blocks, invoking fn per event, until
// ctx cancels, the stream ends, or fn returns an error. data is the full
// event JSON. Requires an authenticated client unless the entity is Public.
func (c *Client) WatchOrderItems(ctx context.Context, fn func(event string, data []byte) error) error {
	return c.watchSSE(ctx, "/order_items/_events", fn)
}

type Reviews struct {
	ID         string `json:"id"`
	ProductId  string `json:"productId,omitempty"`
	AuthorName string `json:"authorName,omitempty"`
	Rating     int    `json:"rating,omitempty"`
	Title      string `json:"title,omitempty"`
	Body       string `json:"body,omitempty"`
	Verified   bool   `json:"verified,omitempty"`
}

type ReviewsInput struct {
	ProductId  string `json:"productId,omitempty"`
	AuthorName string `json:"authorName,omitempty"`
	Rating     int    `json:"rating,omitempty"`
	Title      string `json:"title,omitempty"`
	Body       string `json:"body,omitempty"`
	Verified   bool   `json:"verified,omitempty"`
}

type ReviewsPatch struct {
	ProductId  *string `json:"productId,omitempty"`
	AuthorName *string `json:"authorName,omitempty"`
	Rating     *int    `json:"rating,omitempty"`
	Title      *string `json:"title,omitempty"`
	Body       *string `json:"body,omitempty"`
	Verified   *bool   `json:"verified,omitempty"`
}

type ReviewsListResponse struct {
	Data       []Reviews `json:"data"`
	Total      int       `json:"total"`
	Page       int       `json:"page"`
	PerPage    int       `json:"perPage"`
	TotalPages int       `json:"totalPages"`
}

// ListReviews fetches a page of reviews. Pass nil for params to use server defaults.
func (c *Client) ListReviews(ctx context.Context, params url.Values) (ReviewsListResponse, error) {
	var out ReviewsListResponse
	path := "/reviews"
	if params != nil {
		path += "?" + params.Encode()
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return ReviewsListResponse{}, err
	}
	return out, nil
}

// GetReviews fetches a single record by id. Returns *APIError with 404 when missing.
func (c *Client) GetReviews(ctx context.Context, id string) (Reviews, error) {
	var out Reviews
	if err := c.doSingleJSON(ctx, http.MethodGet, "/reviews/"+url.PathEscape(id), nil, &out); err != nil {
		return Reviews{}, err
	}
	return out, nil
}

// CreateReviews posts a new record and returns the server-canonical row.
func (c *Client) CreateReviews(ctx context.Context, body ReviewsInput) (Reviews, error) {
	var out Reviews
	if err := c.doSingleJSON(ctx, http.MethodPost, "/reviews", body, &out); err != nil {
		return Reviews{}, err
	}
	return out, nil
}

// UpdateReviews updates the record at id with the partial body.
func (c *Client) UpdateReviews(ctx context.Context, id string, body ReviewsInput) (Reviews, error) {
	var out Reviews
	if err := c.doSingleJSON(ctx, http.MethodPut, "/reviews/"+url.PathEscape(id), body, &out); err != nil {
		return Reviews{}, err
	}
	return out, nil
}

// PatchReviews updates exactly the fields whose pointers in body are non-nil.
// A nil field is omitted (the server leaves it untouched); a non-nil pointer
// sets the field — including to a zero value (false, 0, ""), which a value
// payload cannot express. Pass an empty ReviewsPatch to no-op.
func (c *Client) PatchReviews(ctx context.Context, id string, body ReviewsPatch) (Reviews, error) {
	var out Reviews
	if err := c.doSingleJSON(ctx, http.MethodPatch, "/reviews/"+url.PathEscape(id), body, &out); err != nil {
		return Reviews{}, err
	}
	return out, nil
}

// DeleteReviews removes the record at id.
func (c *Client) DeleteReviews(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/reviews/"+url.PathEscape(id), nil, nil)
}

type ReviewsBatchPatch struct {
	ID         string  `json:"id"`
	ProductId  *string `json:"productId,omitempty"`
	AuthorName *string `json:"authorName,omitempty"`
	Rating     *int    `json:"rating,omitempty"`
	Title      *string `json:"title,omitempty"`
	Body       *string `json:"body,omitempty"`
	Verified   *bool   `json:"verified,omitempty"`
}

// BatchCreateReviews creates up to 100 records atomically (one transaction).
// Inspect Committed and the per-item Results — a 400 rollback is returned as
// a BatchResponse, not an error.
func (c *Client) BatchCreateReviews(ctx context.Context, items []ReviewsInput) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPost, "/reviews/_batch", map[string]any{"items": items})
}

// BatchUpdateReviews patches up to 100 records atomically. Each item names its
// target via ID; nil pointer fields are left untouched.
func (c *Client) BatchUpdateReviews(ctx context.Context, items []ReviewsBatchPatch) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPatch, "/reviews/_batch", map[string]any{"items": items})
}

// BatchDeleteReviews deletes the given ids atomically.
func (c *Client) BatchDeleteReviews(ctx context.Context, ids []string) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodDelete, "/reviews/_batch", map[string]any{"ids": ids})
}

// WatchReviews subscribes to the entity's live event feed (entity.created /
// entity.updated / entity.deleted) and blocks, invoking fn per event, until
// ctx cancels, the stream ends, or fn returns an error. data is the full
// event JSON. Requires an authenticated client unless the entity is Public.
func (c *Client) WatchReviews(ctx context.Context, fn func(event string, data []byte) error) error {
	return c.watchSSE(ctx, "/reviews/_events", fn)
}
