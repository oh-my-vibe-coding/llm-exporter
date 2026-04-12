# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/).

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
