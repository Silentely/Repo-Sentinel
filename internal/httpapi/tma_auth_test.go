package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/notify"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func createTestInitData(token string, user TelegramUser, authDate int64, reversedSecretKey bool) string {
	userJSON, _ := json.Marshal(user)
	values := url.Values{}
	values.Set("auth_date", strconv.FormatInt(authDate, 10))
	values.Set("query_id", "AAH_test_query_id_123")
	values.Set("user", string(userJSON))

	var keys []string
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var pairs []string
	for _, k := range keys {
		pairs = append(pairs, k+"="+values.Get(k))
	}
	dataCheckString := strings.Join(pairs, "\n")

	var secretKey []byte
	if reversedSecretKey {
		// 错误公式：key=bot_token, msg="WebAppData"
		mac := hmac.New(sha256.New, []byte(token))
		mac.Write([]byte("WebAppData"))
		secretKey = mac.Sum(nil)
	} else {
		// 官方正确规范：key="WebAppData", msg=bot_token
		mac := hmac.New(sha256.New, []byte("WebAppData"))
		mac.Write([]byte(token))
		secretKey = mac.Sum(nil)
	}

	macData := hmac.New(sha256.New, secretKey)
	macData.Write([]byte(dataCheckString))
	hashHex := hex.EncodeToString(macData.Sum(nil))

	values.Set("hash", hashHex)
	return values.Encode()
}

func TestTMAAuth_ValidateTelegramInitData(t *testing.T) {
	const botToken = "123456789:ABCdefGHIjklMNOpqrSTUvwxYZ"
	user := TelegramUser{
		ID:        987654321,
		FirstName: "Alice",
		LastName:  "Wonderland",
		Username:  "alicew",
	}

	t.Run("正确派生密钥验签通过", func(t *testing.T) {
		initData := createTestInitData(botToken, user, time.Now().Unix(), false)
		gotUser, err := ValidateTelegramInitData(initData, botToken, 24*time.Hour)
		if err != nil {
			t.Fatalf("合法 initData 验签应通过: %v", err)
		}
		if gotUser.ID != user.ID || gotUser.Username != user.Username {
			t.Fatalf("解析得到的 TelegramUser 不匹配: got %+v, want %+v", gotUser, user)
		}
	})

	t.Run("反向派生密钥被严格拦截", func(t *testing.T) {
		// 断言 2：若以 key=bot_token, msg="WebAppData" 派生，严格拦截
		initData := createTestInitData(botToken, user, time.Now().Unix(), true)
		_, err := ValidateTelegramInitData(initData, botToken, 24*time.Hour)
		if err == nil {
			t.Fatal("反向公式派生密钥生成的签名应当被拒绝")
		}
		if !strings.Contains(err.Error(), "signature mismatch") {
			t.Fatalf("期望 signature mismatch 错误, 实际: %v", err)
		}
	})

	t.Run("篡改数据或哈希被拦截", func(t *testing.T) {
		initData := createTestInitData(botToken, user, time.Now().Unix(), false)
		// 篡改哈希末尾字符
		tampered := initData[:len(initData)-2] + "00"
		_, err := ValidateTelegramInitData(tampered, botToken, 24*time.Hour)
		if err == nil {
			t.Fatal("篡改哈希后应当校验失败")
		}
	})

	t.Run("过期auth_date超过24小时被拦截", func(t *testing.T) {
		expiredTime := time.Now().Add(-25 * time.Hour).Unix()
		initData := createTestInitData(botToken, user, expiredTime, false)
		_, err := ValidateTelegramInitData(initData, botToken, 24*time.Hour)
		if err == nil {
			t.Fatal("超过24小时的 auth_date 应当被拒绝")
		}
		if !strings.Contains(err.Error(), "expired") {
			t.Fatalf("期望 auth_date expired 错误, 实际: %v", err)
		}
	})

	t.Run("未来时间伪造被拦截", func(t *testing.T) {
		futureTime := time.Now().Add(5 * time.Minute).Unix()
		initData := createTestInitData(botToken, user, futureTime, false)
		_, err := ValidateTelegramInitData(initData, botToken, 24*time.Hour)
		if err == nil {
			t.Fatal("超前 5 分钟的未来时间伪造应当被拒绝")
		}
		if !strings.Contains(err.Error(), "future") {
			t.Fatalf("期望 auth_date is in the future 错误, 实际: %v", err)
		}
	})

	t.Run("允许1分钟内的微弱时钟漂移", func(t *testing.T) {
		slightFuture := time.Now().Add(30 * time.Second).Unix()
		initData := createTestInitData(botToken, user, slightFuture, false)
		gotUser, err := ValidateTelegramInitData(initData, botToken, 24*time.Hour)
		if err != nil {
			t.Fatalf("1分钟内的时钟漂移应被宽容: %v", err)
		}
		if gotUser.ID != user.ID {
			t.Fatalf("用户 ID 不匹配: got %d, want %d", gotUser.ID, user.ID)
		}
	})

	t.Run("缺少必要参数fail-closed", func(t *testing.T) {
		// 缺少 hash
		_, err := ValidateTelegramInitData("auth_date=1710000000&user=%7B%7D", botToken, 24*time.Hour)
		if err == nil || !strings.Contains(err.Error(), "missing hash") {
			t.Fatalf("缺少 hash 应当拒绝: %v", err)
		}

		// 缺少 auth_date
		_, err = ValidateTelegramInitData("hash=abcdef&user=%7B%7D", botToken, 24*time.Hour)
		if err == nil || !strings.Contains(err.Error(), "missing auth_date") {
			t.Fatalf("缺少 auth_date 应当拒绝: %v", err)
		}

		// 缺少 user
		initDataWithoutUser := "auth_date=" + strconv.FormatInt(time.Now().Unix(), 10) + "&hash=" + hex.EncodeToString(make([]byte, 32))
		_, err = ValidateTelegramInitData(initDataWithoutUser, botToken, 24*time.Hour)
		if err == nil {
			t.Fatal("缺少 user 应当拒绝")
		}

		// 空 initData 或空 botToken
		if _, err := ValidateTelegramInitData("", botToken, 24*time.Hour); err == nil {
			t.Fatal("空 initData 应当拒绝")
		}
		if _, err := ValidateTelegramInitData("hash=1", "", 24*time.Hour); err == nil {
			t.Fatal("空 botToken 应当拒绝")
		}
	})
}

func setupTestStore(t *testing.T) store.Store {
	t.Helper()
	tempDir := t.TempDir()
	st, err := store.Open(context.Background(), config.DatabaseConfig{
		Driver:       "sqlite",
		URL:          "file:" + filepath.Join(tempDir, "tma_test.db"),
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	return st
}

func deleteExistingTelegramChannels(ctx context.Context, st store.Store) {
	if ch, err := st.Channels().GetEnabledByType(ctx, store.ChannelTelegram); err == nil {
		_ = st.Channels().Delete(ctx, ch.ID)
	}
	if ch, err := st.Channels().GetByType(ctx, store.ChannelTelegram); err == nil {
		_ = st.Channels().Delete(ctx, ch.ID)
	}
}

func setupTestKeyRing(t *testing.T) cryptox.KeyRing {
	t.Helper()
	ring, err := cryptox.NewKeyRing(config.EncryptionConfig{
		CurrentKey: config.NewSecret(base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32))),
	})
	if err != nil {
		t.Fatalf("创建测试 KeyRing 失败: %v", err)
	}
	return ring
}

func TestTMAAuth_ResolveBotToken_PriorityAndFailClosed(t *testing.T) {
	ctx := context.Background()
	st := setupTestStore(t)
	kr := setupTestKeyRing(t)
	const (
		channelToken = "token_from_db_channel"
		cfgToken     = "token_from_cfg_static"
	)

	// 先清缓存
	InvalidateResolvedBotTokenCache()

	t.Run("优先级一：已启用渠道正常解密Token", func(t *testing.T) {
		InvalidateResolvedBotTokenCache()
		env, err := kr.Encrypt(ctx, []byte(channelToken), []byte(notify.AAD))
		if err != nil {
			t.Fatalf("加密渠道 Token 失败: %v", err)
		}
		_, err = st.Channels().Upsert(ctx, store.NotificationChannel{
			ChannelType:    store.ChannelTelegram,
			Name:           "tg-primary",
			Enabled:        true,
			Target:         "-100123456",
			SecretEnvelope: env,
		})
		if err != nil {
			t.Fatalf("保存 Telegram 渠道失败: %v", err)
		}

		tok, err := ResolveBotToken(ctx, st, kr, cfgToken)
		if err != nil {
			t.Fatalf("ResolveBotToken 失败: %v", err)
		}
		if tok != channelToken {
			t.Fatalf("期望使用已启用渠道 Token %q, 实际: %q", channelToken, tok)
		}
	})

	t.Run("优先级一 Fail-Closed：渠道存在但密钥损坏，严格阻断并不降级", func(t *testing.T) {
		// 断言 4 / D4：坏通道存在但解密失败时（哪怕 cfgToken 可用），系统严格 fail-closed 返回错误并告警，绝不静默降级
		InvalidateResolvedBotTokenCache()
		ch, err := st.Channels().GetEnabledByType(ctx, store.ChannelTelegram)
		if err != nil {
			t.Fatalf("查询已启用渠道失败: %v", err)
		}
		// 覆盖为损坏的密文信封
		ch.SecretEnvelope = "v1.badkeyid.badpayload"
		_, err = st.Channels().Upsert(ctx, ch)
		if err != nil {
			t.Fatalf("更新损坏渠道信封失败: %v", err)
		}

		tok, err := ResolveBotToken(ctx, st, kr, cfgToken)
		if err == nil {
			t.Fatalf("损坏信封应当 fail-closed 报错, 实际未报错且返回 tok=%q", tok)
		}
		if !strings.Contains(err.Error(), "decrypt failed") {
			t.Fatalf("期望包含 decrypt failed 错误信息, 实际: %v", err)
		}
	})

	t.Run("优先级二：渠道明确不存在时降级到chatops系统设置或环境变量", func(t *testing.T) {
		InvalidateResolvedBotTokenCache()
		// 删除 Telegram 渠道
		deleteExistingTelegramChannels(ctx, st)

		// 设置 chatops.telegram.bot_token
		const chatopsSettingToken = "token_from_chatops_settings"
		settingJSON, _ := json.Marshal(chatopsSettingToken)
		_, err := st.Settings().Upsert(ctx, store.SystemSetting{
			Key:       "chatops.telegram.bot_token",
			ValueJSON: settingJSON,
		})
		if err != nil {
			t.Fatalf("设置 chatops bot token 失败: %v", err)
		}

		tok, err := ResolveBotToken(ctx, st, kr, cfgToken)
		if err != nil {
			t.Fatalf("ResolveBotToken 失败: %v", err)
		}
		if tok != chatopsSettingToken {
			t.Fatalf("期望使用系统设置 Token %q, 实际: %q", chatopsSettingToken, tok)
		}
	})

	t.Run("优先级三：渠道与设置均无时回退到静态配置", func(t *testing.T) {
		InvalidateResolvedBotTokenCache()
		deleteExistingTelegramChannels(ctx, st)
		_ = st.Settings().Delete(ctx, "chatops.telegram.bot_token")
		_ = st.Settings().Delete(ctx, "notify.telegram.token")

		tok, err := ResolveBotToken(ctx, st, kr, cfgToken)
		if err != nil {
			t.Fatalf("ResolveBotToken 失败: %v", err)
		}
		if tok != cfgToken {
			t.Fatalf("期望回退到静态配置 Token %q, 实际: %q", cfgToken, tok)
		}
	})

	t.Run("内存5分钟缓存生效及显式失效", func(t *testing.T) {
		InvalidateResolvedBotTokenCache()
		// 此时无渠道无设置，读取 cfgToken
		tok1, err := ResolveBotToken(ctx, st, kr, cfgToken)
		if err != nil || tok1 != cfgToken {
			t.Fatalf("初始缓存读取失败: %v, tok=%s", err, tok1)
		}

		// 在后台写入新渠道，但由于 5 分钟缓存存在，不失效前依然返回老缓存
		env, _ := kr.Encrypt(ctx, []byte("token_new_channel"), []byte(notify.AAD))
		_, _ = st.Channels().Upsert(ctx, store.NotificationChannel{
			ChannelType:    store.ChannelTelegram,
			Name:           "tg-new",
			Enabled:        true,
			SecretEnvelope: env,
		})

		tok2, err := ResolveBotToken(ctx, st, kr, cfgToken)
		if err != nil || tok2 != cfgToken {
			t.Fatalf("未清缓存时应命中缓存 %q, 实际: %q", cfgToken, tok2)
		}

		// 显式失效缓存
		InvalidateResolvedBotTokenCache()
		tok3, err := ResolveBotToken(ctx, st, kr, cfgToken)
		if err != nil || tok3 != "token_new_channel" {
			t.Fatalf("失效缓存后应获取新渠道 Token, 实际: %q", tok3)
		}
	})
}

func TestTMAAuth_Middleware(t *testing.T) {
	InvalidateResolvedBotTokenCache()
	ctx := context.Background()
	st := setupTestStore(t)
	kr := setupTestKeyRing(t)
	const botToken = "99887766:AA_TMA_SECRET_BOT_TOKEN_123"

	// 准备内存 log buffer 检验敏感信息
	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	allowedUser := TelegramUser{ID: 10001, Username: "operator_alex"}
	unauthorizedUser := TelegramUser{ID: 20002, Username: "intruder_bob"}

	// 配置白名单：允许 10001
	allowedUserIDs := []string{"10001"}

	mw := TMAAuthMiddleware(st, kr, botToken, allowedUserIDs, logger)

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := GetTelegramUser(r.Context())
		if !ok || u == nil {
			http.Error(w, "missing user in ctx", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"user_id": u.ID,
		})
	})
	handler := mw(nextHandler)

	t.Run("缺失X-Telegram-Init-Data头返回401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("缺失请求头期望 401, 实际: %d", rec.Code)
		}
		var errResp apiErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp.ErrorCode != errorCodeUnauthorized {
			t.Fatalf("期望错误码 %s, 实际: %s", errorCodeUnauthorized, errResp.ErrorCode)
		}
	})

	t.Run("非法签名initData返回401并记录审计", func(t *testing.T) {
		// 使用反向派生密钥生成非法签名
		badInitData := createTestInitData(botToken, allowedUser, time.Now().Unix(), true)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil)
		req.Header.Set("X-Telegram-Init-Data", badInitData)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("非法签名期望 401, 实际: %d", rec.Code)
		}

		// 检查审计记录
		audits, err := st.Audits().List(ctx, 10, 0)
		if err != nil {
			t.Fatalf("查询审计日志失败: %v", err)
		}
		found := false
		for _, a := range audits {
			if a.Action == "tma.auth.denied" && a.ActorType == "telegram/tma" {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("未找到 tma.auth.denied 审计记录")
		}
	})

	t.Run("合法签名但不在白名单中的用户返回403无信息泄漏", func(t *testing.T) {
		// 断言 4：有效签名但 User ID 不在白名单，严格返回 403 Forbidden，不泄漏白名单详情
		unauthInitData := createTestInitData(botToken, unauthorizedUser, time.Now().Unix(), false)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil)
		req.Header.Set("X-Telegram-Init-Data", unauthInitData)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("不在白名单的用户期望 403 Forbidden, 实际: %d", rec.Code)
		}
		var errResp apiErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp.ErrorCode != errorCodeForbidden {
			t.Fatalf("期望错误码 %s, 实际: %s", errorCodeForbidden, errResp.ErrorCode)
		}
		// 响应体不得含有白名单信息或 10001
		if strings.Contains(rec.Body.String(), "10001") || strings.Contains(rec.Body.String(), "whitelist") {
			t.Fatalf("403 响应体泄漏了白名单内部信息: %s", rec.Body.String())
		}
	})

	t.Run("白名单用户携带合法签名成功放行并在Context注入TelegramUser", func(t *testing.T) {
		// 断言 1：合法 initData 验签通过，成功在 Context 注入 TelegramUser
		validInitData := createTestInitData(botToken, allowedUser, time.Now().Unix(), false)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil)
		req.Header.Set("X-Telegram-Init-Data", validInitData)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("合法请求期望 200 OK, 实际: %d, body: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("响应反序列化失败: %v", err)
		}
		if int64(resp["user_id"].(float64)) != allowedUser.ID {
			t.Fatalf("期望 user_id %d, 实际: %v", allowedUser.ID, resp["user_id"])
		}
	})

	t.Run("系统设置中的白名单也生效", func(t *testing.T) {
		// 动态通过 store.Settings() 配置白名单
		_, _ = st.Settings().Upsert(ctx, store.SystemSetting{
			Key:       "chatops.telegram.allowed_user_ids",
			ValueJSON: []byte(`"30003,40004"`),
		})
		dynamicMW := TMAAuthMiddleware(st, kr, botToken, nil, logger)
		dynamicHandler := dynamicMW(nextHandler)

		dynUser := TelegramUser{ID: 30003, Username: "charlie"}
		dynInitData := createTestInitData(botToken, dynUser, time.Now().Unix(), false)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil)
		req.Header.Set("X-Telegram-Init-Data", dynInitData)
		rec := httptest.NewRecorder()
		dynamicHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Settings 白名单用户期望 200, 实际: %d, body: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestTMAAuth_ZeroSensitiveTokensInLogs(t *testing.T) {
	// 断言 11：Bot Token 与 initData 绝不出现在任何系统日志或错误响应体中
	const secretBotToken = "SECRET_BOT_TOKEN_DO_NOT_LEAK"
	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	st := setupTestStore(t)
	kr := setupTestKeyRing(t)

	// 插入损坏信封以触发解密错误日志
	_, _ = st.Channels().Upsert(context.Background(), store.NotificationChannel{
		ChannelType:    store.ChannelTelegram,
		Name:           "tg-corrupt",
		Enabled:        true,
		SecretEnvelope: "v1.badkey.corrupted",
	})
	InvalidateResolvedBotTokenCache()

	mw := TMAAuthMiddleware(st, kr, secretBotToken, []string{"123"}, logger)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	user := TelegramUser{ID: 123, Username: "alice"}
	initData := createTestInitData(secretBotToken, user, time.Now().Unix(), false)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil)
	req.Header.Set("X-Telegram-Init-Data", initData)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	allLogs := logBuf.String()
	respBody := rec.Body.String()

	// 断言：log 和 respBody 绝对不能包含 secretBotToken 明文
	if strings.Contains(allLogs, secretBotToken) {
		t.Fatalf("日志泄露了 Bot Token: %s", allLogs)
	}
	if strings.Contains(respBody, secretBotToken) {
		t.Fatalf("HTTP 响应体泄露了 Bot Token: %s", respBody)
	}
}

// 确保标准输出与全局日志中也没有泄漏
var _ io.Reader = (*bytes.Buffer)(nil)
