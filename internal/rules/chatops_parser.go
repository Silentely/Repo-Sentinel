package rules

import (
	"fmt"
	"strings"
)

// ChatOpsCommand 描述解析后的 ChatOps 指令
type ChatOpsCommand struct {
	Raw        string   `json:"raw"`
	Command    string   `json:"command"`
	Subcommand string   `json:"subcommand,omitempty"`
	Args       []string `json:"args"`
	IsValid    bool     `json:"is_valid"`
	Allowed    bool     `json:"allowed"`
	ErrorMsg   string   `json:"error_msg,omitempty"`
}

// ParseAndAuthorizeChatOps 对传入的聊天文本进行命令解析并做白名单校验
func ParseAndAuthorizeChatOps(text string, allowedCommands []string) ChatOpsCommand {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "/") {
		return ChatOpsCommand{
			Raw:      text,
			IsValid:  false,
			ErrorMsg: "不是以 '/' 开头的有效命令",
		}
	}

	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return ChatOpsCommand{IsValid: false, ErrorMsg: "命令内容为空"}
	}

	// 提取根命令，移除前导 '/'，并移除可能的群机器人后缀（如 /status@my_bot -> /status）
	rootCmd := strings.TrimPrefix(parts[0], "/")
	if atIdx := strings.Index(rootCmd, "@"); atIdx != -1 {
		rootCmd = rootCmd[:atIdx]
	}
	rootCmd = strings.ToLower(rootCmd)

	var subCmd string
	var args []string

	if len(parts) > 1 {
		subCmd = strings.ToLower(parts[1])
		if len(parts) > 2 {
			args = parts[2:]
		}
	}

	// 白名单校验
	allowed := false
	if len(allowedCommands) == 0 {
		allowed = true // 未指定白名单时默认放行合法语法
	} else {
		for _, cmd := range allowedCommands {
			cleanCmd := strings.ToLower(strings.TrimPrefix(cmd, "/"))
			if cleanCmd == rootCmd {
				allowed = true
				break
			}
		}
	}

	errMsg := ""
	if !allowed {
		errMsg = fmt.Sprintf("指令 %q 未在授权命令白名单中", rootCmd)
	}

	return ChatOpsCommand{
		Raw:        text,
		Command:    rootCmd,
		Subcommand: subCmd,
		Args:       args,
		IsValid:    true,
		Allowed:    allowed,
		ErrorMsg:   errMsg,
	}
}
