package router

import "net/http"

// HandleFunc registers a handler function for the given method and pattern.
// This is a convenience wrapper around Handle that accepts an http.HandlerFunc.
func (r *Router) HandleFunc(method, pattern string, fn http.HandlerFunc) {
	r.Handle(method, pattern, fn)
}

// GetFunc registers a handler function for GET requests on the given pattern.
func (r *Router) GetFunc(pattern string, fn http.HandlerFunc) {
	r.Get(pattern, fn)
}

// PostFunc registers a handler function for POST requests on the given pattern.
func (r *Router) PostFunc(pattern string, fn http.HandlerFunc) {
	r.Post(pattern, fn)
}

// PutFunc registers a handler function for PUT requests on the given pattern.
func (r *Router) PutFunc(pattern string, fn http.HandlerFunc) {
	r.Put(pattern, fn)
}

// DeleteFunc registers a handler function for DELETE requests on the given pattern.
func (r *Router) DeleteFunc(pattern string, fn http.HandlerFunc) {
	r.Delete(pattern, fn)
}

// PatchFunc registers a handler function for PATCH requests on the given pattern.
func (r *Router) PatchFunc(pattern string, fn http.HandlerFunc) {
	r.Patch(pattern, fn)
}
