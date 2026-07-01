# Observability Handbook

> [Tiếng Việt](observability.md)

When running a long novel, how do you know each mechanism is actually working?

This document is not a verbatim copy of the diag rules; it is oriented toward **actual operation**: you have reached chapter N, so which file should you open, which field should you look at, to judge whether things are healthy or abnormal.

---

## 1. General troubleshooting flow

```
1. /diag                       # automatic diagnosis, look at the Findings section
2. cd output/{novel}/meta/     # cat the key artifacts directly
3. cat meta/sessions/coordinator.jsonl | tail  # look at the most recent LLM rounds
```

Facts that `/diag` does not cover (including the "diagnostics still to be added" items listed in this document) need to be checked manually in steps 2-3.

### Reporting an issue: anonymized diagnostic export

Every `/diag` run additionally writes `output/{novel}/meta/diag-export.md` — an **anonymized** diagnostic (novel body text / prompt / thinking removed, keeping only the behavioral skeleton: tool names, error strings, repeat counts, phase/flow, the stuck step, log error classification). For dead-loop / interruption-type problems, just paste this file into a GitHub issue and the maintainer can locate the cause, with no need for your `output/` data.

---

## 2. Quick-reference table of key artifacts

Ordered by "the most common troubleshooting path when something goes wrong":

| Artifact | Path | What to look at | Healthy | Unhealthy |
|---|---|---|---|---|
| Progress | `meta/progress.json` | `phase` / `flow` / `completed_chapters` | phase advances monotonically, flow within the valid set | phase regresses / flow stuck in one state |
| Compass | `meta/compass.json` | gap between `last_updated` and the latest chapter | gap < 15 chapters | gap > 15 chapters (CompassDrift triggered) |
| Supporting-cast ledger | `meta/cast_ledger.json` | entry count / brief_role fill rate / name consistency | see §4 | see §4 |
| Foreshadowing ledger | `meta/foreshadow.json` | longest stall (in chapters) of an entry with `status="planted"` | < chapters/3 | > chapters/3 (StaleForeshadow triggered) |
| Outline | `meta/layered_outline.json` | remaining unwritten chapters in the current volume | expanded 1-2 chapters ahead | written up to the current chapter but the next chapter has no outline (OutlineExhausted) |
| Character profiles | `meta/characters.json` | whether core/important characters can be found in the last N chapter summaries | all can be found | absent (GhostCharacter triggered) |
| Checkpoint | `meta/checkpoints.jsonl` | whether the latest line's `step` corresponds to progress | consistent | inconsistent (crash recovery did not self-heal) |
| Coordinator session | `meta/sessions/coordinator.jsonl` | the tool_call pattern of the last 5-10 rounds | each round advances quickly | the same tool called empty repeatedly (stuck in a dead loop) |

---

## 3. Compass observation

**Fix date**: 2026-05-08 (commit `fix: update_compass 工具自动填 last_updated`)

### What to look at

```bash
cat output/{novel}/meta/compass.json
```

Field semantics:
- `ending_direction`: the ending direction (should match the "终局方向" section in `premise.md`)
- `open_threads`: active long-running threads (Architect adds/removes them at each volume boundary)
- `estimated_scale`: estimated scale (e.g. "4-6 volumes", updated at each volume boundary)
- `last_updated`: **filled automatically by the tool** as the largest completed chapter number at update time (no longer relying on the LLM to fill it in)

### Health judgment

| Signal | Judgment |
|---|---|
| `last_updated` within the range `[latest-15, latest]` | healthy |
| `last_updated` lags latest by more than 15 chapters | Architect did not update at the arc/volume boundary — check the architect-long.md prompt |
| `last_updated == 0` | **dirty data from before this fix**, the next `update_compass` will self-heal |
| `ending_direction` does not match the "终局方向" section in premise.md | Architect quietly changed the user's intent — record it and decide whether to freeze the field (a design issue, see todo.md) |

### How to verify the fix works

Compare before/after running a long novel:
- **Before the fix**: after running 30+ chapters, `compass.last_updated` is most likely `0` or some early chapter number
- **After the fix**: each time Architect calls `update_compass`, `last_updated` is overwritten by the tool layer to the current latest

---

## 4. Supporting-cast ledger (cast_ledger) observation

**Feature landed**: 2026-05-08 (commit `feat: 新增配角名册自动追踪次要角色`)

### What to look at

```bash
cat output/{novel}/meta/cast_ledger.json | jq 'length'                     # total entry count
cat output/{novel}/meta/cast_ledger.json | jq '[.[] | select(.brief_role == "" or .brief_role == null)] | length'  # count missing brief_role
cat output/{novel}/meta/cast_ledger.json | jq '[.[] | select(.appearance_count >= 3)] | length'   # frequently appearing (≥3 times) count
cat output/{novel}/meta/cast_ledger.json | jq 'sort_by(-.appearance_count) | .[:10]'  # the 10 most-appearing entries
```

### Health judgment

| Dimension | Healthy | Abnormal | Response |
|---|---|---|---|
| **Entry count vs completed chapters** | ledger entry count ≈ completed chapters × 0.3-0.6 | > chapters × 0.8 (walk-on characters wrongly registered) | check whether the `cast_intros` section in writer.md is clear enough |
| **brief_role fill rate** | missing < 30% | missing > 50% | Writer omits seriously — prompt guidance insufficient |
| **Same-name similarity** | no suspected one-person-many-names | "李X" / "老李" / "X掌柜" appear together | LLM name drift — add a constraint to the prompt ("use a consistent name") or add a user-steer merge tool |
| **Frequently appearing characters** | entries with `appearance_count >= 5` are rare | many entries appear at high frequency across arcs | should consider promoting to a core profile (stage-3 promotion channel) |
| **Whether recall is consumed** | when Writer writes an old character, the commit_chapter characters field contains a name already in the ledger | Writer repeatedly invents the same name (both "老周A" and "老周B" appear) | recent_cast recall not consumed — check the "配角连续性" section in writer.md |

### Data-flow verification (end-to-end)

After running 5 chapters:
1. `cat meta/cast_ledger.json` should not be empty (unless every chapter uses only core characters)
2. If Writer introduced "老周" in chapter 1:
   - `cast_ledger` should have a `老周` entry, `appearance_count=1`
3. If chapter 5 writes 老周 again:
   - `老周.appearance_count=2`, `last_seen_chapter=5`
4. In `meta/sessions/agents/writer-*.jsonl`, the novel_context return value for chapter 5 should show 老周 in `episodic_memory.recent_cast`
5. If the previous step shows it but Writer did not consume it (the 老周 written does not match chapter 1) — this is a prompt problem

### No automatic diagnostic yet (but the snapshot is already loaded)

`diag.Snapshot.CastLedger` is already read in `Load()` and can be consumed directly by rules — but no rule has been written yet. Verification still relies on the `jq` commands above, checked manually.

If diagnostic rules are to be added later (candidates):
- `CastBriefRoleMissing`: warn when the missing rate > 50%
- `CastBloat`: warn when entry count > chapters × 0.8
- `CastPromotionCandidate`: appearance_count ≥ 5 and across arcs → suggest promotion

Do not fix the thresholds now — wait for long-novel data, look at the real distribution, then decide. The rule code itself only needs 30-50 lines.

---

## 5. Whether Writer is working as expected

When running a long novel, the biggest concern is **whether Writer is really acting per the prompt**. The most direct observation is the session log:

```bash
ls output/{novel}/meta/sessions/agents/    # one jsonl per subagent
tail -50 output/{novel}/meta/sessions/agents/writer-*.jsonl
```

Look at a few specific behaviors:

| Expected behavior | How it shows in the jsonl |
|---|---|
| Writer looked at recent_cast | the `episodic_memory.recent_cast` field in the novel_context tool return value is non-empty |
| Writer filled cast_intros in commit_chapter | the tool_call argument `cast_intros` is a non-empty array (only in chapters introducing new characters) |
| Writer used the related-chapter recommendations | `read_chapter` call count > 1 (default 1; more means it looked back) |
| Writer did not violate tool order | the tool_call sequence strictly follows `novel_context → read_chapter → plan_chapter → draft_chapter → check_consistency → commit_chapter` |

If in the jsonl you see Writer call novel_context empty multiple times, or call other tools after commit_chapter — the prompt did not rein it in.

---

## 6. Long-run scenario red lines

When running a long novel of 100+ chapters, any one of the following triggering means you should stop and investigate:

- [ ] CompassDrift triggered and persists for 2 arcs without being cleared
- [ ] cast_ledger entry count > completed chapters × 0.8
- [ ] brief_role fill rate in cast_ledger < 30%
- [ ] the same character appears under suspected multiple names ("老李" / "李掌柜" coexist)
- [ ] Writer writes a new chapter without reading an old character already in recent_cast (reinvention)
- [ ] the Coordinator session shows ≥ 5 consecutive empty novel_context calls
- [ ] after any chapter commit, `meta/checkpoints.jsonl` has no corresponding `commit_chapter` step

The first 4 are the health of the new mechanisms this time; the last 3 are the stability of existing mechanisms.

---

## 7. Documentation maintenance conventions

**When adding a new fact-layer artifact (creating a `meta/*.json` / `meta/*.jsonl`), keep in sync:**

1. Add a quick-reference line to §2 of this document
2. If the artifact needs dedicated observation (not a simple "exists / does not exist" judgment), add a §X topic section
3. If you want automatic diagnosis, load it in `internal/diag/snapshot.go::Load` and add a rule in `internal/diag/rules_*.go`

**Do not:**
- Do not copy every rule in `internal/diag/` into this document (that is the rule reference, not the observability handbook)
- Do not write a diagnostic rule for every mechanism — thresholds fixed by gut feel will be wrong; observe first, add later
