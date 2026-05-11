package main

import (
	"fmt"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/html"
	"github.com/gofastr/gofastr/core/render"
)

// ProductListScreen shows a searchable product grid.
type ProductListScreen struct{}

func (s *ProductListScreen) ScreenTitle() string        { return "Products" }
func (s *ProductListScreen) ScreenDescription() string  { return "Browse our products" }
func (s *ProductListScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ProductListScreen) Render() render.HTML {
	search := &SearchFilterComponent{}

	return html.Div(html.DivConfig{},
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Products")),
		search.Render(),
		productCards(),
	)
}

// ProductDetailScreen shows a single product's details.
type ProductDetailScreen struct {
	Slug string
}

func (s *ProductDetailScreen) ScreenTitle() string        { return "Product Detail" }
func (s *ProductDetailScreen) ScreenDescription() string  { return "View product details" }
func (s *ProductDetailScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ProductDetailScreen) Render() render.HTML {
	p, ok := findProductBySlug(s.Slug)
	if !ok {
		return html.Div(html.DivConfig{},
			html.Heading(html.HeadingConfig{Level: 1}, render.Text("Product Not Found")),
			html.Paragraph(html.TextConfig{}, render.Text("The product you're looking for doesn't exist.")),
			html.Link(html.LinkConfig{Href: "/products", Text: "← Back to Products"}),
		)
	}
	return (&ProductDetailComponent{Product: p}).Render()
}

func (s *ProductDetailScreen) SetParams(params map[string]string) {
	s.Slug = params["slug"]
}

// ProductDetailComponent renders a full product detail view.
type ProductDetailComponent struct {
	Product Product
}

func (p *ProductDetailComponent) Render() render.HTML {
	return html.Div(html.DivConfig{Class: "product-detail"},
		html.Link(html.LinkConfig{Href: "/products", Text: "← Back to Products", Class: "back-link"}),
		html.Div(html.DivConfig{Class: "product-detail-content"},
			html.Image(html.ImageConfig{Src: p.Product.ImageSrc, Alt: p.Product.ImageAlt, Class: "product-detail-image"}),
			html.Div(html.DivConfig{Class: "product-detail-info"},
				html.Heading(html.HeadingConfig{Level: 1}, render.Text(p.Product.Name)),
				html.Paragraph(html.TextConfig{Class: "product-detail-price"}, render.Text(fmt.Sprintf("$%.2f", p.Product.Price))),
				html.Paragraph(html.TextConfig{}, render.Text(p.Product.Description)),
				html.Button(html.ButtonConfig{
					Label: "Add to cart",
					Class: "add-to-cart cta-button",
					Attrs: html.Attrs{
						"data-action":      "add-to-cart",
						"data-param-name":  p.Product.Name,
						"data-param-price": fmt.Sprintf("%.2f", p.Product.Price),
					},
				}),
			),
		),
	)
}
