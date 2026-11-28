// Package xpkgiface declares a component interface outside the framework.
package xpkgiface

import "github.com/DonaldMurillo/gofastr/core/render"

type Renderer interface{ Render() render.HTML }
