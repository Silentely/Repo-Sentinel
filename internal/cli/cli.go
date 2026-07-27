package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// Run 使用操作系统标准流与 SIGINT/SIGTERM context 分派 CLI 命令。
func Run(args []string, stdout, stderr io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return NewRunner(os.Stdin, stdout, stderr, Dependencies{}).Run(ctx, args)
}

func reportError(stderr io.Writer, err error) error {
	code := cliErrorCode(err)
	message := cliPublicMessage(err)
	if _, writeErr := fmt.Fprintf(stderr, "error_code=%s message=%s\n", code, message); writeErr != nil {
		return errors.Join(err, writeErr)
	}
	return err
}

type cliError struct {
	code    string
	message string
}

func (e cliError) Error() string         { return e.message }
func (e cliError) ErrorCode() string     { return e.code }
func (e cliError) PublicMessage() string { return e.message }

func newCLIError(message string) error {
	return cliError{code: "validation_failed", message: message}
}

func cliErrorCode(err error) string {
	type coded interface {
		ErrorCode() string
	}
	var target coded
	if errors.As(err, &target) {
		return target.ErrorCode()
	}
	return "internal_error"
}

func cliPublicMessage(err error) string {
	type public interface {
		PublicMessage() string
	}
	var target public
	if errors.As(err, &target) {
		return target.PublicMessage()
	}
	type coded interface {
		ErrorCode() string
	}
	var codeTarget coded
	if errors.As(err, &codeTarget) {
		return err.Error()
	}
	return "命令执行失败。"
}
