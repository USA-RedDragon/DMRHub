# Rules for every DMRHub lane (binding)

Environment: cloud container, 4 cores, 15 GB RAM. Repo main checkout: /home/user/DMRHub (do not touch it). Your worktree is given in your prompt; work only there, on its lane/<name> branch. Base commit 7ae8bc25 (origin/main, contains the dmrgo v2.0.0 port and all lane work A-E).

- Env for every Go command: `export GOCACHE=/home/user/work/gocache GOTMPDIR=/home/user/work/gotmp`. Prefix heavy commands with `nice`. Never use /tmp for builds or data. Use -p 2 for package test runs; other lanes share the 4 cores. Do not run the whole `go test ./...` suite; run the packages you touch plus their dependents. The coordinator runs the full suite.
- Docker is running (postgres:16-alpine and redis:7-alpine are pulled) so the Postgres/Redis test variants work.
- The frontend dist is already copied into your worktree so `go build` works. Lanes that edit the frontend run npm in their worktree's internal/http/frontend.
- Root cause first, proven by observation. Mark inference vs confirmed fact. No band-aids.
- Every behavior change needs a test that fails before the fix and passes after. Show both runs in your report.
- Never panic in production code.
- No code comments except exported doc comments, directives, and comments on genuinely confusing details. Match the surrounding style.
- One commit per logical change. Short subjects in the repo's style (`area: what it does`, lower case, e.g. `hub: link dynamic talkgroups only for repeaters`). Short bodies. No merge commits. Owner rule: every commit is authored AND committed by Jacob McSwain <jacob@mcswain.dev> (the repo git config is set to that; do not override it). NO Co-Authored-By lines, NO Claude attribution, NO session IDs or links in commit messages.
- Never push, never touch deployment config, never contact any public DMR network (BrandMeister, TGIF, FreeDMR public, any public master). Loopback only.
- Before you finish: `go build ./...`, `go vet ./...`, `golangci-lint run` (0 issues) in your worktree, and the tests of the packages you touched with -race. Leave `git status` clean (commit or remove everything) and kill every process you started.
- Compression, if any: zstd.
- Report back: commits (hash + subject), root cause with evidence, the failing-then-passing test output, what you ran and its results, open questions for the owner. Plain prose, no em-dashes.
- The owner's reference docs are in /home/user/work/refs/dmrhub-refs (PLAN.md: design, section 6 tests, section 8 decisions; BRIEF.md: owner decisions and UI rules, which override PLAN; messages-API.md; dmrhub-mockups.html, the approved UI; dmrhub-all-traffic.md, dated owner decisions; hbrp-vs-mmdvm.md; ipsc-rate-data-capture.md). Interop run notes: /home/user/work/refs/interop-results/*-RESULTS.md. Paths in them under /home/reddragon refer to the owner's machine and do not exist here. If a decision truly needs the owner, stop at that point and report the question with options and pros/cons instead of guessing.
- Generated msgp code (*_gen.go) is gitignored; internal/config/config_configulator.go is tracked and must be regenerated and committed when config fields change. It has been generated into every worktree. If you add a msgp type or config field, rerun `go generate ./internal/db/... ./internal/config/...`.
- No polling or progress-check loops. Run one background command and wait for its notification.
- golangci-lint: the installed /usr/local/bin one is too old for go 1.27.3. Use /home/user/work/gobin/golangci-lint (v2.15.0) instead.
