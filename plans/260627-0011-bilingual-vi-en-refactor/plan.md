# Bilingual VI+EN Refactor — Architecture & Plan

**Project:** ainovel-cli (Go 1.25.5, module `github.com/voocel/ainovel-cli`)
**Goal:** Bilingual Vietnamese + English, **Vietnamese primary**. Two independent axes:
1. **UI locale** (what the operator reads: TUI, errors, setup, logs) — VI/EN switch, VI default.
2. **Output language** (what the LLM writes the novel in) — configurable, VI/EN/original, VI default.
**Code comments:** Vietnamese, keeping English technical terms.

**Status:** IN PROGRESS — Waves 1–4 + Wave 5 STEP 1 (seam) + straggler sweep DONE & verified (`go test ./...` + gofmt + i18n parity/race green; 529 vi=en keys; 0 operator-facing CJK). Branch `refactor/bilingual-vi-en` (local only, uncommitted). **REFACTOR COMPLETE** — go build/vet/test ./... all green (20 pkgs), -race green, i18n parity 539=539 keys, 501 contentlang.Pick wraps, 34 vi asset files.

**FINAL STATE (all waves done):** i18n UI (539 keys), comments VI, docs VI+.en.md, output_lang seam (vi default → AI writes Vietnamese), asset locale-subdir zh/vi (translated), straggler (tool labels+slog), Track-C in-source wrapped in contentlang.Pick (tool Description/Schema, ctxpack, cocreate, host, domain MemoryPolicy), operator CLI (version/update + bootstrap) → i18n cli.* catalog. Fixed init-time freeze: const/var using Pick → functions (restore.go, cocreate.go, rules/loader.go homeRulesReadme, tui cocreate opener). Gotcha: fmt.Errorf(i18n.T(key)) with NO args trips Go1.25 non-constant-format vet → use errors.New(i18n.T(key)); with args is fine.

**Ultra-review (16-agent adversarial, 2026-06-29):** 9 findings → verified → 4 confirmed (ALL low/cosmetic), 5 refuted as correct-by-design. NO critical/high. Init-freeze AST audit = ZERO (self-validated detector). Parser-coupling intact (14 premise heading tokens present in vi prompts). All 4 confirmed FIXED & verified (build/vet/test green): (1) main.go SetLang moved before version/update branches + early env-based SetLang for flag-parse errors (--lang/AINOVEL_LANG now honored on all early paths); (2) panels.go:235 dead `待命` branch deleted; (3) resume.go:38 restored `[恢复]` routing anchor in vi arg (was `[Khôi phục]`, broke coordinator.md match); (4) import-{chapter-analyzer,foundation}.md vi fallback values 本章→"Chương này", 推断→"suy luận".

**Intentional residual CJK (by design, must stay zh):** premise_structure.go (parser heading tokens matched by Go), imp/splitter.go (regex parsing imported zh novels), store/* (data round-trip serialization), stylestat (CJK-prose analysis), domain placeholders 书名/实际书名, locale-gated headerZH (paired w/ headerVI). All other CJK = wrapped Pick zh-args (locale-selected, not defects).

**Wave 5 step 2 — vi/ asset tree fully translated zh→VI:** all 33 asset files (9 prompts, 14+6 references, 4 styles, 1 rules) under assets/*/vi/. Parser-coupled architect-*/import-* keep structural heading tokens verbatim (题材和基调…14 headings, # 实际书名 placeholder, 书名 forbidden token — all matched by novel_context_builders.go premise parser + runtime.go ExtractNovelNameFromPremise; verified present in vi). rules/vi/default.md re-authored (forbidden_chars=[], VI fatigue/forbidden phrases). styles done by orchestrator directly. Residual CJK in vi/ assets is intentional (parser/protocol tokens + a few free-text examples kept matching architect convention). zh/ tree untouched (original mode).

**REMAINING (optional, deferred): in-source Track C LLM-facing strings** — tool Description()/Schema() property text, ctxpack builders (restore.go/builder.go), host/cocreate.go + cocreate_stage.go prompts/status builder, premise_structure.go builders, rules/loader.go homeRulesReadme, build.go logRulesLoaded attrs, ParseThinkingLevel/stop_guard inject. These are sent to the LLM but NOT locale-switched; making them per-output-lang needs threading outputLang through tool constructors + ctxpack (substantial). Functionally: with output_lang=vi the model already writes Vietnamese (vi assets + directive); multilingual model understands zh tool schemas. So product works in Vietnamese today; this is polish toward 100%.

**Wave 5 step 2 — locale-subdir architecture DONE & green (vi=zh copies pre-translation):** assets restructured into `assets/{prompts,references,styles,rules}/{zh,vi}/`; `//go:embed` dir-level; `assetLocale(outputLang)` maps original→zh, vi/en→vi; `Load`/loaders thread `loc`; `simulationGuidance(loc)` per-lang (zh + vi consts added in load.go). Build+test green with vi as zh copies. THEN: asset translation workflow (vi/ zh→VI) running — parser-coupled architect-*/import-* keep structural heading tokens verbatim (the "14 二级标题 一字不差" matched by novel_context_builders.go); rules/vi/default.md re-authored (zh forbidden_chars/fatigue_words → VI). In-source Track C NOT in scope (tool Description()/Schema(), host/cocreate.go prompts, cocreate_stage.go status builder, rules/loader.go homeRulesReadme) — flagged for optional follow-up; LLM is multilingual so Chinese tool-docs + VI assets + VI directive still yield VI output.

**Straggler landed (v2):** tool Label()/ActivityDescription wired to ui.tool.* catalog (14 files); 32 slog message sites → log.* catalog (31 keys). Minor leftovers flagged for final cleanup: build.go logRulesLoaded slog *attr keys/value*, host emitEvent Summary TUI strings, ParseThinkingLevel error. Remaining non-test CJK is Track C (LLM-facing): assets/*.md, in-source prompt consts (host/cocreate.go, cocreate_stage.go status builder, assets/load.go simulationGuidance, rules/loader.go homeRulesReadme), tool Description()/Schema() strings.

**Wave 4 landed:** README.md + assets/README.md + docs/*.md (4) translated zh→Vietnamese (primary, overwrites original) + English sibling `.en.md`; language-switch links at top. Residual CJK only in byte-coupled examples (chapter regex, commit-msg literals, character-name examples). Docs-only, no build impact.

**Wave 5 step 1 (seam) landed:** `assets.Load(style, outputLang)` injects per-lang `languageDirective` (vi/en consts; `original`=no-op) into all 5 creative-role prompts via `assembleCreative`. `main.go` passes `cfg.ResolveOutputLang()`. CJK-assuming mechanical rules gated at the call site: `CommitChapterTool.WithOutputLang()` + `filterCJKAssumingRules` drops `non_cjk_fragments`+`chapter_words` for vi/en (kept Lint/Check signatures → zero test churn; gating respects rules pkg "facts only, caller decides" design). Wired at build.go:137 + host.go:1106. Tests: assets/load_test.go (directive injection), commit_chapter_lang_test.go (filter). With default config (OutputLang=vi) the AI now writes Vietnamese. `chapter_words` range for VI deferred to step 2 (needs per-lang rules file).

**Wave 3 landed:** code comments zh→VI across ~140 files (~2750 lines), keeping EN technical terms + identifiers + string literals. Residual CJK comments 2879→24, all intentional (byte-exact protocol tokens `[用户干预]`/`[阶段规划]`, chapter-regex examples `第N章`, byte-example `中` — translating would misdescribe the code). String literals untouched (Wave 2/5 scope).

**Remaining stragglers (deferred clusters):** tool Label() (TUI-visible) + slog operator logs → small sweep; tool Description()/schema text → LLM-facing → Wave 5; in-source Track C prompt consts → Wave 5.

**Wave 2 landed:** 481 catalog keys (vi=en parity), interactive operator UI migrated across TUI / setup / die / notify / diag / host+tools errors. Catalog split into cat_{ui,host,diag,setup,misc}_{vi,en}.go. Residual CJK in .go dropped 2522→1021; the remainder is NOT missed work — it splits into: code comments (Wave 3), tool Description()/schema property text (LLM-facing → Wave 5 w/ output_lang), tool Label() + slog operator logs (small straggler sweep → Wave 3 extra unit, namespace `ui.tool.*`/`log.*`), and in-source Track C prompt consts (Wave 5). Paired test assertions updated in lockstep (exporter_test, rules_quality_test, draft_chapter_test, save_review_test).

**Wave 1 landed:** `internal/i18n/` (i18n.go + lang_vi/en.go seed + tests: parity, verb-parity, fallback, %w-preserve, dup-panic); `bootstrap.Config.UILang`/`OutputLang` + FillDefaults + ValidateBase enum + `ResolveOutputLang` + mergeConfig clauses (+ regression test); `main.go` `--lang` flag + `AINOVEL_LANG` + `resolveLang` precedence + `i18n.SetLang`; migrated `die()` strings as wiring proof; documented keys in `config.example.jsonc`. Catalog uses per-area `Register()` in `init()` so Wave-2 agents own disjoint files (no shared write contention).

---

## 0. Two Independent Localization Axes (do NOT conflate)

| Axis | Config field | Consumed by | Mechanism |
|------|-------------|-------------|-----------|
| **UI locale** | `ui_lang` (`UILang`) | TUI / setup / `die()` / host notify / diag | in-repo message catalog (`internal/i18n`) |
| **Output language** | `output_lang` (`OutputLang`) | agent SystemPrompts via `build.go` + `load.go` | appended language directive + language-gated rules |

An EN-reading operator may want a VI novel. Keep them separate fields. Both default to Vietnamese.

There are **three translation tracks**, kept strictly apart:
- **Track A — UI/runtime strings** (~1,066 literals): message catalog. Wave 2.
- **Track B — code comments** (~2,878 lines): translate in place, keep EN terms. Wave 3.
- **Track C — LLM-facing assets** (prompts/references/styles/rules .md, 33 files + 2 in-source consts): behavior-changing; per-output-language forks behind a directive. Wave 5.

Track C must NEVER enter the Track A catalog — translating it changes model behavior and can break prompt parsers.

---

## 1. i18n Approach (Track A)

### 1.1 Mechanism — minimal in-repo catalog (NO new dep)

`golang.org/x/text` is already in go.mod but only for GB18030 decoding. **Do NOT** adopt `x/text/message` / catalog / plural machinery — it is heavier than this CLI needs and forces a `message.Printer` plumb through every call site. KISS: a tiny in-repo package.

New package `internal/i18n`:

```
internal/i18n/
  i18n.go        # Lang type, current locale state, T() + Tf() lookups, fallback
  catalog.go     # generated/maintained: map[Lang]map[string]string  (or per-lang files)
  lang_vi.go     # var viCatalog = map[string]string{ ... }   (Vietnamese, primary)
  lang_en.go     # var enCatalog = map[string]string{ ... }   (English)
  i18n_test.go   # key-parity test: every vi key exists in en and vice-versa; no %-verb drift
```

Core API (package-level, single global locale set once at startup — this is a single-user CLI, no concurrent locales):

```go
type Lang string
const (LangVI Lang = "vi"; LangEN Lang = "en")
const DefaultLang = LangVI

// SetLang sets the active UI locale once at startup (after config load). Not goroutine-safe by design.
func SetLang(l Lang)

// T returns the localized string for key; falls back VI->EN->key.
func T(key string) string

// Tf returns a formatted localized string. The catalog value is a fmt template.
func Tf(key string, args ...any) string
```

Fallback chain: active lang → VI (primary) → raw key (never panic, never empty). A missing key returns the key literal so it is visible and greppable in output.

### 1.2 Locale selection & precedence

Resolved once in `main.go` right after `LoadConfig`, before TUI/headless dispatch:

1. `--lang vi|en` CLI flag (highest) — add to `parseCLIOptions`.
2. `AINOVEL_LANG` env var.
3. `cfg.UILang` from config JSON.
4. `DefaultLang` (vi).

Then `i18n.SetLang(resolved)`. Headless mode (which refuses first-run setup) gets VI by default with no prompt — satisfied by step 4.

### 1.3 Key naming convention

Dot-namespaced, lowercase, `area.subarea.intent`:

```
setup.welcome           setup.step.apikey         setup.prompt.provider_name
ui.label.overview       ui.label.runtime          ui.status.chapter_progress   # "%d / %d chương"
error.config.invalid    error.tool.args           error.export.load_progress
diag.quality.dim_lowscore  diag.flow.remediation
notify.budget           notify.done               notify.stopped
main.exit.logged        main.exit.press_enter
```

Rules:
- Interpolated strings use **named intent + fmt verbs in the catalog value**, e.g. `"ui.status.chapter_progress": "%d / %d chương"`. Caller: `i18n.Tf("ui.status.chapter_progress", done, total)`.
- Counted nouns: keep the **unit inside the template** so VI/EN measure words differ per catalog, not per call site. VI has no plural inflection (`3 chương`, `1 chương` identical) so no plural engine needed; EN uses the natural English noun. If a specific EN key ever needs singular/plural, split into two keys (`...one` / `...many`) — only when actually required (YAGNI).

### 1.4 Error wrapping (%w) preservation — CRITICAL

30 sites wrap with `%w` (some chain `%w: %w`); `internal/errs` sentinels stay **English** and unchanged. Only the Chinese **prefix** becomes a key; the `%w` verb and its position are preserved.

Migration recipe for a wrapped error:

```go
// BEFORE
return fmt.Errorf("加载 progress 失败：%w", err)
// AFTER  — catalog: "error.export.load_progress": "load progress failed: %w"  (vi: "tải progress thất bại: %w")
return fmt.Errorf(i18n.T("error.export.load_progress"), err)
```

- Keep `%w` (and chained `%w: %w`) verbatim in the catalog value so `errors.Is`/`As` chains survive.
- Normalize full-width punctuation (`：`,`（）`) to ASCII in EN/VI catalog values.
- `errs.ErrToolArgs` etc. remain English `errors.New` sentinels — they are category markers, never user prose.

### 1.5 Migration recipe — plain CJK literal → keyed lookup

1. Pick a key per §1.3.
2. Add value to `lang_vi.go` (translate zh→vi) **and** `lang_en.go` (zh→en). Both required or the parity test fails.
3. Replace literal: `"概览"` → `i18n.T("ui.label.overview")`; `fmt.Sprintf("第 %d 章", n)` → `i18n.Tf("ui.label.chapter_n", n)`.
4. Preserve any `%w`/`%v`/`%d` verbs and their order.
5. `go build ./... && go test ./...` for the touched package.

### 1.6 Dual-audience tool errors (special handling)

`internal/tools/*` `fmt.Errorf` messages are returned to the **LLM** as tool results AND shown in TUI. Risk: agent self-correction loops may pattern-match Chinese error text. **Decision:** route tool errors through the catalog like any other (operator should read them in their locale), BUT verify no agent prompt instructs matching on a literal Chinese error string before translating. If an agent depends on a literal token, that token stays as a stable English constant in `internal/errs` and the wrapper prefix is localized around it.

---

## 2. Output Language (Track C code seam)

### 2.1 Config field

Add to `bootstrap.Config` (config.go:113 block), mirroring `Style`:

```go
OutputLang string `json:"output_lang,omitempty"` // novel output language: vi|en|original; default vi
UILang     string `json:"ui_lang,omitempty"`      // operator UI locale: vi|en; default vi
```

- `FillDefaults` (config.go:304): default both to `vi`. `original` is allowed for OutputLang (write in the prompt's native language = current zh behavior).
- `ValidateBase` (config.go:169): validate enum membership.
- **`mergeConfig` (configfile.go:132): add two clauses** — without them project/`--config` overrides are silently dropped (the issue-#37 bug class):
  ```go
  if overlay.OutputLang != "" { base.OutputLang = overlay.OutputLang }
  if overlay.UILang != "" { base.UILang = overlay.UILang }
  ```
- Add a `ResolveOutputLang()` helper mirroring `ResolveContextWindow` for empty→default.
- Document both keys in `config.example.jsonc`.
- Add an optional locale/output-lang step to `setup.go` `RunSetup` (after model step). Headless gets defaults.

### 2.2 Injection seam — one directive, all roles

The cleanest seam is `assets.load.go` `withSimulationGuidance` (applied to all creative roles) plus a single explicit directive const. Thread `OutputLang` into `assets.Load`:

- `assets.Load(style)` → `assets.Load(style, outputLang Lang)` (main.go:100 passes `cfg.OutputLang`).
- Define `languageDirective(lang)` in `assets/load.go` returning a short instruction (per-lang const). For `vi`: a Vietnamese "Viết toàn bộ nội dung truyện bằng tiếng Việt..." directive; for `en`: English; for `original`: empty string (no-op → current behavior).
- Append the directive to **every** role prompt at the same concat point as `simulationGuidance` (coordinator/architect_short/architect_long/writer/editor), NOT only the writer block. This guarantees architect/editor/coordinator also honor the language.
- `simulationGuidance` itself is Chinese and appended to every creative role → translate it per-lang in Wave 5 (it is Track C).

Default: `vi`. The directive is a thin appended instruction, NOT a fork of the 9 prompt files (avoids forking quality drift). The .md prompt/reference/style content is *additionally* translated per output-language in Wave 5 for quality, but the code seam works on day one with just the directive.

### 2.3 Language-aware rules (MUST move with the directive)

Two rules encode a CJK assumption and will misfire on VI/EN. Gate both by `OutputLang`:

- **`internal/rules/lint.go` `appendNonCJKFragments` (non_cjk_fragments, :55-80):** flags Latin runs `[A-Za-z]{2,}` as a defect. For `vi`/`en` this fires on the entire body. **Gate:** skip this rule entirely when OutputLang != a CJK language. Thread lang into the lint entry point (rules options or `Check` signature).
- **`internal/rules/checker.go` `Check` word count (:24-25):** `utf8.RuneCountInString` counts runes-as-words — correct for CJK, meaningless for whitespace-delimited VI/EN. **Gate:** for non-CJK, count whitespace-delimited tokens (`strings.Fields`). `chapter_words: 3000-6000` in `rules/default.md` is a CJK rune range; provide a VI/EN word-count range in the per-lang rules variant (Wave 5).

Plumbing: add an output-language parameter to `rules.DefaultOptions`/`Check` (mirrors how `cfg.Style` already reaches `tools.NewContextTool`). Static config-time language only — no runtime `/language` switch (YAGNI; matches Style's startup-only nature).

### 2.4 novel_context references bias

`novel_context` injects Chinese reference/style/rules JSON every turn (`anti-ai-tone.md`, `quality-checklist.md` are injected on every chapter). Even with a directive, Chinese injected context biases the model back toward Chinese. Wave 5 provides per-output-language reference/style/rules trees; `tools.NewContextTool` selects by `OutputLang`.

---

## 3. Translation Conventions

### 3.1 Code comments (Track B)

Vietnamese prose, **keep English technical terms verbatim**: `API`, `checkpoint`, `Coordinator`, `Architect`, `Writer`, `Editor`, `Host`, `subagent`, `provider`, `token`, `context window`, `JSON`, `registry`, `prompt`, `tool`, `commit`, `abort`, `budget`, `slog`. Do not translate identifiers, struct/field names, or string keys. Translate only the natural-language portion of `//` and `/* */` comments. Doc comments keep the leading identifier (Go convention): `// Config là ...`.

### 3.2 Domain glossary (agreed VI renderings)

| Source (zh / concept) | EN term | VI rendering (use consistently) |
|---|---|---|
| Coordinator | Coordinator | Coordinator (giữ nguyên) |
| Architect | Architect | Architect (giữ nguyên) |
| Writer | Writer | Writer (giữ nguyên) |
| Editor | Editor | Editor (giữ nguyên) |
| 大纲 / outline | outline | dàn ý |
| 卷弧 | volume-arc | cung truyện (vòng/quyển) |
| 伏笔 | foreshadowing | phục bút (cài cắm) |
| 钩子 | hook | hook (điểm câu kéo) |
| 章 / chapter | chapter | chương |
| 卷 / volume | volume | quyển |
| 草稿 / draft | draft | bản nháp |
| plan / 规划 | plan | kế hoạch |
| check / 审阅 | review/check | rà soát |
| commit / 提交 | commit | commit (lưu chốt) |
| 返工 | rework | làm lại |
| 干预 | intervention | can thiệp |
| 概览 | overview | tổng quan |
| 运行态 | runtime | trạng thái chạy |
| 阶段 | stage/phase | giai đoạn |
| 流程 | flow | quy trình |
| 预算 | budget | ngân sách |
| 仿写画像 | simulation profile | hồ sơ mô phỏng |
| 题材 | genre | thể loại |
| 主角 | protagonist | nhân vật chính |
| 字数 | word count | số chữ |

Keep technical English terms in parentheses on first use where the VI term is unusual (e.g. "hook (điểm câu kéo)"). Apply the same glossary in Track A catalog values and Track C prompt translations for consistency.

---

## 4. Markdown Classification

### 4.1 LLM-FACING (Track C — behavior-changing, per-output-language forks)

Embedded via 4 `//go:embed` in `assets/load.go`. Translating changes AI behavior.

- `assets/prompts/*.md` (9): coordinator, architect-short, architect-long, writer, editor, import-foundation, import-chapter-analyzer, simulation-source, simulation-merge.
- `assets/references/*.md` (18 incl `genres/{fantasy,romance,suspense}/*`): injected verbatim by `novel_context`.
- `assets/styles/*.md` (4): default/fantasy/romance/suspense — **content only; map keys/filenames stay English**.
- `assets/rules/default.md` (1): frontmatter constraints (forbidden_chars/fatigue_words) are zh-specific — **re-author for VI/EN, do not translate 1:1**.
- **In-source consts (missed by .md grep):** `assets/load.go:115` `simulationGuidance`; `internal/rules/loader.go:159` `homeRulesReadme`.

**Parser-coupling hazard:** `import-*` and `architect-*` prompts emit fixed Chinese section headings the Go parser matches by exact string (`premise_structure.go` alias map; architect-long.md "标题名必须一字不差"). Translating these prompts **must** update the parser keys in lockstep, or keep structural markers stable and only translate prose. Treat as a sub-task with paired prompt+parser edits.

### 4.2 HUMAN DOCS (safe to translate, no behavior impact — Wave 4)

`README.md`, `assets/README.md`, `docs/architecture.md`, `docs/context-management.md`, `docs/observability.md`, `docs/refactor-flow-driven.md`.

### 4.3 TEST FIXTURES (do NOT translate — byte-coupled)

`internal/rules/testdata/*.rules.md` (7). Loaded from disk, asserted byte-for-byte by tests. See §5.

---

## 5. Test Safety

### 5.1 Files / literals that MUST NOT change without paired test edits

- `internal/rules/testdata/*.rules.md` (7 fixtures) — asserted via `reflect.DeepEqual`/`strings.Contains` in `parser_test.go`, `merger_test.go`, `checker_test.go` (`不禁`/`竟然`/`仿佛`, `不是……而是`, body marker `# 风格`). **Leave Chinese.** They test the parser, not output language; translating is wrong and pointless.
- `internal/store/world_test.go` — asserts markdown `"边界：..."` and structural `"边界：\n"`. These assert **output template** strings; if any are localized, update test literals in lockstep.
- `internal/rules/loader_test.go` — asserts `"全局偏好"/"项目偏好"`.

### 5.2 Distinguish data-roundtrip vs template assertions

~55 test files contain CJK. **Most are user-DATA round-trips** (store CJK input → read back, e.g. `cast_test.go` `老周`, `run_meta_test.go` steer history) — language-neutral, **do not touch**. i18n must target only **output/template/UI** strings. Before editing any test, classify: is the CJK an input fixture (leave) or an asserted program-produced string (update with the catalog change)?

### 5.3 No CI gate

CI (`release.yml`, `docker.yml`) runs **only on tag push** — no `go test`/`go build` on PR. **Every wave/agent MUST run `go build ./... && go test ./...` locally before handing off.** This is the only safety net. `internal/utils/textenc.go` is untested — avoid touching it.

### 5.4 Embed integrity

`assets/load.go` uses `mustRead` (panics on missing file). Any per-locale asset scheme (Wave 5) must update both the `//go:embed` globs AND path logic, or builds panic at runtime. Prefer a locale subdir convention (`prompts/vi/`, `prompts/en/`) with updated embed directives, decided in Wave 5 design.

---

## 6. Phased Implementation Plan

Ordered waves. Later waves depend on earlier. Within a wave, parallel units own disjoint files (no two agents write the same file).

### Wave 1 — i18n infrastructure (serial, 1 agent; blocks Wave 2)
**Files (new):** `internal/i18n/{i18n.go,catalog.go,lang_vi.go,lang_en.go,i18n_test.go}`.
**Files (edit):** `internal/bootstrap/config.go` (add `UILang`+`OutputLang`, FillDefaults, ValidateBase, ResolveOutputLang); `internal/bootstrap/configfile.go` (mergeConfig clauses); `cmd/ainovel-cli/main.go` (`--lang` flag, `AINOVEL_LANG`, resolve + `i18n.SetLang` after LoadConfig); `internal/bootstrap/config.example.jsonc` (document keys).
**Deliverable:** working `T`/`Tf` with VI+EN catalogs seeded with a handful of keys; parity test; locale resolution wired. No call sites migrated yet.
**Acceptance:** `go build ./... && go test ./internal/i18n/ ./internal/bootstrap/` green; `--lang en` selectable.

### Wave 2 — migrate runtime UI strings (parallel by package; depends on Wave 1)
Each unit owns its package(s); all add keys to `lang_vi.go`/`lang_en.go` — **catalog files are the one shared write surface**, so serialize catalog appends OR give each unit a disjoint key namespace + a final merge step. Recommended: each unit appends only its own namespaced block; a lead merges.

- **U2a TUI:** `internal/entry/tui/*` (panels.go, model.go, cocreate.go, …) — ~370 strings, key prefix `ui.*`.
- **U2b host + tools errors + notify:** `internal/host/**` (incl host.go notify Title/Body, exp/exporter.go), `internal/tools/*` errors — prefixes `error.*`, `notify.*`. Honor §1.4/§1.6.
- **U2c diag:** `internal/diag/*` (rules_quality.go, rules_flow.go) — prefix `diag.*`, parameterized templates (hardest cluster).
- **U2d bootstrap/setup + main die():** `internal/bootstrap/setup.go`, `config.go` error sentences, `cmd/ainovel-cli/main.go` `die()` — prefixes `setup.*`, `main.*`, `error.config.*`.
- **U2e smaller pkgs:** `internal/domain`, `internal/store`, `internal/rules` (Violation-surfaced text), `internal/models`, `internal/entry/startup`.

**Acceptance per unit:** `go build ./... && go test ./<pkg>/` green; parity test green after catalog merge. **Do NOT** touch test data-roundtrip fixtures (§5.2).

### Wave 3 — code comments to Vietnamese (parallel by package; independent of Wave 2 once files settle)
Translate `//` and `/* */` natural-language comments to VI per §3.1 glossary, keeping EN terms and identifiers. Partition by directory to avoid collisions; **must run after Wave 2 edits land in the same files** to avoid merge churn (sequence Wave 3 after Wave 2 per file, or assign the same agent both waves for a given package).
**Acceptance:** `go build ./... && go test ./...` green (comments cannot break build, but verify no accidental code edits); spot-check no identifier/string changed.

### Wave 4 — human docs (parallel by file; fully independent)
Translate to Vietnamese (primary), optionally keep EN versions side-by-side (`README.md` + `README.en.md`?) — see open question. Files: `README.md`, `assets/README.md`, `docs/*.md` (4). No code impact.
**Acceptance:** docs render; links/dates verified; `go build ./...` unaffected.

### Wave 5 — prompts + output-language assets (serial design, then parallel translate; depends on Wave 1 config field)
1. **Code seam (1 agent):** `assets/load.go` (`languageDirective`, thread `outputLang` into `Load`, append to all roles, translate `simulationGuidance` per-lang), `cmd/ainovel-cli/main.go` (pass `cfg.OutputLang`), `internal/agents/build.go` (ensure directive reaches all roles), `internal/rules/lint.go` + `checker.go` + options plumbing (language-gate non_cjk_fragments + word count), `internal/tools/novel_context.go` (select per-lang reference tree), `internal/rules/loader.go` (`homeRulesReadme` per-lang), decide locale-subdir embed convention + update `//go:embed`.
2. **Asset translation (parallel by file group, after seam lands):** per-output-language forks of `assets/prompts/*`, `assets/references/**`, `assets/styles/*`, re-authored `assets/rules/default.md` (VI/EN). **Parser-coupled prompts (`import-*`, `architect-*`) edited with paired parser updates** (§4.1).
**Acceptance:** `go build ./... && go test ./...` green; with `output_lang=vi` an integration smoke (manual) confirms VI directive present in assembled SystemPrompts and non_cjk_fragments does not fire; `output_lang=original` reproduces current zh behavior (regression guard).

### Wave ordering summary
```
Wave 1 (infra) ──► Wave 2 (UI strings) ──► Wave 3 (comments, per-file after W2)
       └─────────► Wave 5 (prompts+output-lang)         Wave 4 (docs) ── independent, any time after W1
```

---

## 7. Risks Carried Forward

- **mergeConfig omission** silently drops overrides — covered in Wave 1, must have a test.
- **%w chain breakage** — catalog values keep verbs verbatim; add a test asserting `errors.Is` survives a localized wrap.
- **Parser-coupled prompts** — Wave 5 pairs prompt+parser edits; highest-risk sub-task.
- **No CI gate** — local `go test ./...` mandatory each handoff.
- **Catalog file contention** in Wave 2 — namespaced blocks + lead merge.
- **Track C in injected context** still biases language until per-lang reference trees land (Wave 5 step 2).

---

## 8b. Resolved Decisions (user-confirmed)

1. **Output-lang enum:** locked to `vi | en | original` (YAGNI; no multi-lang generality now).
2. **Catalog layout:** Go maps in source (compile-time parity test). **Refinement:** split catalog into per-area files with an `init()`-registration mechanism so Wave-2 parallel agents own disjoint files (no shared `lang_vi.go`/`lang_en.go` write contention). Wave 1 must build this registration API.
3. **Human docs:** Vietnamese primary; keep English copies side-by-side (`README.md` VI + `README.en.md` EN; `docs/*.md` VI + `docs/*.en.md` EN).
4. **LLM assets:** seam-first. Wave 5 step 1 (directive + rule gating, zh assets retained, `output_lang=vi` directive makes AI write VI) lands first; translate 18 references + 4 styles + 9 prompts incrementally afterward.
5. **Parser-coupled headings:** keep structural section markers as stable tokens, translate only surrounding prose (lower risk).
6. **Tool-error CJK matching:** orchestrator scouts code to verify no agent matches literal zh error text (not a user question).
7. **Flags:** `--lang` for UI locale only; `output_lang` via config/setup, no `--output-lang` flag (YAGNI).

---

## 8. Open Questions (for user)

1. **Output-language enum:** confirm values `vi | en | original` (original = write in the prompt's native zh). Any need for other languages later, or lock to these three?
2. **Catalog layout:** single `map[Lang]map[string]string` in Go source (compile-time, simplest, chosen here) vs embedded JSON/TOML files (editable without recompile, translator-friendly). Default chosen = Go maps. Override?
3. **EN human docs:** keep English copies of `README.md`/`docs/*` alongside VI (e.g. `README.en.md`), or replace with VI only? VI is primary — do we still ship EN docs?
4. **Per-output-language reference/style depth:** full translation of all 18 references + 4 styles is the largest effort. Acceptable to ship Wave 5 step 1 (directive + rule gating, zh assets retained) first and translate assets incrementally, or must all VI assets land together?
5. **Parser-coupled headings:** prefer (a) keep structural section markers as stable English/zh tokens and only translate surrounding prose, or (b) fully localize headings and update the Go parser keys? (a) is lower-risk.
6. **Tool-error localization:** any known agent prompt that pattern-matches literal Chinese error text? If yes, name it so we keep that token stable (§1.6).
7. **`--lang` vs `--ui-lang`:** is one flag for UI locale enough, with output language set only via config/setup? Or do you also want a `--output-lang` flag?
