package locationtab

import "net/http"

func RefuseNewlineRedirect(writer http.ResponseWriter, request *http.Request) {
	refuseNewlineRedirect(writer, request)
}
