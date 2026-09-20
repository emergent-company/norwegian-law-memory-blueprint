# End-to-end tests

Standalone functional tests for the Norwegian Law blueprint. They exercise the
ingested knowledge graph and the `norwegian-law-assistant` agent against a live
Memory server using the Memory SDK.

The suite is **safe to run without credentials**: every server-touching test
skips when the server URL or API token is missing. Only `TestAgentIsReadOnly` is
fully offline and always runs.

## Environment variables

| Var | Fallback | Purpose |
|---|---|---|
| `TEST_SERVER_URL` | `MEMORY_SERVER_URL` | Memory server base URL |
| `TEST_API_TOKEN` | `MEMORY_PROJECT_API_KEY` | Project API token (`emt_…`) |
| `TEST_PROJECT_ID` | — | Project ID, required only for the install test |
| `LAW_E2E_INSTALL` | — | Set to `1` to run the mutating install test (default off) |
| `MEMORY_CLI` | `memory` | Path to the memory CLI binary |
| `LAW_BLUEPRINT_DIR` | repo root (derived from test file location) | Blueprint directory |

## Running against dev

```bash
export TEST_SERVER_URL="https://api.dev.emergent-company.ai"
export TEST_API_TOKEN="emt_…"

cd /root/norwegian-law-memory-blueprint
PATH="/root/go/bin:$PATH" go test ./tests/e2e/ -v -count=1 -timeout 30m
```

Or via Task:

```bash
PATH="/root/go/bin:$PATH" task e2e
```

## Install test

The install test applies the blueprint to a project with
`memory blueprints install`. It is mutating and opt-in only:

```bash
export TEST_SERVER_URL="https://api.dev.emergent-company.ai"
export TEST_API_TOKEN="emt_…"
export TEST_PROJECT_ID="<project-id>"
export LAW_E2E_INSTALL=1

PATH="/root/go/bin:$PATH" task e2e:install
```

`task e2e:install` runs the install step first, then the full suite.

## Compile-only check

```bash
PATH="/root/go/bin:$PATH" task e2e:compile
```

## Without credentials

```bash
PATH="/root/go/bin:$PATH" go test ./tests/e2e/ -v -count=1
```

Server-touching tests report `SKIP`; the package exits `0` and
`TestAgentIsReadOnly` still passes.
