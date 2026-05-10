# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/).

## [2.1.0] - 2026-05-10

### Added

- **OpenAI Responses API support** via new `api_format: openai-responses`. Parses typed SSE events (`response.output_text.delta`, `response.completed`) and the new top-level `input_tokens` / `output_tokens` / `total_tokens` usage shape.
- **Reasoning and cache token metrics**: `llm_probe_reasoning_tokens`, `llm_probe_cached_input_tokens`, `llm_probe_cache_creation_tokens`. Populated from `usage.completion_tokens_details.reasoning_tokens` (OpenAI Chat), `usage.output_tokens_details.reasoning_tokens` (Responses), `usageMetadata.thoughtsTokenCount` (Gemini), and `cache_creation_input_tokens` / `cache_read_input_tokens` (Anthropic).
- **Rate-limit remaining gauges** `llm_probe_rate_limit_remaining{kind="requests|tokens"}` parsed from `x-ratelimit-remaining-*` and `anthropic-ratelimit-*-remaining` response headers.
- **TLS cert expiry metric** `llm_probe_ssl_earliest_cert_expiry_timestamp_seconds` from `resp.TLS.PeerCertificates`.
- **`llm_exporter_build_info`** gauge with `version` / `git_commit` / `build_time` / `go_version` labels.
- **Multi-target `/probe?target=<url>&module=<name>` endpoint** (blackbox_exporter-style). New `modules:` section in config defines reusable probe profiles. Emits blackbox-compatible metric names (`probe_success`, `probe_duration_seconds`, `probe_ttft_seconds`, ...).
- **Native histograms**: duration / connect / TTFT histograms dual-emit classic + native (`NativeHistogramBucketFactor=1.1`).
- **Example Prometheus rules** in `example-rules.yml` (recording rules + alerts).
- **Project governance files**: `SECURITY.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `MAINTAINERS.md`, issue/PR templates, `.github/dependabot.yml`, `.golangci.yml`, golangci-lint + govulncheck CI workflows.
- Config example refreshed with 2026-05 flagship models (`gpt-5.5`, `claude-opus-4-7`, `gemini-3.1-pro`, `deepseek-v4-pro`) and commented entries for xAI Grok, Groq, Cerebras, Together, Fireworks, Moonshot/Kimi, Zhipu BigModel, SiliconFlow, Perplexity, Cohere.

### Changed

- **Duration histogram buckets extended** to cover reasoning models: `[0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 60, 120, 300, 600]`. TTFT extended to 120s.
- **`llm_probe_errors_total`** now carries an additional `status` label (HTTP status code, `0` for non-HTTP failures).
- **Error classification** refined: `http_4xx`, `http_5xx`, `overloaded` (529 / Anthropic `overloaded_error`), `quota_exceeded` (OpenAI `insufficient_quota`), `context_length`, `content_filter`, `tls_error`, `dns_error`, `connection_refused`. Response bodies on error are inspected for provider-specific codes.
- **Go toolchain** bumped from 1.22 to 1.23 (minimum supported version).
- `prometheus.NewGoCollector` / `NewProcessCollector` replaced with `collectors.New*Collector` (deprecation in client_golang).

### Fixed

- Anthropic prober now merges `usage` from both `message_start` and `message_delta` events, so `cache_creation_input_tokens` / `cache_read_input_tokens` are no longer dropped. `thinking_delta` / `signature_delta` content-block events are tolerated without breaking parsing.

## [2.0.5] - 2026-04-12

### Fixed

- Prevent Slowloris attack by setting ReadHeaderTimeout on HTTP server (CWE-400)

### Tests

- Add comprehensive test coverage for all prober implementations
  (anthropic, google, openai, azure, openai_compat)
- Add scheduler core tests (probe, Run/Stop/Reload lifecycle)
- Add metrics, version, and main HTTP handler tests
- Total coverage: 73.1%

### CI

- Add test coverage threshold (60%) to CI pipeline

## [2.0.4] - 2026-04-10

### Fixed

- Run Docker container as non-root user to prevent privilege escalation (CWE-250/CWE-269)

## [2.0.3] - 2026-04-10

### Other

- Add English README and move Chinese content to README_zh.md
- Add "100% AI-Coded" declaration to both README files

## [2.0.2] - 2026-04-09

### Other

- Migrate Go module path to github.com/oh-my-vibe-coding/llm-exporter
- Add MIT LICENSE file and enhance .gitignore
- Add GoReleaser config and GitHub Actions workflows (CI + Release)

## [2.0.1] - 2026-04-09

### Changed

- Strengthen config validation: reject empty targets, duplicate names,
  invalid api_format, conflicting prompt/prompts, negative durations,
  orphaned light probe and adaptive interval fields
- Merge OpenAI and Azure prober implementations into shared
  openaiCompatProber, reducing ~220 lines of duplication
- Extract status tracker and webhook alerter from scheduler into
  dedicated internal/status and internal/alerter packages

## [2.0.0] - 2025-05-18

### Added

- Azure OpenAI support (api_format: azure)
- Webhook alerting on consecutive probe failures
- Prompt rotation across multiple prompts
- Response validation via expect_pattern regex
- Light probe mode with full_probe_every
- Adaptive probe interval with backoff
- Streaming and non-streaming probe modes
- Connection timing metrics (DNS + TCP + TLS)
- Token generation rate metrics
- Hot config reload via SIGHUP, HTTP endpoint, and file watcher

## [1.0.0] - 2025-05-18

### Added

- Initial release: Prometheus exporter for LLM API monitoring
- OpenAI chat completions probing with streaming support
- TTFT, duration, token count, and success/failure metrics
- YAML configuration with environment variable expansion
