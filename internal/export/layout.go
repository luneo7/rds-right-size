package export

import (
	"fmt"
	"image"
	"image/color"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/luneo7/rds-right-size/internal/rds-right-size/types"
)

// Layout constants
const (
	imgWidth    = 1200
	marginX     = 50.0
	marginY     = 40.0
	sectionGap  = 28.0
	lineHeight  = 30.0
	cardPadding = 22.0
	cardGap     = 20.0
	cardRadius  = 8.0
	badgePadX   = 14.0
	badgePadY   = 6.0
	badgeRadius = 5.0
)

// Font sizes
const (
	fontSizeTitle    = 26.0
	fontSizeSubtitle = 16.0
	fontSizeBody     = 18.0
	fontSizeBadge    = 16.0
	fontSizeSmall    = 15.0
	fontSizeCard     = 16.0
)

// Parsed fonts (loaded once)
var (
	fontRegular *truetype.Font
	fontBold    *truetype.Font
	fontMono    *truetype.Font
)

func init() {
	var err error
	fontRegular, err = truetype.Parse(goregular.TTF)
	if err != nil {
		panic("failed to parse Go Regular font: " + err.Error())
	}
	fontBold, err = truetype.Parse(gobold.TTF)
	if err != nil {
		panic("failed to parse Go Bold font: " + err.Error())
	}
	fontMono, err = truetype.Parse(gomono.TTF)
	if err != nil {
		panic("failed to parse Go Mono font: " + err.Error())
	}
}

func newFace(f *truetype.Font, size float64) font.Face {
	return truetype.NewFace(f, &truetype.Options{
		Size:       size,
		DPI:        72,
		Hinting:    font.HintingNone,
		SubPixelsX: 16,
		SubPixelsY: 4,
	})
}

// setFont is a helper that sets both the font face and the color on the context.
func setFont(dc *gg.Context, f *truetype.Font, size float64, c color.Color) {
	dc.SetFontFace(newFace(f, size))
	dc.SetColor(c)
}

// contentWidth returns the usable content width inside margins.
func contentWidth() float64 {
	return float64(imgWidth) - 2*marginX
}

// drawBadge draws a colored rounded-rect badge with white text and returns its width.
func drawBadge(dc *gg.Context, text string, x, y float64, bg color.Color) float64 {
	setFont(dc, fontBold, fontSizeBadge, textWhite)
	tw, th := dc.MeasureString(text)
	w := tw + 2*badgePadX
	h := th + 2*badgePadY

	dc.SetColor(bg)
	dc.DrawRoundedRectangle(x, y, w, h, badgeRadius)
	dc.Fill()

	setFont(dc, fontBold, fontSizeBadge, textWhite)
	dc.DrawString(text, x+badgePadX, y+badgePadY+th)
	return w
}

// badgeColor returns the background color for a recommendation type.
func badgeColor(rec types.RecommendationType) color.Color {
	switch rec {
	case types.UpScale:
		return colorRed
	case types.DownScale:
		return colorGreen
	case types.Terminate:
		return colorAmber
	}
	return textMedium
}

// drawRoundedCard draws a rounded-rect card background at the given position.
func drawRoundedCard(dc *gg.Context, x, y, w, h float64, bg, border color.Color) {
	dc.SetColor(bg)
	dc.DrawRoundedRectangle(x, y, w, h, cardRadius)
	dc.Fill()
	dc.SetColor(border)
	dc.SetLineWidth(1)
	dc.DrawRoundedRectangle(x, y, w, h, cardRadius)
	dc.Stroke()
}

// drawHeader draws the instance header (badge + instance ID + subtitle).
// Returns the Y position after the header.
func drawHeader(dc *gg.Context, rec *types.Recommendation, y float64) float64 {
	x := marginX

	// Badge
	badgeText := string(rec.Recommendation)
	bw := drawBadge(dc, badgeText, x, y, badgeColor(rec.Recommendation))

	// Instance ID
	instanceID := ""
	if rec.DBInstanceIdentifier != nil {
		instanceID = *rec.DBInstanceIdentifier
	}
	setFont(dc, fontBold, fontSizeTitle, textDark)
	dc.DrawString(instanceID, x+bw+12, y+fontSizeTitle)
	y += fontSizeTitle + badgePadY*2 + 6

	// Subtitle line: engine | AZ | cluster
	var parts []string
	if rec.Engine != nil {
		engine := *rec.Engine
		if rec.EngineVersion != nil {
			engine += " " + *rec.EngineVersion
		}
		parts = append(parts, engine)
	}
	if rec.AvailabilityZone != nil {
		parts = append(parts, *rec.AvailabilityZone)
	}
	if rec.DBClusterIdentifier != nil {
		parts = append(parts, "cluster: "+*rec.DBClusterIdentifier)
	}
	if len(parts) > 0 {
		setFont(dc, fontRegular, fontSizeSubtitle, textMedium)
		subtitle := ""
		for i, p := range parts {
			if i > 0 {
				subtitle += "  |  "
			}
			subtitle += p
		}
		dc.DrawString(subtitle, x, y+fontSizeSubtitle)
		y += fontSizeSubtitle + 4
	}

	return y + sectionGap/2
}

// drawRecommendationInfo draws the reason, metric, projected CPU, and cost lines.
// Returns the Y position after the section.
func drawRecommendationInfo(dc *gg.Context, rec *types.Recommendation, region string, y float64) float64 {
	x := marginX

	// Reason line
	setFont(dc, fontRegular, fontSizeBody, textMedium)
	reason := "Reason: " + string(rec.Reason)
	if rec.MetricValue != nil {
		reason += fmt.Sprintf("  (metric: %.2f%%)", *rec.MetricValue)
	}
	dc.DrawString(reason, x, y+fontSizeBody)
	y += lineHeight

	// Projected CPU + cost line
	infoLine := ""
	if rec.ProjectedCPU != nil {
		infoLine = fmt.Sprintf("Projected CPU: %.1f%%", *rec.ProjectedCPU)
	}
	if rec.MonthlyApproximatePriceDiff != nil {
		diff := *rec.MonthlyApproximatePriceDiff
		costStr := ""
		if diff > 0 {
			costStr = fmt.Sprintf("Monthly cost increase: +$%.2f", diff)
		} else if diff < 0 {
			costStr = fmt.Sprintf("Monthly savings: $%.2f", diff*-1)
		}
		if infoLine != "" && costStr != "" {
			infoLine += "    "
		}
		infoLine += costStr
	}
	if infoLine != "" {
		// Color the projected CPU part
		if rec.ProjectedCPU != nil {
			projColor := colorAmber
			if *rec.ProjectedCPU >= 80 {
				projColor = colorRed
			} else if *rec.ProjectedCPU < 50 {
				projColor = colorGreen
			}
			projText := fmt.Sprintf("Projected CPU: %.1f%%", *rec.ProjectedCPU)
			setFont(dc, fontBold, fontSizeBody, projColor)
			dc.DrawString(projText, x, y+fontSizeBody)
			pw, _ := dc.MeasureString(projText + "    ")
			x += pw
		}
		// Cost
		if rec.MonthlyApproximatePriceDiff != nil {
			diff := *rec.MonthlyApproximatePriceDiff
			costColor := colorGreen
			costText := ""
			if diff > 0 {
				costColor = colorRed
				costText = fmt.Sprintf("Monthly cost increase: +$%.2f/mo (+$%.2f/yr)", diff, diff*12)
			} else if diff < 0 {
				costText = fmt.Sprintf("Monthly savings: $%.2f/mo ($%.2f/yr)", diff*-1, diff*-12)
			}
			if costText != "" {
				setFont(dc, fontBold, fontSizeBody, costColor)
				dc.DrawString(costText, x, y+fontSizeBody)
			}
		}
		y += lineHeight
	}

	// Connections warning
	if rec.MaxConnectionsAdjustRequired {
		setFont(dc, fontRegular, fontSizeSmall, colorAmber)
		connText := "Requires max_connections adjustment"
		if rec.PeakConnections != nil {
			connText += fmt.Sprintf(" (peak: %.0f", *rec.PeakConnections)
			if rec.TargetInstanceProperties != nil && rec.TargetInstanceProperties.MaxConnections != nil {
				connText += fmt.Sprintf(", target default: %d", *rec.TargetInstanceProperties.MaxConnections)
			}
			connText += ")"
		}
		dc.DrawString(connText, marginX, y+fontSizeSmall)
		y += lineHeight
	}

	// Cluster equalization note
	if rec.ClusterEqualized {
		setFont(dc, fontRegular, fontSizeSmall, textLight)
		dc.DrawString("Adjusted for cluster homogeneity", marginX, y+fontSizeSmall)
		y += lineHeight
	}

	return y + sectionGap/2
}

// drawComparison draws the current vs target instance comparison cards side by side.
// Returns the Y position after the cards.
func drawComparison(dc *gg.Context, rec *types.Recommendation, region string, y float64) float64 {
	if rec.Recommendation == types.Terminate || rec.CurrentInstanceProperties == nil || rec.TargetInstanceProperties == nil {
		return y
	}

	current := rec.CurrentInstanceProperties
	target := rec.TargetInstanceProperties
	cw := contentWidth()

	cardW := (cw - cardGap - 60) / 2 // 60 for arrow area
	arrowW := 60.0

	currentName := ""
	if rec.DBInstanceClass != nil {
		currentName = *rec.DBInstanceClass
	}
	targetName := ""
	if rec.RecommendedInstanceType != nil {
		targetName = *rec.RecommendedInstanceType
	}

	currentPrice := current.GetPrice(region)
	targetPrice := target.GetPrice(region)

	// Build card content
	type cardRow struct {
		label string
		value string
	}

	currentRows := []cardRow{
		{"vCPU", fmt.Sprintf("%d", current.Vcpu)},
		{"Memory", fmt.Sprintf("%d GB", current.Mem)},
	}
	targetRows := []cardRow{
		{"vCPU", fmt.Sprintf("%d", target.Vcpu)},
		{"Memory", fmt.Sprintf("%d GB", target.Mem)},
	}

	if current.MaxBandwidth != nil {
		currentRows = append(currentRows, cardRow{"Max BW", fmt.Sprintf("%d Mbps", *current.MaxBandwidth)})
	}
	if target.MaxBandwidth != nil {
		targetRows = append(targetRows, cardRow{"Max BW", fmt.Sprintf("%d Mbps", *target.MaxBandwidth)})
	}
	if current.MaxConnections != nil {
		currentRows = append(currentRows, cardRow{"Max Conns", fmt.Sprintf("%d", *current.MaxConnections)})
	}
	if target.MaxConnections != nil {
		targetRows = append(targetRows, cardRow{"Max Conns", fmt.Sprintf("%d", *target.MaxConnections)})
	}
	currentRows = append(currentRows,
		cardRow{"Price/hr", fmt.Sprintf("$%.4f", currentPrice)},
		cardRow{"Price/mo", fmt.Sprintf("$%.2f", currentPrice*730)},
	)
	targetRows = append(targetRows,
		cardRow{"Price/hr", fmt.Sprintf("$%.4f", targetPrice)},
		cardRow{"Price/mo", fmt.Sprintf("$%.2f", targetPrice*730)},
	)

	maxRows := len(currentRows)
	if len(targetRows) > maxRows {
		maxRows = len(targetRows)
	}

	cardH := cardPadding*2 + lineHeight + float64(maxRows)*lineHeight + 4 // title + rows

	// Current card (left)
	leftX := marginX
	drawRoundedCard(dc, leftX, y, cardW, cardH, bgLight, borderLight)

	// Card title
	setFont(dc, fontBold, fontSizeCard, textDark)
	dc.DrawString("Current: "+currentName, leftX+cardPadding, y+cardPadding+fontSizeCard)
	cardY := y + cardPadding + lineHeight + 4

	// Card rows
	for _, row := range currentRows {
		setFont(dc, fontMono, fontSizeSmall, textMedium)
		dc.DrawString(fmt.Sprintf("%-12s%s", row.label, row.value), leftX+cardPadding, cardY+fontSizeSmall)
		cardY += lineHeight
	}

	// Arrow
	arrowX := leftX + cardW + arrowW/2
	arrowY := y + cardH/2
	setFont(dc, fontBold, fontSizeTitle, colorPurple)
	dc.DrawStringAnchored("-->", arrowX, arrowY, 0.5, 0.5)

	// Target card (right)
	rightX := leftX + cardW + arrowW
	borderClr := colorGreen
	if rec.Recommendation == types.UpScale {
		borderClr = colorRed
	}
	drawRoundedCard(dc, rightX, y, cardW, cardH, bgLight, borderClr)

	setFont(dc, fontBold, fontSizeCard, textDark)
	dc.DrawString("Target: "+targetName, rightX+cardPadding, y+cardPadding+fontSizeCard)
	cardY = y + cardPadding + lineHeight + 4

	for _, row := range targetRows {
		setFont(dc, fontMono, fontSizeSmall, textMedium)
		dc.DrawString(fmt.Sprintf("%-12s%s", row.label, row.value), rightX+cardPadding, cardY+fontSizeSmall)
		cardY += lineHeight
	}

	return y + cardH + sectionGap
}

// drawChartImage draws a chart image onto the canvas at the given position.
// Returns the Y position after the chart.
func drawChartImage(dc *gg.Context, img image.Image, y float64) float64 {
	if img == nil {
		return y
	}
	bounds := img.Bounds()
	// Center the chart horizontally
	x := (imgWidth - bounds.Dx()) / 2
	dc.DrawImage(img, x, int(y))
	return y + float64(bounds.Dy()) + sectionGap/2
}

// drawSeparator draws a horizontal line separator.
func drawSeparator(dc *gg.Context, y float64) float64 {
	dc.SetColor(borderLight)
	dc.SetLineWidth(1)
	dc.DrawLine(marginX, y, float64(imgWidth)-marginX, y)
	dc.Stroke()
	return y + sectionGap
}

// drawClusterHeader draws a cluster-level header for cluster exports.
func drawClusterHeader(dc *gg.Context, clusterID string, memberCount int, engine string, y float64) float64 {
	x := marginX

	setFont(dc, fontBold, fontSizeTitle+4, colorPurple)
	dc.DrawString("Cluster: "+clusterID, x, y+fontSizeTitle+4)
	y += fontSizeTitle + 4 + 8

	setFont(dc, fontRegular, fontSizeSubtitle, textMedium)
	subtitle := fmt.Sprintf("%s  |  %d member(s)", engine, memberCount)
	dc.DrawString(subtitle, x, y+fontSizeSubtitle)
	y += fontSizeSubtitle + 4

	return y + sectionGap
}

// estimateInstanceHeight estimates the pixel height needed to render one instance's details.
func estimateInstanceHeight(rec *types.Recommendation) float64 {
	h := 0.0
	// Header
	h += fontSizeTitle + badgePadY*2 + 6 + fontSizeSubtitle + 4 + sectionGap/2
	// Recommendation info (reason + cost = ~2 lines, plus optional warnings)
	h += lineHeight * 2
	if rec.MaxConnectionsAdjustRequired {
		h += lineHeight
	}
	if rec.ClusterEqualized {
		h += lineHeight
	}
	h += sectionGap / 2
	// Comparison cards
	if rec.Recommendation != types.Terminate && rec.CurrentInstanceProperties != nil && rec.TargetInstanceProperties != nil {
		rows := 6 // vCPU, mem, price/hr, price/mo + possible BW + conns
		if rec.CurrentInstanceProperties.MaxBandwidth != nil {
			rows++
		}
		if rec.CurrentInstanceProperties.MaxConnections != nil {
			rows++
		}
		h += cardPadding*2 + lineHeight + float64(rows)*lineHeight + 4 + sectionGap
	}
	// Charts (up to 4 charts)
	chartCount := 0
	if rec.TimeSeriesMetrics != nil {
		metrics := rec.TimeSeriesMetrics.Metrics
		if m, ok := metrics["CPUUtilization"]; ok && len(m.DataPoints) > 1 {
			chartCount++
		}
		if m, ok := metrics["FreeableMemory"]; ok && len(m.DataPoints) > 1 {
			chartCount++
		}
		if m, ok := metrics["DatabaseConnections"]; ok && len(m.DataPoints) > 1 {
			chartCount++
		}
		rm, hasR := metrics["ReadThroughput"]
		wm, hasW := metrics["WriteThroughput"]
		if hasR && hasW && len(rm.DataPoints) > 1 && len(wm.DataPoints) > 1 {
			chartCount++
		}
	}
	h += float64(chartCount) * (float64(chartHeight) + sectionGap/2)
	h += sectionGap // bottom margin
	return h
}
