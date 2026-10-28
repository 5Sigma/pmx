package secrets

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// PasswordCommandTimeout is exported so tests can override it.
var PasswordCommandTimeout = 5 * time.Second

func runPasswordCommand(cmd string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), PasswordCommandTimeout)
	defer cancel()

	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd.exe", "/C", cmd)
	} else {
		c = exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	if err := c.Run(); err != nil {
		return "", fmt.Errorf("password_command failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	out := strings.TrimRight(stdout.String(), "\r\n")
	if out == "" {
		return "", fmt.Errorf("password_command returned empty output")
	}
	return out, nil
}
