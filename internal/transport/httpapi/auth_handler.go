package httpapi

import (
	"net/http"

	"tracker/internal/auth"
	"tracker/internal/model"
)

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	User *model.User `json:"user"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := decodeJSON(w, r, &req); err != nil {
		failBadRequest(w, "body", "некорректный JSON")
		return
	}

	user, token, err := s.auth.Register(r.Context(), req.Email, req.Password, req.Name)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusCreated, userResponse{User: user})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		failBadRequest(w, "body", "некорректный JSON")
		return
	}

	user, token, err := s.auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, userResponse{User: user})
}

func (s *Server) handleLogout(w http.ResponseWriter, _ *http.Request) {
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserID(r.Context())
	if !ok {
		failUnauthorized(w)
		return
	}

	user, err := s.auth.User(r.Context(), userID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, userResponse{User: user})
}
