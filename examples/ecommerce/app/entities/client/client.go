package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Client is a typed HTTP client targeting the gofastr server's CRUD routes.
// Pass any *http.Client (httptest, retryable wrapper, etc.) — Client never
// closes it.
type Client struct {
	BaseURL string
	HTTP    *http.Client
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
	if err := c.doJSON(ctx, http.MethodGet, "/categories/"+url.PathEscape(id), nil, &out); err != nil {
		return Categories{}, err
	}
	return out, nil
}

// CreateCategories posts a new record and returns the server-canonical row.
func (c *Client) CreateCategories(ctx context.Context, body CategoriesInput) (Categories, error) {
	var out Categories
	if err := c.doJSON(ctx, http.MethodPost, "/categories", body, &out); err != nil {
		return Categories{}, err
	}
	return out, nil
}

// UpdateCategories updates the record at id with the partial body.
func (c *Client) UpdateCategories(ctx context.Context, id string, body CategoriesInput) (Categories, error) {
	var out Categories
	if err := c.doJSON(ctx, http.MethodPut, "/categories/"+url.PathEscape(id), body, &out); err != nil {
		return Categories{}, err
	}
	return out, nil
}

// DeleteCategories removes the record at id.
func (c *Client) DeleteCategories(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/categories/"+url.PathEscape(id), nil, nil)
}

type Products struct {
	ID             string         `json:"id"`
	Name           string         `json:"name,omitempty"`
	Slug           string         `json:"slug,omitempty"`
	Sku            string         `json:"sku,omitempty"`
	Description    string         `json:"description,omitempty"`
	Price          string         `json:"price,omitempty"`
	CompareAtPrice string         `json:"compareAtPrice,omitempty"`
	Cost           string         `json:"cost,omitempty"`
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
	Cost           string         `json:"cost,omitempty"`
	Stock          int            `json:"stock,omitempty"`
	CategoryId     string         `json:"categoryId,omitempty"`
	Status         string         `json:"status,omitempty"`
	Featured       bool           `json:"featured,omitempty"`
	Weight         float64        `json:"weight,omitempty"`
	Image          string         `json:"image,omitempty"`
	Tags           map[string]any `json:"tags,omitempty"`
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
	if err := c.doJSON(ctx, http.MethodGet, "/products/"+url.PathEscape(id), nil, &out); err != nil {
		return Products{}, err
	}
	return out, nil
}

// CreateProducts posts a new record and returns the server-canonical row.
func (c *Client) CreateProducts(ctx context.Context, body ProductsInput) (Products, error) {
	var out Products
	if err := c.doJSON(ctx, http.MethodPost, "/products", body, &out); err != nil {
		return Products{}, err
	}
	return out, nil
}

// UpdateProducts updates the record at id with the partial body.
func (c *Client) UpdateProducts(ctx context.Context, id string, body ProductsInput) (Products, error) {
	var out Products
	if err := c.doJSON(ctx, http.MethodPut, "/products/"+url.PathEscape(id), body, &out); err != nil {
		return Products{}, err
	}
	return out, nil
}

// DeleteProducts removes the record at id.
func (c *Client) DeleteProducts(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/products/"+url.PathEscape(id), nil, nil)
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
	if err := c.doJSON(ctx, http.MethodGet, "/orders/"+url.PathEscape(id), nil, &out); err != nil {
		return Orders{}, err
	}
	return out, nil
}

// CreateOrders posts a new record and returns the server-canonical row.
func (c *Client) CreateOrders(ctx context.Context, body OrdersInput) (Orders, error) {
	var out Orders
	if err := c.doJSON(ctx, http.MethodPost, "/orders", body, &out); err != nil {
		return Orders{}, err
	}
	return out, nil
}

// UpdateOrders updates the record at id with the partial body.
func (c *Client) UpdateOrders(ctx context.Context, id string, body OrdersInput) (Orders, error) {
	var out Orders
	if err := c.doJSON(ctx, http.MethodPut, "/orders/"+url.PathEscape(id), body, &out); err != nil {
		return Orders{}, err
	}
	return out, nil
}

// DeleteOrders removes the record at id.
func (c *Client) DeleteOrders(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/orders/"+url.PathEscape(id), nil, nil)
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
	if err := c.doJSON(ctx, http.MethodGet, "/order_items/"+url.PathEscape(id), nil, &out); err != nil {
		return OrderItems{}, err
	}
	return out, nil
}

// CreateOrderItems posts a new record and returns the server-canonical row.
func (c *Client) CreateOrderItems(ctx context.Context, body OrderItemsInput) (OrderItems, error) {
	var out OrderItems
	if err := c.doJSON(ctx, http.MethodPost, "/order_items", body, &out); err != nil {
		return OrderItems{}, err
	}
	return out, nil
}

// UpdateOrderItems updates the record at id with the partial body.
func (c *Client) UpdateOrderItems(ctx context.Context, id string, body OrderItemsInput) (OrderItems, error) {
	var out OrderItems
	if err := c.doJSON(ctx, http.MethodPut, "/order_items/"+url.PathEscape(id), body, &out); err != nil {
		return OrderItems{}, err
	}
	return out, nil
}

// DeleteOrderItems removes the record at id.
func (c *Client) DeleteOrderItems(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/order_items/"+url.PathEscape(id), nil, nil)
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
	if err := c.doJSON(ctx, http.MethodGet, "/reviews/"+url.PathEscape(id), nil, &out); err != nil {
		return Reviews{}, err
	}
	return out, nil
}

// CreateReviews posts a new record and returns the server-canonical row.
func (c *Client) CreateReviews(ctx context.Context, body ReviewsInput) (Reviews, error) {
	var out Reviews
	if err := c.doJSON(ctx, http.MethodPost, "/reviews", body, &out); err != nil {
		return Reviews{}, err
	}
	return out, nil
}

// UpdateReviews updates the record at id with the partial body.
func (c *Client) UpdateReviews(ctx context.Context, id string, body ReviewsInput) (Reviews, error) {
	var out Reviews
	if err := c.doJSON(ctx, http.MethodPut, "/reviews/"+url.PathEscape(id), body, &out); err != nil {
		return Reviews{}, err
	}
	return out, nil
}

// DeleteReviews removes the record at id.
func (c *Client) DeleteReviews(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/reviews/"+url.PathEscape(id), nil, nil)
}
