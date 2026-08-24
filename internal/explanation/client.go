package explanation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

const (
	defaultProviderTimeout = 30 * time.Second
	maxRequestBytes        = 8 << 20
	maxResponseBytes       = 1 << 20
	maxProviderErrorBytes  = 64 << 10
)

// Client is the provider-neutral read-only explanation interface.
type Client interface {
	Explain(context.Context, Request) (Response, error)
}

// CommandClient exchanges one JSON request and response with a local
// executable. It invokes the executable directly and never through a shell.
type CommandClient struct {
	Path    string
	Args    []string
	Timeout time.Duration
}

func (client CommandClient) Explain(ctx context.Context, request Request) (Response, error) {
	if strings.TrimSpace(client.Path) == "" {
		return Response{}, fmt.Errorf("AI provider command is required")
	}
	if err := validateRequest(request); err != nil {
		return Response{}, err
	}
	contents, err := json.Marshal(request)
	if err != nil {
		return Response{}, fmt.Errorf("encode AI explanation request: %w", err)
	}
	if len(contents) > maxRequestBytes {
		return Response{}, fmt.Errorf("AI explanation request exceeds the %d-byte limit", maxRequestBytes)
	}

	timeout := client.Timeout
	if timeout <= 0 {
		timeout = defaultProviderTimeout
	}
	providerContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(providerContext, client.Path, client.Args...)
	command.Stdin = bytes.NewReader(contents)
	stdout := newCappedBuffer(maxResponseBytes)
	stderr := newCappedBuffer(maxProviderErrorBytes)
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		if errors.Is(providerContext.Err(), context.DeadlineExceeded) {
			return Response{}, fmt.Errorf("AI provider timed out after %s", timeout)
		}
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return Response{}, fmt.Errorf("AI provider failed: %w: %s", err, Redact(message))
		}
		return Response{}, fmt.Errorf("AI provider failed: %w", err)
	}
	if stdout.Exceeded() {
		return Response{}, fmt.Errorf("AI provider response exceeds the %d-byte limit", maxResponseBytes)
	}
	if stderr.Exceeded() {
		return Response{}, fmt.Errorf("AI provider error output exceeds the %d-byte limit", maxProviderErrorBytes)
	}

	response, err := decodeResponse(bytes.NewReader(stdout.Bytes()))
	if err != nil {
		return Response{}, err
	}
	if err := validateResponse(response, request); err != nil {
		return Response{}, err
	}
	return response, nil
}

func decodeResponse(reader io.Reader) (Response, error) {
	decoder := json.NewDecoder(io.LimitReader(reader, maxResponseBytes+1))
	decoder.DisallowUnknownFields()
	var response Response
	if err := decoder.Decode(&response); err != nil {
		return Response{}, fmt.Errorf("decode AI provider response: %w", err)
	}
	var additional any
	if err := decoder.Decode(&additional); err == nil {
		return Response{}, fmt.Errorf("AI provider response must contain exactly one JSON value")
	} else if !errors.Is(err, io.EOF) {
		return Response{}, fmt.Errorf("decode trailing AI provider response: %w", err)
	}
	return response, nil
}

func validateRequest(request Request) error {
	if request.SchemaVersion != RequestSchemaVersion || request.Task != TaskReadOnlyExplain {
		return fmt.Errorf("invalid AI explanation request protocol")
	}
	if strings.TrimSpace(request.Question) == "" {
		return fmt.Errorf("AI explanation question is required")
	}
	return nil
}

func validateResponse(response Response, request Request) error {
	if response.SchemaVersion != ResponseSchemaVersion {
		return fmt.Errorf("AI provider response schemaVersion must be %q", ResponseSchemaVersion)
	}
	if strings.TrimSpace(response.Answer) == "" {
		return fmt.Errorf("AI provider response answer is required")
	}
	if len(response.Answer) > 64<<10 {
		return fmt.Errorf("AI provider response answer exceeds 65536 bytes")
	}
	if len(response.Priorities) > 256 || len(response.Limitations) > 256 {
		return fmt.Errorf("AI provider response contains too many list items")
	}
	knownConsumers := make(map[string]struct{}, len(request.Findings))
	for _, finding := range request.Findings {
		knownConsumers[finding.Consumer.ID] = struct{}{}
	}
	seenOrders := make(map[int]struct{}, len(response.Priorities))
	for index, priority := range response.Priorities {
		if priority.Order <= 0 {
			return fmt.Errorf("AI provider priority %d order must be positive", index)
		}
		if _, exists := seenOrders[priority.Order]; exists {
			return fmt.Errorf("AI provider priority order %d is duplicated", priority.Order)
		}
		seenOrders[priority.Order] = struct{}{}
		if _, exists := knownConsumers[priority.ConsumerID]; !exists {
			return fmt.Errorf("AI provider priority references unknown consumer %q", priority.ConsumerID)
		}
		if strings.TrimSpace(priority.Action) == "" || strings.TrimSpace(priority.Rationale) == "" {
			return fmt.Errorf("AI provider priority %d requires action and rationale", index)
		}
		if len(priority.Action) > 4096 || len(priority.Rationale) > 4096 {
			return fmt.Errorf("AI provider priority %d text exceeds 4096 bytes", index)
		}
	}
	for index, limitation := range response.Limitations {
		if strings.TrimSpace(limitation) == "" || len(limitation) > 4096 {
			return fmt.Errorf("AI provider limitation %d is invalid", index)
		}
	}
	return nil
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
