## Problem

<!-- Link the issue and explain the user/safety problem. -->

Closes #

## Contract and implementation

<!-- Describe important inputs, outputs, evidence, and failure behavior. -->

## Verification

<!-- List exact commands and fixtures used. -->

- [ ] `go test ./...`
- [ ] `go vet ./...`
- [ ] `go test -race ./...` when concurrency or the safety core changed
- [ ] Goldens were reviewed field-by-field, not refreshed blindly

## Safety and compatibility

- [ ] The deterministic evaluator remains authoritative
- [ ] Parse/source/adapter failures cannot be interpreted as absence
- [ ] Evidence and source provenance are preserved
- [ ] Public schema or exit-code changes are versioned and documented
- [ ] No credentials, private telemetry, or generated secrets are committed

## Documentation

- [ ] User-facing behavior and limitations are documented, or not applicable
