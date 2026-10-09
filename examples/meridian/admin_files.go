package main

import (
	"net/http"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework"
)

// Uploaded files: a customer's logo is stored on disk under uploads/
// (writes confined to it through os.Root) and served at /files/ to
// signed-in accounts only. The keys are random, and the record that
// names one is owner-scoped like every other write.

// filesURL is where the screens fetch a stored file.
const filesURL = "/files/"

// fileStore is the app's upload storage.
var fileStore = upload.NewLocalStorage("uploads")

// fileStorageOption gives every entity's CRUD writes the store, so an
// Image or File field takes an upload.
func fileStorageOption() framework.AppOption { return framework.WithFileStorage(fileStore) }

// mountFiles serves stored files to signed-in accounts. ServeHandler
// sniffs the type and sends script-capable content as a download.
func mountFiles(fwApp *framework.App) {
	serve := auth.RequireSession()(http.HandlerFunc(upload.ServeHandler(fileStore)))
	fwApp.Router().Get(filesURL+"{key...}", serve)
}
