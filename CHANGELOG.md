# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.1] - YYYY-MM-DD

### Added
- GitHub Actions release workflow (T029): tag push (`v*`) で 3 経路を同時に release
  - npm publish (OIDC trusted publishing, `--provenance` 付き)
  - GitHub Release tarball (multi-arch binary, goreleaser)
  - homebrew-tap (`hummer98/homebrew-tap`) の formula 更新
- CHANGELOG.md を新設し、本 release から release notes の真のソースとする
- 以降の release はすべて CI 経由 (manual fallback は提供しない)

## [0.2.0] - 2026-05-XX

### Added
- 初回 npm publish (`@hummer98/ctxd-claude-plugin`)。bootstrap として local から手動 publish
- Go CLI binary (`ctxd`) の multi-arch 配布 (linux / darwin / windows × amd64 / arm64) を整備し、
  GitHub Release への upload + homebrew-tap (`hummer98/homebrew-tap`) 経由の配布を本 release から開始
- T010 完成版 Postcondition DSL を `skills/ctxd/SKILL.md` に反映 (T023, plugin v0.2.0)

### Tooling
- T027: `package.json` + `scripts/sync-package-version.sh` を新設 (npm publish の土台)
- T028: `.goreleaser.yaml` + homebrew-tap repo を整備 (Go CLI 配布の土台)
