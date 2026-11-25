package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/theocourtois/ecoplus/internal/api"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) authenticated(next http.Handler) http.Handler {
	want := []byte(s.opts.Token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get(api.HeaderAuthorization)
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || len(want) == 0 || subtle.ConstantTimeCompare([]byte(token), want) != 1 {
			s.writeError(w, api.NewError(api.CodeUnauthorized, "missing or invalid bearer token"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.opts.Logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"user_agent", r.Header.Get(api.HeaderUserAgent), "duration", time.Since(start))
	})
}
