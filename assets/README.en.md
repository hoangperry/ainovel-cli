# assets Content Map

> [Tiếng Việt](README.md)

Before adding "a paragraph / a piece of material / a rule" to the system, check the table below to determine where it belongs, then look at how it is wired.

| Directory | What it holds | Who consumes | Wiring |
|---|---|---|---|
| `prompts/` | system prompts of the resident roles (coordinator / writer / editor / architect×2) and one-off task prompts (import×2 / simulation×2) | `agents/build.go` assembles; imp / sim runner | The Prompts field in `load.go`. Note: simulation_guidance is injected by `load.go` at load time, so it is not visible in the md files |
| `references/` | Genre-agnostic writing-knowledge material. Does not enter the system prompt; `novel_context` trims it per role / chapter and injects it into `reference_pack` | writer / editor / architect | **Wired in three places**: add a field to `tools.References` + `load.go` loadReferences reads it + `novel_context.go` writerReferences / architectReferences injects it. Dropping a file into the directory will NOT auto-load it |
| `references/genres/<style>/` | Genre-specific knowledge (style-references / arc-templates) | Same as above, loaded when `style != default` | `load.go` loadReferences |
| `rules/` | Default values of mechanical rules (word count / forbidden words / fatigue words), enforced by code at commit time | rules loader merges three layers: built-in → `~/.ainovel/rules/` → project `./.ainovel/rules/` | `rules/default.md`; for the user-layer format see `rules.md.example` in the root directory. Only put fixed-length fixed strings here; patterns with variables go to the editor for semantic judgment |
| `styles/<style>.md` | Genre writing-style instructions | Concatenated into the **writer**'s system prompt (`agents/build.go`) | The filename is the `config.style` value. Together with `references/genres/<style>/`, these are two carriers of the same genre concept: the former is style instructions, the latter is knowledge material |

## Deciding where new content belongs (five questions)

1. Must this flow be **guaranteed**? → Do not write a prompt; write a code constraint (StopAfterTools / tool guard / Flow Router)
2. Is this an adjudication criterion (when to dispatch whom)? → `prompts/coordinator.md`
3. Is this the aesthetic / execution standard of a particular role? → `prompts/<role>.md`
4. Is this a mechanically enumerable rule (forbidden words / word count / threshold)? → `rules/` (code-enforced, zero LLM cost)
5. Is this writing-knowledge material? → `references/` (remember the three wiring points)

## Consistency guarantee

The envelope paths referenced by the prompt (`working_memory.*` etc.) and the commit_chapter parameter docs in writer.md
are machine-checked by `prompts_consistency_test.go` — these two kinds of drift do not raise errors, they just quietly make the model dumber, and are exposed by a red test light.
The flow sections in the prompt are a "user manual"; the flow truth lives in the code layer. When the two diverge, treat the code as authoritative and go back to fix the prompt.
