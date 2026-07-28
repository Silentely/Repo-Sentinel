package notify

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateWebhookURLRejectsNonHTTPSAndPrivate(t *testing.T) {
	cases := []struct {
		url     string
		allow   bool
		wantErr bool
	}{
		{"http://example.com/hook", false, true},
		{"https://example.com/hook", false, false},
		{"https://127.0.0.1/hook", false, true},
		{"https://10.0.0.1/hook", false, true},
		{"https://169.254.169.254/latest", false, true},
		{"https://localhost/hook", true, true}, // localhost 始终拦截
		{"https://10.0.0.1/hook", true, false},
		{"ftp://example.com/hook", false, true},
		{"", false, true},
	}
	for _, tc := range cases {
		err := validateWebhookURL(tc.url, tc.allow)
		if tc.wantErr && err == nil {
			t.Fatalf("%s allow=%v 期望错误", tc.url, tc.allow)
		}
		if !tc.wantErr && err != nil {
			t.Fatalf("%s allow=%v 不期望错误: %v", tc.url, tc.allow, err)
		}
	}
}

func TestHTTPClientDoesNotFollowRedirect(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path == "/from" {
			http.Redirect(w, r, "/to", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	w := &Worker{}
	// 触发默认 Client 初始化
	_ = w
	client := &http.Client{
		Timeout: 5e9,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/from", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if hits != 1 {
		t.Fatalf("应只请求一次，实际 %d", hits)
	}
}
