package main

import (
	"fmt"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core/render"
)

// ProductListScreen shows a searchable product grid.
type ProductListScreen struct{}

func (s *ProductListScreen) ScreenTitle() string        { return "Products" }
func (s *ProductListScreen) ScreenDescription() string  { return "Browse our products" }
func (s *ProductListScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ProductListScreen) Render() render.HTML {
	search := &SearchFilterComponent{}

	return elements.Div(nil,
		elements.Heading(1, nil, render.Text("Products")),
		search.Render(),
		productCards(),
	)
}

// ProductDetailScreen shows a single product's details.
// It reads the slug from route params set by the router.
type ProductDetailScreen struct {
	Slug string // set dynamically
}

func (s *ProductDetailScreen) ScreenTitle() string        { return "Product Detail" }
func (s *ProductDetailScreen) ScreenDescription() string  { return "View product details" }
func (s *ProductDetailScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ProductDetailScreen) Render() render.HTML {
	p, ok := findProductBySlug(s.Slug)
	if !ok {
		return elements.Div(nil,
			elements.Heading(1, nil, render.Text("Product Not Found")),
			elements.Paragraph(nil, render.Text("The product you're looking for doesn't exist.")),
			elements.Link("/products", "← Back to Products", nil),
		)
	}
	return (&ProductDetailComponent{Product: p}).Render()
}

// SetParams implements app.ParamSetter — receives route params from the router.
func (s *ProductDetailScreen) SetParams(params map[string]string) {
	s.Slug = params["slug"]
}

// ProductDetailComponent renders a full product detail view.
type ProductDetailComponent struct {
	Product Product
}

func (p *ProductDetailComponent) Render() render.HTML {
	return elements.Div(elements.Attrs{"class": "product-detail"},
		elements.Link("/products", "← Back to Products", elements.Attrs{"class": "back-link"}),
		elements.Div(elements.Attrs{"class": "product-detail-content"},
			elements.Image(p.Product.ImageSrc, p.Product.ImageAlt, elements.Attrs{"class": "product-detail-image"}),
			elements.Div(elements.Attrs{"class": "product-detail-info"},
				elements.Heading(1, nil, render.Text(p.Product.Name)),
				elements.Paragraph(elements.Attrs{"class": "product-detail-price"}, render.Text(fmt.Sprintf("$%.2f", p.Product.Price))),
				elements.Paragraph(nil, render.Text(p.Product.Description)),
				elements.Button("Add to cart", elements.Attrs{
					"class":            "add-to-cart cta-button",
					"data-action":      "add-to-cart",
					"data-param-name":  p.Product.Name,
					"data-param-price": fmt.Sprintf("%.2f", p.Product.Price),
				}),
			),
		),
	)
}
