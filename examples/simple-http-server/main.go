package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/go-flexible/flex"
)

func main() {
	router := http.NewServeMux()
	router.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		fmt.Fprint(rw, "hello, world\n")
	})

	srv := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	flex.MustStart(
		context.Background(),
		NewHTTPServer(srv),
	)
}

type Server struct{ *http.Server }

func NewHTTPServer(s *http.Server) *Server {
	return &Server{Server: s}
}

func (s *Server) Run(_ context.Context) error {
	log.Printf("serving on: http://localhost%s\n", s.Addr)
	return s.ListenAndServe()
}

func (s *Server) Halt(ctx context.Context) error {
	return s.Shutdown(ctx)
}
