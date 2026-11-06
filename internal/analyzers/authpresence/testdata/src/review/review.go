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

func invokedParser(request *http.Request) bool {
	auth := request.Header.Get("Authorization")
	parse := func() {
		header := auth
		strings.Cut(header, " ")
	}
	alias := parse
	alias()
	return auth != "" // want `authpresence: an Authorization`
}

func discardedNested(request *http.Request) bool {
	auth := request.Header.Get("Authorization")
	func() {
		_ = func() { strings.Cut(auth, " ") }
	}()
	return auth != "" // want `authpresence: an Authorization`
}

func immediateParser(request *http.Request) bool {
	auth := request.Header.Get("Authorization")
	func() { strings.Cut(auth, " ") }()
	return auth != "" // want `authpresence: an Authorization`
}

func usedParsedScheme(request *http.Request) bool { // want usedParsedScheme:`parsesAuth`
	auth := request.Header.Get("Authorization")
	parse := func() bool {
		scheme, _, ok := strings.Cut(auth, " ")
		return ok && strings.EqualFold(scheme, "bearer")
	}
	return auth != "" && parse()
}

func deadComposite(request *http.Request) bool {
	auth := request.Header.Get("Authorization")
	holder := struct{ parse func() bool }{
		parse: func() bool { return strings.HasPrefix(auth, "Bearer ") },
	}
	_ = holder
	return auth != "" // want `authpresence: an Authorization`
}

func deadSliceAlias(request *http.Request) bool {
	auth := request.Header.Get("Authorization")
	callbacks := []func() bool{func() bool { return strings.HasPrefix(auth, "Bearer ") }}
	alias := callbacks
	_ = alias
	return auth != "" // want `authpresence: an Authorization`
}

func invokedComposite(request *http.Request) bool { // want invokedComposite:`parsesAuth`
	auth := request.Header.Get("Authorization")
	holder := &struct{ parse func() bool }{
		parse: func() bool { return strings.HasPrefix(auth, "Bearer ") },
	}
	return auth != "" && holder.parse()
}

func returnedComposite(request *http.Request) any { // want returnedComposite:`parsesAuth`
	auth := request.Header.Get("Authorization")
	holder := struct{ parse func() bool }{
		parse: func() bool { return strings.HasPrefix(auth, "Bearer ") },
	}
	return holder
}

func clearedSliceCallback(request *http.Request) bool {
	auth := request.Header.Get("Authorization")
	callbacks := []func() bool{func() bool { return strings.HasPrefix(auth, "Bearer ") }}
	callbacks[0] = nil
	return auth != "" // want `authpresence: an Authorization`
}

func clearedFieldCallback(request *http.Request) bool {
	auth := request.Header.Get("Authorization")
	holder := &struct{ parse func() bool }{
		parse: func() bool { return strings.HasPrefix(auth, "Bearer ") },
	}
	holder.parse = nil
	return auth != "" // want `authpresence: an Authorization`
}

func rangedCallbacks(request *http.Request) bool { // want rangedCallbacks:`parsesAuth`
	auth := request.Header.Get("Authorization")
	callbacks := []func() bool{func() bool { return strings.HasPrefix(auth, "Bearer ") }}
	for _, check := range callbacks {
		if !check() {
			return false
		}
	}
	return auth != ""
}
