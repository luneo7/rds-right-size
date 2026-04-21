package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	fieldProfile = iota
	fieldRegion
	fieldTags
	fieldPeriod
	fieldCPUUpsize
	fieldCPUDownsize
	fieldMemUpsize
	fieldStat
	fieldPreferNewGen
	fieldInstanceTypes
	fieldSubmit
)

var statOptions = []string{"p99", "p95", "p50", "Average"}
var preferNewGenOptions = []string{"Off", "On"}

type ConfigModel struct {
	inputs            []textinput.Model
	focusIndex        int
	statIndex         int
	preferNewGenIndex int
	err               error
	width             int
	height            int

	// Initial/default values
	defaults ConfigValues
}

type ConfigValues struct {
	Profile          string
	Region           string
	Tags             string
	Period           int
	CPUUpsize        float64
	CPUDownsize      float64
	MemUpsize        float64
	Stat             string
	PreferNewGen     bool
	InstanceTypesURL string
}

func NewConfigModel(defaults ConfigValues) ConfigModel {
	inputs := make([]textinput.Model, 10)

	// Profile
	inputs[fieldProfile] = textinput.New()
	inputs[fieldProfile].Placeholder = "default"
	inputs[fieldProfile].CharLimit = 64
	inputs[fieldProfile].Width = 40
	inputs[fieldProfile].SetValue(defaults.Profile)
	inputs[fieldProfile].Focus()

	// Region
	inputs[fieldRegion] = textinput.New()
	inputs[fieldRegion].Placeholder = "us-east-1"
	inputs[fieldRegion].CharLimit = 20
	inputs[fieldRegion].Width = 40
	inputs[fieldRegion].SetValue(defaults.Region)

	// Tags
	inputs[fieldTags] = textinput.New()
	inputs[fieldTags].Placeholder = "env=prod,team=platform"
	inputs[fieldTags].CharLimit = 256
	inputs[fieldTags].Width = 40
	inputs[fieldTags].SetValue(defaults.Tags)

	// Period
	inputs[fieldPeriod] = textinput.New()
	inputs[fieldPeriod].Placeholder = "30"
	inputs[fieldPeriod].CharLimit = 5
	inputs[fieldPeriod].Width = 40
	if defaults.Period > 0 {
		inputs[fieldPeriod].SetValue(strconv.Itoa(defaults.Period))
	}

	// CPU Upsize
	inputs[fieldCPUUpsize] = textinput.New()
	inputs[fieldCPUUpsize].Placeholder = "75"
	inputs[fieldCPUUpsize].CharLimit = 6
	inputs[fieldCPUUpsize].Width = 40
	if defaults.CPUUpsize > 0 {
		inputs[fieldCPUUpsize].SetValue(fmt.Sprintf("%.0f", defaults.CPUUpsize))
	}

	// CPU Downsize
	inputs[fieldCPUDownsize] = textinput.New()
	inputs[fieldCPUDownsize].Placeholder = "30"
	inputs[fieldCPUDownsize].CharLimit = 6
	inputs[fieldCPUDownsize].Width = 40
	if defaults.CPUDownsize > 0 {
		inputs[fieldCPUDownsize].SetValue(fmt.Sprintf("%.0f", defaults.CPUDownsize))
	}

	// Mem Upsize
	inputs[fieldMemUpsize] = textinput.New()
	inputs[fieldMemUpsize].Placeholder = "5"
	inputs[fieldMemUpsize].CharLimit = 6
	inputs[fieldMemUpsize].Width = 40
	if defaults.MemUpsize > 0 {
		inputs[fieldMemUpsize].SetValue(fmt.Sprintf("%.0f", defaults.MemUpsize))
	}

	// Stat (cycling, not a text input - but we use a text input as display)
	inputs[fieldStat] = textinput.New()
	inputs[fieldStat].Placeholder = "p99"
	inputs[fieldStat].CharLimit = 10
	inputs[fieldStat].Width = 40

	// Prefer New Gen (cycling selector)
	inputs[fieldPreferNewGen] = textinput.New()
	inputs[fieldPreferNewGen].Placeholder = "Off"
	inputs[fieldPreferNewGen].CharLimit = 5
	inputs[fieldPreferNewGen].Width = 40

	// Instance types URL
	inputs[fieldInstanceTypes] = textinput.New()
	inputs[fieldInstanceTypes].Placeholder = "https://... or /path/to/file.json"
	inputs[fieldInstanceTypes].CharLimit = 512
	inputs[fieldInstanceTypes].Width = 40
	inputs[fieldInstanceTypes].SetValue(defaults.InstanceTypesURL)

	// Find stat index
	statIdx := 0
	for i, s := range statOptions {
		if s == defaults.Stat {
			statIdx = i
			break
		}
	}

	// Find prefer new gen index
	preferNewGenIdx := 0
	if defaults.PreferNewGen {
		preferNewGenIdx = 1
	}

	return ConfigModel{
		inputs:            inputs,
		focusIndex:        0,
		statIndex:         statIdx,
		preferNewGenIndex: preferNewGenIdx,
		defaults:          defaults,
	}
}

func (m ConfigModel) Init() tea.Cmd {
	return textinput.Blink
}

// modalWidth returns the lipgloss Width value for the config modal.
// This includes padding (3*2=6 chars) but NOT border (1*2=2 chars).
// The actual content area is modalWidth() - 6.
func (m ConfigModel) modalWidth() int {
	// Modal takes up to 80 chars interior, or terminal width minus border/padding/margin
	// modalStyle has: border (2) + padding (3*2=6) = 8 chars horizontal overhead
	maxInterior := m.width - 12
	if maxInterior > 80 {
		maxInterior = 80
	}
	if maxInterior < 40 {
		maxInterior = 40
	}
	return maxInterior
}

// inputWidth returns the appropriate text input width based on modal interior width.
// Label column is ~24 chars wide, so subtract that plus padding.
func (m ConfigModel) inputWidth() int {
	w := m.modalWidth() - 28 // subtract label width + padding
	if w < 20 {
		w = 20
	}
	if w > 60 {
		w = 60
	}
	return w
}

func (m ConfigModel) Update(msg tea.Msg) (ConfigModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Scale input widths based on terminal width
		inputWidth := m.inputWidth()
		for i := range m.inputs {
			m.inputs[i].Width = inputWidth
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "down":
			m.focusIndex++
			if m.focusIndex > fieldSubmit {
				m.focusIndex = 0
			}
			return m, m.updateFocus()

		case "shift+tab", "up":
			m.focusIndex--
			if m.focusIndex < 0 {
				m.focusIndex = fieldSubmit
			}
			return m, m.updateFocus()

		case "left":
			if m.focusIndex == fieldStat {
				m.statIndex--
				if m.statIndex < 0 {
					m.statIndex = len(statOptions) - 1
				}
				return m, nil
			}
			if m.focusIndex == fieldPreferNewGen {
				m.preferNewGenIndex--
				if m.preferNewGenIndex < 0 {
					m.preferNewGenIndex = len(preferNewGenOptions) - 1
				}
				return m, nil
			}

		case "right":
			if m.focusIndex == fieldStat {
				m.statIndex++
				if m.statIndex >= len(statOptions) {
					m.statIndex = 0
				}
				return m, nil
			}
			if m.focusIndex == fieldPreferNewGen {
				m.preferNewGenIndex++
				if m.preferNewGenIndex >= len(preferNewGenOptions) {
					m.preferNewGenIndex = 0
				}
				return m, nil
			}

		case "enter":
			if m.focusIndex == fieldStat {
				m.statIndex = (m.statIndex + 1) % len(statOptions)
				return m, nil
			}
			if m.focusIndex == fieldPreferNewGen {
				m.preferNewGenIndex = (m.preferNewGenIndex + 1) % len(preferNewGenOptions)
				return m, nil
			}
			// Submit is handled by the parent model
		}
	}

	// Update text inputs (skip cycling fields)
	if m.focusIndex != fieldStat && m.focusIndex != fieldPreferNewGen && m.focusIndex != fieldSubmit {
		cmds := make([]tea.Cmd, len(m.inputs))
		for i := range m.inputs {
			if i == fieldStat || i == fieldPreferNewGen {
				continue
			}
			m.inputs[i], cmds[i] = m.inputs[i].Update(msg)
		}
		return m, tea.Batch(cmds...)
	}

	return m, nil
}

func (m ConfigModel) updateFocus() tea.Cmd {
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

func (m ConfigModel) View() string {
	var b strings.Builder

	modalW := m.modalWidth()

	title := titleStyle.Render("RDS Right Size - Configuration")
	subtitle := subtitleStyle.Render("Configure analysis parameters, then press Enter on Run Analysis")
	b.WriteString(title + "\n")
	b.WriteString(subtitle + "\n\n")

	fields := []struct {
		label string
		index int
	}{
		{"AWS Profile", fieldProfile},
		{"AWS Region", fieldRegion},
		{"Tag Filters", fieldTags},
		{"Period (days)", fieldPeriod},
		{"CPU Upsize %", fieldCPUUpsize},
		{"CPU Downsize %", fieldCPUDownsize},
		{"Mem Upsize %", fieldMemUpsize},
		{"Statistic", fieldStat},
		{"Prefer New Gen", fieldPreferNewGen},
		{"Instance Types", fieldInstanceTypes},
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
		if f.index == fieldStat {
			value = m.renderCycleSelector(statOptions, m.statIndex, focused)
		} else if f.index == fieldPreferNewGen {
			value = m.renderCycleSelector(preferNewGenOptions, m.preferNewGenIndex, focused)
		} else {
			value = m.inputs[f.index].View()
		}

		row := lipgloss.JoinHorizontal(lipgloss.Center, label, value)
		b.WriteString(row + "\n")
	}

	b.WriteString("\n")

	// Submit button (centered)
	var button string
	if m.focusIndex == fieldSubmit {
		button = focusedButtonStyle.Render("[ Run Analysis ]")
	} else {
		button = blurredButtonStyle.Render("  Run Analysis  ")
	}
	// Center button within the content area.
	// modalW is the lipgloss Width (includes padding). Content area = modalW - 6.
	contentW := modalW - 6
	b.WriteString(lipgloss.PlaceHorizontal(contentW, lipgloss.Center, button))

	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("tab/shift+tab: navigate  enter: select/submit  left/right: cycle  ctrl+r: run  ctrl+u: generate types  ctrl+c/q: quit"))

	if m.err != nil {
		b.WriteString("\n" + errorStyle.Render(m.err.Error()))
	}

	content := b.String()
	modal := modalStyle.Width(modalW).Render(content)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
}

func (m ConfigModel) renderCycleSelector(options []string, selectedIndex int, focused bool) string {
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

// GetValues returns the current configuration values from the form.
func (m ConfigModel) GetValues() (ConfigValues, error) {
	period, err := strconv.Atoi(m.inputs[fieldPeriod].Value())
	if err != nil || period <= 0 {
		if m.inputs[fieldPeriod].Value() == "" {
			period = 30
		} else {
			return ConfigValues{}, fmt.Errorf("invalid period: %s", m.inputs[fieldPeriod].Value())
		}
	}

	cpuUpsize, err := strconv.ParseFloat(m.inputs[fieldCPUUpsize].Value(), 64)
	if err != nil {
		if m.inputs[fieldCPUUpsize].Value() == "" {
			cpuUpsize = 75
		} else {
			return ConfigValues{}, fmt.Errorf("invalid CPU upsize threshold: %s", m.inputs[fieldCPUUpsize].Value())
		}
	}

	cpuDownsize, err := strconv.ParseFloat(m.inputs[fieldCPUDownsize].Value(), 64)
	if err != nil {
		if m.inputs[fieldCPUDownsize].Value() == "" {
			cpuDownsize = 30
		} else {
			return ConfigValues{}, fmt.Errorf("invalid CPU downsize threshold: %s", m.inputs[fieldCPUDownsize].Value())
		}
	}

	memUpsize, err := strconv.ParseFloat(m.inputs[fieldMemUpsize].Value(), 64)
	if err != nil {
		if m.inputs[fieldMemUpsize].Value() == "" {
			memUpsize = 5
		} else {
			return ConfigValues{}, fmt.Errorf("invalid memory upsize threshold: %s", m.inputs[fieldMemUpsize].Value())
		}
	}

	instanceTypesURL := m.inputs[fieldInstanceTypes].Value()
	if instanceTypesURL == "" {
		instanceTypesURL = m.defaults.InstanceTypesURL
	}

	return ConfigValues{
		Profile:          m.inputs[fieldProfile].Value(),
		Region:           m.inputs[fieldRegion].Value(),
		Tags:             m.inputs[fieldTags].Value(),
		Period:           period,
		CPUUpsize:        cpuUpsize,
		CPUDownsize:      cpuDownsize,
		MemUpsize:        memUpsize,
		Stat:             statOptions[m.statIndex],
		PreferNewGen:     m.preferNewGenIndex == 1,
		InstanceTypesURL: instanceTypesURL,
	}, nil
}
