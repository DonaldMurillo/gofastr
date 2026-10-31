package main

import (
	"bytes"
	"fmt"
	"image/png"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/assets/brand"
)

func TestSiteServesApprovedBrandIcon(t *testing.T) {
	baseURL := siteE2EServer(t)
	source, err := png.Decode(bytes.NewReader(brand.IconPNG()))
	if err != nil {
		t.Fatal(err)
	}
	withinPremultipliedEncodingRounding := func(actual, expected uint32) bool {
		return max(actual, expected)-min(actual, expected) <= 2*257
	}
	for _, size := range []int{32, 180, 192, 512} {
		path := fmt.Sprintf("/__gofastr/icons/icon-%d.png", size)
		if size == 32 {
			path = "/favicon.ico"
		}
		t.Run(path, func(t *testing.T) {
			response, err := http.Get(baseURL + path)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("icon status: %s", response.Status)
			}
			icon, err := png.Decode(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if icon.Bounds().Dx() != size || icon.Bounds().Dy() != size {
				t.Fatalf("icon bounds: %v; want %dx%d", icon.Bounds(), size, size)
			}
			if size == 512 {
				for row := 0; row < size; row++ {
					for column := 0; column < size; column++ {
						red, green, blue, alpha := icon.At(column, row).RGBA()
						wantRed, wantGreen, wantBlue, wantAlpha := source.At(column, row).RGBA()
						if !withinPremultipliedEncodingRounding(red, wantRed) || !withinPremultipliedEncodingRounding(green, wantGreen) || !withinPremultipliedEncodingRounding(blue, wantBlue) || !withinPremultipliedEncodingRounding(alpha, wantAlpha) {
							t.Fatalf("served icon differs from approved asset at %d,%d: got %v, want %v", column, row, icon.At(column, row), source.At(column, row))
						}
					}
				}
			}
		})
	}
}
