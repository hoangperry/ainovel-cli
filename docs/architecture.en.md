# ainovel-cli Runtime Architecture

> [Tiếng Việt](architecture.md)

> Let the LLM finish a whole novel in a single Run; the Host only handles startup / recovery / routing / observation, leaving decision power to the model as much as possible.

---

## 1. Goals (by priority)

1. **Stability**: one-sentence input, stably write a whole novel (200~500 chapters). Never self-interrupt midway due to architectural issues.
2. **Iterable quality**: prompt / reference material / review dimensions / context strategy can be tuned independently without touching the architecture.
3. **Recoverable**: after a crash, network loss, or pause, resume from the most recent checkpoint.
4. **Observable**: the progress, artifacts, and elapsed time of every chapter and every step are queryable.

"Stability" is the premise, "quality" is the upper layer. Every architectural decision serves stability first.

---

## 2. Core Principles

### 2.1 LLM drives authoring and adjudication, Host drives flow routing

The decision space of the vertical agent is closed: the flowchart is fixed, branches are finite, and it is fact-driven. The two kinds of decisions ride different carriers:

- **Authoring and adjudication** (semantic/quality/intent understanding) → LLM. The adjudication capability of Writer/Editor/Architect/Coordinator benefits linearly as the model improves
- **Flow routing** (read facts, look up a table) → code. `flow.Router` is a pure function + unit tests, error rate approaching 0

The Host does not call SubAgents directly; instead, at each Coordinator TurnEnd the Flow Router computes an instruction and issues it via `coordinator.FollowUp("[Host instruction]…")`.

### 2.2 Tools are the only interface to the fact layer

All interaction with the file system, Progress, and Checkpoint is done by tools. **Write tools must do the atomic triple**: artifact written to disk + Progress advanced + Checkpoint appended, completed within a mutex. Re-running the same tool yields the identical result or skips outright (digest idempotency).

### 2.3 The observation layer only observes

UI, diagnostics, and event logs are all passive consumers projected from the event stream / read-only artifacts. They read facts, do not produce facts, and do not affect control flow.

**`internal/diag` is the engine's only observability subsystem** — a first-class supporting facility, but not the product core (the core is the authoring engine in §6; without diag you can still write novels). It cross-reads almost all artifacts + session + log + checkpoint and carries two duties: ① **authoring-quality diagnostics** (rule → Finding, on-screen `/diag` report); ② **runtime troubleshooting + de-sensitized export** (strip the body to a behavior skeleton + aggregate loops → overwrite `meta/diag-export.md`, for users to paste into an issue; a maintainer without the local output can still locate dead-loop/mid-break class problems).

**Observer discipline (non-negotiable)**: diag may diagnose and may suggest, but **never acts on its own** — no auto-fix, no auto-resume, no flow changes. The stronger it gets, the more someone wants it to "just fix it while you're at it", and the harder you must hold this line, or you crash back into the kind of pits like idleResume / StallDetector that were already deleted (see §10.5, §10.14). Treat external structures (such as `RuntimeCapture`) as infrastructure contracts; do not casually change fields.

### 2.4 The fact layer is flat

There are only three kinds of facts:

- **Progress** — progress index (which chapter has been written, the rewrite-pending list)
- **Checkpoint** — step-level advancement record (plan / draft / commit / review / arc_summary)
- **Artifact** — chapter body, outline, characters, summaries, and other products

No abstractions like WorkflowInstance / TaskInstance / Command / Dispatcher are introduced.

### 2.5 Three iron laws

**Iron law one: tools return only facts, not cross-dispatch instructions**. `commit_chapter` returns structured fields like `arc_end_reached` / `next_skeleton_arc`; it does not smuggle `[system]`-style instruction strings. The `next_step` field inside a subagent is an inline guidance that states a fact ("I just saved the plan, the next step is draft") and does not count as a violation — see §6.4.

**Iron law two: flow routing is carried by the Flow Router**. `Route(state) → *Instruction` in `internal/host/flow/router.go` is a pure function; after subscribing to `EventToolExecEnd` it issues instructions via `FollowUp`. Returning nil means "this is an adjudication scenario, let the LLM act autonomously". **The instruction channel does not go silent**: when Route computes the same instruction consecutively (indicating state did not advance after the previous dispatch), the Dispatcher attaches an "issued for the Nth time" fact and re-sends rather than swallowing it silently — "duplicate routing result" is a fact only the Host can observe, and silence would drop the Coordinator into the dual contradiction of "may not act without an instruction / StopGuard won't let it stop". No thresholds, no circuit breaker; how to escape is adjudicated by the LLM.

**Iron law three: the Coordinator cannot physically end_turn unless Phase=Complete**. StopGuard intercepts `end_turn` at the agentcore layer and injects a user message; if it fails to intercept 5 times in a row, it escalates to terminate. The three subagents (architect / writer / editor) have their own `CheckpointDeltaGuard`.

---

## 3. Architecture Panorama

```
[Entry: TUI / headless]
        │ prompt / steer
[Host thin shell]
   ├── observer        events → UI/log projection
   ├── flow.Dispatcher subscribe ToolExecEnd → Route(state) → FollowUp
   └── usage / model management
        │
[Coordinator (LLM, MaxTurns=100_000)]
   ├── adjudicates architect_short / long at startup
   ├── receives [Host instruction] → emits subagent tool_call
   └── receives [User intervention] → adjudicates autonomously
        │
[architect / writer / editor SubAgent (each its own run + context + model)]
        │ tool calls
[Tools]  novel_context · read_chapter · plan_chapter · draft_chapter · edit_chapter
         check_consistency · commit_chapter · save_review · save_arc_summary
         save_volume_summary · save_foundation
        │ atomic triple
[Store: file system (tmp + rename)]
   Progress · Checkpoints · Outline · Drafts · Summaries · Characters · World · Signals
```

| Layer | Does | Does not do |
|---|---|---|
| Entry | Display, receive input | Business decisions |
| Host | Startup/recovery/intervention/event projection/Flow routing | Bypass Coordinator to call SubAgents directly; write state |
| Coordinator | Execute Host instructions, adjudicate user Steer, pick planner at startup | Decide each chapter's next step itself; write files |
| Agents | Think, write, review | Read/write Store directly |
| Tools | Atomic IO + checkpoint + idempotency | Cross-subagent dispatch instructions |
| Store | File-system persistence | Business logic |

Dependencies are one-way: `entry → host → agents → tools → store → domain`. `tools/` does not reference `agents/host/`, and `host/` does not reference `tools/store/` directly. Horizontally independent modules: `errs/` may be referenced by any layer, `diag/` subscribes to the host event stream + read-only `store/`.

---

## 4. Data Model

### 4.1 Progress (`internal/domain/runtime.go`)

```go
type Progress struct {
    NovelName         string
    Phase             Phase           // init / premise / outline / writing / complete
    CurrentChapter    int
    TotalChapters     int
    CompletedChapters []int
    TotalWordCount    int
    ChapterWordCounts map[int]int
    InProgressChapter int             // the chapter being written
    Flow              FlowState       // writing / reviewing / rewriting / polishing / steering
    PendingRewrites   []int
    StrandHistory     []string        // dominant_strand sequence
    HookHistory       []string        // hook_type sequence
    CurrentVolume, CurrentArc int     // long-novel layering
    Layered           bool
}
```

The control logic only reads the fact fields above; it depends on no "update timestamp" — time information is carried by the checkpoint's `OccurredAt`.

### 4.2 Checkpoint (`internal/domain/checkpoint.go`)

```go
type Scope      struct { Kind ScopeKind; Chapter, Volume, Arc int }
type Checkpoint struct {
    Seq        int64       // monotonically increasing
    Scope      Scope       // chapter / arc / volume / global
    Step       string      // plan / draft / commit / review / arc_summary / ...
    Artifact   string
    Digest     string
    OccurredAt time.Time
}
```

Storage: `meta/checkpoints.jsonl`, append-only. Writing the same `Scope+Step+Digest` again is treated as idempotent and produces no new line.

### 4.3 Artifact and Signals

Artifacts live in `store/outline.go` `drafts.go` `summaries.go` `characters.go` `world.go` — every kind of product can be referenced by a checkpoint.

Signals: `PendingCommit` (commit-interruption recovery) / `PendingSteer` (user intervention during shutdown). Read at startup/recovery, not read at runtime.

---

## 5. Tool Contract

Tools are the only interaction point between the fact layer and the Agent.

### 5.1 Read tools

`novel_context(scope)` / `read_chapter(n)` — callable at any time, with no dependence on prior state, returning data sufficient for the LLM to decide independently.

### 5.2 Write tools (atomic triple)

Each successful call must: write the artifact to disk → advance Progress → append a checkpoint. All three steps complete within a mutex.

| Tool | Artifact | Step |
|---|---|---|
| `plan_chapter` | drafts/chXX.plan.json | plan |
| `draft_chapter` | drafts/chXX.draft.md | draft |
| `edit_chapter` | drafts/chXX.draft.md | edit |
| `check_consistency` | none (read-only, inline return) | consistency_check |
| `commit_chapter` | chapters/chXX.md + Progress | commit |
| `save_review` | reviews/chXX.json (global is chXX-global.json) | review |
| `save_arc_summary` | summaries/arc-vNNaNN.json | arc_summary |
| `save_volume_summary` | summaries/vol-vNN.json | volume_summary |
| `save_foundation` | foundation/*.json | premise / outline / layered_outline / characters / world_rules / expand_arc / append_volume / update_compass / complete_book |

`commit_chapter` carries arc/volume/whole-book completion detection and returns 19 fact fields (`arc_end` / `needs_expansion` / `book_complete`, etc.; when mechanical rule checking is enabled it also attaches `rule_violations`). `save_review` carries verdict escalation (scorecard gate, contract missed → rewrite). These logics, once scattered in the policy layer, are now fixed inside the tools.

`edit_chapter` is a thin wrapper over `agentcore.EditTool`; the ownership check guarantees a completed chapter must be in `PendingRewrites` to be editable.

### 5.3 Error layering

| Error type | Handling layer | Action |
|---|---|---|
| Network timeout / stream EOF | Tools | Retry 3 times |
| provider 429/503 | litellm | failover to backup provider |
| Auth / model not found | Tools | raise terminal |
| Missing prerequisite artifact | Tools | raise conflict, LLM calls `novel_context` then retries |
| Invalid tool arguments | Tools | raise validation, LLM fixes arguments |
| MaxTurns exhausted | agentcore | run ends, Host emits done |
| Non-compliant LLM message (thinking-only stop, etc.) | agentcore (`llm/litellm.go` `convertMessages`) | inbound fallback + outbound filter; Host unaware |
| Empty stream response / long thinking | litellm (`StreamIdleTimeout=5min`) | watchdog triggers retry |

### 5.4 Idempotency

Before executing, every write tool first checks the checkpoint: if the latest checkpoint of the current scope has the same `Step+Digest` as this call, it returns the existing artifact directly. The LLM can retry with confidence and will not produce duplicate chapters or misaligned progress.

---

## 6. Agent Assembly

> A single oversized Prompt + a single Agent running a whole book is theoretically feasible, but three things block stability: **context explosion** (200 chapters degrade even under aggressive compression), **role interference** (rigorous planning / imaginative writing / critical review in the same prompt dilute one another), and **loss of the heterogeneous-model dividend** (planning on Opus, writing on Sonnet, review on Pro — picking models independently is a significant cost/quality optimization space for long novels). A multi-agent topology is therefore necessary.

### 6.1 Coordinator

The sole driver of the main loop. Assembled in `internal/agents/build.go`:

```go
agent := agentcore.NewAgent(
    agentcore.WithModel(coordinatorModel),
    agentcore.WithSystemPrompt(bundle.Prompts.Coordinator),
    agentcore.WithTools(subagentTool, contextTool),
    agentcore.WithMaxTurns(100_000),
    agentcore.WithToolsAreIdempotent(true),
    agentcore.WithMaxToolErrors(0),  // subagent does not circuit-break
    agentcore.WithMaxRetries(subagentMaxRetries),
    agentcore.WithContextManager(...),
    agentcore.WithStopGuard(reminder.NewStopGuard(store, nil)),
    agentcore.WithToolGate(completePhaseGate(store)),  // phase=complete hard-blocks subagent dispatch
)
```

Responsibilities: pick the planner at startup → planning completion loop → on receiving `[Host instruction]`, immediately emit the corresponding `subagent` tool_call → handle `[User intervention]` by adjudicating autonomously → after `book_complete=true`, output a summary.

Does not: write files, read Progress directly (uses novel_context), decide the next step itself when a Host instruction arrives.

> **Why not delete the Coordinator and let the Host call subagents directly?** It looks "cleaner", but it loses four things: (1) the "what to do next" decision stays at the LLM layer, directly benefiting from model upgrades; (2) the soft judgment of review verdicts (accept/polish/rewrite + scope of impact) moves out of Go code; (3) the impact assessment of a user Steer is handed to the model — which chapters should "the supporting character's motivation must be clearer" rewrite? The Coordinator can judge, hardcoding in the Host cannot; (4) exceptional branches (writer outline feedback, editor discovering a worldbuilding hole) are handled by the model itself, avoiding writing a Go state machine for every branch. **Deleting the Coordinator means swapping the bet from "the model keeps getting stronger" to "my Go code keeps getting stronger" — that is not a good bet**.

### 6.2 Subagent topology and heterogeneous models

```
Coordinator (1 agent run, MaxTurns=100_000)
    ↓ subagent()
architect_short/long  ·  writer  ·  editor
    ↓ tool calls
Store (collaboration medium, subagents do not communicate directly)
```

Subagent turn counts are independent (native to agentcore) and do not consume the Coordinator's 100_000-turn quota. Subagents communicate through structured artifacts in the Store; the Coordinator passes only "task descriptions", not content.

`bootstrap.ModelSet` supports role-level models: coordinator/architect/writer/editor are each configured independently + provider failover. Running the Writer on Sonnet instead of Opus can save an order of magnitude of cost on a 200-chapter long novel.

### 6.3 Three collaboration modes

Subagents do not communicate directly; all information flow passes through structured artifacts in the Store. Three modes cover all of the system's workflows:

**Mode A · Serial handoff (mainline)**: Coordinator → Architect plans → Writer chapters 1..N → Editor reviews at arc end → Writer rewrites. The most common mode; the Coordinator queries the current state via `novel_context` to decide who to call next.

**Mode B · Review feedback (closed loop)**: the Writer discovers an outline deviation in a draft → `commit_chapter`'s return value carries a `writer_feedback` field → the Coordinator sees the feedback and judges whether to escalate to an architect call to adjust the outline. The Writer does not call the Architect directly; feedback is sent back to the Coordinator via a structured field.

**Mode C · Skeleton expansion (rolling planning)**: `commit_chapter` detects that the next arc is still a skeleton → returns `arc_end_reached + next_skeleton_arc` → the Flow Router dispatches an instruction → the Coordinator calls architect_long to expand the next arc's detailed chapters → the Writer continues. The long-novel "rolling planning" capability is exactly this closed loop made real.

### 6.4 Code constraints on the subagent flow (no prompt crutch)

> Early on the writer flow relied on the "proceed strictly in the following order" constraint in `writer.md`. The LLM frequently violated it — skipping plan to draft directly, saying one more token-consuming paragraph after commit, writing the body only into the chat without persisting it. **Prompt-based flow constraints are unstable**; their strength depends entirely on how "obedient" the model is at the moment, and a model upgrade may even make it "creatively disobey".

Four layers of code constraint (all active simultaneously):

| Layer | Placement | Effect |
|---|---|---|
| `StopAfterTools` / `StopAfterToolResult` | `agents/build.go` SubAgentConfig | A key tool's success triggers end_turn to exit the subagent run. The Writer stops the moment `commit_chapter` hits (`StopAfterTools`); the Editor's `save_arc_summary`/`save_volume_summary` and the Architect's arc/volume wrap-up go through `StopAfterToolResult`. The Editor's `save_review` does not hard-stop — otherwise it would bypass StopGuard and sever the arc-summary run; the wrap-up is handed to `NewEditorStopGuard` |
| `CheckpointDeltaGuard` | `host/reminder/subagent_guards.go` | Using the baseline checkpoint as the boundary, before this round ends a new checkpoint of the corresponding step must be seen, otherwise `end_turn` is refused; intercepting 3 times in a row escalates to terminate (a dead-loop fallback for weak models) |
| In-tool inline `next_step` | a field in each tool's return value | Each fact carries its own "next-step suggestion". For example `plan_chapter` returns `next_step: "immediately call draft_chapter..."`. The LLM knows the next step from the fact and needn't go back to the system prompt to find it |
| In-tool ownership/prerequisite checks | `edit_chapter` `commit_chapter`, etc. | Physical interception at the data layer: `edit_chapter` refuses to edit a completed chapter not listed in `PendingRewrites`; `commit_chapter` refuses an empty commit where draft==final; `ConcurrencySafe=false` blocks concurrent races |

In the new architecture writer.md carries only: writing-quality guidance, the resume-from-breakpoint cognitive model, and chapter-contract interpretation. **It no longer does flow orchestration** — when the LLM skips a step the prompt won't save the day, the code will. architect / editor have the same four layers of constraint in their respective tools/Guards.

> On iron law one: `next_step` is an in-tool inline fact statement ("I just saved the plan"), not Host-injected cross-call flow orchestration. Cross-subagent dispatch at the Coordinator layer still strictly goes through Flow Router → FollowUp.

### 6.5 agentcore dependency

`../agentcore` is this project's own general-purpose Agent library (linked via go.work). The primitives the new architecture uses all already exist: `Prompt` / `Inject` / `FollowUp` / `Subscribe` / `WithMaxTurns` / `WithStopGuard` / `WithToolGate` / `SubAgentConfig` / `WithContextManager`.

**Modification boundary**:

- May enter agentcore: new ContextManager strategies, new provider adapters, new event types, general message-injection patterns
- Must not enter agentcore: business models like Progress/Checkpoint/Scope, business tools like novel_context/commit_chapter, business rules like arc-end detection/review gating

Decision criterion: assume agentcore will one day be adopted by a coding agent / customer-service agent; only allow a new capability in if it still makes sense in that scenario. **No fallback patches in the application layer** (proxies, wrappers, monkey patches) — if a capability is missing, go change agentcore directly.

**Intentionally unused capabilities** (to avoid misuse):

- `Agent.TaskRuntime() / Tasks() / StopTask()` — agentcore's built-in background task manager (fire-and-forget background subagent). All subagent calls in the new architecture are foreground and synchronous, so it is **unused**
- `Agent.FollowUp(msg)` — **the only legitimate user is `flow.Dispatcher`**, used to issue `[Host instruction]`. Other public Host methods are forbidden to call it. User Steer goes through `Inject` (preserving immediate-interrupt capability), Resume goes through `Prompt` to start a new run
- `Agent.Steer(msg)` — the old steering interface; the new architecture uniformly uses `Inject`
- `WithPermission*` — the permission-approval mechanism (human approval of dangerous operations); the novel application has no dangerous operations, so it is **unused**

**Enabled policy hooks**: `WithToolGate` — its only use is to hard-block `subagent` dispatch when `phase=complete` (`agents/build.go` `completePhaseGate`). After completion, if the user requests continuation/rewrite, the Coordinator LLM may still dispatch a subagent on its own, and a Writer writing an out-of-bounds chapter will be refused by `commit_chapter` while `CheckpointDeltaGuard` won't release `end_turn` → dead loop. The Flow Router returning nil at complete only blocks the Host's automatic dispatch, not the LLM's proactive dispatch, so the Gate adds a terminal-state defense at the chokepoint. It is a narrow-purpose flow fallback, **not the `WithPermission*` kind of approval flow**; do not conflate the two.

---

## 7. Host Layer

### 7.1 Structure

```go
type Host struct {
    cfg               bootstrap.Config
    bundle            assets.Bundle
    store             *store.Store
    models            *bootstrap.ModelSet
    coordinator       *agentcore.Agent
    coordinatorCtxMgr *corecontext.ContextEngine  // links the context window when switching models
    askUser           *tools.AskUserTool
    writerRestore     *ctxpack.WriterRestorePack

    observer     *observer
    router       *flow.Dispatcher  // subscribe + Route + FollowUp
    routerDetach func()
    usage        *UsageTracker
    usageCancel  context.CancelFunc
    budget       *BudgetSentinel   // Host policy component: enforces the user's budget declaration (equivalent to aborting on their behalf), subscribes before the Dispatcher
    notifier     *notify.Notifier  // observation layer: an off-screen copy of the three alert types run_end/repeat/budget, never intervenes in control flow

    events, streamCh, done chan ...

    mu        sync.Mutex
    lifecycle lifecycle  // idle / running / paused / completed
    closeOnce sync.Once
}
```

### 7.2 Public API

**Lifecycle** (the Coordinator's Run entry points): `Start` / `StartPrepared` / `Resume` / `Continue` / `Steer` / `Abort` / `Close`

**Observation channels**: `Events` / `Stream` / `Done` (draining the stream goes through a sentinel in streamCh)

**UI aggregation**: `Snapshot()` — the TUI pulls all display data in one call

**Config/extension**: model management (`SwitchModel`), external-novel reverse-engineering import (`ImportFrom`), co-creation dialogue (`CoCreateStream`), event replay (`ReplayQueue`), simulation profile (`Simulate`/`ImportSimulationProfile`), export (`Export`)

No business-scheduling methods like `decideNext` or `retryActiveTask`. The Flow Router is a thin composition of a pure function + FollowUp, holding no implicit state like "the task currently being retried".

### 7.3 The shape of `waitDone`

```go
func (h *Host) waitDone() {
    h.coordinator.WaitForIdle()
    h.observer.finalize()

    if Phase == Complete { lifecycle=completed; emit "Authoring complete" event }
    else if running        { lifecycle=idle;     emit "Coordinator stopped (N chapters done)" event }

    select { case h.done <- struct{}{}: default: }
}
```

Three things: wait for idle → switch lifecycle → emit the terminal-state event + deliver the done signal. **`Inject` / `FollowUp` / `Prompt` are forbidden in the function body**. After the LLM finishes one Run, the entire Host enters its terminal state.

There are only two ways to make it move again: the user actively `Continue`/`Start`, or restart the process and go through `Resume`.

> Historical lesson: an `idleResumeCount` patch that auto-restarted the Run was once added to this function. In the one time it actually triggered, during the long mimo run, it failed to save the day 100% of the time and instead masked the true cause at the agentcore layer — "thinking-only stop messages entering history". **A "defensive restart" at the Host layer is always a misplaced fix**. See `feedback_no_host_resilience.md` and §10 item 5.

---

## 8. Startup and Recovery

### 8.1 New creation

```
User: "one-sentence requirement"
  → Host.Start
    → store.Progress.Init / store.Checkpoints.Reset
    → coordinator.Prompt(userPrompt) + flow.Dispatcher.Enable + Dispatch
    → Coordinator long loop: plan → write 1..N → review → done
```

### 8.2 Recovery (restart after a crash)

```
Process startup
  → read Progress + the most recent Checkpoint + PendingCommit + PendingSteer
  → buildResumePrompt → a short notice (not a step-level instruction)
  → coordinator.Prompt(resumePrompt) + Dispatcher.Enable + Dispatch
  → Coordinator continues per the Host instructions
```

Resume uses `Prompt` to start a new Run (turn count reset, clean context), not `FollowUp`. The specific next step is derived by the Flow Router from the fact layer after the first TurnEnd.

### 8.3 User intervention

| Entry | Prefix | Semantics | Implementation |
|---|---|---|---|
| `Steer(text)` | `[User intervention]` | modify/query, requires Coordinator adjudication | while running goes through `Inject`; while shut down writes PendingSteer to `meta/run.json` |
| `Continue(text)` | `[User intervention]` | continue writing, wake after shutdown | while running goes through `FollowUp`; while shut down goes through `Inject` to auto-recover the run |

Both entries uniformly go through the `interventionMsg` helper, which adds the `[User intervention]` prefix — it is the anchor for the intervention classification in `coordinator.md`; once, sending Continue as raw text bypassed the classification and was wrongly dispatched to the writer to edit an already-written chapter (fixed).

`Inject` semantics: while running, jump the current run queue; while idle, auto-recover the run and inject; while paused, queue and wait for recovery.

**The persistence layer for long-lived intervention**: within the intervention classification, "long-lived requirements that only affect subsequent writing" (style/tendency type) are persisted by the Coordinator calling `save_directive` to `meta/user_directives.json` (max 20 entries, add deduplicates / remove by index), and `novel_context` injects them into `working_memory.user_directives` — every subagent sees them automatically each chapter, in effect across compression and across restarts, depending neither on the Coordinator's conversational memory nor on dispatch relaying. The other three kinds of intervention already land in the store anyway (length→compass/outline, settings→foundation, edit old chapters→PendingRewrites). Riding the envelope, not the system prompt: protecting the writer's cross-chapter system-prefix cache.

When each directive is persisted, a **progress snapshot at issue time** is attached (at_chapter / at_total_chapters): the directive takes effect from at_chapter onward (the editor does not retroactively touch old chapters); should a relative directive ("add 10 chapters") be mis-stored as a long-lived requirement, the reader can use the snapshot to judge it already satisfied rather than execute it repeatedly. The proper path for action-type directives is still the write-time translation of the corresponding route (architect/editor → the absolute state of outline/compass/PendingRewrites); the snapshot is the insurance for misclassification.

---

## 9. Directory Structure

```
internal/
  domain/         pure data: Phase / FlowState / Progress / Checkpoint / Scope / Story / Plan /
                  Review / StateChange / Phase-Flow transition rules
  store/          file-system persistence (tmp+rename + the triple): progress / checkpoints / outline /
                  drafts / summaries / characters / world / signals / run_meta / runtime / session
  tools/          11 Agent tools, all write tools do the atomic triple + digest idempotency + ConcurrencySafe=false
                  + premise_structure (used internally by save_foundation) + ask_user
  agents/         build.go assembles the Coordinator + three subagents; ctxpack/ the Writer's context-compression strategy
  host/           host.go + resume.go + observer.go + events.go + usage.go + usage_replay.go
                  + stream_extract.go + cocreate.go
    flow/         router.go (pure function, 11 branches) + state.go + dispatcher.go + router_test.go
    reminder/     stop_guard.go (Coordinator) + subagent_guards.go (CheckpointDeltaGuard ×3)
    imp/          external-novel reverse-engineering import: split → foundation → per-chapter analysis
    exp/          completed-chapter export: merge chapters → TXT / EPUB 3, driven by path suffix; purely read-only, no LLM dependency
  entry/          tui (Bubble Tea) / headless / startup
  bootstrap/      config + ModelSet + provider failover + setup wizard
  models/         public model registry like OpenRouter + price refresh (24h disk cache)
  errs/           error layering
  diag/           read-only diagnostics module subscribing to the host event stream
  utils/          legacy from the old architecture (a few parsing utilities; new code should not depend on it)

assets/
  prompts/        coordinator (~55 lines) / architect-short|long / writer / editor / import-* / simulation-*
  references/     writing techniques + genre templates + long-novel planning, etc.
  styles/         default/fantasy/romance/suspense

../agentcore     general-purpose Agent framework (go.work sibling directory, may add general capabilities, not business ones)
../litellm       LLM gateway
```

### 9.1 Evolution milestones

| Time | Refactor | Net effect |
|---|---|---|
| 2026-04-10 | `internal/orchestrator/` (6342 lines) → `host/` + `agents/` | runtime core -74% |
| 2026-04-20 | Hybrid Coordinator: new `host/flow/`, `reminder/` slimmed down, `coordinator.md` 88 lines → 45 lines | routing error rate approaching 0 |
| 2026-05-02 | agentcore `WithMaxToolErrors(0)` + `isReasoningOnlyStopAssistant`; `StreamIdleTimeout=5min`; deleted the `idleResumeCount` continuation patch | mimo / slow-thinking streaming runs through |
| 2026-06-05 | rolling-planning closed loop (`expand_arc`/`append_volume`) + `/import` reverse-engineered layered continuation + user length intervention | 200+ chapters run through for the first time |

Measured: hy3-preview free 12 chapters / 73 minutes, mimo-v2.5-pro 10 chapters / 84k characters (avg 8400 per chapter), both completed in one run; the long novel gpt-5.4 "Fan Gu" 235 chapters / 1.27M characters / avg 5407 per chapter, with the rolling-planning closed loop running through.

---

## 10. Things explicitly not done

Violating these means the architecture has drifted.

1. **Do not introduce a Task / Job / WorkItem concept**. The "current task" the UI shows is an event-stream projection, not a fact.
2. **Do not introduce a Dispatcher / Scheduler / Ready Evaluator**. Decision power lies with the Coordinator LLM and the tool layer.
3. **Do not build an "idle continuation" mechanism like `idle_dispatch`**. Coordinator Run ends = Host emits done.
4. **Do not bypass the Coordinator in the Host to call SubAgents directly**. The Flow Router has the Coordinator emit tool_calls via `coordinator.FollowUp`. Resume uses `Prompt` to start a new Run.
5. **Do not add an auto-continuation patch in the Host for abnormal LLM shutdown**. Run ends = Host enters its terminal state. The former `idleResumeCount` has been deleted (see §7.3, `feedback_no_host_resilience.md`).
6. **Do not infer task completion from "tool exec end"**. The only evidence of completion is a checkpoint write.
7. **Do not build a four-layer model like WorkflowInstance / TaskInstance / Command + Apply**. The fact layer has only the three kinds Progress + Checkpoint + Artifact.
8. **Do not support parallel tasks**. A single active Coordinator Run, a single book advancing serially. For multiple novels, use multiple processes.
9. **Do not make LLM calls in the tool layer** (except the Agent tools themselves). Pure IO + validation + idempotency.
10. **Do not let the UI read the Store directly**. It may only subscribe to events or read the Host's `Snapshot()`.
11. **Do not use signal files for IPC**. The Host reads Progress + Checkpoint + the layered outline directly; `flow.Route` deriving instructions from facts is reasonable vertical routing.
12. **Do not write a Host-side Flow state machine**. Flow labels are updated only by tools; the Router reads but does not write.
13. **Do not write hardcoded fallbacks for "LLM hallucination"**. Optimize the prompt, improve the structure of tool return values, let `novel_context` present facts more clearly — rather than having the Host force a flow change.
14. **Do not let diag / the observation layer intervene in control flow**. Diagnostics are read-only and only produce Findings and de-sensitized exports; auto-fix / continuation / flow changes are categorically not done (see §2.3, observer discipline).
15. **Budget and alerts do not enter the Route/tool layer, and alerts do not enter control flow**. `BudgetSentinel` is a Host policy component (enforcing the user's pre-signed Abort, not evaluating model behavior); `notify` is purely observational (no retry, no re-dispatch, no shutdown). `flow.Route` stays a pure function, oblivious to both.

---

## 11. Verification Strategy

### 11.1 Stability scenarios

- **A Long run**: 80~200 chapters completed in one run, Phase=complete. provider failover and tools transient retries are allowed; Host continuation or multiple Coordinator Runs are forbidden.
- **B Crash recovery**: after chapter N's draft / before commit, kill the process → Resume → continue from consistency_check without rewriting the persisted draft. `checkpoints.jsonl` has no duplicate steps.
- **C provider jitter**: simulate intermittent 503 → litellm failover; the LLM main loop is unaware.
- **D User intervention**: a runtime Steer → the Coordinator handles it on the next turn; a Steer after shutdown → the next Resume prompt includes it.

### 11.2 Compliance (can be written as a linter / test)

- `internal/host/` must not `import "internal/scheduler"`-style scheduling packages
- the number of lifecycle APIs in `host.go` is stable; newly added public methods may only be the "extension entry" kind (co-creation/import/model management)
- `coordinator.Inject` / `FollowUp` / `Prompt` are not allowed in the `waitDone` function body
- `recovery`-related code may only appear in `host/resume.go`
- `flow.Route` must be a pure function: no reading the Store / any IO

### 11.3 Quality iteration

Editing `writer.md` immediately produces a style change; adding an editor review dimension is backward compatible (save_review accepts structured JSON). Adding a new reference-material md requires wiring in three places (the `tools.References` field + `loadReferences` in `assets/load.go` + the `writerReferences`/`architectReferences` injection in `novel_context.go`); it is not auto-loaded by dropping it into the directory — `References` is an explicit field mapping, convenient for trimming by role/chapter.

**Whole-book-level style statistics (`internal/stylestat`)**: the in-arc review window is naturally blind to whole-book-level ossification like "a sentence tic dozens of times per chapter, isomorphic chapter-end shapes, verbatim re-reading across chapters" — looking at a single chapter, every instance looks normal. The `novel_context` chapter path runs deterministic statistics over all completed chapters (sentence-pattern classes / near-window high-frequency phrases / cross-chapter repeated sentences / chapter-end shape / mixed title formats) and injects them into `episodic_memory.style_stats`: the editor adjudicates by numbers on the aesthetic dimension, and the writer self-avoids accordingly. **Statistics belong to code, adjudication belongs to the LLM** — thresholds are not hardcoded; whether a number has become a disease is judged by the model per genre. Standing alongside it, the product bottom line `rules.Lint` (markdown residue / non-Chinese fragments) is always executed at commit_chapter and only returns facts.

---

## 12. Summary

> **Let the LLM finish a whole novel in a single Run; the Host only handles startup / recovery / routing / observation, fact records are atomically persisted by tools, and decision power is left to the model as much as possible.**

No workflow engine, no task queue, no dispatcher, no scheduler. There is only:

- One 100_000-turn Coordinator
- Three functional subagents (independent context and model)
- 11 atomic tools
- One jsonl checkpoint file
- A ~860-line Host shell
- A ~150-line Flow Router pure function (11 branches + unit tests)

Every line of Host business code is a bet hedging against model upgrades. **The smallest Host, the fattest Prompt (the quality layer), the strongest tools** make the architecture automatically get better every year — the Coordinator decides more accurately, the Writer writes better, the Editor reviews more accurately, the Architect plans more precisely, all gains that the architecture reaps unawares when swapping models.

Conversely, hardcoding rules in the Host like "last review said to rewrite chapters 3 and 5" or "stop after 3 consecutive non-progressing rounds" turns into a **negative return** as the model improves: judgments that should be the LLM's become redundant, and protective logic becomes false alarms. **Worst of all, no one dares delete it — deleting it amounts to "trusting the model", and the psychological baggage is harder to clear than the code**. The more such code is left behind, the higher the future refactoring cost.

**Extensibility comes from the right extension points**: change style → change the prompt; new review dimension → change the prompt; new genre → add reference material; new subagent type → add one line of SubAgentConfig; parallel multiple novels → multiple processes.

The only discipline: **when someone wants to "make the Host a little smarter", first ask "why not make the LLM a little smarter"**. If that question cannot yield a reason why "the Host must", then do not add code to the Host.
