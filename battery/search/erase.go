package search

import "github.com/DonaldMurillo/gofastr/core/datexport"

func registerOwnerEraser(tables ...string) {
	for _, table := range tables {
		datexport.RegisterEraser(datexport.DataEraser{
			Name: table, Source: "search", Table: table, Column: "owner",
			Mode: datexport.EraseDelete,
		})
	}
}
