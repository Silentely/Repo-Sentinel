package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/app"
	"github.com/Silentely/Repo-Sentinel/internal/config"
)

func TestCLIRotateKeys_DryRunAndApply(t *testing.T) {
	ctx := context.Background()

	calledDryRun := false
	calledApply := false

	deps := Dependencies{
		LoadConfig: func(context.Context, config.LoadOptions) (config.Config, error) {
			return config.Config{}, nil
		},
		RotateKeys: func(ctx context.Context, cfg config.Config, opts app.RotateKeysOptions) (app.RotateKeysStats, error) {
			if opts.DryRun {
				calledDryRun = true
				return app.RotateKeysStats{
					ChannelsScanned: 1,
					ChannelsRotated: 1,
					SettingsScanned: 3,
					SettingsRotated: 3,
					AlreadyCurrent:  0,
					DryRun:          true,
				}, nil
			}
			calledApply = true
			return app.RotateKeysStats{
				ChannelsScanned: 1,
				ChannelsRotated: 1,
				SettingsScanned: 3,
				SettingsRotated: 3,
				AlreadyCurrent:  0,
				DryRun:          false,
			}, nil
		},
	}

	// 1. Dry run
	var stdoutDry, stderrDry bytes.Buffer
	runnerDry := NewRunner(nil, &stdoutDry, &stderrDry, deps)
	err := runnerDry.Run(ctx, []string{"secret", "rotate-keys", "--dry-run"})
	if err != nil {
		t.Fatalf("expected dry-run success, got %v", err)
	}
	if !calledDryRun {
		t.Fatalf("expected RotateKeys called with DryRun=true")
	}
	outDryStr := stdoutDry.String()
	if !strings.Contains(outDryStr, "mode=dry_run") || !strings.Contains(outDryStr, "channels_rotated=1") {
		t.Fatalf("unexpected stdout: %s", outDryStr)
	}

	// 2. Real apply
	var stdoutApply, stderrApply bytes.Buffer
	runnerApply := NewRunner(nil, &stdoutApply, &stderrApply, deps)
	err = runnerApply.Run(ctx, []string{"secret", "rotate-keys"})
	if err != nil {
		t.Fatalf("expected apply success, got %v", err)
	}
	if !calledApply {
		t.Fatalf("expected RotateKeys called with DryRun=false")
	}
	outApplyStr := stdoutApply.String()
	if !strings.Contains(outApplyStr, "mode=applied") || !strings.Contains(outApplyStr, "settings_rotated=3") {
		t.Fatalf("unexpected stdout: %s", outApplyStr)
	}
}
