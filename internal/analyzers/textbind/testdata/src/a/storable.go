package a

import (
	"net/http"

	"textsafe"
)

func sharedStorableCheck(writer http.ResponseWriter, request *http.Request) {
	value := request.FormValue("name")
	if !textsafe.Storable(value) {
		http.Error(writer, "invalid name", http.StatusBadRequest)
		return
	}
	db.ExecContext(request.Context(), "INSERT INTO names (name) VALUES ($1)", value)
}

func sharedStorableCheckTooLate(writer http.ResponseWriter, request *http.Request) {
	value := request.FormValue("name")
	db.ExecContext(request.Context(), "INSERT INTO names (name) VALUES ($1)", value) // want `request text reaches a SQL bind`
	if !textsafe.Storable(value) {
		http.Error(writer, "invalid name", http.StatusBadRequest)
		return
	}
}
