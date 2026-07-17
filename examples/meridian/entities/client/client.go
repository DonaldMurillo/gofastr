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

func (c *Client) doSingleJSON(ctx context.Context, method, path string, body, out any) error {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := c.doJSON(ctx, method, path, body, &envelope); err != nil {
		return err
	}
	return json.Unmarshal(envelope.Data, out)
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

func (c *Client) PatchPlans(ctx context.Context, id string, body PlansInput) (Plans, error) {
	var out Plans
	err := c.doSingleJSON(ctx, http.MethodPatch, "/plans/"+url.PathEscape(id), body, &out)
	return out, err
}

// DeletePlans removes the record at id.
func (c *Client) DeletePlans(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/plans/"+url.PathEscape(id), nil, nil)
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

func (c *Client) PatchCustomers(ctx context.Context, id string, body CustomersInput) (Customers, error) {
	var out Customers
	err := c.doSingleJSON(ctx, http.MethodPatch, "/customers/"+url.PathEscape(id), body, &out)
	return out, err
}

// DeleteCustomers removes the record at id.
func (c *Client) DeleteCustomers(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/customers/"+url.PathEscape(id), nil, nil)
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

func (c *Client) PatchSubscriptions(ctx context.Context, id string, body SubscriptionsInput) (Subscriptions, error) {
	var out Subscriptions
	err := c.doSingleJSON(ctx, http.MethodPatch, "/subscriptions/"+url.PathEscape(id), body, &out)
	return out, err
}

// DeleteSubscriptions removes the record at id.
func (c *Client) DeleteSubscriptions(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/subscriptions/"+url.PathEscape(id), nil, nil)
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

func (c *Client) PatchInvoices(ctx context.Context, id string, body InvoicesInput) (Invoices, error) {
	var out Invoices
	err := c.doSingleJSON(ctx, http.MethodPatch, "/invoices/"+url.PathEscape(id), body, &out)
	return out, err
}

// DeleteInvoices removes the record at id.
func (c *Client) DeleteInvoices(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/invoices/"+url.PathEscape(id), nil, nil)
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

func (c *Client) PatchPayments(ctx context.Context, id string, body PaymentsInput) (Payments, error) {
	var out Payments
	err := c.doSingleJSON(ctx, http.MethodPatch, "/payments/"+url.PathEscape(id), body, &out)
	return out, err
}

// DeletePayments removes the record at id.
func (c *Client) DeletePayments(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/payments/"+url.PathEscape(id), nil, nil)
}
