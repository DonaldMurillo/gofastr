package entities

type Categories struct {
	ID          string      `json:"id"`
	Name        string      `json:"name,omitempty"`
	Slug        string      `json:"slug,omitempty"`
	Description string      `json:"description,omitempty"`
	Image       string      `json:"image,omitempty"`
	SortOrder   int         `json:"sortOrder,omitempty"`
	Active      bool        `json:"active,omitempty"`
	Products    []*Products `json:"products,omitempty"`
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
	Category       *Categories    `json:"category,omitempty"`
	Reviews        []*Reviews     `json:"reviews,omitempty"`
	OrderItems     []*OrderItems  `json:"orderItems,omitempty"`
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
	Items           []*OrderItems  `json:"items,omitempty"`
}

type OrderItems struct {
	ID          string    `json:"id"`
	UserId      string    `json:"userId,omitempty"`
	OrderId     string    `json:"orderId,omitempty"`
	ProductId   string    `json:"productId,omitempty"`
	ProductName string    `json:"productName,omitempty"`
	Quantity    int       `json:"quantity,omitempty"`
	UnitPrice   string    `json:"unitPrice,omitempty"`
	TotalPrice  string    `json:"totalPrice,omitempty"`
	Order       *Orders   `json:"order,omitempty"`
	Product     *Products `json:"product,omitempty"`
}

type Reviews struct {
	ID         string    `json:"id"`
	ProductId  string    `json:"productId,omitempty"`
	AuthorName string    `json:"authorName,omitempty"`
	Rating     int       `json:"rating,omitempty"`
	Title      string    `json:"title,omitempty"`
	Body       string    `json:"body,omitempty"`
	Verified   bool      `json:"verified,omitempty"`
	Product    *Products `json:"product,omitempty"`
}
