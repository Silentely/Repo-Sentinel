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
	msg    string
}

func (e jsonDecodeError) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return "invalid JSON request"
}

// isJSONContentType 校验 Content-Type 是否为 application/json。
// 针对常见标准格式（"application/json" 与 "application/json; charset=utf-8"）提供
// 零分配快速路径，避免高频请求重复调用 mime.ParseMediaType 解析并分配参数字典。
func isJSONContentType(ct string) bool {
	ct = strings.TrimSpace(ct)
	if strings.EqualFold(ct, "application/json") {
		return true
	}
	if len(ct) >= 17 && strings.EqualFold(ct[:17], "application/json;") {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(ct)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	if !isJSONContentType(r.Header.Get("Content-Type")) {
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
		return jsonDecodeError{status: http.StatusBadRequest, msg: err.Error()}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return jsonDecodeError{status: http.StatusBadRequest, msg: "请求体只允许包含单个 JSON 对象。"}
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
	if status == http.StatusRequestEntityTooLarge {
		// 明确说明上限：413 只靠通用校验文案会让客户端误以为是格式问题。
		s.writeAPIError(w, r, status, errorCodeValidationFailed, map[string]any{
			"message": "请求体超过 1 MiB 上限。",
		})
		return false
	}
	if status == http.StatusUnsupportedMediaType {
		s.writeAPIError(w, r, status, errorCodeValidationFailed, map[string]any{
			"message": "请求头 Content-Type 必须为 application/json。",
		})
		return false
	}
	if decodeError.msg != "" {
		s.writeAPIError(w, r, status, errorCodeValidationFailed, map[string]any{
			"message": decodeError.msg,
		})
		return false
	}
	s.writeAPIError(w, r, status, errorCodeValidationFailed, nil)
	return false
}
