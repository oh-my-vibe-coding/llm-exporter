<!--
Thanks for the PR! A few things to help the review go smoothly:
- Keep the PR focused on one thing. Split refactors from features.
- If this is user-visible, add a CHANGELOG.md entry.
- Make sure `go test ./...` and `golangci-lint run ./...` pass locally.
-->

## What & why

<!-- One or two sentences. What does this change, and why? -->

## Type of change

- [ ] Bug fix
- [ ] New feature
- [ ] Breaking change (please call out in CHANGELOG)
- [ ] Documentation only
- [ ] Build / CI / tooling

## Test plan

<!-- What did you run? What did you verify? Paste the relevant command line and a short summary of the output. -->

- [ ] `go test ./...`
- [ ] `golangci-lint run ./...`
- [ ] Manual verification (describe below)

## Checklist

- [ ] I added or updated tests covering the behaviour changed.
- [ ] I updated `README.md` / `README_zh.md` / `DESIGN.md` if user-facing behaviour changed.
- [ ] I updated `CHANGELOG.md` if this is user-visible.
- [ ] No secrets or API keys are committed.
