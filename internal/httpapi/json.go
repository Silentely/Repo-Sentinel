package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

const maxJSONBodyBytes int64 = 1 << 20

type jsonDecodeError struct {
	status int
}

func (e jsonDecodeError) Error() string {
	return "invalid JSON request"
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return jsonDecodeError{status: http.StatusUnsupportedMediaType}
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return jsonDecodeError{status: http.StatusRequestEntityTooLarge}
		}
		return jsonDecodeError{status: http.StatusBadRequest}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return jsonDecodeError{status: http.StatusBadRequest}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (s *server) decodeRequestJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	err := decodeJSON(w, r, destination)
	if err == nil {
		return true
	}
	status := http.StatusBadRequest
	var decodeError jsonDecodeError
	if errors.As(err, &decodeError) {
		status = decodeError.status
	}
	s.writeAPIError(w, r, status, errorCodeValidationFailed, nil)
	return false
}
