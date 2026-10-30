package httpapi

import (
	"net/http"

	"tracker/internal/auth"
)

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserID(r.Context())
	if !ok {
		failUnauthorized(w)
		return
	}

	stats, err := s.stats.Get(r.Context(), userID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleInsights(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserID(r.Context())
	if !ok {
		failUnauthorized(w)
		return
	}

	list, err := s.insights(r.Context(), userID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}
