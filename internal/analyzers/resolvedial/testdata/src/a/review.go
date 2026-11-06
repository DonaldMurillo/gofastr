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

func fetchReplaceDifferentClient(client *http.Client, clients []http.Client, host string) {
	_ = checkHost(host)
	if client.Transport == nil {
		clients[0] = http.Client{Transport: guarded()}
	}
}

func fetchReplaceDifferentIndex(clients []http.Client, host string) {
	_ = checkHost(host)
	if clients[0].Transport == nil {
		clients[1] = http.Client{Transport: guarded()}
	}
}

func fetchInstallDifferentField(config *struct{ First, Second http.Client }, host string) {
	_ = checkHost(host)
	if config.First.Transport == nil {
		config.Second.Transport = guarded()
	}
}

func fetchInstallDifferentHelper(client, other *http.Client, host string) {
	_ = checkHost(host)
	if client.Transport == nil {
		installGuarded(other)
	}
}
