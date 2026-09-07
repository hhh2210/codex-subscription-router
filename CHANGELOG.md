# Changelog

All notable changes follow [Keep a Changelog](https://keepachangelog.com/) and
this project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- One-command installer with safe source updates, prerequisite checks, signed
  rebuilds, recoverable upgrades, and automatic launch.
- Reset-aware routing that prioritizes weekly quota at risk of expiring and
  gives a bounded boost to subscriptions with banked usage resets.
- Subscription removal from the native account menu: manage mode selects a
  subscription, a second confirmation step spells out the consequences, and the
  isolated account data is moved into a timestamped local backup.
- Native `auth.json` import for secondary ChatGPT subscriptions, with strict
  shape validation, duplicate-account protection, private atomic writes, and
  first-start app-server verification.

### Fixed

- Ad-hoc and non-OpenAI signatures no longer keep `aps-environment`, which
  made AMFI kill the copied app at launch. First install now falls back to
  ad-hoc when no signing certificate is present.
- Empty `patch_arguments` expansion in `install.sh` under bash 3.2 `set -u`.

## [0.1.0] - 2026-08-15

### Added

- Multi-subscription routing with quota-aware balancing and sticky threads.
- Account isolation, device-code sign-in, pooled usage, and quota failover.
- Native account menu, masked emails, plan labels, and profile photos.
- Combined Profile statistics with per-account selection.
- Account-scoped Apps and MCP connection state in Settings → Plugins.
- Per-account rate-limit reset selection and pooled depletion handling.
- Independently signed Appshots and Computer Use support.
- Fail-closed upstream compatibility checks and deepest-first nested helper signing.
- Loopback-only, token-authenticated diagnostic UI states.
- Source-only CI, draft release automation, security documentation, and smoke tests.

[Unreleased]: https://github.com/b-nnett/codex-subscription-router/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/b-nnett/codex-subscription-router/releases/tag/v0.1.0
