package server

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/theocourtois/hetzner-ecoplus/internal/api"
)

type Engine interface {
	Acquire(ctx context.Context, req api.ServerRequest) (api.Server, bool, error)
	Get(id int64, lease string) (api.Server, error)
	Release(ctx context.Context, id int64, lease string) error
	Patch(ctx context.Context, id int64, lease string, patch api.ServerPatch) (api.Server, error)
	Ready() bool
}

type Options struct {
	Engine Engine
	Token  string
	Logger *slog.Logger
}

type Server struct {
	opts Options
}

func New(opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	return &Server{opts: opts}
}

func (s *Server) APIHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+api.PathServers, s.create)
	mux.HandleFunc("GET "+api.PathServers+"/{id}", s.get)
	mux.HandleFunc("PATCH "+api.PathServers+"/{id}", s.patch)
	mux.HandleFunc("DELETE "+api.PathServers+"/{id}", s.delete)
	return s.logging(s.authenticated(mux))
}

func (s *Server) AdminHandler(metrics http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !s.opts.Engine.Ready() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	if metrics != nil {
		mux.Handle("GET /metrics", metrics)
	}
	return mux
}
