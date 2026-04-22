package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	genFieldEngine = iota
	genFieldTargetRegions
	genFieldOutputFile
	genFieldSubmit
)

var engineOptions = []string{"both", "aurora-mysql", "aurora-postgresql"}

// GenerateSubmitMsg is sent when the user submits the generation form.
type GenerateSubmitMsg struct {
	Engine        string
	TargetRegions string
	OutputFile    string
}

// GenerateCancelMsg is sent when the user cancels the generation dialog.
type GenerateCancelMsg struct{}

type GenerateModel struct {
	inputs      []textinput.Model
	focusIndex  int
	engineIndex int
	region      string // inherited from config (read-only)
	err         error
	width       int
	height      int
}

func NewGenerateModel(region string) GenerateModel {
	inputs := make([]textinput.Model, 3)

	// Engine (cycling selector — uses a dummy text input for focus tracking)
	inputs[genFieldEngine] = textinput.New()
	inputs[genFieldEngine].Placeholder = "both"
	inputs[genFieldEngine].CharLimit = 20
	inputs[genFieldEngine].Width = 40

	// Target Regions
	inputs[genFieldTargetRegions] = textinput.New()
	inputs[genFieldTargetRegions].Placeholder = "all (or comma-separated: us-east-1,eu-west-1)"
	inputs[genFieldTargetRegions].CharLimit = 512
	inputs[genFieldTargetRegions].Width = 40
	inputs[genFieldTargetRegions].SetValue("all")

	// Output File
	inputs[genFieldOutputFile] = textinput.New()
	inputs[genFieldOutputFile].Placeholder = "auto (aurora_instance_types.json)"
	inputs[genFieldOutputFile].CharLimit = 256
	inputs[genFieldOutputFile].Width = 40

	return GenerateModel{
		inputs:     inputs,
		focusIndex: genFieldEngine,
		region:     region,
	}
}

func (m GenerateModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m GenerateModel) modalWidth() int {
	maxInterior := m.width - 12
	if maxInterior > 80 {
		maxInterior = 80
	}
	if maxInterior < 40 {
		maxInterior = 40
	}
	return maxInterior
}

func (m GenerateModel) inputWidth() int {
	w := m.modalWidth() - 28
	if w < 20 {
		w = 20
	}
	if w > 60 {
		w = 60
	}
	return w
}

func (m GenerateModel) Update(msg tea.Msg) (GenerateModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		inputWidth := m.inputWidth()
		for i := range m.inputs {
			m.inputs[i].Width = inputWidth
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return GenerateCancelMsg{} }

		case "tab", "down":
			m.focusIndex++
			if m.focusIndex > genFieldSubmit {
				m.focusIndex = 0
			}
			return m, m.updateFocus()

		case "shift+tab", "up":
			m.focusIndex--
			if m.focusIndex < 0 {
				m.focusIndex = genFieldSubmit
			}
			return m, m.updateFocus()

		case "left":
			if m.focusIndex == genFieldEngine {
				m.engineIndex--
				if m.engineIndex < 0 {
					m.engineIndex = len(engineOptions) - 1
				}
				return m, nil
			}

		case "right":
			if m.focusIndex == genFieldEngine {
				m.engineIndex++
				if m.engineIndex >= len(engineOptions) {
					m.engineIndex = 0
				}
				return m, nil
			}

		case "enter":
			if m.focusIndex == genFieldEngine {
				m.engineIndex = (m.engineIndex + 1) % len(engineOptions)
				return m, nil
			}
			if m.focusIndex == genFieldSubmit {
				return m, m.submit()
			}
		}
	}

	// Update text inputs (skip cycling fields)
	if m.focusIndex != genFieldEngine && m.focusIndex != genFieldSubmit {
		cmds := make([]tea.Cmd, len(m.inputs))
		for i := range m.inputs {
			if i == genFieldEngine {
				continue
			}
			m.inputs[i], cmds[i] = m.inputs[i].Update(msg)
		}
		return m, tea.Batch(cmds...)
	}

	return m, nil
}

func (m GenerateModel) submit() tea.Cmd {
	engine := engineOptions[m.engineIndex]

	targetRegions := m.inputs[genFieldTargetRegions].Value()
	if targetRegions == "" {
		targetRegions = "all"
	}

	outputFile := m.inputs[genFieldOutputFile].Value()
	if outputFile == "" {
		if engine == "both" {
			outputFile = "aurora_instance_types.json"
		} else {
			outputFile = engine + "_instance_types.json"
		}
	}

	return func() tea.Msg {
		return GenerateSubmitMsg{
			Engine:        engine,
			TargetRegions: targetRegions,
			OutputFile:    outputFile,
		}
	}
}

func (m GenerateModel) updateFocus() tea.Cmd {
	cmds := make([]tea.Cmd, len(m.inputs))
	for i := range m.inputs {
		if i == m.focusIndex {
			cmds[i] = m.inputs[i].Focus()
		} else {
			m.inputs[i].Blur()
		}
	}
	return tea.Batch(cmds...)
}

func (m GenerateModel) View() string {
	var b strings.Builder

	modalW := m.modalWidth()

	title := titleStyle.Render("Generate Instance Types")
	subtitle := subtitleStyle.Render("Configure generation options, then press Enter on Generate")
	b.WriteString(title + "\n")
	b.WriteString(subtitle + "\n\n")

	// Show the inherited region as a read-only display
	regionLabel := labelStyle.Render("AWS Region")
	var regionValue string
	if m.region == "" {
		regionValue = lipgloss.NewStyle().Foreground(dangerColor).Render("(not set — set in config)")
	} else if parts := strings.Split(m.region, ","); len(parts) > 1 {
		first := strings.TrimSpace(parts[0])
		note := fmt.Sprintf(" (first of %d regions)", len(parts))
		regionValue = lipgloss.NewStyle().Foreground(dimTextColor).Render(first) +
			lipgloss.NewStyle().Foreground(dimTextColor).Italic(true).Render(note)
	} else {
		regionValue = lipgloss.NewStyle().Foreground(dimTextColor).Render(m.region)
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, regionLabel, regionValue) + "\n\n")

	fields := []struct {
		label string
		index int
	}{
		{"Engine", genFieldEngine},
		{"Target Regions", genFieldTargetRegions},
		{"Output File", genFieldOutputFile},
	}

	for _, f := range fields {
		focused := m.focusIndex == f.index

		var label string
		if focused {
			label = focusedLabelStyle.Render(f.label)
		} else {
			label = labelStyle.Render(f.label)
		}

		var value string
		if f.index == genFieldEngine {
			value = m.renderCycleSelector(engineOptions, m.engineIndex, focused)
		} else {
			value = m.inputs[f.index].View()
		}

		row := lipgloss.JoinHorizontal(lipgloss.Center, label, value)
		b.WriteString(row + "\n")
	}

	b.WriteString("\n")

	// Submit button
	var button string
	if m.focusIndex == genFieldSubmit {
		button = focusedButtonStyle.Render("[ Generate ]")
	} else {
		button = blurredButtonStyle.Render("  Generate  ")
	}
	contentW := modalW - 6
	b.WriteString(lipgloss.PlaceHorizontal(contentW, lipgloss.Center, button))

	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("tab/shift+tab: navigate  enter: select/submit  left/right: cycle  esc: back"))

	if m.err != nil {
		b.WriteString("\n" + errorStyle.Render(m.err.Error()))
	}

	content := b.String()
	modal := modalStyle.Width(modalW).Render(content)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
}

func (m GenerateModel) renderCycleSelector(options []string, selectedIndex int, focused bool) string {
	var parts []string
	for i, opt := range options {
		if i == selectedIndex {
			if focused {
				parts = append(parts, focusedInputStyle.Render("["+opt+"]"))
			} else {
				parts = append(parts, lipgloss.NewStyle().Foreground(textColor).Bold(true).Render("["+opt+"]"))
			}
		} else {
			parts = append(parts, blurredInputStyle.Render(" "+opt+" "))
		}
	}

	prefix := ""
	if focused {
		prefix = lipgloss.NewStyle().Foreground(primaryColor).Render("< ")
	} else {
		prefix = "  "
	}
	suffix := ""
	if focused {
		suffix = lipgloss.NewStyle().Foreground(primaryColor).Render(" >")
	} else {
		suffix = "  "
	}

	return prefix + strings.Join(parts, " ") + suffix
}
