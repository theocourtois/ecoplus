package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/theocourtois/ecoplus/internal/api"
)

const maxBody = 128 * 1024

func decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return api.NewError(api.CodeInvalidRequest, "invalid request body: %v", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return api.NewError(api.CodeInvalidRequest, "request body must contain a single JSON value")
	}
	return nil
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var req api.ServerRequest
	if err := decode(r, &req); err != nil {
		s.writeError(w, err)
		return
	}
	srv, replay, err := s.opts.Engine.Acquire(r.Context(), req)
	if err != nil {
		s.writeError(w, err)
		return
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	s.writeJSON(w, status, srv)
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	srv, err := s.opts.Engine.Get(id, r.Header.Get(api.HeaderLease))
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, srv)
}

func (s *Server) patch(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	var p api.ServerPatch
	if err := decode(r, &p); err != nil {
		s.writeError(w, err)
		return
	}
	srv, err := s.opts.Engine.Patch(r.Context(), id, r.Header.Get(api.HeaderLease), p)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, srv)
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	if err := s.opts.Engine.Release(r.Context(), id, r.Header.Get(api.HeaderLease)); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
