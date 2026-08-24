// Package agentprocess provides a bounded local subprocess boundary for
// optional AI protocols. Commands are invoked directly, never through a shell.
package agentprocess

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const defaultErrorBytes = 64 << 10

type Command struct {
	Path           string
	Args           []string
	Timeout        time.Duration
	MaxInputBytes  int
	MaxOutputBytes int
	Sanitize       func(string) string
}

func (command Command) Run(ctx context.Context, input []byte) ([]byte, error) {
	if strings.TrimSpace(command.Path) == "" {
		return nil, fmt.Errorf("AI provider command is required")
	}
	if command.Timeout <= 0 {
		return nil, fmt.Errorf("AI provider timeout must be positive")
	}
	if command.MaxInputBytes <= 0 || command.MaxOutputBytes <= 0 {
		return nil, fmt.Errorf("AI provider input and output limits must be positive")
	}
	if len(input) > command.MaxInputBytes {
		return nil, fmt.Errorf("AI provider request exceeds the %d-byte limit", command.MaxInputBytes)
	}

	providerContext, cancel := context.WithTimeout(ctx, command.Timeout)
	defer cancel()
	process := exec.CommandContext(providerContext, command.Path, command.Args...)
	process.Stdin = bytes.NewReader(input)
	stdout := newCappedBuffer(command.MaxOutputBytes)
	stderr := newCappedBuffer(defaultErrorBytes)
	process.Stdout = stdout
	process.Stderr = stderr
	if err := process.Run(); err != nil {
		if errors.Is(providerContext.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("AI provider timed out after %s", command.Timeout)
		}
		if errors.Is(providerContext.Err(), context.Canceled) {
			return nil, fmt.Errorf("AI provider canceled: %w", providerContext.Err())
		}
		message := strings.TrimSpace(stderr.String())
		if command.Sanitize != nil {
			message = command.Sanitize(message)
		}
		if message != "" {
			return nil, fmt.Errorf("AI provider failed: %w: %s", err, message)
		}
		return nil, fmt.Errorf("AI provider failed: %w", err)
	}
	if stdout.Exceeded() {
		return nil, fmt.Errorf("AI provider response exceeds the %d-byte limit", command.MaxOutputBytes)
	}
	if stderr.Exceeded() {
		return nil, fmt.Errorf("AI provider error output exceeds the %d-byte limit", defaultErrorBytes)
	}
	return append([]byte(nil), stdout.Bytes()...), nil
}

type cappedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func newCappedBuffer(limit int) *cappedBuffer {
	return &cappedBuffer{limit: limit}
}

func (buffer *cappedBuffer) Write(contents []byte) (int, error) {
	originalLength := len(contents)
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining <= 0 {
		buffer.exceeded = buffer.exceeded || originalLength != 0
		return originalLength, nil
	}
	if len(contents) > remaining {
		buffer.exceeded = true
		contents = contents[:remaining]
	}
	_, _ = buffer.buffer.Write(contents)
	return originalLength, nil
}

func (buffer *cappedBuffer) Bytes() []byte {
	return buffer.buffer.Bytes()
}

func (buffer *cappedBuffer) String() string {
	return buffer.buffer.String()
}

func (buffer *cappedBuffer) Exceeded() bool {
	return buffer.exceeded
}
