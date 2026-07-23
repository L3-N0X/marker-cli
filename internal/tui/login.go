package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// Validator checks an entered value against a provider. It is injected so the
// prompt model stays independent of any particular backend.
type Validator func(ctx context.Context, value string) error

// PromptConfig configures the interactive value prompt used for signing in: an
// API key (hidden) or an endpoint (shown).
type PromptConfig struct {
	Title    string // e.g. "sign in to mistral"
	Hint     string // a line under the title, e.g. where to get a key
	Initial  string // prefilled value (endpoints)
	Password bool   // hide the input (API keys)
	Validate Validator
}

// promptState is the step the prompt flow is on.
type promptState int

const (
	promptEntry promptState = iota
	promptValidating
	promptDone
)

// validatedMsg carries the outcome of a validation attempt.
type validatedMsg struct{ err error }

type promptModel struct {
	cfg       PromptConfig
	input     textinput.Model
	spinner   spinner.Model
	state     promptState
	value     string
	err       error
	ok        bool
	cancelled bool
}

// RunPrompt shows an interactive prompt, validating the entered value before
// returning it. It reports the value and whether the user confirmed it;
// persisting the value is the caller's job.
func RunPrompt(cfg PromptConfig) (string, bool, error) {
	ti := textinput.New()
	ti.CharLimit = 512
	ti.SetWidth(48)
	if cfg.Password {
		ti.Placeholder = "paste your API key"
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '•'
	} else {
		ti.Placeholder = "host:port"
		ti.SetValue(cfg.Initial)
	}
	styleInput(&ti)
	ti.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = titleStyle

	final, err := tea.NewProgram(promptModel{cfg: cfg, input: ti, spinner: sp}).Run()
	if err != nil {
		return "", false, err
	}
	m, ok := final.(promptModel)
	if !ok || m.cancelled {
		return "", false, nil
	}
	return m.value, m.ok, nil
}

func (m promptModel) Init() tea.Cmd { return textinput.Blink }

func (m promptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			if m.state != promptEntry {
				return m, nil
			}
			value := strings.TrimSpace(m.input.Value())
			if value == "" {
				m.err = fmt.Errorf("please enter a value")
				return m, nil
			}
			m.state = promptValidating
			m.err = nil
			return m, tea.Batch(m.spinner.Tick, validateCmd(m.cfg.Validate, value))
		}

	case validatedMsg:
		if msg.err != nil {
			m.state = promptEntry
			m.err = msg.err
			m.input.Focus()
			return m, textinput.Blink
		}
		m.value = strings.TrimSpace(m.input.Value())
		m.ok = true
		m.state = promptDone
		return m, tea.Quit

	case spinner.TickMsg:
		if m.state == promptValidating {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	if m.state == promptEntry {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m promptModel) View() tea.View {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", titleStyle.Render("marker-cli · "+m.cfg.Title))
	if m.cfg.Hint != "" {
		fmt.Fprintf(&b, "%s\n\n", labelStyle.Render(m.cfg.Hint))
	}

	switch m.state {
	case promptValidating:
		fmt.Fprintf(&b, "%s checking…\n", m.spinner.View())
	default:
		fmt.Fprintf(&b, "%s\n", m.input.View())
		if m.err != nil {
			fmt.Fprintf(&b, "\n%s\n", errorStyle.Render("✗ "+m.err.Error()))
		}
		hidden := ""
		if m.cfg.Password {
			hidden = " · input is hidden"
		}
		fmt.Fprintf(&b, "\n%s\n", helpStyle.Render("enter to continue · esc to cancel"+hidden))
	}
	return tea.NewView(b.String())
}

// validateCmd runs the validator off the update loop, as Bubble Tea requires
// for anything slow.
func validateCmd(validate Validator, value string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return validatedMsg{err: validate(ctx, value)}
	}
}

// PickerItem is one choice in the provider picker.
type PickerItem struct {
	Label string
	Desc  string
}

type pickerModel struct {
	title     string
	items     []PickerItem
	cursor    int
	chosen    int
	confirmed bool
	cancelled bool
}

// RunPicker shows a single-choice menu and returns the selected index. The
// bool is false if the user cancelled.
func RunPicker(title string, items []PickerItem) (int, bool, error) {
	final, err := tea.NewProgram(pickerModel{title: title, items: items, chosen: -1}).Run()
	if err != nil {
		return 0, false, err
	}
	m, ok := final.(pickerModel)
	if !ok || m.cancelled || !m.confirmed {
		return 0, false, nil
	}
	return m.chosen, true, nil
}

func (m pickerModel) Init() tea.Cmd { return nil }

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "ctrl+c", "esc", "q":
			m.cancelled = true
			return m, tea.Quit
		case "up", "k":
			m.cursor = clamp(m.cursor-1, 0, len(m.items)-1)
		case "down", "j":
			m.cursor = clamp(m.cursor+1, 0, len(m.items)-1)
		case "enter":
			m.chosen = m.cursor
			m.confirmed = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m pickerModel) View() tea.View {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", titleStyle.Render("marker-cli · "+m.title))
	for i, it := range m.items {
		cursor := "  "
		label := it.Label
		if i == m.cursor {
			cursor = cursorStyle.Render("❯ ")
			label = cursorStyle.Render(label)
		}
		line := cursor + label
		if it.Desc != "" {
			line += dimStyle.Render("  " + it.Desc)
		}
		fmt.Fprintf(&b, "%s\n", line)
	}
	fmt.Fprintf(&b, "\n%s\n", helpStyle.Render("↑↓ move · enter select · esc cancel"))
	return tea.NewView(b.String())
}
