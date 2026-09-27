package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

func newProxy(target *url.URL) *httputil.ReverseProxy {
	return httputil.NewSingleHostReverseProxy(target)
}

func handleProxy(w http.ResponseWriter, r *http.Request) {
	target, _ := url.Parse("http://backend.example.invalid")
	newProxy(target).ServeHTTP(w, r)
}
