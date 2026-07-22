package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/l3-n0x/marker-cli/internal/converter"
)

// Runner converts the job at index i, reporting progress on the channel. It
// returns a one-line summary of what it wrote.
type Runner func(ctx context.Context, i int, progress chan<- converter.Progress) (string, error)

// JobResult is the outcome of one conversion, returned to the caller so it can
// set the process exit code.
type JobResult struct {
	Name    string
	Summary string
	Err     error
}

type progressMsg converter.Progress
type progressClosedMsg struct{}
type jobDoneMsg JobResult

type progressModel struct {
	names   []string
	run     Runner
	results []JobResult

	current  int
	stage    converter.Stage
	detail   string
	pct      float64
	spinner  spinner.Model
	bar      progress.Model
	progress chan converter.Progress
	done     chan JobResult

	ctx       context.Context
	cancel    context.CancelFunc
	cancelled bool

	// standalone is true when the model drives its own program, as opposed to
	// being embedded in the `start` browser. It decides whether finishing
	// quits or just reports back to the parent.
	standalone bool
	finished   bool
}

// allDoneMsg says every job has finished; only sent to an embedded model.
type allDoneMsg struct{}

// newProgressModel builds the conversion view for the named jobs.
func newProgressModel(ctx context.Context, names []string, run Runner) progressModel {
	ctx, cancel := context.WithCancel(ctx)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = titleStyle

	return progressModel{
		names:   names,
		run:     run,
		current: -1,
		spinner: sp,
		bar:     newProgressBar(40),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// RunConversions shows a live progress view while run converts each named job
// in turn, and returns every job's outcome.
func RunConversions(ctx context.Context, names []string, run Runner) ([]JobResult, error) {
	m := newProgressModel(ctx, names, run)
	m.standalone = true
	defer m.cancel()

	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return nil, err
	}
	result, ok := final.(progressModel)
	if !ok {
		return nil, nil
	}
	if result.cancelled {
		return result.results, context.Canceled
	}
	return result.results, nil
}

func (m progressModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, startJob())
}

func (m progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	return next, cmd
}

// update is the real update loop. It returns the concrete type so the `start`
// browser can embed this model as a field.
func (m progressModel) update(msg tea.Msg) (progressModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// When embedded, the parent owns the keyboard.
		if !m.standalone {
			break
		}
		if s := msg.String(); s == "ctrl+c" || s == "esc" {
			return m.stop(), tea.Quit
		}

	case startJobMsg:
		m.current++
		// A cancelled run stops where it is instead of failing every job
		// that is left against a dead context.
		if m.current >= len(m.names) || m.cancelled {
			m.finished = true
			m.cancel()
			if m.standalone {
				return m, tea.Quit
			}
			return m, func() tea.Msg { return allDoneMsg{} }
		}
		m.stage, m.detail, m.pct = "", "", 0
		m.progress = make(chan converter.Progress)
		m.done = make(chan JobResult, 1)

		i, name := m.current, m.names[m.current]
		ch, doneCh := m.progress, m.done
		go func() {
			summary, err := m.run(m.ctx, i, ch)
			close(ch)
			doneCh <- JobResult{Name: name, Summary: summary, Err: err}
		}()
		return m, tea.Batch(waitProgress(m.progress), waitResult(m.done))

	case progressMsg:
		m.stage, m.detail, m.pct = msg.Stage, msg.Detail, msg.Percent
		return m, waitProgress(m.progress)

	case progressClosedMsg:
		return m, nil

	case jobDoneMsg:
		m.results = append(m.results, JobResult(msg))
		return m, startJob()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

// stop cancels the in-flight conversion and marks the run as abandoned.
func (m progressModel) stop() progressModel {
	m.cancelled = true
	m.cancel()
	return m
}

func (m progressModel) View() tea.View {
	return tea.NewView(m.render())
}

func (m progressModel) render() string {
	var b strings.Builder

	for _, r := range m.results {
		if r.Err != nil {
			fmt.Fprintf(&b, "%s %s — %s\n", errorStyle.Render("✗"), r.Name, errorStyle.Render(r.Err.Error()))
		} else {
			fmt.Fprintf(&b, "%s %s → %s\n", successStyle.Render("✓"), r.Name, r.Summary)
		}
	}

	if m.current >= 0 && m.current < len(m.names) {
		name := m.names[m.current]
		fmt.Fprintf(&b, "\n%s %s\n", m.spinner.View(), titleStyle.Render(name))

		status := string(m.stage)
		if m.detail != "" {
			status += " · " + m.detail
		}
		if status == "" {
			status = "starting"
		}
		fmt.Fprintf(&b, "%s\n", labelStyle.Render(status))
		fmt.Fprintf(&b, "%s  %s\n", m.bar.ViewAs(m.pct), labelStyle.Render(fmt.Sprintf("%d/%d", m.current+1, len(m.names))))
		fmt.Fprintf(&b, "\n%s\n", helpStyle.Render("esc to cancel"))
	}

	return b.String()
}

type startJobMsg struct{}

func startJob() tea.Cmd {
	return func() tea.Msg { return startJobMsg{} }
}

// waitProgress blocks off the update loop for the next progress event.
func waitProgress(ch <-chan converter.Progress) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return progressClosedMsg{}
		}
		return progressMsg(p)
	}
}

func waitResult(ch <-chan JobResult) tea.Cmd {
	return func() tea.Msg { return jobDoneMsg(<-ch) }
}
