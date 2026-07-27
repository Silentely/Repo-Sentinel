package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/auth"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestBuild加密探针拒绝缺失或错误主密钥(t *testing.T) {
	databaseURL := "file:" + filepath.Join(t.TempDir(), "key-probe.db")
	firstKey := hex.EncodeToString(bytes.Repeat([]byte{0x11}, 32))
	wrongKey := hex.EncodeToString(bytes.Repeat([]byte{0x22}, 32))

	configured := testAppConfig(databaseURL)
	configured.Encryption.CurrentKey = config.NewSecret(firstKey)
	first, err := Build(t.Context(), configured)
	if err != nil {
		t.Fatalf("使用首把主密钥 Build 失败: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("关闭首个 App 失败: %v", err)
	}

	wrong := configured
	wrong.Encryption.CurrentKey = config.NewSecret(wrongKey)
	if built, err := Build(t.Context(), wrong); built != nil || !errors.Is(err, cryptox.ErrEncryptionKeyMismatch) {
		t.Fatalf("错误主密钥 Build=(%v, %v)，期望 encryption_key_mismatch 且不返回 App", built, err)
	}

	missing := configured
	missing.Encryption.CurrentKey = config.Secret{}
	if built, err := Build(t.Context(), missing); built != nil || !errors.Is(err, cryptox.ErrEncryptionKeyMismatch) {
		t.Fatalf("缺失主密钥 Build=(%v, %v)，期望 encryption_key_mismatch 且不返回 App", built, err)
	}

	restored, err := Build(t.Context(), configured)
	if err != nil {
		t.Fatalf("失败 Build 后数据库资源未正确释放或原密钥不可恢复: %v", err)
	}
	if err := restored.Close(); err != nil {
		t.Fatalf("关闭恢复 App 失败: %v", err)
	}
}

func TestBuild仅在首次启动消费环境管理员配置(t *testing.T) {
	databaseURL := "file:" + filepath.Join(t.TempDir(), "bootstrap-admin.db")
	firstConfig := testAppConfig(databaseURL)
	firstConfig.Admin.Username = "Repo Admin"
	firstConfig.Admin.Password = config.NewSecret("管理员初始密码一二三四五六")
	first, err := Build(t.Context(), firstConfig)
	if err != nil {
		t.Fatalf("首次管理员 Build 失败: %v", err)
	}
	if _, err := first.adminService.Authenticate(t.Context(), "repo admin", "管理员初始密码一二三四五六"); err != nil {
		t.Fatalf("首次环境管理员认证失败: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("关闭首次 App 失败: %v", err)
	}

	secondConfig := testAppConfig(databaseURL)
	secondConfig.Admin.Username = "Repo Admin"
	secondConfig.Admin.Password = config.NewSecret("不应覆盖已有密码一二三四五六")
	second, err := Build(t.Context(), secondConfig)
	if err != nil {
		t.Fatalf("已有管理员再次 Build 失败: %v", err)
	}
	defer func() {
		if err := second.Close(); err != nil {
			t.Errorf("关闭第二个 App 失败: %v", err)
		}
	}()
	if _, err := second.adminService.Authenticate(t.Context(), "Repo Admin", "管理员初始密码一二三四五六"); err != nil {
		t.Fatalf("已有管理员密码被意外覆盖: %v", err)
	}
	if _, err := second.adminService.Authenticate(t.Context(), "Repo Admin", "不应覆盖已有密码一二三四五六"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("第二次启动配置密码认证错误=%v，期望 invalid_credentials", err)
	}
}

func TestBuild拒绝不完整管理员配置(t *testing.T) {
	cfg := testAppConfig("file:" + filepath.Join(t.TempDir(), "invalid-admin.db"))
	cfg.Admin.Username = "Repo Admin"
	if built, err := Build(t.Context(), cfg); built != nil || err == nil || errorCodeOf(err) != "validation_failed" {
		t.Fatalf("不完整管理员配置 Build=(%v, %v)，期望 validation_failed", built, err)
	}
}

func TestBuild后续依赖失败会关闭已打开Store(t *testing.T) {
	tracking := &closeTrackingStore{}
	dependencies := defaultBuildDependencies()
	dependencies.openStore = func(context.Context, config.DatabaseConfig) (store.Store, error) {
		return tracking, nil
	}
	dependencies.validateEncryption = func(context.Context, store.Store, config.EncryptionConfig) (*cryptox.KeyRing, error) {
		return nil, errors.New("injected key validation failure")
	}
	if built, err := buildWithDependencies(t.Context(), testAppConfig("file:ignored.db"), dependencies); built != nil || err == nil {
		t.Fatalf("注入失败 Build=(%v, %v)，期望失败且无 App", built, err)
	}
	if !tracking.closed.Load() {
		t.Fatal("后续依赖失败未关闭已打开 Store")
	}
}

func TestBuild前端资源不可用返回稳定错误并关闭Store(t *testing.T) {
	opened, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver:       "sqlite",
		URL:          "file:" + filepath.Join(t.TempDir(), "frontend.db"),
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("打开前端资源测试 Store 失败: %v", err)
	}
	tracking := &closeTrackingStore{Store: opened}
	dependencies := defaultBuildDependencies()
	dependencies.openStore = func(context.Context, config.DatabaseConfig) (store.Store, error) {
		return tracking, nil
	}
	dependencies.validateEncryption = func(context.Context, store.Store, config.EncryptionConfig) (*cryptox.KeyRing, error) {
		return nil, nil
	}
	dependencies.openFrontend = func() (fs.FS, error) {
		return nil, errors.New("missing frontend index")
	}

	if built, err := buildWithDependencies(t.Context(), testAppConfig("file:ignored.db"), dependencies); built != nil || errorCodeOf(err) != "frontend_unavailable" {
		t.Fatalf("前端资源失败 Build=(%v, %v)，期望 frontend_unavailable 且无 App", built, err)
	}
	if !tracking.closed.Load() {
		t.Fatal("前端资源失败未关闭已打开 Store")
	}
}

func TestRun取消后在三十秒预算内停止HTTPWorker并关闭Store(t *testing.T) {
	server := newFakeHTTPRuntime()
	tracking := &closeTrackingStore{sessions: &cleanupSessionStore{}}
	readiness := &readinessState{}
	readiness.Set(true)
	sessions := auth.NewSessionService(tracking, nil, nil, time.Hour)
	built := &App{
		data:            tracking,
		httpServer:      server,
		sessionService:  sessions,
		readiness:       readiness,
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		cleanupInterval: time.Millisecond,
	}
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		result <- built.Run(ctx)
	}()
	select {
	case <-server.started:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP runtime 未启动")
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("取消后的 Run 错误=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("取消后 App 未及时完成优雅关闭")
	}
	if !tracking.closed.Load() {
		t.Fatal("Run 返回前未关闭 Store")
	}
	if readiness.IsReady() {
		t.Fatal("关闭开始后 readiness 必须变为 false")
	}
	if server.shutdownBudget <= 0 || server.shutdownBudget > 30*time.Second {
		t.Fatalf("HTTP shutdown 预算=%v，期望不超过 30s", server.shutdownBudget)
	}
}

func testAppConfig(databaseURL string) config.Config {
	return config.Config{
		HTTP: config.HTTPConfig{
			Addr:          "127.0.0.1:0",
			PublicBaseURL: "http://127.0.0.1",
		},
		Database: config.DatabaseConfig{
			Driver:       "sqlite",
			URL:          databaseURL,
			MaxOpenConns: 1,
			MaxIdleConns: 1,
		},
		Admin: config.AdminBootstrapConfig{SessionTTL: time.Hour},
		Logging: config.LoggingConfig{
			Format: "json",
			Level:  "error",
		},
	}
}

type closeTrackingStore struct {
	store.Store
	closed   atomic.Bool
	sessions store.SessionStore
}

func (s *closeTrackingStore) Sessions() store.SessionStore {
	if s.sessions != nil {
		return s.sessions
	}
	return s.Store.Sessions()
}

func (s *closeTrackingStore) Close() error {
	s.closed.Store(true)
	if s.Store != nil {
		return s.Store.Close()
	}
	return nil
}

type cleanupSessionStore struct {
	store.SessionStore
	calls atomic.Int32
}

func (s *cleanupSessionStore) CleanupExpired(context.Context, time.Time) (int, error) {
	s.calls.Add(1)
	return 0, nil
}

type fakeHTTPRuntime struct {
	started        chan struct{}
	stopped        chan struct{}
	stopOnce       sync.Once
	shutdownBudget time.Duration
}

func newFakeHTTPRuntime() *fakeHTTPRuntime {
	return &fakeHTTPRuntime{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (s *fakeHTTPRuntime) ListenAndServe() error {
	close(s.started)
	<-s.stopped
	return http.ErrServerClosed
}

func (s *fakeHTTPRuntime) Shutdown(ctx context.Context) error {
	if deadline, ok := ctx.Deadline(); ok {
		s.shutdownBudget = time.Until(deadline)
	}
	s.stopOnce.Do(func() { close(s.stopped) })
	return nil
}
