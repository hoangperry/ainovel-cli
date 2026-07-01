package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/i18n"
)

type slashCommandSpec struct {
	Name        string
	Aliases     []string
	Group       string
	Usage       string
	Description string
	AutoExecute bool
	Hidden      bool
	NeedsIdle   bool
	Run         func(m Model, args []string) (tea.Model, tea.Cmd)
}

type slashCommand struct {
	name string
	args []string
}

func parseSlashCommand(text string) (slashCommand, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return slashCommand{}, false
	}
	fields := strings.Fields(strings.TrimPrefix(text, "/"))
	if len(fields) == 0 {
		return slashCommand{}, false
	}
	return slashCommand{name: strings.ToLower(fields[0]), args: fields[1:]}, true
}

func (s slashCommandSpec) matches(name string) bool {
	if s.Name == name {
		return true
	}
	for _, alias := range s.Aliases {
		if strings.EqualFold(alias, name) {
			return true
		}
	}
	return false
}

func commandRegistryInstance() commandRegistry {
	return newCommandRegistry([]slashCommandSpec{
		{
			Name:        "help",
			Group:       "system",
			Usage:       "/help",
			Description: i18n.T("ui.command.desc.help"),
			AutoExecute: true,
			Run: func(m Model, _ []string) (tea.Model, tea.Cmd) {
				m.help = newHelpState(m.width, m.height)
				m.textarea.Blur()
				return m, nil
			},
		},
		{
			Name:        "model",
			Group:       "system",
			Usage:       "/model [role]",
			Description: i18n.T("ui.command.desc.model"),
			AutoExecute: true,
			Run: func(m Model, args []string) (tea.Model, tea.Cmd) {
				roleHint := ""
				if len(args) > 0 {
					roleHint = args[0]
					if normalizeRoleKey(roleHint) == "" {
						m.applyEvent(host.Event{
							Time: time.Now(), Category: "ERROR", Summary: i18n.Tf("ui.command.err.unknown_role", roleHint), Level: "error",
						})
						m.refreshEventViewport()
						return m, nil
					}
				}
				m.modelSwitch = newModelSwitchState(m.runtime, roleHint)
				m.textarea.Blur()
				return m, nil
			},
		},
		{
			Name:        "diag",
			Group:       "analysis",
			Usage:       "/diag",
			Description: i18n.T("ui.command.desc.diag"),
			AutoExecute: true,
			Run: func(m Model, _ []string) (tea.Model, tea.Cmd) {
				m.reportSeq++
				m.report = newReportState(m.width, m.height, m.reportSeq, time.Now())
				m.textarea.Blur()
				return m, loadReport(m.runtime.Dir(), m.reportSeq)
			},
		},
		{
			Name:        "import",
			Group:       "writing",
			Usage:       "/import <path> [from=N]",
			Description: i18n.T("ui.command.desc.import"),
			NeedsIdle:   true,
			Run: func(m Model, args []string) (tea.Model, tea.Cmd) {
				m.importSeq++
				state, listenCmd, err := startImport(m.runtime, m.importSeq, args, m.width, m.height)
				if err != nil {
					m.applyEvent(host.Event{
						Time: time.Now(), Category: "ERROR", Summary: i18n.Tf("ui.command.err.import_start", err.Error()), Level: "error",
					})
					m.refreshEventViewport()
					return m, nil
				}
				m.importer = state
				m.textarea.Blur()
				return m, listenCmd
			},
		},
		{
			Name:        "cocreate",
			Aliases:     []string{"plan"},
			Group:       "writing",
			Usage:       "/cocreate",
			Description: i18n.T("ui.command.desc.cocreate"),
			AutoExecute: true,
			Run: func(m Model, _ []string) (tea.Model, tea.Cmd) {
				if m.mode != modeRunning {
					m.applyEvent(host.Event{
						Time: time.Now(), Category: "ERROR", Summary: i18n.T("ui.command.err.cocreate_run_only"), Level: "error",
					})
					m.refreshEventViewport()
					return m, nil
				}
				if !m.runtime.PauseForCoCreate() {
					m.applyEvent(host.Event{
						Time: time.Now(), Category: "ERROR", Summary: i18n.T("ui.command.err.cocreate_unavailable"), Level: "error",
					})
					m.refreshEventViewport()
					return m, nil
				}
				m.cocreate = newStageCoCreateState()
				m.resizeTextarea()
				m.textarea.Blur()
				return m, m.sendCoCreate()
			},
		},
		{
			Name:        "simulate",
			Group:       "writing",
			Usage:       "/simulate",
			Description: i18n.T("ui.command.desc.simulate"),
			NeedsIdle:   true,
			Run: func(m Model, args []string) (tea.Model, tea.Cmd) {
				m.simSeq++
				state, listenCmd, err := startSimulate(m.runtime, m.simSeq, args, m.width, m.height)
				if err != nil {
					m.applyEvent(host.Event{
						Time: time.Now(), Category: "ERROR", Summary: i18n.Tf("ui.command.err.simulate_start", err.Error()), Level: "error",
					})
					m.refreshEventViewport()
					return m, nil
				}
				m.simulator = state
				m.textarea.Blur()
				return m, listenCmd
			},
		},
		{
			Name:        "importsim",
			Group:       "writing",
			Usage:       "/importsim <profile.json>",
			Description: i18n.T("ui.command.desc.importsim"),
			NeedsIdle:   true,
			Run: func(m Model, args []string) (tea.Model, tea.Cmd) {
				m.simSeq++
				state, listenCmd, err := startImportSimulation(m.runtime, m.simSeq, args, m.width, m.height)
				if err != nil {
					m.applyEvent(host.Event{
						Time: time.Now(), Category: "ERROR", Summary: i18n.Tf("ui.command.err.importsim_start", err.Error()), Level: "error",
					})
					m.refreshEventViewport()
					return m, nil
				}
				m.simulator = state
				m.textarea.Blur()
				return m, listenCmd
			},
		},
		{
			Name:        "export",
			Group:       "writing",
			Usage:       "/export [path] [from=N] [to=M] [--overwrite]",
			Description: i18n.T("ui.command.desc.export"),
			AutoExecute: true,
			Run: func(m Model, args []string) (tea.Model, tea.Cmd) {
				cmd, err := startExport(m.runtime, args)
				if err != nil {
					m.applyEvent(host.Event{
						Time: time.Now(), Category: "ERROR", Summary: i18n.Tf("ui.command.err.export_start", err.Error()), Level: "error",
					})
					m.refreshEventViewport()
					return m, nil
				}
				m.applyEvent(host.Event{
					Time: time.Now(), Category: "SYSTEM", Summary: i18n.T("ui.command.event.exporting"), Level: "info",
				})
				m.refreshEventViewport()
				return m, cmd
			},
		},
	})
}

func commandSpecs() []slashCommandSpec {
	return commandRegistryInstance().Visible()
}

func (m Model) handleSlashCommand(cmd slashCommand) (tea.Model, tea.Cmd) {
	spec, ok := commandRegistryInstance().Find(cmd.name)
	if !ok {
		m.applyEvent(host.Event{
			Time: time.Now(), Category: "ERROR", Summary: i18n.Tf("ui.command.err.unknown_command", cmd.name), Level: "error",
		})
		m.refreshEventViewport()
		return m, nil
	}
	if spec.NeedsIdle && m.snapshot.IsRunning {
		m.applyEvent(host.Event{
			Time: time.Now(), Category: "ERROR", Summary: i18n.Tf("ui.command.err.idle_only", spec.Name), Level: "error",
		})
		m.refreshEventViewport()
		return m, nil
	}
	return spec.Run(m, cmd.args)
}
