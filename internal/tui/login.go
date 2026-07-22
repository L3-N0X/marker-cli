package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/l3-n0x/marker-cli/internal/secrets"
)

// Validator checks an API key against the provider. It is injected so the
// login model stays independent of any particular backend.
type Validator func(ctx context.Context, apiKey string) error

// loginState is the step the sign-in flow is on.
type loginState int

const (
	stateEntry loginState = iota
	stateValidating
	stateDone
)

// validatedMsg carries the outcome of a key validation attempt.
type validatedMsg struct{ err error }

type loginModel struct {
	provider  string
	keyURL    string
	validate  Validator
	input     textinput.Model
	spinner   spinner.Model
	state     loginState
	err       error
	saved     bool
	cancelled bool
}

// RunLogin shows the interactive sign-in flow for provider, validating the
// entered key with validate before storing it in the OS keyring. It reports
// whether a key was saved.
func RunLogin(provider, keyURL string, validate Validator) (bool, error) {
	ti := textinput.New()
	ti.Placeholder = "paste your API key"
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.CharLimit = 256
	ti.SetWidth(48)
	ti.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = titleStyle

	m := loginModel{
		provider: provider,
		keyURL:   keyURL,
		validate: validate,
		input:    ti,
		spinner:  sp,
	}

	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return false, err
	}
	result, ok := final.(loginModel)
	if !ok {
		return false, nil
	}
	// Escaping out is a deliberate choice, not a failure — even if the last
	// attempt was rejected, so don't report that stale error.
	if result.cancelled {
		return false, nil
	}
	if result.err != nil && !result.saved {
		return false, result.err
	}
	return result.saved, nil
}

func (m loginModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m loginModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			switch m.state {
			case stateEntry:
				key := strings.TrimSpace(m.input.Value())
				if key == "" {
					m.err = fmt.Errorf("please enter a key")
					return m, nil
				}
				m.state = stateValidating
				m.err = nil
				return m, tea.Batch(m.spinner.Tick, validateCmd(m.validate, key))
			case stateDone:
				return m, tea.Quit
			}
		}

	case validatedMsg:
		if msg.err != nil {
			// Let the user correct the key instead of dropping them out.
			m.state = stateEntry
			m.err = msg.err
			m.input.Focus()
			return m, textinput.Blink
		}
		if err := secrets.Set(m.provider, strings.TrimSpace(m.input.Value())); err != nil {
			m.state = stateEntry
			m.err = err
			m.input.Focus()
			return m, textinput.Blink
		}
		m.state = stateDone
		m.saved = true
		return m, tea.Quit

	case spinner.TickMsg:
		if m.state == stateValidating {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	if m.state == stateEntry {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m loginModel) View() tea.View {
	var b strings.Builder

	fmt.Fprintf(&b, "%s\n\n", titleStyle.Render("marker-cli · sign in to "+m.provider))
	if m.keyURL != "" {
		fmt.Fprintf(&b, "%s\n\n", labelStyle.Render("Get a key at "+m.keyURL))
	}

	switch m.state {
	case stateValidating:
		fmt.Fprintf(&b, "%s checking the key…\n", m.spinner.View())
	case stateDone:
		fmt.Fprintf(&b, "%s\n", successStyle.Render("✓ key validated and saved to your OS keyring"))
	default:
		fmt.Fprintf(&b, "%s\n", m.input.View())
		if m.err != nil {
			fmt.Fprintf(&b, "\n%s\n", errorStyle.Render("✗ "+m.err.Error()))
		}
		fmt.Fprintf(&b, "\n%s\n", helpStyle.Render("enter to continue · esc to cancel · input is hidden"))
	}

	return tea.NewView(b.String())
}

// validateCmd runs the validator off the update loop, as Bubble Tea requires
// for anything slow.
func validateCmd(validate Validator, key string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return validatedMsg{err: validate(ctx, key)}
	}
}
