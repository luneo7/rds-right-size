package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	rds "github.com/luneo7/rds-right-size/internal/rds-right-size"
	"github.com/luneo7/rds-right-size/internal/rds-right-size/types"
)

// Breakpoint thresholds for responsive table layout
const (
	breakpointWide   = 160
	breakpointMedium = 120
	breakpointNarrow = 100
	breakpointTight  = 80
)

type columnLayout struct {
	instanceW   int
	regionW     int
	clusterW    int
	engineW     int
	currentW    int
	actionW     int
	targetW     int
	projCpuW    int
	reasonW     int
	costW       int
	showRegion  bool
	showCluster bool
	showEngine  bool
	showReason  bool
	showCurrent bool
	showProjCpu bool
}

func computeColumns(width int) columnLayout {
	if width >= breakpointWide {
		// Wide: all columns including Region, Cluster, Engine, and Proj CPU
		reasonW := width - 28 - 14 - 18 - 14 - 20 - 12 - 20 - 10 - 14 - 2
		if reasonW < 10 {
			reasonW = 10
		}
		return columnLayout{
			instanceW:   28,
			regionW:     14,
			clusterW:    18,
			engineW:     14,
			currentW:    20,
			actionW:     12,
			targetW:     20,
			projCpuW:    10,
			reasonW:     reasonW,
			costW:       14,
			showRegion:  true,
			showCluster: true,
			showEngine:  true,
			showReason:  true,
			showCurrent: true,
			showProjCpu: true,
		}
	} else if width >= breakpointMedium {
		// Medium-wide: show Region, hide Engine
		reasonW := width - 28 - 14 - 18 - 20 - 12 - 20 - 10 - 14 - 2
		if reasonW < 10 {
			reasonW = 10
		}
		return columnLayout{
			instanceW:   28,
			regionW:     14,
			clusterW:    18,
			currentW:    20,
			actionW:     12,
			targetW:     20,
			projCpuW:    10,
			reasonW:     reasonW,
			costW:       14,
			showRegion:  true,
			showCluster: true,
			showEngine:  false,
			showReason:  true,
			showCurrent: true,
			showProjCpu: true,
		}
	} else if width >= breakpointNarrow {
		// Narrow: hide Region, Cluster, Engine, Proj CPU
		reasonW := width - 26 - 18 - 12 - 18 - 10 - 14 - 2
		if reasonW < 10 {
			reasonW = 10
		}
		return columnLayout{
			instanceW:   26,
			currentW:    18,
			actionW:     12,
			targetW:     18,
			projCpuW:    10,
			reasonW:     reasonW,
			costW:       14,
			showRegion:  false,
			showEngine:  false,
			showReason:  true,
			showCurrent: true,
			showProjCpu: true,
		}
	} else if width >= breakpointTight {
		// Tight: Instance, Current, Action, Recommended, Cost
		return columnLayout{
			instanceW:   22,
			currentW:    18,
			actionW:     12,
			targetW:     18,
			costW:       width - 22 - 18 - 12 - 18 - 2,
			showRegion:  false,
			showEngine:  false,
			showReason:  false,
			showCurrent: true,
			showProjCpu: false,
		}
	}
	// Very narrow: Instance ID, Action, Recommended, Cost
	return columnLayout{
		instanceW:   width - 12 - 18 - 12 - 2,
		actionW:     12,
		targetW:     18,
		costW:       12,
		showRegion:  false,
		showEngine:  false,
		showReason:  false,
		showCurrent: false,
		showProjCpu: false,
	}
}

type ResultsModel struct {
	recommendations []types.Recommendation
	warnings        []string
	cursor          int
	scrollOffset    int
	width           int
	height          int
	exportPath      string
	exportErr       string
}

func NewResultsModel(recommendations []types.Recommendation) ResultsModel {
	return ResultsModel{
		recommendations: recommendations,
		cursor:          0,
		scrollOffset:    0,
	}
}

func (m ResultsModel) Init() tea.Cmd {
	return nil
}

func (m ResultsModel) Update(msg tea.Msg) (ResultsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.scrollOffset {
					m.scrollOffset = m.cursor
				}
			}
		case "down", "j":
			if m.cursor < len(m.recommendations)-1 {
				m.cursor++
				visibleRows := m.visibleRows()
				if m.cursor >= m.scrollOffset+visibleRows {
					m.scrollOffset = m.cursor - visibleRows + 1
				}
			}
		case "home":
			m.cursor = 0
			m.scrollOffset = 0
		case "end":
			m.cursor = len(m.recommendations) - 1
			visibleRows := m.visibleRows()
			if m.cursor >= visibleRows {
				m.scrollOffset = m.cursor - visibleRows + 1
			}
		}
	}

	return m, nil
}

// distinctRegions returns the number of distinct regions in the recommendations.
func (m ResultsModel) distinctRegions() int {
	seen := make(map[string]bool)
	for _, rec := range m.recommendations {
		if rec.Region != "" {
			seen[rec.Region] = true
		}
	}
	return len(seen)
}

func (m ResultsModel) visibleRows() int {
	// Reserve lines for fixed elements:
	// Title (2: text + margin), Summary box (~7: margin + border + content(3 lines w/ terminate) + border + newline),
	// Header (3: prefix + text + border + newline), Scroll indicator (1), Export status (1),
	// Help (2: newline + margin + text), Extra padding (2)
	reserved := 18
	// Add extra lines for per-region breakdown in summary
	regionCount := m.distinctRegions()
	if regionCount > 1 {
		reserved += regionCount
	}
	// Add extra line for skipped instances warning
	if len(m.warnings) > 0 {
		reserved++
	}
	available := m.height - reserved
	if available < 3 {
		available = 3
	}
	return available
}

func (m ResultsModel) View() string {
	var b strings.Builder

	title := titleStyle.Render("RDS Right Size - Recommendations")
	b.WriteString(title + "\n")

	if len(m.recommendations) == 0 {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(successColor).Bold(true).Render("  All instances are properly sized! No recommendations."))
		b.WriteString("\n\n")
		// Pad to fill screen
		contentLines := 4 // title + blank + message + blank
		footerLines := 1  // help
		if m.height > 0 {
			padLines := m.height - contentLines - footerLines
			for i := 0; i < padLines; i++ {
				b.WriteString("\n")
			}
		}
		b.WriteString(helpStyle.Render("b: back to config  q: quit"))
		return b.String()
	}

	// Summary
	summary := m.renderSummary()
	b.WriteString(summary)
	b.WriteString("\n")

	// Table header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Table rows
	visibleRows := m.visibleRows()
	endIdx := m.scrollOffset + visibleRows
	if endIdx > len(m.recommendations) {
		endIdx = len(m.recommendations)
	}

	for i := m.scrollOffset; i < endIdx; i++ {
		rec := m.recommendations[i]
		selected := i == m.cursor
		b.WriteString(m.renderRow(rec, selected))
		b.WriteString("\n")
	}

	// Pad remaining table rows with blank lines to fill available space
	renderedRows := endIdx - m.scrollOffset
	for i := renderedRows; i < visibleRows; i++ {
		b.WriteString("\n")
	}

	// Scroll indicator
	if len(m.recommendations) > visibleRows {
		scrollInfo := fmt.Sprintf("  Showing %d-%d of %d", m.scrollOffset+1, endIdx, len(m.recommendations))
		b.WriteString(lipgloss.NewStyle().Foreground(mutedColor).Render(scrollInfo))
		b.WriteString("\n")
	} else {
		b.WriteString("\n")
	}

	// Export status
	if m.exportPath != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(successColor).Render("  Exported: " + m.exportPath))
	} else if m.exportErr != "" {
		b.WriteString(errorStyle.Render("  Export error: " + m.exportErr))
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("enter: details  e: export JSON  p: export PNG  P: cluster PNG  b: back  q: quit"))

	return b.String()
}

func (m ResultsModel) renderSummary() string {
	upscale := 0
	downscale := 0
	terminate := 0

	for _, rec := range m.recommendations {
		switch rec.Recommendation {
		case types.UpScale:
			upscale++
		case types.DownScale:
			downscale++
		case types.Terminate:
			terminate++
		}
	}

	cb := rds.CalculateCostBreakdown(m.recommendations)

	counts := fmt.Sprintf("  Total: %d  |  ", len(m.recommendations))
	counts += upscaleStyle.Render(fmt.Sprintf("Upscale: %d", upscale)) + "  |  "
	counts += downscaleStyle.Render(fmt.Sprintf("Downscale: %d", downscale)) + "  |  "
	counts += terminateStyle.Render(fmt.Sprintf("Terminate: %d", terminate))

	formatCostLine := func(label string, monthly float64) string {
		yearly := monthly * 12
		if monthly > 0 {
			return costIncreaseStyle.Render(fmt.Sprintf("  %s: +$%.2f/mo (+$%.2f/yr)", label, monthly, yearly))
		} else if monthly < 0 {
			savings := monthly * -1
			yearlySavings := yearly * -1
			return savingsStyle.Render(fmt.Sprintf("  %s: $%.2f/mo ($%.2f/yr)", label, savings, yearlySavings))
		}
		return lipgloss.NewStyle().Foreground(dimTextColor).Render(fmt.Sprintf("  %s: no cost impact", label))
	}

	var costLines string
	if cb.HasTerminations && cb.ScalingMonthly != cb.TotalMonthly {
		costLines = formatCostLine("Savings (scaling)", cb.ScalingMonthly) + "\n" +
			formatCostLine("Savings (w/ terminate)", cb.TotalMonthly)
	} else {
		costLines = formatCostLine("Savings", cb.TotalMonthly)
	}

	// Per-region breakdown when multiple regions are present
	regionalCB, regions := rds.CalculateRegionalCostBreakdown(m.recommendations)
	if len(regions) > 1 {
		for _, region := range regions {
			rcb := regionalCB[region]
			costLines += "\n" + formatCostLine("  "+region, rcb.TotalMonthly)
		}
	}

	// Skipped instances warning
	if len(m.warnings) > 0 {
		warnText := fmt.Sprintf("  %d instance(s) skipped (missing CloudWatch metrics)", len(m.warnings))
		costLines += "\n" + lipgloss.NewStyle().Foreground(warningColor).Render(warnText)
	}

	content := counts + "\n" + costLines
	return summaryBoxStyle.Render(content)
}

func (m ResultsModel) renderHeader() string {
	layout := computeColumns(m.width)

	var cols []string
	cols = append(cols, tableHeaderStyle.Width(layout.instanceW).Render("Instance ID"))
	if layout.showRegion {
		cols = append(cols, tableHeaderStyle.Width(layout.regionW).Render("Region"))
	}
	if layout.showCluster {
		cols = append(cols, tableHeaderStyle.Width(layout.clusterW).Render("Cluster"))
	}
	if layout.showEngine {
		cols = append(cols, tableHeaderStyle.Width(layout.engineW).Render("Engine"))
	}
	if layout.showCurrent {
		cols = append(cols, tableHeaderStyle.Width(layout.currentW).Render("Current Type"))
	}
	cols = append(cols, tableHeaderStyle.Width(layout.actionW).Render("Action"))
	cols = append(cols, tableHeaderStyle.Width(layout.targetW).Render("Recommended"))
	if layout.showProjCpu {
		cols = append(cols, tableHeaderStyle.Width(layout.projCpuW).Render("Proj CPU"))
	}
	if layout.showReason {
		cols = append(cols, tableHeaderStyle.Width(layout.reasonW).Render("Reason"))
	}
	cols = append(cols, tableHeaderStyle.Width(layout.costW).Render("Cost/mo"))

	return "  " + lipgloss.JoinHorizontal(lipgloss.Top, cols...)
}

func (m ResultsModel) renderRow(rec types.Recommendation, selected bool) string {
	layout := computeColumns(m.width)

	instanceID := ""
	if rec.DBInstanceIdentifier != nil {
		instanceID = *rec.DBInstanceIdentifier
	}
	maxInstLen := layout.instanceW - 2
	if maxInstLen < 4 {
		maxInstLen = 4
	}
	if len(instanceID) > maxInstLen {
		instanceID = instanceID[:maxInstLen-2] + ".."
	}

	region := rec.Region
	maxRegionLen := layout.regionW - 2
	if maxRegionLen > 0 && len(region) > maxRegionLen {
		region = region[:maxRegionLen-2] + ".."
	}

	clusterID := ""
	if rec.DBClusterIdentifier != nil {
		clusterID = *rec.DBClusterIdentifier
	}
	maxClusterLen := layout.clusterW - 2
	if maxClusterLen > 0 && len(clusterID) > maxClusterLen {
		clusterID = clusterID[:maxClusterLen-2] + ".."
	}

	engine := ""
	if rec.Engine != nil {
		engine = *rec.Engine
	}
	maxEngLen := layout.engineW - 2
	if maxEngLen > 0 && len(engine) > maxEngLen {
		engine = engine[:maxEngLen-2] + ".."
	}

	currentType := ""
	if rec.DBInstanceClass != nil {
		currentType = *rec.DBInstanceClass
	}

	recType := ""
	var recStyle lipgloss.Style
	switch rec.Recommendation {
	case types.UpScale:
		recType = "UPSCALE"
		recStyle = upscaleStyle
	case types.DownScale:
		recType = "DOWNSCALE"
		if rec.MaxConnectionsAdjustRequired {
			recType = "DOWNSCALE*"
		}
		recStyle = downscaleStyle
	case types.Terminate:
		recType = "TERMINATE"
		recStyle = terminateStyle
	}

	target := ""
	if rec.RecommendedInstanceType != nil {
		target = *rec.RecommendedInstanceType
	}

	projCpu := ""
	if rec.ProjectedCPU != nil {
		projCpu = fmt.Sprintf("%.1f%%", *rec.ProjectedCPU)
	}

	reason := string(rec.Reason)
	maxReasonLen := layout.reasonW - 2
	if maxReasonLen > 0 && len(reason) > maxReasonLen {
		reason = reason[:maxReasonLen-2] + ".."
	}

	costDiff := ""
	if rec.MonthlyApproximatePriceDiff != nil {
		if *rec.MonthlyApproximatePriceDiff > 0 {
			costDiff = costIncreaseStyle.Render(fmt.Sprintf("+$%.2f", *rec.MonthlyApproximatePriceDiff))
		} else {
			costDiff = savingsStyle.Render(fmt.Sprintf("-$%.2f", *rec.MonthlyApproximatePriceDiff*-1))
		}
	}

	baseStyle := normalRowStyle
	if selected {
		baseStyle = selectedRowStyle
	}

	var cols []string
	cols = append(cols, baseStyle.Width(layout.instanceW).Render(instanceID))
	if layout.showRegion {
		cols = append(cols, baseStyle.Width(layout.regionW).Render(region))
	}
	if layout.showCluster {
		cols = append(cols, baseStyle.Width(layout.clusterW).Render(clusterID))
	}
	if layout.showEngine {
		cols = append(cols, baseStyle.Width(layout.engineW).Render(engine))
	}
	if layout.showCurrent {
		cols = append(cols, baseStyle.Width(layout.currentW).Render(currentType))
	}
	cols = append(cols, baseStyle.Width(layout.actionW).Render(recStyle.Render(recType)))
	cols = append(cols, baseStyle.Width(layout.targetW).Render(target))
	if layout.showProjCpu {
		cols = append(cols, baseStyle.Width(layout.projCpuW).Render(projCpu))
	}
	if layout.showReason {
		cols = append(cols, baseStyle.Width(layout.reasonW).Render(reason))
	}
	cols = append(cols, baseStyle.Width(layout.costW).Render(costDiff))

	cursor := "  "
	if selected {
		cursor = lipgloss.NewStyle().Foreground(primaryColor).Bold(true).Render("> ")
	}

	return cursor + lipgloss.JoinHorizontal(lipgloss.Top, cols...)
}

func (m ResultsModel) SelectedRecommendation() *types.Recommendation {
	if len(m.recommendations) == 0 {
		return nil
	}
	return &m.recommendations[m.cursor]
}

func (m ResultsModel) SetExportPath(path string) ResultsModel {
	m.exportPath = path
	m.exportErr = ""
	return m
}

func (m ResultsModel) SetExportErr(err string) ResultsModel {
	m.exportErr = err
	m.exportPath = ""
	return m
}
