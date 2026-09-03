package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"github.com/Silentely/Repo-Sentinel/internal/config"
)

func Test版本命令输出当前开发版本(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := Run([]string{"version"}, &stdout, &stderr); err != nil {
		t.Fatalf("执行 version 失败: %v", err)
	}
	if got := stdout.String(); !bytes.Contains([]byte(got), []byte("version=dev")) ||
		!bytes.Contains([]byte(got), []byte("build_channel=local")) {
		t.Fatalf("version 输出=%q，期望包含开发版本与 local 渠道", got)
	}
	if !bytes.Contains([]byte(stdout.String()), []byte("repository=https://github.com/Silentely/Repo-Sentinel")) {
		t.Fatalf("version 输出应包含仓库地址，实际: %q", stdout.String())
	}
}

func Test缺少命令会失败(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := Run(nil, &stdout, &stderr); err == nil {
		t.Fatal("缺少命令未返回错误")
	}
}

func TestReadPasswordLineNilStdin(t *testing.T) {
	_, err := readPasswordLine(nil)
	if err == nil {
		t.Fatal("expected error on nil stdin")
	}
}

func TestShorthandConfigFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := Dependencies{
		LoadConfig: func(ctx context.Context, opts config.LoadOptions) (config.Config, error) {
			if opts.ConfigPath != "custom.yaml" {
				t.Fatalf("expected ConfigPath=custom.yaml, got %q", opts.ConfigPath)
			}
			return config.Config{}, nil
		},
	}
	runner := NewRunner(strings.NewReader(""), &stdout, &stderr, deps)
	_ = runner.Run(t.Context(), []string{"config", "validate", "-c", "custom.yaml"})
}
