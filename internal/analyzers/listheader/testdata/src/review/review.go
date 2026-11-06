package review

import (
	"net/http"
	"strings"
)

func nested(header http.Header) bool {
	value := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(header.Get("Connection"), " "))) // want `listheader: Header.Get`
	return strings.Contains(value, "upgrade")
}

func deeplyNested(header http.Header) bool {
	var value = (strings.ToLower(strings.TrimSpace(strings.TrimSpace(strings.TrimSpace(strings.TrimSpace(strings.TrimSpace(strings.TrimSpace(strings.TrimSpace(header.Get("Upgrade")))))))))) // want `listheader: Header.Get`
	return value == "websocket"
}

func constant(string) string { return "upgrade" }

func ignoredInput(header http.Header) bool {
	value := constant(header.Get("Connection"))
	return strings.Contains(value, "upgrade")
}

func nonValueArgument(header http.Header) bool {
	value := strings.TrimPrefix("constant", header.Get("Upgrade"))
	return value == "websocket"
}

func transformedConstant(header http.Header) bool {
	value := strings.ToLower(constant(header.Get("Connection")))
	return value == "upgrade"
}
