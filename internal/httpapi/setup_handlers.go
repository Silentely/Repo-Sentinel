package httpapi

import (
	"errors"
	"net"
	"net/http"

	"github.com/Silentely/Repo-Sentinel/internal/auth"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

type setupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	required, err := s.setupRequired(r)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]bool{"required": required})
}

func (s *server) handleSetup(w http.ResponseWriter, r *http.Request) {
	required, err := s.setupRequired(r)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	if !required {
		s.writeAPIError(w, r, http.StatusNotFound, errorCodeNotFound, nil)
		return
	}
	if !s.dependencies.Config.Setup.AllowRemote && !isLoopbackIP(remoteIPFromContext(r.Context())) {
		s.writeAPIError(w, r, http.StatusForbidden, errorCodeForbidden, nil)
		return
	}
	var request setupRequest
	if !s.decodeRequestJSON(w, r, &request) {
		return
	}
	admin, err := s.dependencies.AdminService.BootstrapAdmin(r.Context(), request.Username, request.Password)
	if errors.Is(err, auth.ErrConflict) {
		s.writeAPIError(w, r, http.StatusNotFound, errorCodeNotFound, nil)
		return
	}
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	created, err := s.dependencies.SessionService.Create(
		r.Context(),
		admin.ID,
		remoteIPFromContext(r.Context()),
		r.UserAgent(),
	)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	s.setAuthCookies(w, created)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, newAuthenticationResponse(admin, created.Session))
}

func (s *server) setupRequired(r *http.Request) (bool, error) {
	_, err := s.dependencies.AdminStore.GetOnly(r.Context())
	if errors.Is(err, store.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

func isLoopbackIP(rawIP string) bool {
	parsed := net.ParseIP(rawIP)
	return parsed != nil && parsed.IsLoopback()
}
