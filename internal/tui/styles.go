package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Color palette
	primaryColor   = lipgloss.Color("#7C3AED") // Purple
	secondaryColor = lipgloss.Color("#06B6D4") // Cyan
	successColor   = lipgloss.Color("#10B981") // Green
	warningColor   = lipgloss.Color("#F59E0B") // Amber
	dangerColor    = lipgloss.Color("#EF4444") // Red
	mutedColor     = lipgloss.Color("#6B7280") // Gray
	textColor      = lipgloss.Color("#F9FAFB") // White
	dimTextColor   = lipgloss.Color("#9CA3AF") // Light gray
	bgColor        = lipgloss.Color("#111827") // Dark background
	surfaceColor   = lipgloss.Color("#1F2937") // Slightly lighter background
	borderColor    = lipgloss.Color("#374151") // Border gray

	// Title styles
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor).
			MarginBottom(1)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(dimTextColor).
			MarginBottom(1)

	// Form styles
	focusedInputStyle = lipgloss.NewStyle().
				Foreground(primaryColor).
				Bold(true)

	blurredInputStyle = lipgloss.NewStyle().
				Foreground(dimTextColor)

	labelStyle = lipgloss.NewStyle().
			Foreground(textColor).
			Width(22).
			Align(lipgloss.Right).
			MarginRight(2)

	focusedLabelStyle = lipgloss.NewStyle().
				Foreground(primaryColor).
				Bold(true).
				Width(22).
				Align(lipgloss.Right).
				MarginRight(2)

	inputFieldStyle = lipgloss.NewStyle().
			Foreground(textColor).
			Background(surfaceColor).
			Padding(0, 1)

	// Button styles
	focusedButtonStyle = lipgloss.NewStyle().
				Foreground(textColor).
				Background(primaryColor).
				Bold(true).
				Padding(0, 3).
				MarginTop(1)

	blurredButtonStyle = lipgloss.NewStyle().
				Foreground(dimTextColor).
				Background(surfaceColor).
				Padding(0, 3).
				MarginTop(1)

	// Recommendation type styles
	upscaleStyle = lipgloss.NewStyle().
			Foreground(dangerColor).
			Bold(true)

	downscaleStyle = lipgloss.NewStyle().
			Foreground(successColor).
			Bold(true)

	terminateStyle = lipgloss.NewStyle().
			Foreground(warningColor).
			Bold(true)

	// Table styles
	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(secondaryColor).
				BorderBottom(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(borderColor).
				Padding(0, 1)

	selectedRowStyle = lipgloss.NewStyle().
			Background(surfaceColor).
			Foreground(textColor).
			Bold(true).
			Padding(0, 1)

	normalRowStyle = lipgloss.NewStyle().
			Foreground(dimTextColor).
			Padding(0, 1)

	// Detail view styles
	detailBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(1, 2).
			MarginBottom(1)

	detailLabelStyle = lipgloss.NewStyle().
				Foreground(secondaryColor).
				Bold(true).
				Width(24)

	detailValueStyle = lipgloss.NewStyle().
				Foreground(textColor)

	chartTitleStyle = lipgloss.NewStyle().
			Foreground(primaryColor).
			Bold(true).
			MarginTop(1).
			MarginBottom(0)

	// Summary styles
	summaryBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(0, 2).
			MarginTop(1)

	savingsStyle = lipgloss.NewStyle().
			Foreground(successColor).
			Bold(true)

	costIncreaseStyle = lipgloss.NewStyle().
				Foreground(dangerColor).
				Bold(true)

	// Help styles
	helpStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			MarginTop(1)

	// Spinner/loading styles
	spinnerTextStyle = lipgloss.NewStyle().
				Foreground(dimTextColor)

	// Error styles
	errorStyle = lipgloss.NewStyle().
			Foreground(dangerColor).
			Bold(true)

	// Badge/pill styles
	badgeUpscale = lipgloss.NewStyle().
			Foreground(textColor).
			Background(dangerColor).
			Bold(true).
			Padding(0, 1)

	badgeDownscale = lipgloss.NewStyle().
			Foreground(textColor).
			Background(successColor).
			Bold(true).
			Padding(0, 1)

	badgeTerminate = lipgloss.NewStyle().
			Foreground(textColor).
			Background(warningColor).
			Bold(true).
			Padding(0, 1)

	// Comparison styles (current vs recommended)
	currentInstanceStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(borderColor).
				Padding(1, 2).
				Width(36)

	recommendedInstanceStyle = lipgloss.NewStyle().
					Border(lipgloss.RoundedBorder()).
					BorderForeground(successColor).
					Padding(1, 2).
					Width(36)

	arrowStyle = lipgloss.NewStyle().
			Foreground(primaryColor).
			Bold(true).
			Padding(2, 2)

	// Modal styles
	modalStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(1, 3)
)
