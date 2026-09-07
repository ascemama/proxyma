package executor

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunSuccessfulCommand(t *testing.T) {
	result := Executor{Timeout: time.Second, OutputLimit: 1024}.Run(context.Background(), "echo Proxyma")
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.ExitStatus == nil || *result.ExitStatus != 0 {
		t.Fatalf("exit status = %#v", result.ExitStatus)
	}
	if !strings.Contains(result.Stdout, "Proxyma") {
		t.Fatalf("stdout = %q", result.Stdout)
	}
}

func TestRunFailedCommand(t *testing.T) {
	result := Executor{Timeout: time.Second, OutputLimit: 1024}.Run(context.Background(), "exit 7")
	if result.Error == "" {
		t.Fatal("expected error")
	}
	if result.ExitStatus == nil || *result.ExitStatus != 7 {
		t.Fatalf("exit status = %#v", result.ExitStatus)
	}
}

func TestRunTimeout(t *testing.T) {
	result := Executor{Timeout: 100 * time.Millisecond, OutputLimit: 1024}.Run(context.Background(), sleepCommand())
	if !result.TimedOut {
		t.Fatalf("expected timeout, got %#v", result)
	}
}

func TestRunCapturesStderrAndTruncates(t *testing.T) {
	result := Executor{Timeout: time.Second, OutputLimit: 4}.Run(context.Background(), stderrCommand("abcdef"))
	if !result.Truncated {
		t.Fatal("expected truncation")
	}
	if len(result.Stderr) != 4 {
		t.Fatalf("stderr length = %d, stderr = %q", len(result.Stderr), result.Stderr)
	}
}

func sleepCommand() string {
	if runtime.GOOS == "windows" {
		return "ping -n 3 127.0.0.1 > nul"
	}
	return "sleep 2"
}

func stderrCommand(text string) string {
	if runtime.GOOS == "windows" {
		return "echo " + text + " 1>&2"
	}
	return "echo " + text + " >&2"
}
