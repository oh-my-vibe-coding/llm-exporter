# Security Policy

## Supported versions

The `main` branch and the latest tagged release receive security fixes. Older
tags are not patched.

## Reporting a vulnerability

**Please do not open public GitHub issues for security vulnerabilities.**

Use GitHub's private vulnerability reporting instead:

1. Open <https://github.com/oh-my-vibe-coding/llm-exporter/security/advisories/new>
2. Describe the issue (affected versions, reproduction steps, impact).
3. If you have a proposed fix, include it in the advisory — we will credit you
   in the release notes unless you prefer to remain anonymous.

We aim to:

- Acknowledge your report within 3 business days.
- Issue a fix and coordinated disclosure within 30 days for high-severity
  issues, longer for issues requiring deeper redesign.

## Scope

In scope:

- Code execution, authentication bypass, privilege escalation on the exporter
  process or its HTTP endpoints.
- Information disclosure (leaking API keys, tokens, or probe payloads).
- Supply-chain or build-time integrity issues affecting release artifacts.

Out of scope:

- Misconfiguration of the exporter (e.g. committing `api_key` to source
  control). These are user responsibilities.
- Vulnerabilities in LLM provider APIs themselves — please report those to
  the provider.
- DoS via unbounded probe intervals or malformed upstream SSE streams. We
  accept hardening PRs but do not treat these as embargoed.
