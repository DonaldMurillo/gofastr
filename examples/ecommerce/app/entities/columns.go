package entities

import (
	"github.com/DonaldMurillo/gofastr/framework"
)

// ====== Categories column references ======

var (
	CategoriesID          = framework.NewUUIDColumn("id")
	CategoriesName        = framework.NewStringColumn("name")
	CategoriesSlug        = framework.NewStringColumn("slug")
	CategoriesDescription = framework.NewStringColumn("description")
	CategoriesImage       = framework.NewStringColumn("image")
	CategoriesSortOrder   = framework.NewIntColumn("sort_order")
	CategoriesActive      = framework.NewBoolColumn("active")
)

// Categories include names — pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	CategoriesInclProducts = "products"
)

// ====== OrderItems column references ======

var (
	OrderItemsID          = framework.NewUUIDColumn("id")
	OrderItemsUserId      = framework.NewStringColumn("user_id")
	OrderItemsOrderId     = framework.NewUUIDColumn("order_id")
	OrderItemsProductId   = framework.NewUUIDColumn("product_id")
	OrderItemsProductName = framework.NewStringColumn("product_name")
	OrderItemsQuantity    = framework.NewIntColumn("quantity")
	OrderItemsUnitPrice   = framework.NewFloatColumn("unit_price")
	OrderItemsTotalPrice  = framework.NewFloatColumn("total_price")
)

// OrderItems include names — pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	OrderItemsInclOrder   = "order"
	OrderItemsInclProduct = "product"
)

// ====== Orders column references ======

var (
	OrdersID              = framework.NewUUIDColumn("id")
	OrdersUserId          = framework.NewStringColumn("user_id")
	OrdersOrderNumber     = framework.NewStringColumn("order_number")
	OrdersStatus          = framework.NewStringColumn("status")
	OrdersCustomerName    = framework.NewStringColumn("customer_name")
	OrdersCustomerEmail   = framework.NewStringColumn("customer_email")
	OrdersCustomerPhone   = framework.NewStringColumn("customer_phone")
	OrdersShippingAddress = framework.NewStringColumn("shipping_address")
	OrdersBillingAddress  = framework.NewStringColumn("billing_address")
	OrdersSubtotal        = framework.NewFloatColumn("subtotal")
	OrdersTax             = framework.NewFloatColumn("tax")
	OrdersShippingCost    = framework.NewFloatColumn("shipping_cost")
	OrdersTotal           = framework.NewFloatColumn("total")
	OrdersNotes           = framework.NewStringColumn("notes")
	OrdersShippedAt       = framework.NewTimestampColumn("shipped_at")
	OrdersDeliveredAt     = framework.NewTimestampColumn("delivered_at")
)

// Orders include names — pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	OrdersInclItems = "items"
)

// ====== Products column references ======

var (
	ProductsID             = framework.NewUUIDColumn("id")
	ProductsName           = framework.NewStringColumn("name")
	ProductsSlug           = framework.NewStringColumn("slug")
	ProductsSku            = framework.NewStringColumn("sku")
	ProductsDescription    = framework.NewStringColumn("description")
	ProductsPrice          = framework.NewFloatColumn("price")
	ProductsCompareAtPrice = framework.NewFloatColumn("compare_at_price")
	ProductsCost           = framework.NewFloatColumn("cost")
	ProductsStock          = framework.NewIntColumn("stock")
	ProductsCategoryId     = framework.NewUUIDColumn("category_id")
	ProductsStatus         = framework.NewStringColumn("status")
	ProductsFeatured       = framework.NewBoolColumn("featured")
	ProductsWeight         = framework.NewFloatColumn("weight")
	ProductsImage          = framework.NewStringColumn("image")
	ProductsTags           = framework.NewStringColumn("tags")
)

// Products include names — pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	ProductsInclCategory   = "category"
	ProductsInclReviews    = "reviews"
	ProductsInclOrderItems = "order_items"
)

// ====== Reviews column references ======

var (
	ReviewsID         = framework.NewUUIDColumn("id")
	ReviewsProductId  = framework.NewUUIDColumn("product_id")
	ReviewsAuthorName = framework.NewStringColumn("author_name")
	ReviewsRating     = framework.NewIntColumn("rating")
	ReviewsTitle      = framework.NewStringColumn("title")
	ReviewsBody       = framework.NewStringColumn("body")
	ReviewsVerified   = framework.NewBoolColumn("verified")
)

// Reviews include names — pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	ReviewsInclProduct = "product"
)
