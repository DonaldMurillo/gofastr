package a

import "net/http"

func PointerLatchWrite(next http.Handler, started *bool) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer func() {
			if recover() != nil {
				*started = true
				http.Error(writer, "internal", http.StatusInternalServerError) // want `recoverwrite: a deferred recover writes a response`
			}
		}()
		next.ServeHTTP(writer, request)
	})
}

func PointerLatchGuard(next http.Handler, started *bool) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer func() {
			if recover() != nil {
				if *started {
					panic(http.ErrAbortHandler)
				}
				http.Error(writer, "internal", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(writer, request)
	})
}
