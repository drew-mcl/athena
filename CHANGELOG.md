# Changelog

## [1.2.0](https://github.com/drew-mcl/athena/compare/athena-v1.1.0...athena-v1.2.0) (2026-02-16)


### Features

* agent archetypes for specialized workflows ([#79](https://github.com/drew-mcl/athena/issues/79)) ([7795ff7](https://github.com/drew-mcl/athena/commit/7795ff7b1bb4b49a6a0423b66f49db6c33d6514e))

## [1.1.0](https://github.com/drew-mcl/athena/compare/athena-v1.0.0...athena-v1.1.0) (2026-02-11)


### Features

* **cli:** add ath agent command for listing and inspecting agents ([#54](https://github.com/drew-mcl/athena/issues/54)) ([f755608](https://github.com/drew-mcl/athena/commit/f755608d363961f93de6180d8674cc84b0faf623))
* **cli:** add auto-linking for goal -&gt; feat -&gt; spawn workflow ([#61](https://github.com/drew-mcl/athena/issues/61)) ([a163253](https://github.com/drew-mcl/athena/commit/a163253120832483aaa17d18a7129194fb8cb11b))
* **daemon:** rate limit watcher + auto-run loop ([#62](https://github.com/drew-mcl/athena/issues/62)) ([cee084e](https://github.com/drew-mcl/athena/commit/cee084e7c55926783b9e272223a0291271ba192d))
* **hooks:** add ath enable/disable for Claude Code lifecycle hooks ([#63](https://github.com/drew-mcl/athena/issues/63)) ([430c9a4](https://github.com/drew-mcl/athena/commit/430c9a4c7a909befc3159cdacdb443a4d1386103))
* **queue:** add ath queue reconcile command ([#58](https://github.com/drew-mcl/athena/issues/58)) ([26c5433](https://github.com/drew-mcl/athena/commit/26c5433e266e16de2934ba19afba932738ba3e4f))
* **tasks:** blocked/completed styling and persist on cleanup ([#59](https://github.com/drew-mcl/athena/issues/59)) ([2a1776f](https://github.com/drew-mcl/athena/commit/2a1776f8c2ff5c8702e670b4c1dde18d7484a81a))


### Bug Fixes

* **store:** handle NULL completed count for items with no descendants ([#60](https://github.com/drew-mcl/athena/issues/60)) ([716eeb3](https://github.com/drew-mcl/athena/commit/716eeb3bed90576a3459330a58c490b5ad839a22))
* **tasks:** rewrite claude provider for directory-per-list format ([#57](https://github.com/drew-mcl/athena/issues/57)) ([62fdd4e](https://github.com/drew-mcl/athena/commit/62fdd4ecd5aa702dac34a5ef94a1e3d9a3ad4a00))

## [1.0.0](https://github.com/drew-mcl/athena/compare/athena-v0.6.0...athena-v1.0.0) (2026-02-07)


### ⚠ BREAKING CHANGES

* The Bubble Tea TUI dashboard has been removed. Athena is now a CLI-first tool (`ath`) that wraps Claude Code's native orchestration. The daemon API, work item model, and plugin interfaces have been redesigned around spawn-based workflows with worktrees, merge queue, and PM integration.

### v1.0

* unified spawn, plugins, cleanup ([#51](https://github.com/drew-mcl/athena/issues/51)) ([dc024a0](https://github.com/drew-mcl/athena/commit/dc024a0a7b81cbe586de231efa632ecb0e167ea5))


### Features

* **cli:** add ath CLI with beautiful output styling ([#43](https://github.com/drew-mcl/athena/issues/43)) ([f2472ad](https://github.com/drew-mcl/athena/commit/f2472ad5c1d4353eab4c2cf5163128da96280770))
* **cli:** extract styles, add git status caching and work items API ([#45](https://github.com/drew-mcl/athena/issues/45)) ([71eda8e](https://github.com/drew-mcl/athena/commit/71eda8ef4fd52a9f563c95b5cca2198ba4795e18))
* **queue:** add merge queue system with plugin architecture ([#47](https://github.com/drew-mcl/athena/issues/47)) ([473780d](https://github.com/drew-mcl/athena/commit/473780dc26cf3b8fe94e5fddb8bd0f35c1acd9c8))
* **queue:** improve merge queue with queue head and worktree integration ([#50](https://github.com/drew-mcl/athena/issues/50)) ([2771897](https://github.com/drew-mcl/athena/commit/2771897300f440d824aee3c1c028a71faaf0df3b))
* **wt:** add comprehensive worktree pruning ([#46](https://github.com/drew-mcl/athena/issues/46)) ([8a8a1db](https://github.com/drew-mcl/athena/commit/8a8a1db14e446e7ac9864bee2a9dcfc1f46203d9))

## [0.6.0](https://github.com/drew-mcl/athena/compare/athena-v0.5.0...athena-v0.6.0) (2026-01-27)


### Features

* gemini persistence & context optimization ([#34](https://github.com/drew-mcl/athena/issues/34)) ([7ee8814](https://github.com/drew-mcl/athena/commit/7ee8814ef5fa371345965502e1c4e9ba2376507f))
* **gemini:** functional Gemini runner with tools ([#32](https://github.com/drew-mcl/athena/issues/32)) ([154d345](https://github.com/drew-mcl/athena/commit/154d3452c62f91924c30dbf5c9fff4fc92ddf836))
* **tui:** comprehensive UX overhaul with improved layouts and styling ([#35](https://github.com/drew-mcl/athena/issues/35)) ([aed5b38](https://github.com/drew-mcl/athena/commit/aed5b38a46c8b5db85b42f2d20ef48b845a3d520))


### Bug Fixes

* **tui:** optimize dashboard plan view and layout ([#37](https://github.com/drew-mcl/athena/issues/37)) ([29a3968](https://github.com/drew-mcl/athena/commit/29a3968a10701998b491dda6cfdbec3b202db092))

## [0.5.0](https://github.com/drew-mcl/athena/compare/athena-v0.4.0...athena-v0.5.0) (2026-01-25)


### Features

* add CLI format flags and cache hit rate tracking ([1144e5f](https://github.com/drew-mcl/athena/commit/1144e5f23b6e2c9c436c8bac6126becbf7d61640))
* **cli:** add athena-cli for spawning agents and jobs ([a5923b7](https://github.com/drew-mcl/athena/commit/a5923b75f07665e57be0ad57ca3dea0122b71f6a))
* **context:** add code index and context optimization system ([f8a47d4](https://github.com/drew-mcl/athena/commit/f8a47d4091dd70f811012dae80b22dc18d68f211))
* **context:** increase default context budget and make it configurable ([d0f1cb2](https://github.com/drew-mcl/athena/commit/d0f1cb2472403e029af6ecb99b5459ee596c72aa))
* **tasks:** integrate Claude Code Tasks into Athena dashboard ([7083d2a](https://github.com/drew-mcl/athena/commit/7083d2a253595488fcc0338ae93fc8f6c6910185))


### Bug Fixes

* **tasks:** address code review findings ([26300c5](https://github.com/drew-mcl/athena/commit/26300c52cc5f1d9fd2f1b86e6724be4c63b579c9))
* **tui:** add scrolling to detail views ([c8bb1af](https://github.com/drew-mcl/athena/commit/c8bb1af8ac6fcb63ea3873388b4de0ad3b86e5db))
* **tui:** improve detail view scrolling and footer ([938ceb9](https://github.com/drew-mcl/athena/commit/938ceb98ae3dc16ef6bba1dd2ebe8b400b55daf7))

## [0.4.0](https://github.com/drew-mcl/athena/compare/athena-v0.3.0...athena-v0.4.0) (2026-01-20)


### Features

* **agent:** auto-publish PRs when executor completes in automatic mode ([17ee1dc](https://github.com/drew-mcl/athena/commit/17ee1dc621643e629f01912c6dca4162229425b7))
* **tui:** add admin monitoring tab and fix context truncation ([a78067b](https://github.com/drew-mcl/athena/commit/a78067b60c01bd27bfc8032bbb292430d1589a6a))
* **tui:** polish admin view and add debug logging ([8f713f4](https://github.com/drew-mcl/athena/commit/8f713f491adcab812d445f7ffceb295628d4a611))
* **viz:** add real-time data flow visualization tool ([ffcdd8a](https://github.com/drew-mcl/athena/commit/ffcdd8affa65b357b722cb97c40503988fe71685))
* **viz:** connect agent activity events to stream ([f68bc1f](https://github.com/drew-mcl/athena/commit/f68bc1ff43e16472a2b623e91272d63e593a4771))


### Bug Fixes

* **config:** add default archetypes and require executor commits ([e8de8e4](https://github.com/drew-mcl/athena/commit/e8de8e4eaf7213a0d76f57786a4df77a7f1bfb50))
* **tui:** formatting fixes and validation improvements ([dbc260b](https://github.com/drew-mcl/athena/commit/dbc260b265db72f9bf9e6d55077ac897fd28811f))

## [0.3.0](https://github.com/drew-mcl/athena/compare/athena-v0.2.2...athena-v0.3.0) (2026-01-19)


### Features

* gemini integration and agent runner abstraction ([7da6003](https://github.com/drew-mcl/athena/commit/7da60034aefbeb8dfada4e10cecd9c432377c913))
* implement comprehensive bug fixes ([ef235a1](https://github.com/drew-mcl/athena/commit/ef235a1f0e509a1eea5dae62b5d19f2f84c1d012))
* initial gemini config and runner stub ([1608ee2](https://github.com/drew-mcl/athena/commit/1608ee23c64b1dc5fca423441d0eec2b2492402d))
* **tui:** polish dashboard and add provider selection ([e231c1a](https://github.com/drew-mcl/athena/commit/e231c1a5157719d9ee682f92cc3474b797d40ca1))


### Bug Fixes

* **executil:** allow group-writable dirs on macOS for Homebrew ([68366f9](https://github.com/drew-mcl/athena/commit/68366f91541c9c5d64d1b909fcc8fcfdb294a753))
* **identity:** inject git identity env vars correctly ([e74617a](https://github.com/drew-mcl/athena/commit/e74617a72b31426632eabfd7eb1b15f5c5395501))
* **sonar:** various sonar fixes ([c66fdac](https://github.com/drew-mcl/athena/commit/c66fdac0a5681055c1d2b01ba2c7eb16cd745346))
* **test:** update spawner test for buildRunSpec rename ([3bb5cb4](https://github.com/drew-mcl/athena/commit/3bb5cb4b5e8aad89d3a7906e9e9b972eb94dd2d1))

## [0.2.2](https://github.com/drew-mcl/athena/compare/athena-v0.2.1...athena-v0.2.2) (2026-01-19)


### Bug Fixes

* **agent:** implement execution logic for feature/merge jobs ([526167f](https://github.com/drew-mcl/athena/commit/526167f4482a750e45a6885e477fcc90232767f5))

## [0.2.1](https://github.com/drew-mcl/athena/compare/athena-v0.2.0...athena-v0.2.1) (2026-01-19)


### Bug Fixes

* **agent:** implement execution logic for feature/merge jobs ([#7](https://github.com/drew-mcl/athena/issues/7)) ([368e7e1](https://github.com/drew-mcl/athena/commit/368e7e178dab7a7216fda4cc1e115ad30cc9c035))

## [0.2.0](https://github.com/drew-mcl/athena/compare/athena-v0.1.0...athena-v0.2.0) (2026-01-19)


### Features

* **context:** add shared agent memory system ([91c83b6](https://github.com/drew-mcl/athena/commit/91c83b6b9d1d9bc675107717088b07e66d2bd25a))
* **core:** add docs, message store, and eventlog backend ([3553ddd](https://github.com/drew-mcl/athena/commit/3553ddd1cd9cd9d54ad3dbf88271af7c12587697))
* **core:** add runner abstraction, data plane, and event sourcing ([a185145](https://github.com/drew-mcl/athena/commit/a185145e8bc8e65774424e0ee1e8e40f4cf05776))
* **core:** init of poc ([ee9fdfc](https://github.com/drew-mcl/athena/commit/ee9fdfcf81cd819ac058ee4b84c88c8e03527b98))
* **merge:** auto-spawn resolver agent on conflicts ([bf56bc9](https://github.com/drew-mcl/athena/commit/bf56bc93afe537fadfd8aecff30f27ffd2d99bf9))
* **mvp:** mvp build out ([f59409d](https://github.com/drew-mcl/athena/commit/f59409d95d33155bc06993a3e6a2d4d4a640b797))
* **plans:** add plan viewer and executor spawning ([eb740a6](https://github.com/drew-mcl/athena/commit/eb740a6ca29e2a5fb89376a14e14afefeafdb7e6))
* **publish:** add merge/publish workflow for worktrees ([54d220b](https://github.com/drew-mcl/athena/commit/54d220b13b0c3b3580bed13594c310a11edf7626))
* **store:** add DeleteAgentCascade for proper foreign key handling ([a95cea4](https://github.com/drew-mcl/athena/commit/a95cea4ed4bf3fbb36279db74c536f21459cd117))
* **store:** add message store ([969d83c](https://github.com/drew-mcl/athena/commit/969d83cee8037b5413af45ed5d195eff582de5f1))
* **tasks:** track merge resolver as task ([2ff5910](https://github.com/drew-mcl/athena/commit/2ff591049c1a79a8fa64ecfc402a2d6dc9d4fa94))
* **tui:** add quick approve/execute workflow for planners ([811a0e1](https://github.com/drew-mcl/athena/commit/811a0e1aa4d9d570bce731a74f09412cd430a5c5))
* **tui:** complete UX overhaul with new dashboard structure ([#1](https://github.com/drew-mcl/athena/issues/1)) ([5c22373](https://github.com/drew-mcl/athena/commit/5c22373bdf5f3e3b77beef5c979da0c677507460))
* **tui:** expand dashboard model ([71d749f](https://github.com/drew-mcl/athena/commit/71d749fa706362ffdf838e06d040beeb3c32b641))
* **ui:** various ui fixes to improve workflows ([def48bd](https://github.com/drew-mcl/athena/commit/def48bdef3cae587ca96c56409a6b0df8813e783))


### Bug Fixes

* **agent:** embed plan content in auto-spawned executor prompts ([a69afa1](https://github.com/drew-mcl/athena/commit/a69afa18441f9c31df1d13ae73df5f5441fea32d))
* **tui:** add error display with auto-dismiss in dashboard ([a034410](https://github.com/drew-mcl/athena/commit/a034410b83d35b67eeae492b2977141083ca7c8c))
* **tui:** prevent note truncation, fix n key textInput reset ([b249a73](https://github.com/drew-mcl/athena/commit/b249a7364dd61c38458b3f2e637d12eb44b2e68a))
* **worktree:** handle prunable and invalid worktrees gracefully ([7ae75f5](https://github.com/drew-mcl/athena/commit/7ae75f5eda44753ebd566e8d128544a8dca44a63))

## Changelog

Release notes are generated by release-please from conventional commits.
