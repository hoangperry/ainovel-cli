> [Tiếng Việt](refactor-flow-driven.md)

# Refactor Proposal: Hybrid Coordinator — Host Routing × LLM Adjudication

> Status: **Adopted and landed** (2026-04-20)
> Research date: 2026-04-20
> Corresponding current docs: `docs/architecture.md` §2 / §3 / §7 / §8 / §13 updated in sync
>
> **This document is the second draft.** The problems of the first-draft radical approach (fully removing the Coordinator) are detailed in Appendix A; that section is kept to avoid repeating the same detour.
>
> Landing results:
> - `internal/host/flow/` created (router.go / state.go / dispatcher.go / router_test.go, all 15 branch unit tests pass)
> - `internal/host/reminder/` deleted `flow.go` / `queue_guard.go` / `book_complete.go`; kept StopGuard and the subagent Guard
> - `assets/prompts/coordinator.md` compressed from 88 lines to ~45 (responsibilities narrowed to executing Host instructions + adjudication + startup selection)
> - `internal/host/resume.go` greatly simplified, only generates a label and a short prompt; the concrete next step is dispatched by the Router after the first TurnEnd
> - `internal/store/` added helper methods `HasArcReview` / `HasArcSummary` / `HasVolumeSummary` / `CheckConsistency`
> - the `observer.go` bug where agent state was stuck in `working` is fixed along the way

---

## 1. Background

### 1.1 Project positioning

```
agentcore       — generic agent framework
litellm         — generic LLM gateway
ainovel-cli     — novel-writing vertical agent (this project)
```

A vertical agent's decision space is **closed**: the flow chart is fixed, branches are finite, and it is fact-driven. The design philosophy of a generic agent ("bet on model capability") applied to a vertical scenario smells of excessive purism.

### 1.2 User goals (by priority)

1. **Stability** — keep writing continuously, never interrupted by routing errors
2. **Reap LLM-upgrade dividends** — the architecture must not fight model capability
3. **Fully leverage multi-agent capability** — clear division of responsibilities

This proposal makes a **Pareto improvement** across all three (no goal is sacrificed to trade for another).

---

## 2. Current-State Research

### 2.1 Classification of Coordinator decision points

Extracting `coordinator.md` decision points one by one:

| # | Decision point | Nature | Frequency |
|---|---|---|---|
| 1 | Select architect_long / short at startup | Adjudication (semantic understanding) | once per book |
| 2 | Input expansion (auto-augment when <20 chars) | Adjudication (creative) | 0-1 times per book |
| 3 | Planning completion loop | Routing (fact-driven) | 1-3 times |
| 4 | Next step after each chapter commit | **Routing** | **1-2 times per chapter** |
| 5 | Step-by-step execution of end-of-arc review | Routing | 3-5 times per arc |
| 6 | Review verdict fork | Routing (already coded, see §2.3) | once per arc |
| 7 | User-intervention handling | Adjudication (must be LLM) | arbitrary |
| 8 | Subagent error re-dispatch | Routing | occasional |
| 9 | Output summary when the whole book is done | Routing | once |

**Conclusion**: of the 9 decision points, 6 are pure routing (table lookup), and 3 genuinely need an LLM to adjudicate. **Routing happens far more frequently than adjudication** (1-2 times per chapter vs a few times per book).

### 2.2 The Reminder channel is already a half-finished form of code-driven flow

The generators under `internal/host/reminder/` produce **action-specific instructions** from facts every turn:

- `flow.go` → `"current flow=writing, next_chapter=37. Please call subagent(writer, \"write chapter 37\") directly..."`
- `queue_guard.go` → `"current flow=rewriting, pending queue: [3,5]. Please call writer immediately to rewrite chapter by chapter..."`
- `book_complete.go` → `"the whole book is done. Please output the full-book summary..."`

**The current architecture has a double dispatch**:
```
Rules layer: coordinator.md defines "if A then B"
  ↓
Reminder layer: each turn turns the rule concrete from facts → generates "now please do B"
  ↓
LLM layer: reads the reminder, generates a tool_call (essentially restating the reminder)
  ↓
SubAgent executes
```

**The LLM is effectively just "executing" the instruction the Reminder gave it**. This middle link both consumes tokens and introduces uncertainty (the LLM may not fully obey the reminder, e.g. the observed mid routing error).

### 2.3 The tool layer already carries a lot of judgment

- `save_review.evaluateScorecardGate()`: scorecard gate, automatically upgrades accept to polish/rewrite
- `save_review.ContractStatus` check: contract=missed auto-upgrades to rewrite
- `commit_chapter.CheckArcBoundary()`: computes `arc_end / needs_expansion / needs_new_volume` on the spot
- `commit_chapter.applyCompletion()`: decides `book_complete` on the spot
- `CommitResult` returns 17 fact fields

**Conclusion**: the tool layer has already coded most of the "judgment"; the decisions the Coordinator makes from these facts are basically if-else.

### 2.4 Actual cost of the current state

Per-chapter Coordinator LLM turns:
- **1-2 turns per chapter** (read system prompt ~3000 tokens + reminder ~200 tokens + history + CommitResult ~500 tokens → generate tool_call ~50 tokens)
- A 200-chapter long novel: roughly **200-400 turns** of Coordinator LLM calls
- Of which **~90% is pure routing** (LLM restating the reminder), **~10% is adjudication**

**~3500-7000 tokens per chapter are spent on Coordinator decisions, 95% of which is redundant** (the Reminder already computed the answer).

---

## 3. Design: Hybrid Coordinator

### 3.1 Core idea

**Move flow decisions from the LLM to the Host, but keep the Coordinator as an adjudication node and instruction-execution channel**.

```
┌──────────────────────────────────────────────────────────┐
│                   Entry (TUI / headless)                   │
└────────────────────────────────┬─────────────────────────┘
                                 │ Start / Resume / Steer
┌────────────────────────────────▼─────────────────────────┐
│                            Host                            │
│                                                             │
│   ┌──────────────────────────────────────────────────┐     │
│   │  Flow Router (new core)                           │     │
│   │  ───────────                                      │     │
│   │  Subscribes to Coordinator events: triggers when  │     │
│   │  a subagent tool returns                          │     │
│   │  Pure function: route(Progress, Checkpoint,       │     │
│   │      Boundary) → NextInstruction                  │     │
│   │  Has instruction → coordinator.FollowUp(instr)    │     │
│   │  No instruction (adjudication) → no intervention, │     │
│   │      let the LLM decide                           │     │
│   └──────────────────────────────────────────────────┘     │
│                                                             │
│   Kept: lifecycle API / Observer / Usage Tracker           │
│   Kept: resume.go (simplified, core logic unchanged)        │
└────────────────────────────────┬─────────────────────────┘
                                 │
┌────────────────────────────────▼─────────────────────────┐
│                    Coordinator Agent (LLM)                  │
│                                                             │
│   Responsibilities narrowed to two kinds:                   │
│   1. Receive Host FollowUp instruction → generate the       │
│      corresponding tool_call                                │
│   2. Adjudicate autonomously when a user Steer arrives       │
│      (query/modify evaluation)                              │
│                                                             │
│   coordinator.md: 88 lines → ~25 lines                      │
│   MaxTurns: 1000 kept (respond to user steer + execute      │
│      Host instructions)                                     │
└────────────────────────────────┬─────────────────────────┘
                                 │
                                 ▼
         ┌──────────────────────┼───────────────────────┐
         ▼                      ▼                       ▼
    ┌────────┐             ┌────────┐             ┌────────┐
    │Architect│             │ Writer │             │ Editor │
    └────────┘             └────────┘             └────────┘
```

### 3.2 Re-division of responsibilities

| Layer | Does | Does not |
|---|---|---|
| **Host / Flow Router** | read facts → pure-function routing → FollowUp instruction | call the SubAgent itself (still via the Coordinator) |
| **Coordinator** | execute Host instructions + adjudicate user intervention + select planner at startup | autonomously decide "what to do next" |
| **SubAgent (A/W/E)** | their own jobs | no change |
| **Tool layer** | atomic persistence + return facts | no change |

**Key invariants**:
- ✅ The Coordinator is still a single continuous agent run, preserving whole-book "continuous awareness"
- ✅ User Steer still goes through `coordinator.Inject()`, the immediate-interrupt capability is preserved
- ✅ The SubAgentTool is still called by the LLM (the agentcore-native path); event stream / ContextManager / model switching are all unchanged
- ✅ Zero changes to agentcore

### 3.3 Concrete logic of the Flow Router

```go
// internal/host/flow/router.go

type NextInstruction struct {
    Agent  string   // architect_long / architect_short / writer / editor
    Task   string   // task description for the subagent
    Reason string   // rationale shown to the Coordinator (optional, eases debugging)
}

type RouterState struct {
    Progress        *domain.Progress
    LatestCheckpoint *domain.Checkpoint
    // arc boundary in layered mode (computed when the previous chapter is done)
    LastCompleted   int
    ArcBoundary     *store.ArcBoundary
    HasArcReview    bool
    HasArcSummary   bool
    // missing foundation items
    FoundationMissing []string
}

// Route returns the next instruction. Returning nil means let the Coordinator adjudicate (adjudication scenario).
func Route(s RouterState) *NextInstruction {
    p := s.Progress

    // 0. Terminal state: let the LLM output a summary, do not route
    if p.Phase == domain.PhaseComplete {
        return nil
    }

    // 1. Planning phase: adjudication (pick planner) done by the LLM, do not route
    if p.Phase != domain.PhaseWriting {
        return nil
    }

    // 2. Writing phase
    // 2a. Rewrite/polish queue first
    if len(p.PendingRewrites) > 0 {
        ch := p.PendingRewrites[0]
        verb := "rewrite"
        if p.Flow == domain.FlowPolishing {
            verb = "polish"
        }
        return &NextInstruction{
            Agent:  "writer",
            Task:   fmt.Sprintf("%s chapter %d", verb, ch),
            Reason: fmt.Sprintf("PendingRewrites queue has %d chapters left", len(p.PendingRewrites)),
        }
    }

    // 2b. Under review: do not route, let the Coordinator follow the verdict fork from save_review
    if p.Flow == domain.FlowReviewing {
        return nil
    }

    // 2c. End-of-arc post-processing in layered mode
    if p.Layered && s.ArcBoundary != nil && s.ArcBoundary.IsArcEnd {
        b := s.ArcBoundary
        if !s.HasArcReview {
            return &NextInstruction{
                Agent:  "editor",
                Task:   fmt.Sprintf("do an arc-level review of volume %d arc %d", b.Volume, b.Arc),
                Reason: "end-of-arc review not done",
            }
        }
        if !s.HasArcSummary {
            return &NextInstruction{
                Agent:  "editor",
                Task:   fmt.Sprintf("generate the summary for volume %d arc %d", b.Volume, b.Arc),
                Reason: "arc summary not done",
            }
        }
        if b.NeedsExpansion {
            return &NextInstruction{
                Agent:  "architect_long",
                Task:   fmt.Sprintf("expand volume %d arc %d (save_foundation type=expand_arc)", b.NextVolume, b.NextArc),
                Reason: "next arc skeleton pending expansion",
            }
        }
        if b.NeedsNewVolume {
            return &NextInstruction{
                Agent:  "architect_long",
                Task:   "evaluate and execute save_foundation(type=append_volume) or mark_final",
                Reason: "volume ended, must decide whether to append a new volume",
            }
        }
    }

    // 2d. Normal continuation
    next := p.NextChapter()
    return &NextInstruction{
        Agent:  "writer",
        Task:   fmt.Sprintf("write chapter %d", next),
        Reason: "continue writing",
    }
}
```

**Function properties**:
- Pure function (input RouterState, output NextInstruction)
- Unit-testable (given a state, assert the routing result)
- **Returning nil is legitimate** — it means "this is an adjudication scenario, let the LLM decide"

### 3.4 Trigger timing

The Host subscribes to the `agentcore.EventToolExecEnd` event:

```go
coordinator.Subscribe(func(ev agentcore.Event) {
    if ev.Type == agentcore.EventToolExecEnd && ev.Tool == "subagent" && !ev.IsError {
        // SubAgent just returned → read latest state → route
        h.flowRouter.Dispatch()
    }
})
```

```go
func (r *FlowRouter) Dispatch() {
    state := r.loadState()
    instruction := Route(state)
    if instruction == nil {
        return // adjudication scenario, let the LLM decide
    }
    msg := formatInstruction(instruction)
    _ = r.coordinator.FollowUp(agentcore.UserMsg(msg))
}

func formatInstruction(i *NextInstruction) string {
    return fmt.Sprintf(
        "[Host instruction] Next step: call subagent(%s, %q)\n"+
        "Reason: %s\n"+
        "This is an explicit flow-layer instruction; execute it immediately, do not call novel_context first, do not output reasoning first.",
        i.Agent, i.Task, i.Reason,
    )
}
```

### 3.5 Responsiveness and concurrency

**User Steer path** (unchanged):
```
Steer → coordinator.Inject(UserMsg("[user intervention] xxx"))
```

- Running: the message is inserted into the current run queue
- Idle: resume run
- Paused: queued

**Concurrency of routing instruction + Steer**:
- Both enter the Coordinator's message queue, processed in agentcore-native order
- If the Host just sent `FollowUp("[Host instruction] write chapter 37")`, immediately followed by a user Steer `"hold on, adjust the style"`
  - Does the Coordinator process the Host instruction first? Or the Steer first?
  - **The semantics of `Inject` is to jump to the head of the current queue**, so the Steer is processed first
  - This is the desired behavior: user intervention has higher priority than routine Host scheduling

**Avoiding conflict between Host instruction and Steer**:
- After receiving a "Steer injected" signal, the Flow Router **briefly pauses** for a few turns (letting the Coordinator finish the Steer before routing)
- It senses the Steer-processing result by subscribing to `agentcore.EventMessageEnd` + checking Progress state changes

### 3.6 coordinator.md simplification example

Cut from 88 lines to about 25:

```markdown
You are the chief coordinator of novel creation.

## Your working mode

**Main line**: After each subagent returns, the Host issues a `[Host instruction]` message telling you which subagent to call next and what to do. On receiving the instruction, immediately generate the corresponding tool_call; do not call novel_context to reason first, do not restate.

**Adjudication**: In the following situations you must judge autonomously (the Host issues no instruction, you must act proactively):

### At startup: pick a planner

- Default → `architect_long`
- Only when the user explicitly asks for a short piece / single volume / vignette and length is capped within 25 chapters → `architect_short`

If the user input is < 20 chars, first add a differentiated direction, target readership, and at least one unconventional story hook in the task description, then dispatch.

### User Steer

Format: `[user intervention] xxx`

- **Query type** (asking about state/setting): output a text answer directly, no need to call a tool again; the Host will keep dispatching.
- **Modify type** (asking to change settings/rewrite/adjust style): assess the scope of impact:
  - Involves setting changes → call architect_* to do `save_foundation(type=...)`
  - Involves already-written chapters → let the tool automatically write the target chapters into `PendingRewrites` (you can state the rewrite intent the next time you call writer)
  - Only affects subsequent style → after a short description of the requirement, attach it to the writer's task description the next time you receive a Host instruction

## Tools

- `subagent(agent, task)`: call a subagent
- `novel_context`: use only when a user query needs it, do not call it first when a Host instruction arrives

## Subagents

- `architect_long` / `architect_short` / `writer` / `editor`

## Forbidden

- Calling novel_context first before acting when a Host instruction arrives
- Deciding the next step on your own with no user Steer and no Host instruction
```

### 3.7 The Reminder channel slims down significantly

**Deleted**:
- `flow.go` (the Host FollowUp already issues a concrete instruction; the Reminder's routing prompt loses value)
- `queue_guard.go` (the queue is guaranteed by the Host Router)
- `book_complete.go` (the Host FollowUps a summary-output instruction when Phase=Complete)

**Kept**:
- `subagent_guards.go` (the StopGuard for Writer/Architect/Editor, ensuring subagents do not finish empty-handed)
- A new lightweight `foundation_reminder.go`: tells the Coordinator about missing items in the planning phase (this is **information the adjudication needs**, not a routing instruction)

**StopGuard kept**:
- The Coordinator's StopGuard is kept (intercepts end_turn as a fallback when `Phase != Complete`)
- Add a reminder when "a Host instruction was received but the corresponding subagent was not called this turn"

### 3.8 Minor simplification of resume.go

The current `buildResumePrompt` generates step-precise natural-language instructions from the checkpoint (121 lines).

New architecture:
- On Resume, read Progress first; the Flow Router computes the `NextInstruction`
- The Coordinator receives a **very short** resume prompt, then waits for the Host's FollowUp instruction

```
[Resume] The book "xxx" has completed N chapters and entered the XX phase.
Please wait for the Host's next instruction, or handle any user intervention left during downtime.
```

Almost all branch logic sinks into the Flow Router (the Router has to route by state anyway, Resume needs no special path).

---

## 4. Goal-Attainment Assessment

### 4.1 Stability

| Risk | Current | New architecture |
|---|---|---|
| Coordinator picks the wrong architect | happened (mid routing error) | still adjudicated at startup, but the prompt went from three options to binary (done), shrinking the error surface |
| Coordinator disobeys "only say write chapter N" | happened | Host issues a fixed-format instruction, no longer needs the LLM to generate the task description |
| Coordinator misses the queue_drained check | happened | Host Router forces the order |
| Coordinator forgets to call editor after an end-of-arc commit | possible | Host Router detects IsArcEnd && !HasArcReview and dispatches directly |
| Crash-recovery branch omission | known gap | the Flow Router's state machine naturally covers all branches |
| StopGuard escalates to fatal after 5 consecutive intercepts | exists | once the Host instruction is explicit the LLM is unlikely to intercept repeatedly (unless the prompt is severely broken) |

### 4.2 LLM-upgrade dividend

| Dimension | Retention |
|---|---|
| Writer model upgrade → writing quality | 100% |
| Editor model upgrade → review accuracy | 100% |
| Architect model upgrade → finer planning | 100% |
| **Coordinator model upgrade → more accurate adjudication** | **100%** (adjudication scenarios kept) |
| ~~Coordinator model upgrade → more accurate routing~~ | dropped (routing error rate should already be 0, no need for the LLM to get smarter) |

**Important retention**: adjudication scenarios such as user-intervention assessment, planner selection, and verdict boundary judgment are still handled by the LLM, benefiting directly from model upgrades.

### 4.3 Multi-agent capability

- Number, function, and assembly of SubAgents are **completely unchanged**
- Model heterogeneity (coordinator/architect/writer/editor configured independently) is **completely unchanged**
- The Coordinator is still a continuous run, preserving the "whole-book view"
- The collaboration medium (artifacts in the Store) is unchanged

### 4.4 Responsiveness

- The ability to interrupt via `coordinator.Inject` for a user Steer is **fully preserved**
- The Host Router dispatches instructions when a SubAgent returns, going through the same message channel as a user Steer
- Inject has higher priority than FollowUp (`Inject` semantics is queue-jumping), a Steer will not be squeezed out by a Host instruction

### 4.5 Token cost

Current per chapter: Coordinator ~3500-7000 tokens × 1-2 turns = 3500-14000 tokens

New architecture per chapter:
- Coordinator prompt compressed from ~3000 tokens to ~800 tokens
- Still 1 turn per chapter (Coordinator reads the FollowUp instruction + generates a tool_call)
- Total ~1000-1500 tokens

**Saving 60-80%**. A 200-chapter long novel saves about 400k-1M tokens (not the radical approach's 100%, but without sacrificing responsiveness or the whole-book view).

---

## 5. Impact on docs/architecture.md

### 5.1 §2 core-principle adjustment

**Principle 1** (LLM-driven main loop) → adjusted to:
```
The LLM drives creation and adjudication; the Host drives flow routing.

- Creation and adjudication (decisions needing semantic understanding, quality judgment,
  intent recognition) stay with the LLM
- Flow routing (read facts → look up table → issue instruction) is carried by Host code
- The Host does not bypass the Coordinator to call the SubAgent directly; it issues an
  explicit instruction via FollowUp, keeping the Coordinator as the instruction-execution
  channel and adjudication node
```

**Principle 2** (bet on model capability, not on hardcoding) → adjusted to:
```
Bet on the model in the creation and adjudication dimensions (Writer/Editor/Architect/
Coordinator adjudication capability), express the flow-routing dimension in code (a vertical
agent's decision space is closed, and table-lookup tasks yield the LLM no dividend).
```

### 5.2 §13 prohibition-list adjustment

- §13.13 "do not build a deterministic control plane where the Host reads a signal file → injects the next-step instruction" →
  **revised wording**: "do not use a signal file for IPC (just read Progress + Checkpoint directly); the Host reading facts and then issuing an explicit subagent-call instruction via `coordinator.FollowUp` is reasonable vertical routing"
- §13.14 "do not hardcode a state machine for Flow transitions" →
  **revised wording**: "the Flow label is still updated only by tools (no 'if A then SetFlow(B)' state machine inside the Host), but the Flow Router may decide whom to call next based on the Flow and other facts"

### 5.3 §7 agent-assembly adjustment

- Keep the Coordinator assembly
- `coordinator.md` cut from 88 lines to ~25
- The Reminder channel shrinks (delete flow/queue_guard/book_complete, keep foundation/subagent_guards)
- Add the `internal/host/flow/` package

---

## 6. Known Weaknesses (listed honestly)

### 6.1 Long-term evolution of the Flow Router

- As new scenarios are added (new flow states, new end-of-arc post-processing), the Router's switch-case grows longer
- Strict constraint needed: **handle only routing, not business logic**; write decision rules as unit tests
- The warning of the v0.0.1 `handleSubAgentDone` is always valid; but this approach avoids sliding toward a god object via "pure function + unit tests + only consume pure facts"

### 6.2 Complexity of user intervention

- The current design fully hands the Steer to the Coordinator's LLM to adjudicate
- But some Steers span multiple categories (e.g. "make character A clearer in the early chapters + add a side plot for him later")
- This relies on the LLM's capability to decompose; the prompt must give clear guidance
- **This part benefits directly from model upgrades** (compared with a hardcoded InterventionAgent enum classification, flexible LLM adjudication matches real scenarios better)

### 6.3 Up-front dependency on fact-layer consistency

- The Router decides based on Progress + Checkpoint, so the fact layer must be reliable
- The current `withWriteLock` is well-encapsulated, and commit_chapter's three-piece set completes atomically
- But if the fact layer becomes inconsistent (e.g. Progress says chapter 3 is done but it is not under chapters/), the Router makes the wrong decision
- Suggestion: add a **fact-layer consistency check** at startup (if Progress.CompletedChapters does not match the chapters/ directory, raise a warning)

### 6.4 The Coordinator still keeps the possibility of LLM routing

- Even with an explicit instruction, the LLM may "creatively" not execute it (e.g. generate a paragraph of reasoning before calling the tool)
- StopGuard fallback: inject a reminder when a Host instruction is received but no subagent is called this turn
- This is a fallback, not a prohibition — a strong model's occasional "one more step of thinking" is not bad either

### 6.5 Higher test-coverage requirement

- The Flow Router is a pure function and must have complete unit tests (covering all Phase × Flow × Boundary combinations)
- Integration test: simulate the full chain "commit → router → FollowUp → coordinator response → subagent"
- Crash-recovery test: kill the process then resume, assert the Router derives the correct next step

---

## 7. Implementation Roadmap

### Phase 1: Fact-layer hardening (~0.5 day)

- Complete the §6.3 consistency check: scan once at startup/Resume, generate a warning
- Ensure `store.HasArcReview(vol, arc)` and `HasArcSummary(vol, arc)` APIs are available (add them if not)

### Phase 2: Introduce the Flow Router skeleton (~1 day)

- Create the `internal/host/flow/` package:
  - `route.go` — pure function `Route(state) → *NextInstruction`
  - `dispatcher.go` — subscribe to events + FollowUp dispatch
  - `route_test.go` — unit tests covering all branches
- Control activation via a config switch `flow_driven: true/false`
- Off by default (false), run a comparison first

### Phase 3: Activate and validate (~1 day)

- Turn `flow_driven: true` on
- Run a 30-50 chapter novel and compare metrics:
  - Number of Coordinator LLM calls
  - Number of routing errors (should be 0)
  - Responsiveness (does the steer interrupt work normally)
- Fix bugs, adjust Router rules

### Phase 4: coordinator.md simplification + Reminder slim-down (~0.5 day)

- Change coordinator.md per §3.6
- Delete `reminder/flow.go / queue_guard.go / book_complete.go`
- Keep the necessary foundation reminder
- Update the subagent StopGuard if needed (usually not)

### Phase 5: resume.go simplification (~0.5 day)

- Delete most branches of `buildResumePrompt`
- Replace with a short generic "[Resume] please wait for the Host instruction" message
- After Resume the Router naturally derives the continuation action

### Phase 6: Architecture-doc update (~0.5 day)

- Modify `docs/architecture.md` §2 / §13 / §7 per §5
- Change this proposal's status to "Adopted", archive to `docs/history/`

### Phase 7: Observation period (2-4 weeks)

- Run 2-3 long novels continuously (100+ chapters each)
- Record all routing errors (if any), responsiveness issues, Coordinator unexpected behavior
- Fine-tune the Router rules and coordinator.md based on observation

**Total roughly 4 days of implementation + observation period.**

---

## 8. Comparison Table

| Dimension | Current architecture | Hybrid (this proposal) | Radical approach (Appendix A) |
|---|---|---|---|
| Stability | medium (LLM occasionally mis-routes) | **high** | high |
| Responsiveness | high | **high** | **low** (Host calls SubAgent directly, cannot interrupt) |
| LLM dividend | 100% | **100%** | 85% (routing dimension dropped) |
| Token saving | 0 | ~70% | ~95% |
| Whole-book view | yes | **yes** | no (each SubAgent independent) |
| Implementation cost | - | medium (~4 days) | high (~1 week + agentcore changes) |
| Doc update | - | small (§2/§13 tweaks) | large (§2 principles rewritten) |
| Needs agentcore change | - | no | maybe (call SubAgent directly) |
| Rollback difficulty | - | low (config switch) | high |

---

## 9. Decision Points

1. **Adopt this proposal (Hybrid Coordinator)?** [ ] Adopt · [ ] Adopt after revision · [ ] Reject
2. Should Phase 3 land first as a standalone PR for validation? [ ]
3. Should the `docs/architecture.md` §2 / §13 adjustments be handled together this time? [ ]
4. Observation-period length: [ ] 2 weeks · [ ] 4 weeks · [ ] longer

---

## Appendix A: Evaluated Radical Approach (fully removing the Coordinator)

> First-draft approach. Downgraded to reference because of responsiveness regression, questionable technical feasibility, and loss of the Coordinator's whole-book view.

The core of the radical approach: the Host calls `SubAgentTool.Execute` directly, without the Coordinator LLM.

**Identified problems**:

1. **Responsiveness regression**: `SubAgentTool.Execute` is a blocking synchronous call; a user Steer must wait until the current SubAgent returns before it can be handled. The current architecture's `Inject` can interrupt immediately.
2. **Questionable technical feasibility**:
   - The Host calling SubAgentTool directly violates agentcore usage convention
   - The event stream (the Event of `Subscribe`) may not bubble up to the observer correctly
   - The SubAgent's `ContextManagerFactory` / `OnMessage` callback paths are unknown
   - Requires changing agentcore or heavily reworking the observer
3. **Loss of the Coordinator's whole-book view**: each SubAgent is an independent run, with no "continuous LLM watcher". Over a long run, problems like style drift and character fragmentation lose a layer of invisible protection.
4. **InterventionAgent over-simplified**: the radical approach classifies user intent with an enum (query/modify_setting/rewrite_chapters/adjust_style/noop); a real Steer may span multiple categories, and forcing a schema would misclassify.
5. **Large architecture-doc rewrite effort**: the §2 core principles are overturned, affecting 30% of the doc's argument.
6. **FlowDriver would grow into a god object**: one loop stuffed with all routing logic, changed every time a scenario is added, isomorphic to the v0.0.1 `handleSubAgentDone`.

The Hybrid approach avoids the first 4 problems, reduces the 5th to a tweak, and controls the 6th via "pure function + unit tests".

---

## Appendix B: Detailed Placement of Decision Points

| Decision point | Current location | New location | Type |
|---|---|---|---|
| Pick planner | coordinator.md L26-29 | Coordinator LLM adjudication (at startup) | adjudication |
| Input expansion | coordinator.md L31 | Coordinator LLM adjudication (at startup) | adjudication |
| Planning completion loop | coordinator.md L36-38 | Host Router Phase=Premise/Outline branch (return nil to let the LLM decide, or explicit FollowUp architect) | hybrid |
| Next step per chapter | coordinator.md L46-51 + reminder/flow | **Host Router 2d branch** (FollowUp writer) | routing |
| End-of-arc review | coordinator.md L78-82 | **Host Router 2c branch** (FollowUp editor/architect) | routing |
| verdict fork | coordinator.md L59-61 + save_review tool | already coded in the tool layer, the Router only reads Flow | routing (done) |
| User intervention | coordinator.md L67-70 | Coordinator LLM adjudication (when an Inject message arrives) | adjudication |
| Planner error re-dispatch | coordinator.md L40 | Host Router detects FoundationMissing unchanged, retry counter | routing |
| Whole-book completion summary | coordinator.md L63-65 + reminder/book_complete | Host Router detects Phase=Complete → FollowUp "output summary" | routing |

---

## Appendix C: Reference Source Locations

- `assets/prompts/coordinator.md` — to be simplified
- `internal/host/reminder/flow.go` / `queue_guard.go` / `book_complete.go` — to be deleted
- `internal/host/reminder/subagent_guards.go` — kept
- `internal/host/reminder/stop_guard.go` — kept + add a "must execute a received Host instruction" check
- `internal/host/resume.go` — greatly simplified
- `internal/host/observer.go` — new subscription to EventToolExecEnd to trigger the Router
- `internal/host/flow/` — new package
- `internal/tools/commit_chapter.go` L220-280 — the 17 CommitResult fields are complete
- `internal/tools/save_review.go` L76-116 — verdict upgrade and Flow transition already coded
- `internal/store/outline.go` `CheckArcBoundary` — arc-boundary fact API
