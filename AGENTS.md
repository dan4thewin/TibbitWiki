# AGENTS.md — Agent Instructions

## Project

TibbitWiki is a Go replacement for TiddlyWiki's Node.js server.
Single HTML+JS client embedded via go:embed. Server-side markdown
rendering (goldmark + chroma). REST API (18+ endpoints). Multi-wiki
via subdirectory auto-discovery. RCS version control. Single-user
design — no auth, no concurrent-writer resolution.

DG maintains this project. The AI implements under his direction.

## Build and test

    mk                  # build (Plan 9 mk, Go port)
    mk test             # unit tests (needs -gcflags=all=-l for gomonkey)
    mk check            # lint + test + complexity
    mk coverage         # statement coverage report

All 11 internal/ packages are at 100% statement coverage.
The pre-commit hook enforces this — don't let it regress.

## The mistakes you will make

### 1. gomonkey + coverprofile SIGBUS

On darwin/arm64, gomonkey binary patching causes SIGBUS under
`-coverprofile`. Use package-level function variables instead
(see `newFsWatcher` in watcher.go, `searchFunc` in wiki.go).
If adding new mocks that SIGBUS under coverage, convert them
the same way.

### 2. Safari caches error pages

If the server dies and Safari shows "Can't Connect", restarting
the server isn't enough. You must navigate Safari to the URL again.

### 3. Poll timing after changes

After triggering a change, wait at least 5-6 seconds: ~1s for the
watcher to fire, up to 3s for the next poll cycle, plus render time.
Tests that check too early will flake.

### 4. Spaces in note titles

File paths with spaces are handled correctly by the editor command
template. But if you ever change the splitting logic, test with a
title like "21st March 2026".

### 5. You will theorize when you should test

Build the smallest repro. Check coverage. Read TESTING.md. The
adversarial test audit (see below) found 8 production bugs that
100% statement coverage missed — hollow assertions, live pointer
returns, missing adversarial inputs. Don't trust green tests blindly.

## Code style

DRY helpers with CC ≤ half the project average, but balance DRY
with clarity. Flat switches are acceptable above the CC=12 threshold.
Clarity over DRY in tests — each test self-contained and obvious.

## Adversarial test audit

An audit using 5 LLM models (50 runs) found that 100% statement
coverage hides significant gaps: 8 production bugs, 8 security
findings, 270 total findings with 0 false alarms. The full lab
notebook is `AdversarialTestAudit` in the hold wiki. The prompt
template and model comparison are in TESTING.md.

## Attribution

Code in this project is AI-generated under DG's direction. Make
this clear in commit messages and docs — it matters for open source
trust.
