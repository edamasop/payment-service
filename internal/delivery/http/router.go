package http

import "net/http"

func NewRouter() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/health", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	return mux
}
