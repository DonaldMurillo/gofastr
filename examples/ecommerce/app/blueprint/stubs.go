package blueprint

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

// BlueprintSeedEntity is one entity's ordered seed rows.
type BlueprintSeedEntity struct {
	Entity string
	Rows   []map[string]any
}

// BlueprintSeedData returns the initial seed data in blueprint-declared
// order (so entities that reference others are inserted after them).
func BlueprintSeedData() []BlueprintSeedEntity {
	return []BlueprintSeedEntity{
		{Entity: "categories", Rows: []map[string]any{
			{"name": "Electronics", "sort_order": 1, "slug": "electronics", "description": "Gadgets, devices, and tech accessories", "active": true},
			{"description": "Apparel and fashion", "active": true, "sort_order": 2, "name": "Clothing", "slug": "clothing"},
			{"slug": "home-garden", "description": "Everything for your home", "name": "Home & Garden", "active": true, "sort_order": 3},
		}},
		{Entity: "products", Rows: []map[string]any{
			{"status": "active", "featured": true, "name": "Wireless Headphones", "slug": "wireless-headphones", "sku": "WH-001", "description": "Premium noise-cancelling wireless headphones with 30-hour battery life.", "price": "79.99", "stock": 150},
			{"description": "7-in-1 USB-C hub with HDMI, USB 3.0, SD card reader, and PD charging.", "price": "49.99", "stock": 200, "status": "active", "name": "USB-C Hub", "slug": "usb-c-hub", "sku": "UCH-002"},
			{"name": "Mechanical Keyboard", "price": "129.99", "stock": 75, "status": "active", "featured": true, "slug": "mechanical-keyboard", "sku": "MK-003", "description": "Hot-swappable mechanical keyboard with RGB backlighting."},
			{"price": "39.99", "compare_at_price": "59.99", "stock": 300, "status": "draft", "name": "Webcam HD", "slug": "webcam-hd", "sku": "WC-004", "description": "1080p HD webcam with auto-focus and built-in microphone."},
		}},
		{Entity: "reviews", Rows: []map[string]any{
			{"author_name": "Sarah M.", "rating": 5, "title": "Best headphones I ever owned", "body": "The noise cancellation is incredible. Battery lasts forever.", "verified": true},
			{"author_name": "Mike T.", "rating": 4, "title": "Great value", "body": "Solid build quality for the price. Wish the case was included.", "verified": true},
		}},
	}
}
