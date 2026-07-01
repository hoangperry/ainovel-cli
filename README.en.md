# ainovel-cli

> [Tiếng Việt](README.md)

A fully automated AI long-form novel writing engine. In a single Prompt, the Coordinator drives three subagents — Architect / Writer / Editor — to complete an entire book, while the Host only handles startup, recovery, and observation. From a one-sentence request to a complete novel, the whole process needs no human intervention.

<p align="center">
  <img src="scripts/sample.gif" alt="ainovel-cli demo" width="800">
  <img src="scripts/novel.png" alt="ainovel-cli bg" width="800">
</p>

## Features

- **Multi-agent collaboration** — The Coordinator schedules the three subagents Architect / Writer / Editor within a single long loop, autonomously deciding the writing process
- **LLM-driven long loop** — A single Prompt writes the whole book; the Host does not intervene in scheduling. The simpler, the more stable — it rejects complex orchestration
- **Step-level checkpoint recovery** — A checkpoint is written after each tool executes successfully, so after a crash recovery is precise down to the plan/draft/check/commit step
- **Two-layer rolling volume-arc planning** — Long-form works no longer plan all chapters at once. Initially only the skeleton of the first 2 arcs plus the detailed chapters of arc 1 are planned; later arcs/volumes are expanded by the Architect as the writing advances to them. Each expansion references the prior-context summary and character state, so the long-range plan is never hollow
- **Smart related-chapter recommendation** — While writing each chapter, the system automatically recommends relevant historical chapters across four dimensions — foreshadowing, character appearances, state changes, and relationships — together with a next-chapter preview, ensuring continuity for long-form works of 500+ chapters
- **Adaptive context strategy** — Automatically switches between full / sliding-window / layered-summary based on total chapter count, supporting 500+ chapter long-form works
- **Seven-dimension quality review** — The Editor reviews across seven dimensions: setting consistency, character behavior, pacing, narrative coherence, foreshadowing, hooks, and aesthetic quality. The aesthetic dimension is further broken into five items (descriptive texture / narrative technique / dialogue distinctiveness / word-choice quality / emotional impact), each of which must cite the original text as evidence
- **Real-time user intervention** — During writing you can inject revision notes into the input box at any time (no pause needed); the system automatically assesses the scope of impact and rewrites affected chapters
- **Unified TUI entry** — An interactive interface observes progress in real time, and also supports launching directly with a single-sentence request
- **Multi-LLM support** — Freely switch between OpenRouter / Anthropic / Gemini / OpenAI, etc.

## Architecture

Core design: **LLM-driven, Host-as-service**. The Coordinator autonomously decides the writing process of the whole book within a single Run; the Host only handles startup, recovery, and event observation.

```
┌─────────────────────────────────────────────────┐
│                Host (thin shell)                 │
│   Startup / Recovery / Observe / Inject intervention │
└──────────────────────┬──────────────────────────┘
                       │ one Prompt
┌──────────────────────▼──────────────────────────┐
│           Coordinator (LLM long loop)            │
│  read novel_context → call subagent → read result → continue │
└────┬──────────┬──────────┬──────────────────────┘
     │          │          │
 ┌───▼────┐ ┌───▼───┐ ┌────▼────┐
 │Architect│ │Writer │ │ Editor  │
 └───┬────┘ └───┬───┘ └────┬────┘
     └──────────┼──────────┘
                │ tool calls (IO + checkpoint)
┌───────────────▼─────────────────────────────────┐
│                   Store                         │
│  Progress / Checkpoint / Outline / Drafts / ... │
└─────────────────────────────────────────────────┘
```

- **Host** — Starts the Coordinator, handles crash recovery, projects events to the TUI. Makes no scheduling decisions
- **Coordinator** — The sole decision-maker, driving the full planning → writing → review → summary process within a single Run
- **SubAgents** — Architect / Writer / Editor each have an independent context and collaborate via artifacts in the Store
- **Tools** — Atomic IO + checkpoint writes, returning only factual JSON, carrying no instructions

### Agent responsibilities

| Agent | Responsibility | Tools |
|--------|------|------|
| **Coordinator** | Schedules globally, handles review adjudication and user intervention | `subagent` `novel_context` |
| **Architect** | Generates premise, outline, character profiles, world rules | `novel_context` `save_foundation` |
| **Writer** | Autonomously handles conception, writing, self-review, and commit of a chapter | `novel_context` `read_chapter` `plan_chapter` `draft_chapter` `check_consistency` `commit_chapter` |
| **Editor** | Reads the original text, reviewing at both the structural and aesthetic layers | `novel_context` `read_chapter` `save_review` `save_arc_summary` `save_volume_summary` |

### Writing process

```
User request → Architect plans skeleton + first-arc chapters → Writer writes chapter by chapter → Editor arc-level review
                                                  ↑                   │
                                                  ├── rewrite/polish ◄──────┘
                                                  │
                                       Architect expands next arc/volume
                                      (referencing prior-context summary + character snapshot)
```

The Writer completes each chapter in a fixed order (the writing content is fully autonomous; the tool-call order is strict):

1. `novel_context` — Loads context (prior-plot summary, foreshadowing, character state, style rules, related-chapter recommendations)
2. `read_chapter` — Re-reads the prior text to recover tone and pacing
3. `plan_chapter` — Conceives this chapter's goal, conflict, and emotional arc
4. `draft_chapter` — Writes the whole chapter body
5. `check_consistency` — Checks consistency against state data (must come after draft)
6. `commit_chapter` — Commits the final draft, returning factual fields (`arc_end_reached` / `next_chapter`, etc.); the next step is driven by the Reminder

### State-transition rules

Internally the system splits the running state into two layers:

- **Phase** — The major stage, indicating whether the work is currently in the setting period, the writing period, or completed
- **Flow** — The currently active flow, indicating whether the system is at this moment writing normally, reviewing, rewriting, polishing, or handling user intervention

#### Phase

`Phase` follows an "advance only, never revert" rule:

```text
init -> premise -> outline -> writing -> complete
  \-------> outline ------^
  \--------------> writing
```

Meaning:

- `init` — The task is created but no stable setting has formed yet
- `premise` — The story premise has been saved
- `outline` — The outline has been saved; formal writing can begin
- `writing` — The chapter-writing period has begun
- `complete` — The whole-book process is finished

Rule notes:

- Same-state updates are allowed, e.g. `writing -> writing`
- Advancing is allowed, e.g. `outline -> writing`
- Reverting is not allowed, e.g. `writing -> premise`, `complete -> writing`

#### Flow

`Flow` describes only the active flow within the writing period, allowing switching among several workflows:

```text
writing   -> reviewing / rewriting / polishing / steering / writing
reviewing -> writing / rewriting / polishing / steering / reviewing
rewriting -> writing / steering / rewriting
polishing -> writing / steering / polishing
steering  -> writing / reviewing / rewriting / polishing / steering
```

Meaning:

- `writing` — Advances to the next chapter normally
- `reviewing` — The Editor is reviewing
- `rewriting` — Handling chapters that must be rewritten
- `polishing` — Handling chapters that only need polishing
- `steering` — Assessing and handling user intervention

Rule notes:

- `writing -> reviewing` is allowed, e.g. a chapter commit triggers review
- `reviewing -> rewriting/polishing/writing` is allowed, decided by the review result
- `steering -> writing/reviewing/rewriting/polishing` is allowed, decided by the intervention's scope of impact
- Clearly abnormal jumps are not allowed, e.g. `rewriting -> reviewing`

These rules are now uniformly enforced by a lightweight validator in the code, preventing state reversion or jumps to unreasonable flow branches.

### Long-form rolling planning

The traditional approach plans all chapters at once; at 300+ chapters the outline turns hollow and the pacing feels like rushing to meet a deadline. This system uses **compass + horizon rolling planning**, simulating the real creative process of a web-fiction author:

```
Initial planning            At arc end                    At volume end
┌────────────────────┐    ┌─────────────────────┐    ┌─────────────────────┐
│ Endgame direction    │    │ Editor arc-level review │    │ Editor volume-level review │
│ (compass)            │    │ Arc summary +          │    │ Volume summary         │
│ Start with 2 volumes,│    │ character snapshot     │    │                        │
│ rest on demand       │ →  │ Architect expands next │ →  │ Architect autonomously │
│ Arc 1 detailed chapters│   │ arc                    │    │ creates next volume +  │
│ Characters + worldview│   │ Writer keeps writing   │    │ updates compass        │
└────────────────────┘    └─────────────────────┘    └─────────────────────┘
```

- **Compass** — Endgame direction + active long threads + scale estimate; updated by the Architect at each volume boundary, so the story direction can evolve along with the writing
- **On-demand generation** — Once the current volume is written, the Architect autonomously creates the next volume based on what has been written. The initial plan generates 2 volumes as a starting point; later volumes are generated on demand
- **Skeleton arcs** — Have only a goal + estimated chapter count; detailed chapters are expanded only on arrival
- **Progressive refinement** — Each expansion references the prior-context summary, character snapshot, and style rules, becoming more precise the further it writes
- **Generic pacing templates** — Growth-breakthrough arc / competitive-confrontation arc / exploration-discovery arc / grievance-conflict arc / everyday-transition arc; each arc type has a reference density and a mapping of suitable genres

### Long-form context management

A 500+ chapter novel uses three-level summaries + a four-level compression pipeline + smart recommendation:

```
Volume → volume summary
└── Arc → arc summary + character snapshot + style rules
    └── Chapter → chapter summary (sliding window of the last 3 chapters)
```

- **Layered summaries** — Nearby uses chapter summaries, mid-distance uses arc summaries, far uses volume summaries — compressing layer by layer without losing information
- **Related-chapter recommendation** — While writing each chapter, it traces back historical chapters across the four dimensions of foreshadowing, character appearances, state changes, and relationships, recommending the Writer re-read on demand
- **Next-chapter preview** — Loads the next chapter's outline, helping the Writer design end-of-chapter hooks and foreshadowing handoffs
- **Arc-boundary detection** — Automatically identifies arc/volume endings, triggering review, summary generation, and the next arc/volume expansion

#### Context compression pipeline

When the conversation exceeds the model's context window, it compresses level by level from lowest to highest cost:

```
ToolResultMicrocompact → LightTrim → StoreSummaryCompact → FullSummary
   clean old tool results   trim long text   store zero-LLM compress   LLM summary fallback
```

- **StoreSummaryCompact** — Writer-only; directly replaces old messages with the chapter summaries, character snapshots, and foreshadowing ledger already in the store, at zero LLM cost
- **FullSummary novel customization** — The Writer uses a summary prompt oriented toward narrative continuity, explicitly requiring retention of character state, foreshadowing threads, pending review fixes, and style anchors
- **Post-compression recovery pack** — After FullSummary it automatically injects the current chapter plan, outline, and character snapshot, preventing the Writer from "amnesia" after compression
- **Circuit breaker** — When compression fails consecutively it automatically skips and warns explicitly, using a half-open mode that auto-retries on the next round
- **CJK token estimation** — Chinese uses `runes × 1.5`, so compression is not triggered late due to a `bytes/4` underestimate
- **TUI health gradient** — Context usage displays in real time: green (<70%) → yellow (70-85%) → red (>85%)

## Quick start

```bash
# One-command install (macOS / Linux, no Go required)
curl -fsSL https://raw.githubusercontent.com/voocel/ainovel-cli/main/scripts/install.sh | sh

# Install a specific version
curl -fsSL https://raw.githubusercontent.com/voocel/ainovel-cli/main/scripts/install.sh | sh -s -- v1.2.3

# Or install via Go
go install github.com/voocel/ainovel-cli/cmd/ainovel-cli@latest

# Check version / update to the latest version
ainovel-cli --version
ainovel-cli update

# On first run, the guided flow starts automatically (choose Provider → enter API Key → Base URL → model name)
ainovel-cli
```

> Windows or manual installation: go to [Releases](https://github.com/voocel/ainovel-cli/releases/latest) to download the package for your platform.

### Docker

The Docker image suits running headless long tasks on a server/NAS, and you can also use `-it` to enter the TUI. It is recommended to mount the config and works directories to the host:

```bash
mkdir -p config workspace

# TUI
docker run --rm -it \
  -v "$PWD/config:/root/.ainovel" \
  -v "$PWD/workspace:/workspace" \
  ghcr.io/voocel/ainovel-cli:latest

# Headless
docker run --rm \
  -v "$PWD/config:/root/.ainovel" \
  -v "$PWD/workspace:/workspace" \
  ghcr.io/voocel/ainovel-cli:latest \
  --headless --prompt "Write a long Eastern fantasy novel; the protagonist starts from a small frontier town"
```

You can also use Compose:

```bash
docker compose run --rm ainovel
docker compose run --rm ainovel --headless --prompt "Write a short suspense story"
```

After entering the TUI, the startup stage supports two preliminary interactions:

- `Quick start`: a single sentence goes straight into writing
- `Co-creation planning`: clarify the requirement through multiple rounds of dialogue with the AI, **with the assembled creation-instruction draft synced live on the right**; each round the AI proactively offers 1-3 guiding suggestions — press a number key to fill the input box, and press `Ctrl+S` to enter formal writing

Both modes ultimately converge into the same creation instruction, then enter the same writing engine.

### Managing multiple novels

Each novel is bound to its launch directory, with outputs landing in `{cwd}/output/novel/`. Launching from a different directory = a different book; `cd` back and launch = automatic recovery from the latest checkpoint. The config `~/.ainovel/config.json` is shared globally, no copying needed.

### Configuration file

On first run it automatically guides you through generating the config file `~/.ainovel/config.json`, which you can then edit directly to adjust settings. Deleting the config file and rerunning re-enters the guided flow.

You can also create the config file manually, referencing `~/.ainovel/config.example.jsonc` (generated automatically during the guided flow).

```jsonc
{
  "provider": "openrouter",
  "model": "google/gemini-2.5-flash",
  "providers": {
    "openrouter": {
      "api_key": "sk-or-v1-xxx",
      "base_url": "https://openrouter.ai/api/v1",
      "models": ["google/gemini-2.5-flash", "google/gemini-2.5-pro"],
      "extra": {
        "user_agent": "my-client/1.0",
        "headers": { "X-Custom-Client": "my-client" }
      }
    }
  },
  "style": "default"
}
```

#### Config file lookup order (later overrides earlier)

1. `~/.ainovel/config.json` — Global config
2. `./.ainovel/config.json` — Project-level override (optional)
3. `--config path/to/config.json` — Specified on the command line

> The project-level `.ainovel/` is a mirror of the global `~/.ainovel/`: same structure, only the root directory changes from the home directory to the current project. Put the config at `./.ainovel/config.json` and writing rules at `./.ainovel/rules/*.md` (see "De-AI tone and custom rules" below). This directory contains secrets and is added to `.gitignore` by default.

Override rule notes:

- Scalar fields follow later-overrides-earlier, e.g. `provider`, `model`, `style`
- `providers` and `roles` merge by key, with same-named entries overriding field by field internally
- Unfilled fields inherit from the upper layer, e.g. if a project-level config writes only `base_url`, it keeps the `api_key` from the global config
- Explicitly clearing an upper-layer value with an empty string is not currently supported; if you need to clear it, edit the higher-priority config file directly

> ⚠️ The value of `provider` (and `roles.*.provider`) is the **key name** in `providers` — a pointer, not a protocol name. If a project level switches `provider` to an account that does not exist in the global `providers`, you must also supply that account's credentials at the project level (`api_key` / `base_url`), otherwise startup will report "no credentials configured".

`providers.<name>.models` is an optional field used to declare the list of models that may be switched in the TUI `/model` panel under that provider; if not configured, the system falls back to the models of that provider that have already appeared in the current config file.

`providers.<name>.extra` is provider-level config passed to the underlying HTTP client, suitable for configuring client-identification fields like `user_agent`, `headers`, `anthropic_beta`; `providers.<name>.extra_body` is the request-body extension parameter — do not mix the two.

## Diagnostic report

In the TUI, enter `/diag` to run a diagnostic analysis on the current novel's output artifacts, producing actionable findings and improvement suggestions.

Diagnosis covers four dimensions:

- **Flow** — Rewrite-loop stalls, unconsumed steering instructions, abnormal Phase/Flow states, chapter number jumps
- **Quality** — Persistently low scores in review dimensions, contract fulfillment rate, rewrite rate, abnormal chapter word counts
- **Planning** — Foreshadowing stagnation, outdated compass, exhausted outline, missing summaries
- **Context** — Vanished characters, timeline gaps, stagnant relationship data

Each finding includes: a problem description, data evidence, and an improvement suggestion (pointing to a specific prompt/flow/config).

`/diag` also writes out a **de-sensitized** `meta/diag-export.md` (removing the novel body, keeping only the behavioral skeleton such as tool calls, error strings, repeat counts, etc.). When you hit infinite-loop / interruption-type problems, just paste it into a GitHub issue so maintainers can locate the issue without access to your local data.

## Simulation profile

Place reference articles into the `simulate/` folder of the current launch directory, then enter `/simulate` in the TUI. The system recursively reads `.txt`, `.md`, `.markdown` files, analyzes the corpus with the architect model, and writes to:

```text
output/novel/meta/simulation_profile.json
```

When you run `/simulate` again, it skips unchanged files by `relative_path + sha256`; if there is no new or changed content, it reports "profile is already up to date" and does not call the LLM. If a profile already exists and `simulate/` has new or modified articles, the system continues synthesizing on top of the existing profile.

You can also import a previously generated profile to avoid re-analyzing the same batch of articles:

```text
/simulate
/importsim ./profile.json
```

`/importsim` accepts only the `simulation_profile.v1` JSON produced by this feature, and merges by corpus fingerprint, skipping duplicate sources. Import profile files only from trusted sources; imported content becomes context reference for subsequent Agents. The profile is injected into `novel_context` in compact form, readable by the Coordinator, Architect, Writer, and Editor; each Agent only learns from the structure, pacing, hooks, and reader-attraction techniques, never copying the original wording or proprietary settings.

## Import

In the TUI, enter `/import <file path>` to reverse-engineer and import an existing novel: first split by chapter, then use the LLM to reverse-derive premise / characters / worldview / layered outline / compass, writing to disk chapter by chapter. The original text is built into volume one as a continuable serial; after the import finishes it **automatically continues writing** — the Coordinator does review/summary at the end of volume one, appends a new volume, and continues from the next chapter.

```
/import ~/my-novel.txt              # Import from the beginning and reverse-derive the foundation
/import ~/my-novel.txt from=50      # Continue importing from chapter 50 (skip reverse-derivation)
```

**Chapter-splitting rules**: It automatically recognizes these title formats (at line start, optionally with a `#`/`##` Markdown prefix, wrapped in `【】`/`〖〗`, full-width spaces, compatible with GBK/BOM encoding):

- Chinese numbering: `第一章` `第3回` `第十话` `第二卷` `第五节` `第二幕`, standalone `卷一`; numbers support uppercase forms (`第壹章`), and may include a subtitle (`第三章：决战`)
- Chinese special units: `序章` `楔子` `引子` `前言` `尾声` `终章` `后记` `番外` `外传`
- English: `Chapter 1` `Chapter II`, `Prologue` `Epilogue`, optionally with a subtitle (`Chapter 1: The Beginning`)

If it reports **"no chapters recognized"**, confirm the file is indeed a chapter-divided novel text (chapter titles on their own line, at the start of the line).

> Import is a deterministic replay, bypassing the Coordinator; the original text is written to disk verbatim as completed chapters, so it suits "continuing the same book". If you only want to borrow settings for a brand-new creation, start a new book the normal way and describe the desired style and settings in your request.

## Export

In the TUI, enter `/export` to merge and export completed chapters, defaulting to TXT, written to `{novelDir}/{NovelName}.txt`. Export is a read-only operation; you can grab the "current-stage product" at any time mid-writing without affecting the running Coordinator.

The format is determined by the **output path suffix** (`.txt` / `.epub`):

```text
/export                            # Default TXT, {novelDir}/{NovelName}.txt
/export ~/light-spot.txt           # Suffix .txt → TXT
/export ~/light-spot.epub          # Suffix .epub → EPUB (readable by Apple Books / WeChat Read / Kindle converters)
/export from=10 to=30 --overwrite  # Chapter range + overwrite
/export from=10 ~/x.epub --overwrite
```

- **TXT** — `《Book Title》` → volume separators → chapter body (long-form layered mode automatically adds volume separators). Two kinds of internal data **do not enter the export**: premise (the creative blueprint, containing backstage information like target readers / writing no-go zones, written for the author and the engine), and arc separators (from the reader's viewpoint, arcs are over-fine internal structure). The exporter uniformly generates "Chapter N Title"; a duplicate title the writer included in the body (`# 第N章…` or `# chapter name`) is stripped off.
- **EPUB** — A standard EPUB 3 container with a cover page, table of contents, and per-chapter XHTML; the identifier is stably derived from the content (re-exporting the same book is recognized by readers as an updated version). It carries no cover image.

Unfinished chapters within the range are skipped and shown in the result, not counted as errors.

#### Using different models per role

Use the `roles` field to assign different models to different agents; roles not configured use the default model:

```jsonc
{
  "provider": "openrouter",
  "model": "google/gemini-2.5-flash",
  "providers": {
    "openrouter": { "api_key": "sk-or-v1-xxx", "base_url": "https://openrouter.ai/api/v1" },
    "anthropic": { "api_key": "sk-ant-xxx" }
  },
  "roles": {
    "writer": { "provider": "anthropic", "model": "claude-sonnet-4" },
    "architect": { "provider": "openrouter", "model": "google/gemini-2.5-pro" }
  }
}
```

Configurable roles: `coordinator` / `architect` / `writer` / `editor`

#### Custom proxy

After choosing any Provider, just fill in the proxy address, or use Custom Proxy and specify the API protocol type. The `api_key` for a custom proxy is optional; if your proxy needs no authentication, you can omit it:

```jsonc
{
  "provider": "my-proxy",
  "model": "gpt-4o",
  "providers": {
    "my-proxy": {
      "type": "openai",
      "base_url": "https://proxy.example.com/v1",
      "extra": {
        "user_agent": "my-client/1.0",
        "headers": { "X-Custom-Client": "my-client" }
      }
    }
  }
}
```

Supported Providers: `openrouter` / `anthropic` / `gemini` / `openai` / `deepseek` / `qwen` / `glm` / `grok` / `ollama` / `bedrock` and any custom proxy.

If the proxy uses the Anthropic protocol and requires client-identification fields, `type` should be set to `anthropic`, `anthropic_beta` placed at the top level of `extra`, and HTTP headers like Stainless placed in `extra.headers`:

```jsonc
{
  "provider": "claude-proxy",
  "model": "claude-sonnet-4-6",
  "providers": {
    "claude-proxy": {
      "type": "anthropic",
      "api_key": "sk-xxx",
      "base_url": "https://proxy.example.com",
      "extra": {
        "user_agent": "claude-code/2.1.183",
        "anthropic_beta": "claude-code-20250219",
        "headers": {
          "X-Stainless-Lang": "js",
          "X-Stainless-Package-Version": "0.94.0",
          "X-Stainless-Runtime": "node"
        }
      }
    }
  }
}
```

About `api_key`:

- Hosted interfaces like `openrouter` / `anthropic` / `gemini` / `openai` / `deepseek` / `qwen` / `glm` / `grok` usually require an `api_key`
- `ollama` and `bedrock` allow omitting `api_key`; Bedrock requires configuring `region`, `access_key_id`, `secret_access_key` in `extra` (optionally `session_token`)
- A custom proxy with an explicitly specified `type` is allowed to omit `api_key`

For example, a local `ollama` config:

```jsonc
{
  "provider": "ollama",
  "model": "qwen3:latest",
  "providers": {
    "ollama": {
      "base_url": "http://localhost:11434/v1"
    }
  }
}
```

### Writing style

Switch via the `style` field of the config file:

- `default` — General style
- `suspense` — Suspense / mystery
- `fantasy` — Fantasy / xianxia
- `romance` — Romance

### De-AI tone and custom rules

A de-AI-tone baseline is built in (under `assets/`, the factory default): a mechanical blacklist `rules/default.md` (stock phrases / fatigue words, deterministically checked at commit time) + semantic criteria `references/anti-ai-tone.md` (injected into the writer / editor for avoidance and evidence-citing).

To layer on your own preferences **without modifying source code**: in the `~/.ainovel/rules/` directory (global, place any `.md`, merged by filename lexical order) or the `./.ainovel/rules/` directory (per book, also place any `.md`, in the same form as global), **just write your preferences in plain language** (e.g. "don't write the protagonist as a saint", "use more bodily perception"), and the editor will review semantically — zero format, zero YAML. For hard, deterministic checks like "word count / forbidden words", **optionally** add a front-matter block at the top of the file. Nearest overrides, layered on top of the built-in baseline; for the full set of fields see [`rules.md.example`](rules.md.example).

## Output structure

All creation data (chapters, outline, characters, progress, etc.) is saved in the output directory. After an interruption, rerunning automatically continues writing from the last progress. Deleting the output directory restarts the creation from scratch.

```
output/{novel_name}/
├── chapters/           # Final drafts (Markdown)
│   ├── 01.md
│   └── ...
├── summaries/          # Chapter summaries (JSON)
├── drafts/             # Chapter drafts
├── reviews/            # Review reports
├── meta/
│   ├── premise.md      # Story premise
│   ├── outline.json    # Flat chapter outline (only expanded chapters)
│   ├── layered_outline.json # Layered outline (current volume + preview volume, long-form mode)
│   ├── compass.json   # Endgame-direction compass (long-form mode)
│   ├── characters.json # Character profiles
│   ├── world_rules.json# World rules
│   ├── progress.json   # Progress state
│   ├── timeline.json   # Timeline
│   ├── foreshadow.json # Foreshadowing ledger
│   ├── state_changes.json # Character state-change records
│   ├── style_rules.json# Writing style rules (distilled at arc boundaries)
│   ├── snapshots/      # Character state snapshots (long-form)
│   ├── checkpoints.jsonl # Step-level checkpoints (appended after each successful tool)
│   ├── characters.md   # Character profiles (readable version)
│   └── world_rules.md  # World rules (readable version)
```

## Checkpoint recovery

Writing a long novel can take hours or even days; crashes, network drops, and Ctrl+C mid-way are all common. The system **automatically recovers when rerun in the same directory**, with no manual operation needed.

### Recovery scenarios

| Interruption moment | Recovery behavior |
|---|---|
| Planning stage (building worldview/outline) | Checks saved settings, automatically fills in missing items |
| A chapter being written (draft uncommitted) | Continues from that chapter, reading the existing draft to proceed |
| Review in progress | Re-triggers the Editor review |
| Rewrite/polish queue not cleared | Continues processing chapters pending rewrite |
| Arc/volume expansion interrupted (review done but next arc not expanded) | Automatically detects skeleton arc/volume, triggers Architect expansion |
| User intervention unfinished | Re-injects the previous intervention instruction |
| Normal writing interrupted | Continues from the next chapter |

### How it works

All creation artifacts are persisted in the `output/` directory. A checkpoint (`meta/checkpoints.jsonl`) is written after each tool executes successfully. On restart:

1. Read `progress.json` + the latest checkpoint + pending signals
2. Generate a step-level recovery instruction (e.g. "chapter 7's draft is written to disk; please continue with check_consistency")
3. A single `Prompt` launches the Coordinator, entering the long loop to continue writing

> File writes use atomic temp + fsync + rename operations, so even a power loss mid-write will not corrupt existing data.

## Real-time intervention (Steer)

During creation you can inject revision notes through the input box at any time, **with no pause or restart needed**.

### TUI mode

After creation starts, the bottom input box automatically switches to intervention mode:

```
❯ Move the romance line up to chapter 4, add more sparring scenes between the male and female leads
```

After entering and pressing Enter, the system automatically:
1. Records the intervention instruction to `run.json` (for crash recovery)
2. Injects it into the running Coordinator
3. The Coordinator assesses the scope of impact and decides whether to modify settings, rewrite existing chapters, or adjust in later chapters

### Intervention examples

| Intervention instruction | Possible system response |
|---|---|
| "Make the protagonist female" | Modifies character settings, assesses whether already-written chapters need rewriting |
| "Move the romance line up to chapter 4" | Adjusts the outline, possibly rewriting chapter 4 onward |
| "Add a villain character" | Updates character profiles and world rules, introduces them in later chapters |
| "The pacing is too slow, speed it up" | Adjusts the outline density of later chapters |

## Design philosophy

> **Move complexity from the code into the model.** Less code means fewer things that can break. Hand decision-making to the role better at making decisions.

### LLM-driven — the simpler, the more stable

- **Decision-making belongs to the LLM** — All process decisions are made autonomously by the Coordinator; the Host does not intervene. On tool failure, a structured error is returned, and the LLM itself decides to retry or adjust strategy
- **Tools return only facts** — Atomic IO + checkpoint writes; return values are factual JSON fields (`final_verdict` / `pending_rewrites` / `arc_end_reached`), carrying no instruction strings
- **Reminder drives each round** — Before each LLM call, the Host reads the fact layer and runs a pure-function generator to produce a `<system-reminder>` for injection; the instruction does not enter persistent history and is recomputed from facts each round
- **StopGuard as a physical gate** — When `Phase ≠ Complete`, the Coordinator physically cannot `end_turn`; only when consecutive blocks exceed the limit does it escalate to termination
- **Rejecting complex orchestration** — No task queue, no scheduler, no policy engine. A single Run of the Coordinator is the only control flow
- **The stronger the model, the greater the gain** — The architecture keeps decision-making in the prompt and tool semantics, so a model upgrade directly reaps the benefit without changing a single line of the Host

### Fully automated closed loop

A single sentence in, a complete novel out:

```
"Write a suspense novel" → build worldview → design characters → plan outline
                → write chapter by chapter → quality review → automatic rewrite
                → arc-level summary → character snapshot → complete book
```

- **Coordinator autonomous scheduling** — Within a single long loop it reads the fact layer + Reminder to decide the next step, no Host intervention needed
- **Writer autonomous creation** — Each chapter independently completes the full plan → draft → check → commit closed loop
- **Editor autonomous review** — Analyzes cross-chapter structural issues, outputting adjudications and scope of impact
- **Architect autonomous construction** — Derives a complete setting from a one-sentence request, autonomously expanding subsequent planning at arc/volume boundaries
- **Automatic foreshadowing management** — Planting, advancing, and recovering are all tracked by the Agent itself throughout
- **Automatic pacing regulation** — Tracks the history of narrative threads and hook types, avoiding structural sameness across consecutive chapters

### Decoupling facts from instructions

Tools return only facts; instructions are recomputed each round from the fact layer by the Reminder:

- `commit_chapter` / `save_review` return structured facts (`final_verdict` / `pending_rewrites` / `arc_end_reached` / `next_chapter`), carrying no `[system]` strings
- The pure-function generators under `internal/host/reminder/` read `Progress` + `Outline`, generating a `<system-reminder>` each pre-turn round: `flow` (what to do now / arc-end braking) / `queue_guard` (no new chapter while the queue is uncleared) / `book_complete` (only released when the whole book is complete). The physical backstop is borne by `StopGuard`, which refuses `end_turn` while `phase≠Complete`
- The Reminder lives for only one round — it does not enter history and does not participate in compression; the rules have unit tests, so regression can catch any degradation

This way the instruction is not swallowed by chained calls, nor does it drift inside tool artifacts. Fixing a bug only takes adding one generator + one test.

## Tech stack

- **Go 1.25** — Main language
- **[agentcore](https://github.com/voocel/agentcore)** — A minimal Agent kernel (tool-calling + streaming)
- **[litellm](https://github.com/voocel/litellm)** — Unified LLM interface adaptation
- **[Bubble Tea](https://github.com/charmbracelet/bubbletea)** — Terminal TUI framework

## License

MIT

This project actively participates in and endorses the [linux.do community](https://linux.do/).
