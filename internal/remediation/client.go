package remediation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/agentprocess"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/explanation"
)

const (
	maxRequestBytes  = 8 << 20
	maxResponseBytes = 2 << 20
)

type CommandClient struct {
	Path    string
	Args    []string
	Timeout time.Duration
}

func (client CommandClient) Propose(ctx context.Context, request Request) (Response, error) {
	if err := validateRequest(request); err != nil {
		return Response{}, err
	}
	contents, err := json.Marshal(request)
	if err != nil {
		return Response{}, fmt.Errorf("encode AI remediation request: %w", err)
	}
	output, err := (agentprocess.Command{
		Path:           client.Path,
		Args:           client.Args,
		Timeout:        client.Timeout,
		MaxInputBytes:  maxRequestBytes,
		MaxOutputBytes: maxResponseBytes,
		Sanitize:       explanation.Redact,
	}).Run(ctx, contents)
	if err != nil {
		return Response{}, err
	}
	response, err := decodeResponse(bytes.NewReader(output))
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
		return Response{}, fmt.Errorf("decode AI remediation response: %w", err)
	}
	var additional any
	if err := decoder.Decode(&additional); err == nil {
		return Response{}, fmt.Errorf("AI remediation response must contain exactly one JSON value")
	} else if !errors.Is(err, io.EOF) {
		return Response{}, fmt.Errorf("decode trailing AI remediation response: %w", err)
	}
	return response, nil
}
