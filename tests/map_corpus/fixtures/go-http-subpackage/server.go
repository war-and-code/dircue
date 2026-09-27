package main

import (
	"net/http"
	_ "net/http/pprof"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	http.ListenAndServe(":8080", nil)
}
