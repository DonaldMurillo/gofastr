// Package app is outside the framework's roots: an app writes what it
// likes, so every shape here is quiet.
package app

import (
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

func refuse(w http.ResponseWriter, msg string) {
	http.Error(w, msg, http.StatusGone)
}

func fail() error { return handler.Errorf(http.StatusConflict, "taken") }
