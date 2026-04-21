package export

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/fogleman/gg"
	cwTypes "github.com/luneo7/go-rds-right-size/internal/cw/types"
	"github.com/luneo7/go-rds-right-size/internal/rds-right-size/types"
)

// ExportInstancePNG renders a single instance recommendation to a PNG file.
// Returns the absolute path of the output file.
func ExportInstancePNG(rec *types.Recommendation, region string, outputDir string) (string, error) {
	// Estimate height
	totalHeight := marginY + estimateInstanceHeight(rec) + marginY

	dc := gg.NewContext(imgWidth, int(totalHeight))
	dc.SetColor(bgWhite)
	dc.Clear()

	y := marginY

	// Render all sections
	y = drawHeader(dc, rec, y)
	y = drawRecommendationInfo(dc, rec, region, y)
	y = drawComparison(dc, rec, region, y)

	// Render charts
	y = renderAndDrawCharts(dc, rec, y)

	// Determine output filename
	instanceID := "instance"
	if rec.DBInstanceIdentifier != nil {
		instanceID = sanitizeFilename(*rec.DBInstanceIdentifier)
	}
	filename := filepath.Join(outputDir, instanceID+".png")

	if err := dc.SavePNG(filename); err != nil {
		return "", fmt.Errorf("failed to save PNG: %w", err)
	}

	return filepath.Abs(filename)
}

// ExportClusterPNG renders all instances in a cluster to a single PNG file.
// Returns the absolute path of the output file.
func ExportClusterPNG(recs []types.Recommendation, clusterID string, region string, outputDir string) (string, error) {
	if len(recs) == 0 {
		return "", fmt.Errorf("no recommendations to export")
	}

	// Determine engine from first rec
	engine := ""
	if recs[0].Engine != nil {
		engine = *recs[0].Engine
	}

	// Estimate total height: cluster header + all instances + separators
	totalHeight := marginY * 2 // top/bottom
	totalHeight += fontSizeTitle + 4 + 8 + fontSizeSubtitle + 4 + sectionGap // cluster header
	for i, rec := range recs {
		totalHeight += estimateInstanceHeight(&rec)
		if i < len(recs)-1 {
			totalHeight += sectionGap // separator
		}
	}

	dc := gg.NewContext(imgWidth, int(totalHeight))
	dc.SetColor(bgWhite)
	dc.Clear()

	y := marginY

	// Cluster header
	y = drawClusterHeader(dc, clusterID, len(recs), engine, y)
	y = drawSeparator(dc, y)

	// Render each instance
	for i := range recs {
		rec := &recs[i]
		y = drawHeader(dc, rec, y)
		y = drawRecommendationInfo(dc, rec, region, y)
		y = drawComparison(dc, rec, region, y)
		y = renderAndDrawCharts(dc, rec, y)

		if i < len(recs)-1 {
			y = drawSeparator(dc, y)
		}
	}

	filename := filepath.Join(outputDir, sanitizeFilename(clusterID)+".png")

	if err := dc.SavePNG(filename); err != nil {
		return "", fmt.Errorf("failed to save PNG: %w", err)
	}

	return filepath.Abs(filename)
}

// renderAndDrawCharts generates chart images and draws them onto the canvas.
// Returns the Y position after all charts.
func renderAndDrawCharts(dc *gg.Context, rec *types.Recommendation, y float64) float64 {
	if rec.TimeSeriesMetrics == nil {
		return y
	}
	metrics := rec.TimeSeriesMetrics.Metrics

	// CPU Utilization chart
	if metric, ok := metrics[cwTypes.CPUUtilization]; ok && len(metric.DataPoints) > 1 {
		var projectedValues []float64

		// Generate projected CPU values when scaling with different vCPU counts
		canProject := rec.Recommendation != types.Terminate &&
			rec.CurrentInstanceProperties != nil &&
			rec.TargetInstanceProperties != nil &&
			rec.CurrentInstanceProperties.Vcpu != rec.TargetInstanceProperties.Vcpu

		if canProject {
			vcpuRatio := float64(rec.CurrentInstanceProperties.Vcpu) / float64(rec.TargetInstanceProperties.Vcpu)
			projectedValues = make([]float64, len(metric.DataPoints))
			for i, dp := range metric.DataPoints {
				pv := dp.Value * vcpuRatio
				if pv > 100 {
					pv = 100
				}
				projectedValues[i] = pv
			}
		}

		if chartImg, err := RenderCPUChart(metric, projectedValues); err == nil {
			y = drawChartImage(dc, chartImg, y)
		}
	}

	// Freeable Memory chart
	if metric, ok := metrics[cwTypes.FreeableMemory]; ok && len(metric.DataPoints) > 1 {
		if chartImg, err := RenderMemoryChart(metric); err == nil {
			y = drawChartImage(dc, chartImg, y)
		}
	}

	// Database Connections chart
	if metric, ok := metrics[cwTypes.DatabaseConnections]; ok && len(metric.DataPoints) > 1 {
		if chartImg, err := RenderConnectionsChart(metric); err == nil {
			y = drawChartImage(dc, chartImg, y)
		}
	}

	// Throughput chart (combined read+write)
	readMetric, hasRead := metrics[cwTypes.ReadThroughput]
	writeMetric, hasWrite := metrics[cwTypes.WriteThroughput]
	if hasRead && hasWrite && len(readMetric.DataPoints) > 1 && len(writeMetric.DataPoints) > 1 {
		if chartImg, err := RenderThroughputChart(readMetric, writeMetric); err == nil {
			y = drawChartImage(dc, chartImg, y)
		}
	}

	return y
}

// sanitizeFilename replaces characters that are invalid in filenames.
func sanitizeFilename(name string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		" ", "_",
	)
	return replacer.Replace(name)
}

// RegionFromAZ extracts the region from an availability zone string.
// e.g., "us-east-1a" -> "us-east-1"
func RegionFromAZ(az *string) string {
	if az == nil || len(*az) == 0 {
		return ""
	}
	s := *az
	if len(s) > 0 && s[len(s)-1] >= 'a' && s[len(s)-1] <= 'z' {
		return s[:len(s)-1]
	}
	return s
}
