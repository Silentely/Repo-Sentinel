package config

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Load 按默认值、YAML 文件、环境变量的顺序合并配置，并返回已校验配置。
func Load(ctx context.Context, options LoadOptions) (Config, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return Config{}, ctx.Err()
		default:
		}
	}

	cfg := defaultConfig()
	maxOpenExplicit := false

	if options.ConfigPath != "" {
		fileSystem := options.FileSystem
		if fileSystem == nil {
			fileSystem = osFileSystem{}
		}
		data, err := fs.ReadFile(fileSystem, options.ConfigPath)
		if err != nil {
			return Config{}, fmt.Errorf("read config file %q: %w", options.ConfigPath, err)
		}
		maxOpenExplicit, err = decodeYAML(data, &cfg)
		if err != nil {
			return Config{}, err
		}
	}

	lookupEnv := options.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	if explicit, err := applyEnvironment(&cfg, lookupEnv); err != nil {
		return Config{}, err
	} else if explicit {
		maxOpenExplicit = true
	}

	if cfg.Database.Driver == "postgres" && !maxOpenExplicit {
		cfg.Database.MaxOpenConns = defaultPostgresMaxOpenConns
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// osFileSystem 是极小的操作系统文件适配器，允许测试注入 fs.FS。
type osFileSystem struct{}

func (osFileSystem) Open(name string) (fs.File, error) {
	return os.Open(name)
}

func decodeYAML(data []byte, cfg *Config) (bool, error) {
	var root yaml.Node
	if err := yaml.NewDecoder(bytes.NewReader(data)).Decode(&root); err != nil {
		if err == io.EOF {
			return false, nil
		}
		return false, newValidationError("config file", "YAML contains an invalid document")
	}
	if err := validateYAMLTypedFields(&root); err != nil {
		return false, err
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil {
		return false, newValidationError("config file", "YAML contains an unknown or invalid field")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return false, newValidationError("config file", "YAML must contain one document")
	}

	return yamlHasKey(&root, "database", "max_open_conns"), nil
}

func yamlHasKey(root *yaml.Node, path ...string) bool {
	return yamlValue(root, path...) != nil
}

func yamlValue(root *yaml.Node, path ...string) *yaml.Node {
	node := root
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil
		}
		node = node.Content[0]
	}
	for _, key := range path {
		if node == nil || node.Kind != yaml.MappingNode {
			return nil
		}
		var next *yaml.Node
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == key {
				next = node.Content[i+1]
				break
			}
		}
		if next == nil {
			return nil
		}
		node = next
	}
	return node
}

func validateYAMLTypedFields(root *yaml.Node) error {
	checks := []struct {
		field    string
		path     []string
		message  string
		validate func(*yaml.Node) bool
	}{
		{field: "database.max_open_conns", path: []string{"database", "max_open_conns"}, message: "must be an integer", validate: yamlInt},
		{field: "database.max_idle_conns", path: []string{"database", "max_idle_conns"}, message: "must be an integer", validate: yamlInt},
		{field: "github.app_id", path: []string{"github", "app_id"}, message: "must be an integer", validate: yamlInt64},
		{field: "admin.session_ttl", path: []string{"admin", "session_ttl"}, message: "must be a duration", validate: yamlDuration},
		{field: "setup.allow_remote", path: []string{"setup", "allow_remote"}, message: "must be a boolean", validate: yamlBool},
		{field: "notify.http_webhook.allow_private", path: []string{"notify", "http_webhook", "allow_private"}, message: "must be a boolean", validate: yamlBool},
	}
	for _, check := range checks {
		node := yamlValue(root, check.path...)
		if node == nil || node.Tag == "!!null" {
			continue
		}
		if !check.validate(node) {
			return newValidationError(check.field, check.message)
		}
	}
	return nil
}

func yamlInt(node *yaml.Node) bool {
	var value int
	return node.Decode(&value) == nil
}

func yamlInt64(node *yaml.Node) bool {
	var value int64
	return node.Decode(&value) == nil
}

func yamlBool(node *yaml.Node) bool {
	var value bool
	return node.Decode(&value) == nil
}

func yamlDuration(node *yaml.Node) bool {
	var value time.Duration
	return node.Decode(&value) == nil
}

func applyEnvironment(cfg *Config, lookup func(string) (string, bool)) (bool, error) {
	maxOpenExplicit := false
	if value, ok := lookup("REPOSENTINEL_HTTP_ADDR"); ok {
		cfg.HTTP.Addr = value
	}
	if value, ok := lookup("REPOSENTINEL_PUBLIC_BASE_URL"); ok {
		cfg.HTTP.PublicBaseURL = value
	}
	if value, ok := lookup("REPOSENTINEL_DATABASE_DRIVER"); ok {
		cfg.Database.Driver = value
	}
	if value, ok := lookup("REPOSENTINEL_DATABASE_URL"); ok {
		cfg.Database.URL = value
	}
	if value, ok := lookup("REPOSENTINEL_DATABASE_MAX_OPEN_CONNS"); ok {
		parsed, err := parseIntEnvironment("REPOSENTINEL_DATABASE_MAX_OPEN_CONNS", value)
		if err != nil {
			return false, err
		}
		cfg.Database.MaxOpenConns = parsed
		maxOpenExplicit = true
	}
	if value, ok := lookup("REPOSENTINEL_DATABASE_MAX_IDLE_CONNS"); ok {
		parsed, err := parseIntEnvironment("REPOSENTINEL_DATABASE_MAX_IDLE_CONNS", value)
		if err != nil {
			return false, err
		}
		cfg.Database.MaxIdleConns = parsed
	}
	if value, ok := lookup("REPOSENTINEL_ADMIN_USERNAME"); ok {
		cfg.Admin.Username = value
	}
	if value, ok := lookup("REPOSENTINEL_ADMIN_PASSWORD"); ok {
		cfg.Admin.Password = NewSecret(value)
	}
	if value, ok := lookup("REPOSENTINEL_ADMIN_SESSION_TTL"); ok {
		parsed, err := parseDurationEnvironment("REPOSENTINEL_ADMIN_SESSION_TTL", value)
		if err != nil {
			return false, err
		}
		cfg.Admin.SessionTTL = parsed
	}
	if value, ok := lookup("REPOSENTINEL_SETUP_ALLOW_REMOTE"); ok {
		parsed, err := parseBoolEnvironment("REPOSENTINEL_SETUP_ALLOW_REMOTE", value)
		if err != nil {
			return false, err
		}
		cfg.Setup.AllowRemote = parsed
	}
	if value, ok := lookup("REPOSENTINEL_ENCRYPTION_KEY"); ok {
		cfg.Encryption.CurrentKey = NewSecret(value)
	}
	if value, ok := lookup("REPOSENTINEL_ENCRYPTION_KEY_PREVIOUS"); ok {
		cfg.Encryption.PreviousKey = NewSecret(value)
	}
	if value, ok := lookup("REPOSENTINEL_GITHUB_APP_ID"); ok {
		parsed, err := parseInt64Environment("REPOSENTINEL_GITHUB_APP_ID", value)
		if err != nil {
			return false, err
		}
		cfg.GitHub.AppID = parsed
	}
	if value, ok := lookup("REPOSENTINEL_GITHUB_CLIENT_ID"); ok {
		cfg.GitHub.ClientID = value
	}
	if value, ok := lookup("REPOSENTINEL_GITHUB_PRIVATE_KEY_PATH"); ok {
		cfg.GitHub.PrivateKeyPath = value
	}
	if value, ok := lookup("REPOSENTINEL_GITHUB_WEBHOOK_SECRET"); ok {
		cfg.GitHub.WebhookSecret = NewSecret(value)
	}
	if value, ok := lookup("REPOSENTINEL_GITHUB_WEBHOOK_PREVIOUS_SECRET"); ok {
		cfg.GitHub.WebhookPreviousSecret = NewSecret(value)
	}
	if value, ok := lookup("REPOSENTINEL_EXTERNAL_PAT"); ok {
		cfg.GitHub.ExternalPAT = NewSecret(value)
	}
	if value, ok := lookup("REPOSENTINEL_TELEGRAM_TOKEN"); ok {
		cfg.Notify.Telegram.Token = NewSecret(value)
	}
	if value, ok := lookup("REPOSENTINEL_TELEGRAM_CHAT_ID"); ok {
		cfg.Notify.Telegram.ChatID = value
	}
	if value, ok := lookup("REPOSENTINEL_HTTP_WEBHOOK_URL"); ok {
		cfg.Notify.HTTPWebhook.URL = value
	}
	if value, ok := lookup("REPOSENTINEL_HTTP_WEBHOOK_SECRET"); ok {
		cfg.Notify.HTTPWebhook.Secret = NewSecret(value)
	}
	if value, ok := lookup("REPOSENTINEL_HTTP_WEBHOOK_ALLOW_PRIVATE"); ok {
		parsed, err := parseBoolEnvironment("REPOSENTINEL_HTTP_WEBHOOK_ALLOW_PRIVATE", value)
		if err != nil {
			return false, err
		}
		cfg.Notify.HTTPWebhook.AllowPrivate = parsed
	}
	if value, ok := lookup("REPOSENTINEL_LOG_FORMAT"); ok {
		cfg.Logging.Format = value
	}
	if value, ok := lookup("REPOSENTINEL_LOG_LEVEL"); ok {
		cfg.Logging.Level = value
	}
	// 设计规格使用 LOG_* 作为标准变量；放在兼容别名之后以保证确定性优先级。
	if value, ok := lookup("LOG_FORMAT"); ok {
		cfg.Logging.Format = value
	}
	if value, ok := lookup("LOG_LEVEL"); ok {
		cfg.Logging.Level = value
	}
	if value, ok := lookup("REPOSENTINEL_METRICS_ENABLED"); ok {
		parsed, err := parseBoolEnvironment("REPOSENTINEL_METRICS_ENABLED", value)
		if err != nil {
			return false, err
		}
		cfg.Metrics.Enabled = parsed
	}
	if value, ok := lookup("REPOSENTINEL_METRICS_TOKEN"); ok {
		cfg.Metrics.Token = NewSecret(value)
	}
	if value, ok := lookup("REPOSENTINEL_UPDATE_CHECK"); ok {
		parsed, err := parseBoolEnvironment("REPOSENTINEL_UPDATE_CHECK", value)
		if err != nil {
			return false, err
		}
		cfg.UpdateCheck.Enabled = parsed
	}
	if value, ok := lookup("REPOSENTINEL_UPDATE_CHECK_URL"); ok {
		cfg.UpdateCheck.URL = value
	}
	if value, ok := lookup("REPOSENTINEL_UPDATE_CHECK_TOKEN"); ok {
		cfg.UpdateCheck.Token = NewSecret(value)
	}
	if value, ok := lookup("REPOSENTINEL_AGGREGATION_WINDOW"); ok {
		d, err := time.ParseDuration(value)
		if err != nil {
			return false, newValidationError("REPOSENTINEL_AGGREGATION_WINDOW", "must be a duration")
		}
		cfg.Aggregation.Window = d
	}
	if value, ok := lookup("REPOSENTINEL_AGGREGATION_BURST_THRESHOLD"); ok {
		n, err := parseIntEnvironment("REPOSENTINEL_AGGREGATION_BURST_THRESHOLD", value)
		if err != nil {
			return false, err
		}
		cfg.Aggregation.BurstThreshold = n
	}
	if value, ok := lookup("REPOSENTINEL_AGGREGATION_BURST_WINDOW"); ok {
		d, err := time.ParseDuration(value)
		if err != nil {
			return false, newValidationError("REPOSENTINEL_AGGREGATION_BURST_WINDOW", "must be a duration")
		}
		cfg.Aggregation.BurstWindow = d
	}
	return maxOpenExplicit, nil
}

func parseIntEnvironment(name, value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, newValidationError(name, "must be an integer")
	}
	return parsed, nil
}

func parseInt64Environment(name, value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, newValidationError(name, "must be an integer")
	}
	return parsed, nil
}

func parseBoolEnvironment(name, value string) (bool, error) {
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, newValidationError(name, "must be a boolean")
	}
	return parsed, nil
}

func parseDurationEnvironment(name, value string) (time.Duration, error) {
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, newValidationError(name, "must be a duration")
	}
	return parsed, nil
}
