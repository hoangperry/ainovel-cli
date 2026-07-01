# Context Management Guide

> [Tiếng Việt](context-management.md)

This document describes the current context-management system of `ainovel-cli`, covering:

- Why context management is needed
- Where context comes from
- How it is compacted, restored, and handed off at runtime
- The value, trigger conditions, and applicable scenarios of each strategy
- Where to look first when something goes wrong

The goal is not to introduce abstract concepts, but to let a future maintainer open this single document and quickly understand the current implementation and the entry point for troubleshooting.

## 1. Design Goals

Context management in this project is not a general chat scenario but is oriented toward novel-writing. It has to solve several classes of problems at once:

1. Long conversations will exceed the model's context window.
2. What novel writing needs to retain is not the "chat history itself" but structured narrative memory.
3. After compaction, the Writer must not lose character state, foreshadowing, chapter plans, style constraints, or pending review fixes.
4. When resuming writing, we cannot assume the model still "remembers what was discussed before"; it must rely first on persisted artifacts.

Therefore we adopt a "layered memory" scheme:

- Short-term memory: the tail of the most recently retained messages
- Mid-term memory: the `ContextSummary` produced by compaction
- Long-term memory: structured artifacts in the project store
- Recovery memory: handoff / restore pack / novel_context

## 2. Overall Architecture

### 2.1 Main Layers

Current context management is split into four layers:

1. `agentcore/context`
   Responsible for the general-purpose context budget, strategy pipeline, and compaction/recovery framework.

2. `internal/tools/novel_context`
   Responsible for assembling the structured data of a novel project into context usable for the current turn.

3. `internal/orchestrator/store_summary_*`
   Responsible for the Writer-specific store-based fast compaction.

4. `internal/orchestrator/writer_restore.go`
   Responsible for appending a post-compaction restore pack after `FullSummary`, ensuring the Writer can keep writing.

### 2.2 Data Flow

At runtime there are two main context paths:

1. Normal work path
   - The Agent calls `novel_context`
   - `novel_context` reads chapter summaries, plans, characters, timeline, and other data from the store
   - This data enters the current turn's prompt

2. Context-too-long path
   - The `ContextManager` detects token pressure
   - Compacts in strategy order
   - Prefers lightweight compaction and store-based compaction first
   - Only falls back to the LLM `FullSummary` when those are still insufficient
   - Injects a restore pack after `FullSummary`

## 3. Key Files

### 3.1 General-Purpose Context Engine

- `../agentcore/context/strategy.go`
- `../agentcore/context/engine.go`
- `../agentcore/context/strategy_tool.go`
- `../agentcore/context/strategy_trim.go`
- `../agentcore/context/strategy_summary.go`
- `../agentcore/context/message.go`
- `../agentcore/context/summary_run.go`

Purpose:

- Define `Strategy` / `ForceCompactionStrategy`
- Execute the strategy chain based on the budget
- Represent `ContextSummary` and convert it for the LLM
- Perform the LLM summary compaction of `FullSummary`

### 3.2 Project-Side Wiring

- `internal/orchestrator/agents.go`

Purpose:

- Assemble the `ContextManager` for the Writer / Coordinator
- Inject the extra `StoreSummaryCompact` for the Writer
- Configure the novel-customized `FullSummary` prompt for the Writer
- Configure `writerRestorePack` for the Writer

### 3.3 Project-Side Compaction and Recovery

- `internal/orchestrator/store_summary_strategy.go`
- `internal/orchestrator/store_summary_builder.go`
- `internal/orchestrator/writer_restore.go`

Purpose:

- Before the LLM summary, prefer using store data for fast compaction
- Uniformly build the structured context needed for the Writer's compaction and recovery
- Append a pure in-memory restore message after `FullSummary`

### 3.4 Structured Context Assembly

- `internal/tools/novel_context.go`
- `internal/tools/novel_context_builders.go`
- `internal/domain/runtime.go`

Purpose:

- Define `ContextProfile` / `MemoryPolicy`
- Decide how many chapter summaries and how much timeline to load, and whether to enable layered summaries
- Assemble chapters, characters, foreshadowing, timeline, review experience, and so on from the store

### 3.5 Handoff and Recovery

- `internal/orchestrator/handoff_policy.go`
- `internal/orchestrator/recovery_engine.go`

Purpose:

- Prefer relying on handoff during long-form / rework / review stages
- Splice the structured handoff pack into the prompt on recovery

### 3.6 Observability

- `internal/orchestrator/run.go`
- `internal/orchestrator/runtime.go`
- `internal/entry/tui/panels.go`

Purpose:

- Record context-rewrite events
- Output the strategy name, token change, and number of messages retained
- Let the TUI see whether the current context is `projected` or `compacted`

## 4. How the ContextManager Is Assembled

Both the Writer and the Coordinator go through `newContextManager`, but with different configurations.

The key parameters of the current `contextManagerConfig`:

- `ContextWindow`
  The model's total context window.

- `ReserveTokens`
  Tokens reserved for the model's output.

- `KeepRecentTokens`
  The budget for the tail of recent messages that compaction tries to keep.

- `ToolMicrocompact`
  Tool-result microcompact configuration.

- `ExtraStrategies`
  Extra project-side compaction strategies. The Writer currently uses this to attach `StoreSummaryCompact`.

- `Summary`
  The configuration for `FullSummary`, including the custom prompt and post-summary hook.

The actual current configuration values:

| Parameter | Writer | Coordinator |
|------|--------|-------------|
| ReserveTokens | 16,384 | 32,000 |
| KeepRecentTokens | 20,000 | 30,000 |
| CommitOnProject | false | true |
| IdleThreshold | 5min | none |
| ExtraStrategies | StoreSummaryCompact | none |
| Custom Summary Prompt | novel-narrative version | default (code-assistant version) |

Compaction trigger threshold = `ContextWindow - ReserveTokens`. For example, with a 128K window, the Writer triggers at ~112K and the Coordinator at ~96K.

The Writer's current strategy pipeline order is:

1. `ToolResultMicrocompact`
2. `LightTrim`
3. `StoreSummaryCompact`
4. `FullSummary`

This order has a clear meaning:

- First use the cheapest method to clean up tool noise
- Then trim overly long text blocks
- If the store data is sufficient, do a zero-LLM structured compaction directly
- Only fall back to the LLM summary last

## 5. The Role of Each Strategy

### 5.1 ToolResultMicrocompact

Implementation location:

- `../agentcore/context/strategy_tool.go`

Purpose:

- Clean up historical `tool_result` entries
- Replace old tool results with short placeholder text

Value:

- Tool return content is usually large in size and low in information density
- Many old tool results are just "process noise," not novel memory

Configuration traits of the current Writer:

- Sets `IdleThreshold = 5m`

This means:

- If the most recent assistant message has been idle past the threshold
- It will more aggressively reduce the number of old tool results retained

Applicable scenarios:

- Many turns of `novel_context`
- After many turns of read / check / draft tools

### 5.2 LightTrim

Implementation location:

- `../agentcore/context/strategy_trim.go`

Purpose:

- Truncate very long text blocks
- Keep the head and tail, replacing the middle with a placeholder

Value:

- Keeps the message structure unchanged
- Low cost
- Well suited for handling overly long original chapter text or large output blocks

Applicable scenarios:

- A single message is too long, but the whole history does not yet need a summary

### 5.3 StoreSummaryCompact

Implementation location:

- `internal/orchestrator/store_summary_strategy.go`
- `internal/orchestrator/store_summary_builder.go`

Purpose:

- When the Writer's context is too long
- Prefer using the structured memory in the persisted store to replace old messages
- Does not call the LLM

It is not a conversation summary but a "structured memory replacement."

The core data currently retained includes:

- Current progress
- Most recent chapter summaries
- Current chapter plan
- Current chapter outline
- Current arc summary
- Current volume summary
- Character snapshots
- Active foreshadowing
- Pending review issues to fix
- Most recent timeline
- Style rules

Trigger preconditions:

- The current chapter is greater than 1
- The store already has enough historical summaries
- And the current chapter has at least working-state data
  - `chapter_plan` or `current_outline`

Value:

- Reduces the number of LLM compactions
- Avoids drift of the novel's key information during summarization
- Lets long-term memory rely first on persisted facts rather than chat history

Why it is for the Writer only:

- This is a novel business strategy, not a general-framework strategy
- The Coordinator / Editor have different context modes
- Validating it first on the Writer — which most needs continuous creative memory — is the most reasonable approach

### 5.4 FullSummary

Implementation location:

- `../agentcore/context/strategy_summary.go`
- `../agentcore/context/summary_run.go`

Purpose:

- When the layers above are still insufficient, use the model to generate a `ContextSummary`
- Keep the tail of recent messages
- Turn earlier context into a structured checkpoint

Where the Writer differs from the default code assistant:

- The Writer uses a custom summary prompt
- The summary content explicitly requires retaining:
  - Current progress
  - Immediate character state
  - Active foreshadowing and clues
  - Review feedback and pending fixes
  - Style and pacing
  - Key decisions
  - Next step
  - Key context

Value:

- It is the final fallback strategy
- Even when store data is insufficient, continuity can still be maintained through the LLM

### 5.5 Circuit Breaker

Implementation location:

- `../agentcore/context/engine.go`

Purpose:

- When compaction fails consecutively up to the threshold (default 3 times), skip compaction for the current turn
- When skipping, it still emits a `RewriteEvent` (`Reason = "circuit_breaker"`)
- The TUI shows the scope as "circuit-breaker skip"
- Uses a half-open mode: after skipping one turn, it retries next time; on success it resets, and on another failure it skips again

Why it is needed:

- The LLM summary may fail consecutively due to network issues, model refusals, and so on
- Without the circuit breaker, every Project turn would attempt and fail, wasting API calls
- In a long-form writing session this waste accumulates

Troubleshooting:

- If the TUI continuously shows "circuit-breaker skip," the LLM summary path has a problem
- Check the context-rewrite events with `reason=circuit_breaker` in slog
- The circuit breaker does not affect `StoreSummaryCompact` (which does not call the LLM)

### 5.6 Token Estimation (CJK-Aware)

Implementation location:

- `../agentcore/context/usage.go`

Purpose:

- All budget control and compaction-trigger timing depend on token estimation
- `estimateTextTokens` automatically detects whether the text is mainly CJK characters
- CJK-dominant text: `runes × 1.5`
- ASCII-dominant text: `bytes / 4`

Why the standard `bytes/4` cannot be used:

- One Chinese character in UTF-8 = 3 bytes
- `bytes/4` would estimate one Chinese character as 0.75 token, when it is actually about 1.5 token
- Underestimating by 2x would cause compaction to trigger far too late

Scope of impact:

- `EstimateTokens` (a single message)
- `EstimateTotal` (a list of messages)
- `EstimateContextTokens` (mixed estimation: LLM-reported Usage + tail-message estimation)
- The budget trimming in `store_summary_builder.go`

Note: a ToolCall's args are JSON (ASCII-dominant) and still use `bytes/4`, unaffected by the CJK adjustment.

## 6. Why the Writer Has Two Sets of "Post-Compaction Memory"

The current Writer has two paths that look similar but have different responsibilities:

### 6.1 StoreSummaryCompact

Responsibility:

- Replace old messages directly during the compaction process

Traits:

- Occurs before `FullSummary`
- Zero LLM
- Replaces earlier history with the store

### 6.2 writerRestorePack

Implementation location:

- `internal/orchestrator/writer_restore.go`

Responsibility:

- Append a restore message after `FullSummary`

Traits:

- Occurs after the LLM compaction
- Injected via `PostSummaryHook`
- Used to supplement the structured information the Writer must see when resuming creation

Why both are needed:

- `StoreSummaryCompact` does not always hit
  - For example, in the first chapter or when store data is insufficient
- No matter how well `FullSummary` is done, it may still miss precise information in the store
- So the restore pack serves as the last line of insurance

These two now share `store_summary_builder.go`, avoiding drift in their definitions.

## 7. The Role of novel_context

Implementation location:

- `internal/tools/novel_context.go`
- `internal/tools/novel_context_builders.go`

`novel_context` is not a compaction strategy; it is the runtime "structured context assembler."

It divides the data in the store into several categories:

- `working_memory`
  - Current chapter plan
  - Current chapter outline
  - Most recent chapter summaries
  - Timeline
  - checkpoint
  - previous tail

- `episodic_memory`
  - Character state
  - Relationship state
  - Most recent state changes
  - Foreshadowing

- `reference_pack`
  - More stable setting and reference data

- `selected_memory`
  - A small amount of important memory selected for the current task

Value:

- It determines the structured novel context actually "fed to the model" each turn
- `StoreSummaryCompact` does not call it directly, but reuses the same kind of data source and assembly approach

## 8. ContextProfile and MemoryPolicy

Implementation location:

- `internal/domain/runtime.go`

### 8.1 ContextProfile

Purpose:

- Decide the load window size based on the total chapter count

Current rules:

- `<= 15` chapters
  - The `10` most recent chapter summaries
  - The `10` most recent chapter timelines

- `<= 50` chapters
  - The `5` most recent chapter summaries
  - The `8` most recent chapter timelines

- `> 50` chapters
  - The `3` most recent chapter summaries
  - The `5` most recent chapter timelines
  - Enable layered summaries

Value:

- Controls the context size
- Avoids stuffing all history into the prompt for long-form work

### 8.2 MemoryPolicy

Purpose:

- Make the current context-usage strategy explicit
- Provide it for `novel_context` to output
- Provide it for handoff / reminder / diagnostic logic

Key fields:

- `SummaryWindow`
- `TimelineWindow`
- `LayeredSummaries`
- `SummaryStrategy`
- `HandoffPreferred`
- `ReadOnlyThreshold`

Value:

- Turns "how the current system should use memory" from implicit logic into an explicit runtime strategy

## 9. The Role of handoff

Implementation location:

- `internal/orchestrator/handoff_policy.go`

When the work enters a longer, more complex stage that relies more on structured artifacts, the system leans toward handoff.

The handoff pack records:

- The current stage and flow
- The next chapter position
- The most recent commit
- The most recent review
- The most recent summary
- The current memory policy
- The recovery guidance text

Value:

- Recovery from interruption does not depend on chat history
- In rework, review, and long-form scenarios it prefers structured artifacts

## 10. Observability and Troubleshooting

### 10.1 Context-Rewrite Events

Implementation location:

- `internal/orchestrator/run.go`

Every context rewrite is output via `contextRewriteCallback`:

- `reason`
- `strategy`
- `committed`
- `tokens_before`
- `tokens_after`
- `messages_before`
- `messages_after`
- `compacted_count`
- `kept_count`
- `split_turn`
- `incremental`
- `summary_runes`
- `duration_ms`

This simultaneously enters:

- `slog`
- the runtime boundary queue
- the TUI `COMPACT` event

### 10.2 What You Can See in the TUI

The TUI displays:

- The current context tokens (with a health-graded color gradient)
- The context window
- The current context scope (including "circuit-breaker skip")
- The name of the most recent strategy
- The summary count

The meaning of the context-percentage colors (implemented in `internal/entry/tui/layout.go`):

| Color | Condition | Meaning |
|------|------|------|
| Green | < 70% | Ample, far from the compaction threshold |
| Yellow | 70-85% | Approaching the compaction threshold |
| Red | > 85% | About to compact or compacting |

The Scope labels:

| Scope | Display | Meaning |
|-------|------|------|
| baseline | baseline | Normal state |
| projected | projected | Temporary compaction preview |
| compacted | committed | Compaction has taken effect |
| recovered | recovered | Recovered after overflow |
| skipped | circuit-breaker skip | Compaction skipped by the circuit breaker |

Value:

- You can quickly judge the current context health
- When yellow/red, you can expect compaction to happen soon
- Seeing "circuit-breaker skip" means the LLM summary path has a problem

### 10.3 Where to Look First When Something Goes Wrong

#### Scenario 1: The Writer loses the chapter plan after compaction

Look first at:

- Whether `novel_context` stably injects `chapter_plan`
- Whether `store_summary_builder.go` obtains `chapterPlan`
- Whether `writerRestorePack` is refreshed

Key files:

- `internal/tools/novel_context_builders.go`
- `internal/orchestrator/store_summary_builder.go`
- `internal/orchestrator/session.go`

#### Scenario 2: Character state / foreshadowing is lost after compaction

Look first at:

- `LoadLatestSnapshots`
- `LoadActiveForeshadow`
- `store_summary_builder.go`
- Whether the Writer summary prompt is being overridden

#### Scenario 3: Compaction is frequent but always misses store_summary

Look first at:

- Whether the current chapter is `<= 1`
- Whether recent summaries / arc / volume summary already exist
- Whether `chapter_plan` or `current_outline` exists
- Whether `writer.Context.Strategy` ultimately records `full_summary`

#### Scenario 4: Context is insufficient after recovery

Look first at:

- Whether the handoff was generated
- Whether the restore pack was refreshed
- Whether the recovery prompt injected the handoff

#### Scenario 5: Too many tool results bloat the context

Look first at:

- Whether `ToolResultMicrocompact` hit
- Whether `IdleThreshold` took effect

## 11. Trade-offs of the Current Implementation

### Directions Explicitly Committed To

1. Do not stuff novel business logic into `agentcore`
2. Rely first on the structured store rather than chat history
3. The Writer uses a dedicated novel-summary prompt
4. Compaction and recovery share a builder as much as possible to avoid definition drift

### Limitations Currently Retained on Purpose

1. `StoreSummaryCompact` is for the Writer only
2. The first chapter will not hit store-based compaction
3. When store data is insufficient, it still falls back to `FullSummary`
4. `writerRestorePack` is an append-style compensation; it does not replace `FullSummary`

These limitations are not defects but boundaries set at the current stage to control complexity.

## 12. One-Sentence Summary

Context management in this project is not as simple as "compressing a long conversation into a short one." It is:

`Prefer using structured novel memory to maintain continuity, and only let the LLM summarize the conversation when necessary; and across all three stages of compaction, recovery, and handoff, rely on the same set of persisted artifacts as much as possible.`

If you later want to change this system, prioritize holding the following three lines:

1. Do not let the Writer's key memory depend solely on chat history again.
2. Do not let `store_summary` and `writer_restore` diverge in their definitions.
3. When continuity problems arise, first check whether the structured artifacts entered the context, then decide whether to change the prompt.
