package main

import (
	"net/http"
	"fmt"
	"github.com/gorilla/mux"
)

func handleFoo(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("Foo: %s\n", r.Method)
	w.WriteHeader(http.StatusOK)
}

func handleCall(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	fmt.Printf("Call %s's [%s]\n", id, r.URL.Path)
	w.WriteHeader(http.StatusOK)
}

func main() {
	r := mux.NewRouter()
	r.HandleFunc("/foo", handleFoo)
	r.PathPrefix("/call/{id}").HandlerFunc(handleCall)

	srv := &http.Server{
		Handler:      r,
		Addr:         "127.0.0.1:8999",
	}

	srv.ListenAndServe()
}
