package main

import (
	"github.com/DonaldMurillo/gofastr/framework"
	"net/http"
)

func ConfirmOrder(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "TODO: implement confirmOrder", http.StatusNotImplemented)
}

func ShipOrder(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "TODO: implement shipOrder", http.StatusNotImplemented)
}

func RequestLoggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

type AnalyticsPlugin struct{}

func (AnalyticsPlugin) Name() string                  { return "analytics" }
func (AnalyticsPlugin) Init(app *framework.App) error { return nil }

// seedEntity is one entity's ordered seed rows.
type seedEntity struct {
	Entity string
	Rows   []map[string]any
}

// seedData returns the initial seed data in blueprint-declared
// order (so entities that reference others are inserted after them).
func seedData() []seedEntity {
	return []seedEntity{
		{Entity: "categories", Rows: []map[string]any{
			{"active": true, "description": "Gadgets, devices, and tech accessories", "name": "Electronics", "slug": "electronics", "sort_order": 1},
			{"active": true, "description": "Apparel and fashion", "name": "Clothing", "slug": "clothing", "sort_order": 2},
			{"active": true, "description": "Everything for your home", "name": "Home & Garden", "slug": "home-garden", "sort_order": 3},
		}},
		{Entity: "products", Rows: []map[string]any{
			{"description": "Premium noise-cancelling wireless headphones with 30-hour battery life.", "featured": true, "name": "Wireless Headphones", "price": "79.99", "sku": "WH-001", "slug": "wireless-headphones", "status": "active", "stock": 150},
			{"description": "7-in-1 USB-C hub with HDMI, USB 3.0, SD card reader, and PD charging.", "name": "USB-C Hub", "price": "49.99", "sku": "UCH-002", "slug": "usb-c-hub", "status": "active", "stock": 200},
			{"description": "Hot-swappable mechanical keyboard with RGB backlighting.", "featured": true, "name": "Mechanical Keyboard", "price": "129.99", "sku": "MK-003", "slug": "mechanical-keyboard", "status": "active", "stock": 75},
			{"compare_at_price": "59.99", "description": "1080p HD webcam with auto-focus and built-in microphone.", "name": "Webcam HD", "price": "39.99", "sku": "WC-004", "slug": "webcam-hd", "status": "draft", "stock": 300},
		}},
		{Entity: "reviews", Rows: []map[string]any{
			{"author_name": "Sarah M.", "body": "The noise cancellation is incredible. Battery lasts forever.", "rating": 5, "title": "Best headphones I ever owned", "verified": true},
			{"author_name": "Mike T.", "body": "Solid build quality for the price. Wish the case was included.", "rating": 4, "title": "Great value", "verified": true},
		}},
	}
}
