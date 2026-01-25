# AGENTS.md

Guidance for agents working on this repo.

## Navigation
- **[Codebase Map](docs/CODEBASE_MAP.md)**: Detailed index mapping functionality to specific files and symbols. Use this to orient yourself.

## Git Workflow
- `main` is protected; do not commit directly.
- Work on feature branches and keep them short-lived.
- Rebase locally to keep 1-3 meaningful commits per PR.
- Merge to `main` with rebase/fast-forward (no merge commits).

## Conventional Commits
- Format: `type(scope?): subject`
- Types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `chore`, `build`, `ci`, `revert`
- Use `!` for breaking changes and include a body line explaining impact.
- Keep subjects imperative and under ~72 chars.

## Release Notes
- Release notes are generated from conventional commits via release-please.
- Workflow: `.github/workflows/release-please.yml`.
- Config: `release-please-config.json`, `release-please-manifest.json`.
- Merge the release PR to publish a new release and update `CHANGELOG.md`.

## Hooks (Local)
- Enable repo hooks: `git config core.hooksPath .githooks`
- `pre-commit` blocks direct commits on `main`.
- Use `ALLOW_MAIN_COMMIT=1` only for emergency fixes.
- `commit-msg` enforces conventional commit subjects.

## Changelog Entries
- Continue to add in-app changelog entries using `athena changelog add` when the daemon is running (see `CLAUDE.md`).
