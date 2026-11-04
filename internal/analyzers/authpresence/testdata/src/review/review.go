package review

import (
	"net/http"
	"strings"
)

func unusedLiteral(request *http.Request) bool {
	auth := request.Header.Get("Authorization")
	_ = func() { strings.Cut(auth, " ") }
	return auth != "" // want `authpresence: an Authorization`
}

func unusedBoundLiteral(request *http.Request) bool {
	auth := request.Header.Get("Authorization")
	parse := func() { strings.Cut(auth, " ") }
	_ = parse
	return auth != "" // want `authpresence: an Authorization`
}

func goroutineAlias(request *http.Request) {
	auth := request.Header.Get("Authorization")
	go func() {
		header := auth
		if header != "" { // want `authpresence: an Authorization`
			println("present")
		}
	}()
}

func deferredAlias(request *http.Request) {
	auth := request.Header.Get("Authorization")
	defer func() {
		var header = auth
		if len(header) > 0 { // want `authpresence: an Authorization`
			println("present")
		}
	}()
}

func invokedParser(request *http.Request) bool { // want invokedParser:`parsesAuth`
	auth := request.Header.Get("Authorization")
	parse := func() {
		header := auth
		strings.Cut(header, " ")
	}
	alias := parse
	alias()
	return auth != ""
}

func discardedNested(request *http.Request) bool {
	auth := request.Header.Get("Authorization")
	func() {
		_ = func() { strings.Cut(auth, " ") }
	}()
	return auth != "" // want `authpresence: an Authorization`
}

func immediateParser(request *http.Request) bool { // want immediateParser:`parsesAuth`
	auth := request.Header.Get("Authorization")
	func() { strings.Cut(auth, " ") }()
	return auth != ""
}
