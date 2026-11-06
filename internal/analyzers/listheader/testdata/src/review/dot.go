package review

import (
	"net/http"
	. "strings"
)

func dotTrim(header http.Header) bool {
	value := TrimSpace(header.Get("Connection")) // want `listheader: Header.Get`
	return value == "upgrade"
}

func dotNested(header http.Header) bool {
	value := ToLower(TrimSpace(header.Get("Upgrade"))) // want `listheader: Header.Get`
	return value == "websocket"
}
