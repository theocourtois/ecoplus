package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/theocourtois/ecoplus/internal/api"
)

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.opts.Logger.Warn("write response", "error", err)
	}
}

func retryAfter(code api.Code) string {
	if code == api.CodeRateLimited {
		return "30"
	}
	return "5"
}

func (s *Server) writeError(w http.ResponseWriter, err error) {
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		s.opts.Logger.Error("internal error", "error", err)
		apiErr = api.NewError(api.CodeInternal, "internal error")
	}
	if apiErr.Retryable() {
		w.Header().Set(api.HeaderRetryAfter, retryAfter(apiErr.Code))
	}
	s.writeJSON(w, apiErr.Status(), apiErr)
}

func parseID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, api.NewError(api.CodeInvalidRequest, "invalid server id")
	}
	return id, nil
}
