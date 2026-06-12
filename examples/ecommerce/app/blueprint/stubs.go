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

// BlueprintSeedData returns the initial seed data for the application.
// Call this from main.go to populate empty tables on first boot.
func BlueprintSeedData() map[string][]map[string]any {
	return map[string][]map[string]any{
		"categories": {
			{"name": "Electronics", "slug": "electronics", "description": "Gadgets, devices, and tech accessories", "active": true, "sort_order": 1},
			{"description": "Apparel and fashion", "name": "Clothing", "active": true, "sort_order": 2, "slug": "clothing"},
			{"sort_order": 3, "name": "Home & Garden", "slug": "home-garden", "description": "Everything for your home", "active": true},
		},
		"products": {
			{"price": 79.99, "name": "Wireless Headphones", "stock": 150, "status": "active", "featured": true, "slug": "wireless-headphones", "sku": "WH-001", "description": "Premium noise-cancelling wireless headphones with 30-hour battery life."},
			{"slug": "usb-c-hub", "sku": "UCH-002", "description": "7-in-1 USB-C hub with HDMI, USB 3.0, SD card reader, and PD charging.", "price": 49.99, "stock": 200, "name": "USB-C Hub", "status": "active"},
			{"description": "Hot-swappable mechanical keyboard with RGB backlighting.", "price": 129.99, "stock": 75, "status": "active", "featured": true, "name": "Mechanical Keyboard", "slug": "mechanical-keyboard", "sku": "MK-003"},
			{"status": "draft", "slug": "webcam-hd", "sku": "WC-004", "description": "1080p HD webcam with auto-focus and built-in microphone.", "name": "Webcam HD", "price": 39.99, "compare_at_price": 59.99, "stock": 300},
		},
		"reviews": {
			{"verified": true, "author_name": "Sarah M.", "rating": 5, "title": "Best headphones I ever owned", "body": "The noise cancellation is incredible. Battery lasts forever."},
			{"rating": 4, "title": "Great value", "body": "Solid build quality for the price. Wish the case was included.", "verified": true, "author_name": "Mike T."},
		},
	}
}
