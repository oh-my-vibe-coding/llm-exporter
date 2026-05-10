# Maintainers

Maintainers triage issues, review PRs, and cut releases.

## Current maintainers

- `@oh-my-vibe-coding` — project owner.

_Maintainership is intentionally narrow while the project is young._

## Becoming a maintainer

Consistent, high-quality contributions over several months (code, review,
triage) are the path in. If you're interested, open a discussion thread or
ping the current maintainers in a PR.

## Release process

1. Update `CHANGELOG.md` with the new version and date.
2. Bump references to the version in `README.md` if pinned anywhere.
3. `git tag vX.Y.Z && git push --tags` — the `release.yml` workflow runs
   goreleaser and publishes the GitHub release with multi-arch archives.
4. Announce in `Discussions` or the README badge list if the release includes
   user-visible changes.
