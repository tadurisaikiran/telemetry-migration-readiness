package agentprocess

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCommandRunsBoundedDirectProcess(t *testing.T) {
	t.Setenv("TMR_AGENT_PROCESS_HELPER", "1")

	output, err := helperCommand("echo", 10*time.Second, 1024).Run(context.Background(), []byte("request"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(output), "request"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestCommandEnforcesTimeoutAndOutputBound(t *testing.T) {
	t.Setenv("TMR_AGENT_PROCESS_HELPER", "1")

	_, err := helperCommand("slow", 10*time.Millisecond, 1024).Run(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}
	_, err = helperCommand("oversize", 10*time.Second, 16).Run(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversize error = %v", err)
	}
}

func TestCommandSanitizesProviderError(t *testing.T) {
	t.Setenv("TMR_AGENT_PROCESS_HELPER", "1")

	command := helperCommand("failure", 10*time.Second, 1024)
	command.Sanitize = func(value string) string {
		return strings.ReplaceAll(value, "provider-secret", "[REDACTED]")
	}
	_, err := command.Run(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "[REDACTED]") || strings.Contains(err.Error(), "provider-secret") {
		t.Fatalf("error = %v", err)
	}
}

func helperCommand(mode string, timeout time.Duration, outputLimit int) Command {
	return Command{
		Path:           os.Args[0],
		Args:           []string{"-test.run=TestAgentProcessHelper", "--", mode},
		Timeout:        timeout,
		MaxInputBytes:  1024,
		MaxOutputBytes: outputLimit,
	}
}

func TestAgentProcessHelper(t *testing.T) {
	if os.Getenv("TMR_AGENT_PROCESS_HELPER") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	switch mode {
	case "echo":
		contents, _ := io.ReadAll(os.Stdin)
		fmt.Fprint(os.Stdout, string(contents))
	case "slow":
		time.Sleep(200 * time.Millisecond)
	case "oversize":
		fmt.Fprint(os.Stdout, strings.Repeat("x", 128))
	case "failure":
		fmt.Fprint(os.Stderr, "token=provider-secret")
		os.Exit(7)
	default:
		os.Exit(2)
	}
	os.Exit(0)
}
