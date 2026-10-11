package rules

import (
	"testing"
)

func TestParseAndAuthorizeChatOps(t *testing.T) {
	whitelist := []string{"status", "retry", "mute"}

	// 合法命令，带机器人员工后缀
	cmd1 := ParseAndAuthorizeChatOps("/retry@sentinel_bot dl-1024 force", whitelist)
	if !cmd1.IsValid || !cmd1.Allowed {
		t.Fatalf("expected valid & allowed command: %+v", cmd1)
	}
	if cmd1.Command != "retry" || cmd1.Subcommand != "dl-1024" || len(cmd1.Args) != 1 || cmd1.Args[0] != "force" {
		t.Errorf("unexpected command structure: %+v", cmd1)
	}

	// 未授权命令
	cmd2 := ParseAndAuthorizeChatOps("/reboot now", whitelist)
	if !cmd2.IsValid || cmd2.Allowed {
		t.Fatalf("expected valid but not allowed: %+v", cmd2)
	}

	// 非命令普通文本
	cmd3 := ParseAndAuthorizeChatOps("hello world", whitelist)
	if cmd3.IsValid {
		t.Fatalf("expected regular text to be invalid command")
	}
}

func TestParseAndAuthorizeChatOpsEmptyWhitelistDenies(t *testing.T) {
	cmd := ParseAndAuthorizeChatOps("/retry job-1", nil)
	if !cmd.IsValid {
		t.Fatalf("expected syntactically valid command: %+v", cmd)
	}
	if cmd.Allowed {
		t.Fatalf("empty ChatOps whitelist must deny commands: %+v", cmd)
	}
}
