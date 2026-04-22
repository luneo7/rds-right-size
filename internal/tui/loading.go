package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luneo7/rds-right-size/internal/rds-right-size/types"
)

type ProgressMsg struct {
	Current    int
	Total      int
	InstanceID string
}

type AnalysisDoneMsg struct {
	Err             error
	Recommendations []types.Recommendation
	Warnings        []string
}

type LoadingModel struct {
	spinner  spinner.Model
	status   string
	current  int
	total    int
	width    int
	height   int
}

func NewLoadingModel() LoadingModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(primaryColor)

	return LoadingModel{
		spinner: s,
		status:  "Initializing...",
	}
}

func (m LoadingModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m LoadingModel) Update(msg tea.Msg) (LoadingModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case ProgressMsg:
		m.current = msg.Current
		m.total = msg.Total
		m.status = fmt.Sprintf("Analyzing instance %d of %d: %s", msg.Current, msg.Total, msg.InstanceID)
		return m, nil
	}

	return m, nil
}

// modalInteriorWidth returns the lipgloss Width value for the loading modal.
// This includes padding (3*2=6 chars) but NOT border (1*2=2 chars).
// The actual content area is modalInteriorWidth() - 6.
func (m LoadingModel) modalInteriorWidth() int {
	modalW := m.width - 12
	if modalW > 90 {
		modalW = 90
	}
	if modalW < 40 {
		modalW = 40
	}
	return modalW
}

func (m LoadingModel) View() string {
	var b strings.Builder

	modalW := m.modalInteriorWidth()

	title := titleStyle.Render("RDS Right Size - Analyzing")
	b.WriteString(title + "\n\n")

	// Spinner with status — truncate if too long for the content area
	status := m.status
	// Content area = modalW - 6. Account for spinner (2 chars) + space (1 char).
	maxStatusLen := modalW - 6 - 3
	if maxStatusLen < 10 {
		maxStatusLen = 10
	}
	if len(status) > maxStatusLen {
		status = status[:maxStatusLen-3] + "..."
	}
	spinnerLine := fmt.Sprintf("%s %s", m.spinner.View(), spinnerTextStyle.Render(status))
	b.WriteString(spinnerLine + "\n\n")

	// Progress bar
	if m.total > 0 {
		b.WriteString(m.renderProgressBar() + "\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("esc: cancel"))

	content := b.String()

	modal := modalStyle.Width(modalW).Render(content)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
}

func (m LoadingModel) renderProgressBar() string {
	if m.total == 0 {
		return ""
	}

	// Use most of the modal content area for the bar itself.
	// modalW is the lipgloss Width (includes padding). Content area = modalW - 6.
	// Subtract 2 more for the "  " prefix on the bar line.
	modalW := m.modalInteriorWidth()
	contentW := modalW - 6
	width := contentW - 2
	if width < 20 {
		width = 20
	}

	filled := int(float64(m.current) / float64(m.total) * float64(width))
	if filled > width {
		filled = width
	}

	bar := lipgloss.NewStyle().Foreground(primaryColor).Render(strings.Repeat("█", filled))
	barLine := "  " + bar
	if remaining := width - filled; remaining > 0 {
		empty := lipgloss.NewStyle().Foreground(borderColor).Render(strings.Repeat("░", remaining))
		barLine += empty
	}

	// Percentage on its own line, centered within the content area
	pct := fmt.Sprintf("%d%%", m.current*100/m.total)
	pctStyled := lipgloss.NewStyle().Foreground(dimTextColor).Render(pct)
	pctLine := lipgloss.PlaceHorizontal(contentW, lipgloss.Center, pctStyled)

	return barLine + "\n" + pctLine
}

func (m LoadingModel) SetStatus(status string) LoadingModel {
	m.status = status
	return m
}
