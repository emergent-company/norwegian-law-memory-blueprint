# Agent experiments

How we iterate on the `norwegian-law-assistant` prompt, tool grants, and the
eval dataset. Every change is an **experiment**: one variable, a fixed probe,
measured numbers, an explicit verdict.

## Rule: only keep changes that improve the tests

A change is **kept** only if it improves the primary metric on the fixed probe
without a material regression elsewhere. Otherwise it is **reverted**.

- Primary: **citation F1** (overall).
- Guardrails: **recall must not drop** (a precision win that loses recall is a
  loss); watch `resolution_rate` (citations that don't exist) and
  `hallucinated_refs`.
- Tolerance (measured, E15): overall citation F1 has a run-to-run sd of ≈0.013
  (n=3) → require **ΔF1 ≳ 0.03 (~2 sd)** before calling a change real. Per-area
  metrics are far noisier (E12: utlendingsrett swung 0.265↔0.484 on identical
  items) — never judge on a single small area.
- Two-dimensional from E16: also watch **point_recall** (judge) — a change must
  not regress answer coverage while raising citation F1.
- Record every experiment in `experiments/LOG.md`, **including rejected ones**,
  with the numbers that justified the verdict.

## Fixed probe

Experiments run against a small, frozen probe so runs are quick (~4 min) and
comparable. Materialize it from the committed sets:

```sh
python3 - <<'EOF'
import json, glob
want = {
  'core-aml-oppsigelsesfrist-001','core-avtaleloven-36-001','core-fkjl-mangel-001',
  'agency-forbrukerradet-klagebrev-001','agency-skatteetaten-virksomhet-001',
  'agency-forbrukerradet-avtale-001',
}
out = []
for f in ['evals/golden/core.jsonl'] + sorted(glob.glob('evals/curated/*.jsonl')):
    for ln in open(f):
        if json.loads(ln)['id'] in want:
            out.append(ln.rstrip('\n'))
open('/tmp/probe.jsonl','w').write('\n'.join(out)+'\n')
EOF
```

Larger confirmation runs use the committed set
(`EVAL_DATASET=evals/golden/core.jsonl,evals/curated/*.jsonl`, 72 items) and the
generated UiO set (`evals/dataset/uio.jsonl`, 599 items, `needs_curation`).

## Protocol

1. **Snapshot** the current agent definition (prompt + tools) before changing it:
   `GET /api/projects/{pid}/agent-definitions/{defId}` -> save JSON.
2. **Apply one change** to one variable (prompt text, tool grant, dataset build).
3. **Run the probe** and capture metrics + cited refs:
   ```sh
   set -a; . /root/norwegian-law-memory-blueprint-wt/regen-seed-section-label/.env; set +a
   EVAL_TRANSPORT=trigger EVAL_AGENT_ID=d3643c52-98c3-4286-9600-99737137cee3 \
   EVAL_DATASET=/tmp/probe.jsonl \
     go test ./tests/eval/ -run TestAgentEval -v -count=1 -timeout 40m
   ```
   Reports land in `evals/results/<timestamp>.json` (gitignored).
4. **Inspect transcripts** for *why* the numbers moved — do not score in a
   vacuum:
   ```sh
   GET /api/projects/{pid}/agents/{agentId}/runs?limit=100
   GET /api/projects/{pid}/agent-runs/{runId}/tool-calls
   GET /api/projects/{pid}/agent-runs/{runId}/steps   # includes reasoning
   ```
   Look at: which tool, `query`, `limit`, `types`, `namespace`,
   `field_strategy`, and errors.
5. **Decide** keep vs revert by the rule above; if a change touches the deployed
   agent, either leave it applied (kept) or restore the snapshot via
   `PATCH …/agent-definitions/{defId}`.
6. **Log** the result in `experiments/LOG.md` (id, date, change, dataset,
   before/after, verdict, follow-ups).

## Change surface

| Variable | Where | Kept when |
|---|---|---|
| System prompt | `agents/norwegian-law-assistant.yaml` (+ live def PATCH) | probe F1 improves, recall holds |
| Tool grants | agent definition `tools[]` | improves F1 at equal/better recall, or materially cheaper |
| Extraction / scoring | `internal/evaldata/*` | offline checks pass and gold quality improves |
| Dataset build | `cmd/loveval`, `Taskfile.yml` | integrity passes; reproducible (same sha256) |

## Metrics (from `tests/eval/scoring.go`)

`f1` (primary), `recall`, `precision`, `res_rate` (resolved/cited), `refusal`,
plus the optional LLM judge `point_recall`. Precision is strict: any cited ref
that isn't in `gold_refs` counts against it, so over-citation shows up as low
precision even when the extra refs are valid.
