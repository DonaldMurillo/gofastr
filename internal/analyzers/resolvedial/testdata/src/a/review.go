package a

import "net/http"

func fetchReplaceDereferencedClient(client *http.Client, host string) {
	_ = checkHost(host)
	if client.Transport == nil { // want `resolvedial: the dial-time guard is installed only when Transport is nil`
		*client = http.Client{Transport: guarded()}
	}
}

func fetchReplaceIndexedClient(clients []http.Client, host string) {
	_ = checkHost(host)
	if clients[0].Transport == nil { // want `resolvedial: the dial-time guard is installed only when Transport is nil`
		clients[0] = http.Client{Transport: guarded()}
	}
}

func fetchReplaceFieldClient(config *struct{ Client http.Client }, host string) {
	_ = checkHost(host)
	if config.Client.Transport == nil { // want `resolvedial: the dial-time guard is installed only when Transport is nil`
		config.Client = http.Client{Transport: guarded()}
	}
}
