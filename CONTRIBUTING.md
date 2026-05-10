# Contributing to llm-exporter

Thanks for your interest in improving llm-exporter! This document covers the
essentials — code style, testing, and the PR workflow.

## Getting started

```bash
git clone https://github.com/oh-my-vibe-coding/llm-exporter.git
cd llm-exporter
go build ./...
go test ./...
```

The project targets **Go 1.23+** and has three direct dependencies
(`prometheus/client_golang`, `gopkg.in/yaml.v3`, `fsnotify/fsnotify`). Keep
that list short — we do not ship LLM SDKs.

## Running locally

```bash
cp config.example.yaml config.yaml
# edit config.yaml: at minimum set one API key via ${ENV_VAR}
export OPENAI_API_KEY=sk-...
make run
# then: curl http://localhost:9101/metrics
```

## Tests

- Run the full suite with `go test ./...`.
- Coverage gate in CI is **60% minimum** across the project — keep or raise
  it, never lower.
- Add unit tests for any new prober, new error classification branch, or new
  metric. Use `httptest.NewServer` with canned SSE bodies; real provider
  calls must never run in CI.
- Table-driven tests are preferred for classification helpers.

## Linting

```bash
# Install the same version CI uses:
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
golangci-lint run ./...

# Vulnerability check:
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

The enabled linter set lives in `.golangci.yml`. PRs that disable linters or
broaden the ignore list need justification in the PR description.

## Commit style

We use a lightweight Conventional Commits flavor:

```
feat(prober): add openai-responses api_format
fix(anthropic): merge usage from message_start and message_delta
chore(ci): bump golangci-lint to v1.64.8
docs(readme): document /probe endpoint
test(prober): cover cached_tokens parsing
```

Scope is optional but encouraged. Avoid trailing periods in the subject line.

## Opening a PR

1. Fork, branch from `main`.
2. Keep PRs focused — one feature or one fix per PR.
3. Include tests. If a change is not covered, explain why in the description.
4. Run `go test ./...` and `golangci-lint run ./...` locally.
5. Fill out the PR template honestly — it's there to help reviewers.

## Adding a new provider

Most new providers are OpenAI-compatible. Prefer this order:

1. Add an example entry (commented out) in `config.example.yaml`.
2. If the provider uses OpenAI's wire format verbatim: no code change needed.
3. If it uses a distinct protocol (SigV4, different SSE framing): implement a
   new prober under `internal/prober/<provider>.go`, register it in
   `prober.New()`, add `<provider>` to `validAPIFormats`, and cover it with
   tests.
4. Document it in `README.md` / `README_zh.md`.

## Release process

Maintainers only. See `MAINTAINERS.md`.
