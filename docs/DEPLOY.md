# Deploy runbook — Norwegian Law Assistant

Target: **dev instance** — account `m+test@kucharz.net`, org "Maciej Kucharz's Org".
Coordinates in §5. **Not yet deployed.**

## 0. Preconditions

- A reachable Memory server and a project under the target account.
- `memory` CLI logged in / configured for that server, or `--server` passed explicitly.
- Enough time + budget: applying the seed creates ~101k objects and ~122k
  relationships and may trigger embedding cost on the target instance.

## 1. Source

Local, prepared: `/root/norwegian-law-memory-blueprint`, branch `feat/agent-and-seed`
(commit `0d31d9c`). Not pushed yet — can be installed from disk as-is, or pushed
and installed from the GitHub URL.

## 2. Validate (offline, no API calls)

```bash
memory blueprints validate ./norwegian-law-memory-blueprint
memory blueprints inspect  ./norwegian-law-memory-blueprint   # preview contents
```

Expected: 1 pack, 7 object types, 13 relationship types, 1 agent,
101,207 seed objects, 122,260 seed relationships.

## 3. Apply

```bash
# Full blueprint: schema + agent + seed
memory blueprints install ./norwegian-law-memory-blueprint \
  --project <project-id> \
  --server  <server-url>

# Concrete dev target:
#   --server http://emergent-dev:3002
#   --project f131d865-5d9c-43ae-834f-bcb749c3359d   # "test" project
```

Re-runs are idempotent by object `key`; existing keys are skipped unless
`--upgrade` is passed.

### Staged rollout (recommended for a first run)

If the instance should not take the whole corpus in one shot:

1. Apply schema + agent only — temporarily move `seed/` aside:
   ```bash
   mv seed /tmp/nl-seed-hold
   memory blueprints install ./norwegian-law-memory-blueprint --project <id> --server <url>
   mv /tmp/nl-seed-hold seed
   ```
2. Then either apply the seed later, or ingest directly from source with the
   seeder (skips the portable JSONL entirely):
   ```bash
   cd /root/norwegian-law-memory-blueprint
   go build ./cmd/seeder/
   ./seeder --server <url> --token <project-api-token> --project <project-id>
   # smoke first: add --limit 50 --skip-eu
   ```

## 4. Verify

- Schema: `memory schemas list --project <id> --server <url>` shows pack
  `norwegian-law` with 7 object types / 13 relationship types.
- Agent: `memory agents list --project <id> --server <url>` shows
  `norwegian-law-assistant`.
- Graph: query an object, e.g. `Law` key `lov/2005-06-17-90` (arbeidsmiljøloven),
  confirm its `HAS_PARAGRAPH` edges resolve.
- Functional: ask the agent a cited question, e.g. "Hva sier arbeidsmiljøloven om
  oppsigelsesfrist?" — expect `short_title` + `ref_id` + paragraf citation.

## 5. Target (resolved)

- Instance: **dev** — internal `http://emergent-dev:3002`, public `https://api.dev.emergent-company.ai`.
- Account: `m+test@kucharz.net` → user `198d334e-90e8-458f-a826-15779102e975`.
- Org: "Maciej Kucharz's Org" (`f6b5b71c-8c37-4a2f-9ec6-2761cd477744`), role org_admin.
- Existing projects: `test` (`f131d865-5d9c-43ae-834f-bcb749c3359d`),
  `Maciej's Project` (`ed473064-fc14-4289-9cbe-c6269c390324`).
  No "Norwegian Law" project exists yet — create one, or apply into `test`.
- `mcj-emergent` is **offline** (last seen 175d) — do not use it.
- Seeder route only: mint a project API token (`core.api_tokens`), e.g.
  `memory apitokens create --project <project-id> --server http://emergent-dev:3002`
  (verify the exact subcommand with `memory apitokens --help`).

## Rollback

Delete the created project (or its objects) — the blueprint writes only to the
target project; the repository keeps no state.
