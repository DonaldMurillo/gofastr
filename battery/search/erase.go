package search

import "github.com/DonaldMurillo/gofastr/core/datexport"

// registerOwnerEraser declares each table to App.EraseUserData, which
// deletes the rows whose owner is the erased user (Document.Owner). The
// SQLite backend passes its prefix companion too, since the companion
// holds the index tokens of the same text. Each table registers under
// its own name, so two indexes in one app do not replace each other.
//
// No exporter is registered: an index is derived from records the
// entity graph already exports, and a restore re-indexes them.
func registerOwnerEraser(tables ...string) {
	for _, table := range tables {
		datexport.RegisterEraser(datexport.DataEraser{
			Name: table, Source: "search", Table: table, Column: "owner",
			Mode: datexport.EraseDelete,
		})
	}
}
