package httpapi

import (
	"net/http"

	"tracker/internal/auth"
)

type vapidKeyResponse struct {
	PublicKey string `json:"public_key"`
}

type pushSubscriptionRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

type pushUnsubscribeRequest struct {
	Endpoint string `json:"endpoint"`
}

func (s *Server) handleVapidKey(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, vapidKeyResponse{PublicKey: s.push.PublicKey()})
}

func (s *Server) handlePutPushSubscription(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserID(r.Context())
	if !ok {
		failUnauthorized(w)
		return
	}

	var req pushSubscriptionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		failBadRequest(w, "body", "некорректный JSON")
		return
	}

	err := s.push.Save(r.Context(), userID, req.Endpoint, req.Keys.P256dh, req.Keys.Auth, r.UserAgent())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleDeletePushSubscription(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserID(r.Context())
	if !ok {
		failUnauthorized(w)
		return
	}

	var req pushUnsubscribeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		failBadRequest(w, "body", "некорректный JSON")
		return
	}

	if err := s.push.Delete(r.Context(), userID, req.Endpoint); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
