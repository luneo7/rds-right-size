package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/guptarohit/asciigraph"
	cwTypes "github.com/luneo7/rds-right-size/internal/cw/types"
	"github.com/luneo7/rds-right-size/internal/rds-right-size/types"
)

type DetailModel struct {
	recommendation *types.Recommendation
	viewport       viewport.Model
	ready          bool
	width          int
	height         int
	exportStatus   string
}

func NewDetailModel(rec *types.Recommendation, width, height int) DetailModel {
	m := DetailModel{
		recommendation: rec,
		width:          width,
		height:         height,
	}

	vp := viewport.New(width, height-4)
	vp.SetContent(m.renderContent())
	m.viewport = vp
	m.ready = true

	return m
}

func (m DetailModel) Init() tea.Cmd {
	return nil
}

func (m DetailModel) Update(msg tea.Msg) (DetailModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.ready {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - 4
			m.viewport.SetContent(m.renderContent())
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m DetailModel) View() string {
	var b strings.Builder

	title := titleStyle.Render("RDS Right Size - Instance Detail")
	b.WriteString(title + "\n")

	if m.ready {
		b.WriteString(m.viewport.View())
	}

	b.WriteString("\n")

	// Export status
	if m.exportStatus != "" {
		if strings.HasPrefix(m.exportStatus, "Export error:") {
			b.WriteString(errorStyle.Render("  " + m.exportStatus))
		} else {
			b.WriteString(lipgloss.NewStyle().Foreground(successColor).Render("  " + m.exportStatus))
		}
		b.WriteString("\n")
	}

	scrollPct := fmt.Sprintf(" %d%%", int(m.viewport.ScrollPercent()*100))
	help := helpStyle.Render("esc/b: back to list  p: export PNG  scroll: up/down/pgup/pgdn" + scrollPct)
	b.WriteString(help)

	return b.String()
}

// SetExportStatus sets the export status message displayed at the bottom.
func (m DetailModel) SetExportStatus(status string) DetailModel {
	m.exportStatus = status
	return m
}

func (m DetailModel) renderContent() string {
	rec := m.recommendation
	if rec == nil {
		return "No recommendation selected"
	}

	var sections []string

	// Instance info section
	sections = append(sections, m.renderInstanceInfo())

	// Recommendation badge
	sections = append(sections, m.renderRecommendationBadge())

	// Instance comparison (current vs recommended)
	if rec.Recommendation != types.Terminate && rec.CurrentInstanceProperties != nil && rec.TargetInstanceProperties != nil {
		sections = append(sections, m.renderComparison())
	}

	// CloudWatch metric graphs
	if rec.TimeSeriesMetrics != nil {
		sections = append(sections, m.renderCharts())
	} else {
		sections = append(sections, "\n"+lipgloss.NewStyle().Foreground(dimTextColor).Render("  No time-series data available for graphs"))
	}

	return strings.Join(sections, "\n")
}

func (m DetailModel) renderInstanceInfo() string {
	rec := m.recommendation
	var rows []string

	addRow := func(label, value string) {
		rows = append(rows, detailLabelStyle.Render(label)+detailValueStyle.Render(value))
	}

	if rec.DBInstanceIdentifier != nil {
		addRow("Instance ID:", *rec.DBInstanceIdentifier)
	}
	if rec.DBInstanceArn != nil {
		addRow("ARN:", *rec.DBInstanceArn)
	}
	if rec.AvailabilityZone != nil {
		addRow("Availability Zone:", *rec.AvailabilityZone)
	}
	if rec.Engine != nil {
		engine := *rec.Engine
		if rec.EngineVersion != nil {
			engine += " " + *rec.EngineVersion
		}
		addRow("Engine:", engine)
	}
	if rec.DBInstanceClass != nil {
		addRow("Instance Class:", *rec.DBInstanceClass)
	}
	if rec.DBClusterIdentifier != nil {
		addRow("Cluster:", *rec.DBClusterIdentifier)
	}

	// Tags
	if len(rec.Tags) > 0 {
		tagStr := ""
		for k, v := range rec.Tags {
			if tagStr != "" {
				tagStr += ", "
			}
			tagStr += k + "=" + v
		}
		addRow("Tags:", tagStr)
	}

	content := strings.Join(rows, "\n")
	return detailBoxStyle.Render(content)
}

func (m DetailModel) renderRecommendationBadge() string {
	rec := m.recommendation

	var badge string
	switch rec.Recommendation {
	case types.UpScale:
		badge = badgeUpscale.Render(" UPSCALE ")
	case types.DownScale:
		badge = badgeDownscale.Render(" DOWNSCALE ")
	case types.Terminate:
		badge = badgeTerminate.Render(" TERMINATE ")
	}

	reason := lipgloss.NewStyle().Foreground(dimTextColor).Render("  " + string(rec.Reason))

	metricInfo := ""
	if rec.MetricValue != nil {
		metricInfo = lipgloss.NewStyle().Foreground(textColor).Render(fmt.Sprintf("  (metric: %.2f%%)", *rec.MetricValue))
	}

	projCPUInfo := ""
	if rec.ProjectedCPU != nil {
		projStyle := lipgloss.NewStyle().Foreground(warningColor)
		if *rec.ProjectedCPU >= 80 {
			projStyle = lipgloss.NewStyle().Foreground(dangerColor)
		} else if *rec.ProjectedCPU < 50 {
			projStyle = lipgloss.NewStyle().Foreground(successColor)
		}
		projCPUInfo = projStyle.Render(fmt.Sprintf("  Projected CPU: %.1f%%", *rec.ProjectedCPU))
	}

	costInfo := ""
	if rec.MonthlyApproximatePriceDiff != nil {
		if *rec.MonthlyApproximatePriceDiff > 0 {
			costInfo = costIncreaseStyle.Render(fmt.Sprintf("  Monthly impact: +$%.2f", *rec.MonthlyApproximatePriceDiff))
		} else {
			costInfo = savingsStyle.Render(fmt.Sprintf("  Monthly savings: $%.2f", *rec.MonthlyApproximatePriceDiff*-1))
		}
	}

	connWarning := ""
	if rec.MaxConnectionsAdjustRequired {
		peakStr := ""
		if rec.PeakConnections != nil {
			peakStr = fmt.Sprintf("peak: %.0f", *rec.PeakConnections)
		}
		defaultMax := ""
		if rec.TargetInstanceProperties != nil && rec.TargetInstanceProperties.MaxConnections != nil {
			defaultMax = fmt.Sprintf(", target default: %d", *rec.TargetInstanceProperties.MaxConnections)
		}
		connWarning = "\n  " + lipgloss.NewStyle().Foreground(warningColor).Render(
			fmt.Sprintf("Requires max_connections adjustment (%s%s)", peakStr, defaultMax))
	}

	clusterNote := ""
	if rec.ClusterEqualized {
		clusterNote = "\n  " + lipgloss.NewStyle().Foreground(dimTextColor).Italic(true).Render(
			"Adjusted for cluster homogeneity")
	}

	return "  " + badge + reason + metricInfo + projCPUInfo + "\n" + "  " + costInfo + connWarning + clusterNote
}

func regionFromAZ(az *string) string {
	if az == nil || len(*az) == 0 {
		return ""
	}
	// Strip the trailing availability zone letter (e.g., "us-east-1a" -> "us-east-1")
	s := *az
	if len(s) > 0 && s[len(s)-1] >= 'a' && s[len(s)-1] <= 'z' {
		return s[:len(s)-1]
	}
	return s
}

func (m DetailModel) renderComparison() string {
	rec := m.recommendation
	current := rec.CurrentInstanceProperties
	target := rec.TargetInstanceProperties
	region := regionFromAZ(rec.AvailabilityZone)

	currentName := ""
	if rec.DBInstanceClass != nil {
		currentName = *rec.DBInstanceClass
	}
	targetName := ""
	if rec.RecommendedInstanceType != nil {
		targetName = *rec.RecommendedInstanceType
	}

	// Compute card widths based on terminal width
	cardWidth := 36
	if m.width > 0 {
		// Each card + arrow (~7 chars) + margins (~8 chars)
		available := (m.width - 15) / 2
		if available > 20 && available < cardWidth {
			cardWidth = available
		}
		if available > cardWidth {
			cardWidth = available
		}
		if cardWidth > 50 {
			cardWidth = 50
		}
	}

	// Current instance card
	var currentRows []string
	currentRows = append(currentRows, lipgloss.NewStyle().Bold(true).Foreground(textColor).Render(currentName))
	currentRows = append(currentRows, "")
	if current != nil {
		currentPrice := current.GetPrice(region)
		currentRows = append(currentRows, fmt.Sprintf("vCPU:       %d", current.Vcpu))
		currentRows = append(currentRows, fmt.Sprintf("Memory:     %d GB", current.Mem))
		if current.MaxBandwidth != nil {
			currentRows = append(currentRows, fmt.Sprintf("Max BW:     %d Mbps", *current.MaxBandwidth))
		}
		if current.MaxConnections != nil {
			currentRows = append(currentRows, fmt.Sprintf("Max Conns:  %d", *current.MaxConnections))
		}
		currentRows = append(currentRows, fmt.Sprintf("Price/hr:   $%.4f", currentPrice))
		currentRows = append(currentRows, fmt.Sprintf("Price/mo:   $%.2f", currentPrice*730))
	}

	// Target instance card
	var targetRows []string
	targetRows = append(targetRows, lipgloss.NewStyle().Bold(true).Foreground(textColor).Render(targetName))
	targetRows = append(targetRows, "")
	if target != nil {
		targetPrice := target.GetPrice(region)
		targetRows = append(targetRows, renderComparisonValue("vCPU", current.Vcpu, target.Vcpu))
		targetRows = append(targetRows, renderComparisonMem("Memory", current.Mem, target.Mem))
		if target.MaxBandwidth != nil {
			var currentBW int64 = 0
			if current.MaxBandwidth != nil {
				currentBW = *current.MaxBandwidth
			}
			targetRows = append(targetRows, renderComparisonBW("Max BW", currentBW, *target.MaxBandwidth))
		}
		if target.MaxConnections != nil {
			var currentConns int64 = 0
			if current.MaxConnections != nil {
				currentConns = *current.MaxConnections
			}
			targetRows = append(targetRows, renderComparisonConns("Max Conns", currentConns, *target.MaxConnections))
		}
		targetRows = append(targetRows, fmt.Sprintf("Price/hr:   $%.4f", targetPrice))
		targetRows = append(targetRows, fmt.Sprintf("Price/mo:   $%.2f", targetPrice*730))
	}

	currentCardStyle := currentInstanceStyle.Width(cardWidth)
	targetCardStyle := recommendedInstanceStyle.Width(cardWidth)

	currentCard := currentCardStyle.Render(strings.Join(currentRows, "\n"))
	arrow := arrowStyle.Render("-->")
	targetCard := targetCardStyle.Render(strings.Join(targetRows, "\n"))

	return "\n" + lipgloss.JoinHorizontal(lipgloss.Center, currentCard, arrow, targetCard)
}

func renderComparisonValue(label string, current, target int64) string {
	indicator := ""
	if target > current {
		indicator = lipgloss.NewStyle().Foreground(successColor).Render(" (+)")
	} else if target < current {
		indicator = lipgloss.NewStyle().Foreground(dangerColor).Render(" (-)")
	}
	return fmt.Sprintf("%-12s%d%s", label+":", target, indicator)
}

func renderComparisonMem(label string, current, target int64) string {
	indicator := ""
	if target > current {
		indicator = lipgloss.NewStyle().Foreground(successColor).Render(" (+)")
	} else if target < current {
		indicator = lipgloss.NewStyle().Foreground(dangerColor).Render(" (-)")
	}
	return fmt.Sprintf("%-12s%d GB%s", label+":", target, indicator)
}

func renderComparisonBW(label string, current, target int64) string {
	indicator := ""
	if target > current {
		indicator = lipgloss.NewStyle().Foreground(successColor).Render(" (+)")
	} else if target < current {
		indicator = lipgloss.NewStyle().Foreground(dangerColor).Render(" (-)")
	}
	return fmt.Sprintf("%-12s%d Mbps%s", label+":", target, indicator)
}

func renderComparisonConns(label string, current, target int64) string {
	indicator := ""
	if target > current {
		indicator = lipgloss.NewStyle().Foreground(successColor).Render(" (+)")
	} else if target < current {
		indicator = lipgloss.NewStyle().Foreground(dangerColor).Render(" (-)")
	}
	return fmt.Sprintf("%-12s%d%s", label+":", target, indicator)
}

func (m DetailModel) renderCharts() string {
	var charts []string
	rec := m.recommendation
	tsMetrics := rec.TimeSeriesMetrics

	chartWidth := m.width - 20
	if chartWidth < 30 {
		chartWidth = 30
	}
	if chartWidth > 120 {
		chartWidth = 120
	}
	chartHeight := 10
	if m.height > 0 && m.height < 40 {
		chartHeight = 8
	}
	if m.height > 60 {
		chartHeight = 14
	}

	// CPU Utilization chart (with projected overlay when scaling)
	if metric, ok := tsMetrics.Metrics[cwTypes.CPUUtilization]; ok && len(metric.DataPoints) > 1 {
		actualValues := extractValues(metric)
		caption := formatDateRange(metric)

		// Show overlaid projected CPU when we have a scaling recommendation with different vCPU counts
		canProject := rec.Recommendation != types.Terminate &&
			rec.CurrentInstanceProperties != nil &&
			rec.TargetInstanceProperties != nil &&
			rec.CurrentInstanceProperties.Vcpu != rec.TargetInstanceProperties.Vcpu

		if canProject {
			vcpuRatio := float64(rec.CurrentInstanceProperties.Vcpu) / float64(rec.TargetInstanceProperties.Vcpu)
			projectedValues := make([]float64, len(actualValues))
			for i, v := range actualValues {
				pv := v * vcpuRatio
				if pv > 100 {
					pv = 100
				}
				projectedValues[i] = pv
			}
			chart := asciigraph.PlotMany([][]float64{actualValues, projectedValues},
				asciigraph.Height(chartHeight),
				asciigraph.Width(chartWidth),
				asciigraph.Caption(caption),
				asciigraph.Precision(1),
				asciigraph.SeriesColors(asciigraph.Red, asciigraph.Yellow),
				asciigraph.SeriesLegends("Actual", "Projected"),
			)
			charts = append(charts, chartTitleStyle.Render("  CPU Utilization (%)")+"\n"+indentChart(chart))
		} else {
			chart := asciigraph.Plot(actualValues,
				asciigraph.Height(chartHeight),
				asciigraph.Width(chartWidth),
				asciigraph.Caption(caption),
				asciigraph.Precision(1),
				asciigraph.SeriesColors(asciigraph.Red),
			)
			charts = append(charts, chartTitleStyle.Render("  CPU Utilization (%)")+"\n"+indentChart(chart))
		}
	}

	// Freeable Memory chart (convert to GB)
	if metric, ok := tsMetrics.Metrics[cwTypes.FreeableMemory]; ok && len(metric.DataPoints) > 1 {
		values := make([]float64, len(metric.DataPoints))
		for i, dp := range metric.DataPoints {
			values[i] = dp.Value / (1 << 30) // bytes to GB
		}
		caption := formatDateRange(metric)
		chart := asciigraph.Plot(values,
			asciigraph.Height(chartHeight),
			asciigraph.Width(chartWidth),
			asciigraph.Caption(caption),
			asciigraph.Precision(2),
			asciigraph.SeriesColors(asciigraph.Blue),
		)
		charts = append(charts, chartTitleStyle.Render("  Freeable Memory (GB)")+"\n"+indentChart(chart))
	}

	// Database Connections chart
	if metric, ok := tsMetrics.Metrics[cwTypes.DatabaseConnections]; ok && len(metric.DataPoints) > 1 {
		values := extractValues(metric)
		caption := formatDateRange(metric)
		chart := asciigraph.Plot(values,
			asciigraph.Height(chartHeight),
			asciigraph.Width(chartWidth),
			asciigraph.Caption(caption),
			asciigraph.Precision(0),
			asciigraph.SeriesColors(asciigraph.Green),
		)
		charts = append(charts, chartTitleStyle.Render("  Database Connections")+"\n"+indentChart(chart))
	}

	// Read + Write Throughput chart (combined)
	readMetric, hasRead := tsMetrics.Metrics[cwTypes.ReadThroughput]
	writeMetric, hasWrite := tsMetrics.Metrics[cwTypes.WriteThroughput]
	if hasRead && hasWrite && len(readMetric.DataPoints) > 1 {
		readValues := extractValues(readMetric)
		writeValues := extractValues(writeMetric)

		// Ensure same length
		minLen := len(readValues)
		if len(writeValues) < minLen {
			minLen = len(writeValues)
		}

		totalValues := make([]float64, minLen)
		for i := 0; i < minLen; i++ {
			totalValues[i] = (readValues[i] + writeValues[i]) / 1024 // bytes/s to KB/s
		}

		caption := formatDateRange(readMetric)
		chart := asciigraph.Plot(totalValues,
			asciigraph.Height(chartHeight),
			asciigraph.Width(chartWidth),
			asciigraph.Caption(caption),
			asciigraph.Precision(1),
			asciigraph.SeriesColors(asciigraph.Yellow),
		)
		charts = append(charts, chartTitleStyle.Render("  Total Throughput (KB/s)")+"\n"+indentChart(chart))
	}

	if len(charts) == 0 {
		return "\n" + lipgloss.NewStyle().Foreground(dimTextColor).Render("  No time-series data points to graph")
	}

	return "\n" + strings.Join(charts, "\n\n")
}

func extractValues(metric cwTypes.TimeSeriesMetric) []float64 {
	values := make([]float64, len(metric.DataPoints))
	for i, dp := range metric.DataPoints {
		values[i] = dp.Value
	}
	return values
}

func formatDateRange(metric cwTypes.TimeSeriesMetric) string {
	if len(metric.DataPoints) == 0 {
		return ""
	}
	start := metric.DataPoints[0].Timestamp.Format("Jan 02")
	end := metric.DataPoints[len(metric.DataPoints)-1].Timestamp.Format("Jan 02")
	return fmt.Sprintf("%s - %s (%d days)", start, end, len(metric.DataPoints))
}

func indentChart(chart string) string {
	lines := strings.Split(chart, "\n")
	for i, line := range lines {
		lines[i] = "    " + line
	}
	return strings.Join(lines, "\n")
}
