package export

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"time"

	chart "github.com/wcharczuk/go-chart/v2"
	cwTypes "github.com/luneo7/rds-right-size/internal/cw/types"
)

const (
	chartWidth  = 1100
	chartHeight = 250
)

// baseChartStyle returns common chart styling for the light theme.
func baseChartStyle() chart.Style {
	return chart.Style{
		FillColor:   chartBg,
		FontColor:   chartText,
		StrokeColor: chartGrid,
	}
}

// renderChartToImage renders a go-chart Chart to an image.Image.
func renderChartToImage(c chart.Chart) (image.Image, error) {
	var buf bytes.Buffer
	if err := c.Render(chart.PNG, &buf); err != nil {
		return nil, err
	}
	return png.Decode(&buf)
}

// formatDateRange returns a caption string for the chart date range.
func formatChartDateRange(dataPoints []cwTypes.TimeSeriesDataPoint) string {
	if len(dataPoints) == 0 {
		return ""
	}
	start := dataPoints[0].Timestamp.Format("Jan 02")
	end := dataPoints[len(dataPoints)-1].Timestamp.Format("Jan 02")
	return fmt.Sprintf("%s – %s (%d days)", start, end, len(dataPoints))
}

// RenderCPUChart renders a CPU utilization chart, optionally with a projected overlay.
// projectedValues may be nil if no projection is needed.
func RenderCPUChart(metric cwTypes.TimeSeriesMetric, projectedValues []float64) (image.Image, error) {
	if len(metric.DataPoints) < 2 {
		return nil, fmt.Errorf("insufficient data points for CPU chart")
	}

	times := make([]time.Time, len(metric.DataPoints))
	values := make([]float64, len(metric.DataPoints))
	for i, dp := range metric.DataPoints {
		times[i] = dp.Timestamp
		values[i] = dp.Value
	}

	series := []chart.Series{
		chart.TimeSeries{
			Name:    "Actual CPU",
			XValues: times,
			YValues: values,
			Style: chart.Style{
				StrokeColor: chartRed,
				StrokeWidth: 2,
			},
		},
	}

	if projectedValues != nil && len(projectedValues) == len(times) {
		series = append(series, chart.TimeSeries{
			Name:    "Projected CPU",
			XValues: times,
			YValues: projectedValues,
			Style: chart.Style{
				StrokeColor:     chartYellow,
				StrokeWidth:     2,
				StrokeDashArray: []float64{5, 3},
			},
		})
	}

	graph := chart.Chart{
		Title:  "CPU Utilization (%)",
		Width:  chartWidth,
		Height: chartHeight,
		TitleStyle: chart.Style{
			FontColor: chartText,
			FontSize:  14,
		},
		Background: baseChartStyle(),
		Canvas:     baseChartStyle(),
		XAxis: chart.XAxis{
			Style: chart.Style{
				FontColor: chartText,
				FontSize:  11,
			},
			ValueFormatter: chart.TimeValueFormatterWithFormat("Jan 02"),
		},
		YAxis: chart.YAxis{
			Style: chart.Style{
				FontColor: chartText,
				FontSize:  11,
			},
			Range: &chart.ContinuousRange{
				Min: 0,
				Max: 100,
			},
			ValueFormatter: func(v interface{}) string {
				return fmt.Sprintf("%.0f%%", v.(float64))
			},
		},
		Series: series,
	}

	if projectedValues != nil {
		graph.Elements = []chart.Renderable{chart.LegendThin(&graph)}
	}

	return renderChartToImage(graph)
}

// RenderMemoryChart renders a freeable memory chart (values in GB).
func RenderMemoryChart(metric cwTypes.TimeSeriesMetric) (image.Image, error) {
	if len(metric.DataPoints) < 2 {
		return nil, fmt.Errorf("insufficient data points for memory chart")
	}

	times := make([]time.Time, len(metric.DataPoints))
	values := make([]float64, len(metric.DataPoints))
	for i, dp := range metric.DataPoints {
		times[i] = dp.Timestamp
		values[i] = dp.Value / (1 << 30) // bytes to GB
	}

	graph := chart.Chart{
		Title:  "Freeable Memory (GB)",
		Width:  chartWidth,
		Height: chartHeight,
		TitleStyle: chart.Style{
			FontColor: chartText,
			FontSize:  14,
		},
		Background: baseChartStyle(),
		Canvas:     baseChartStyle(),
		XAxis: chart.XAxis{
			Style: chart.Style{
				FontColor: chartText,
				FontSize:  11,
			},
			ValueFormatter: chart.TimeValueFormatterWithFormat("Jan 02"),
		},
		YAxis: chart.YAxis{
			Style: chart.Style{
				FontColor: chartText,
				FontSize:  11,
			},
			ValueFormatter: func(v interface{}) string {
				return fmt.Sprintf("%.1f", v.(float64))
			},
		},
		Series: []chart.Series{
			chart.TimeSeries{
				Name:    "Freeable Memory",
				XValues: times,
				YValues: values,
				Style: chart.Style{
					StrokeColor: chartBlue,
					StrokeWidth: 2,
				},
			},
		},
	}

	return renderChartToImage(graph)
}

// RenderConnectionsChart renders a database connections chart.
func RenderConnectionsChart(metric cwTypes.TimeSeriesMetric) (image.Image, error) {
	if len(metric.DataPoints) < 2 {
		return nil, fmt.Errorf("insufficient data points for connections chart")
	}

	times := make([]time.Time, len(metric.DataPoints))
	values := make([]float64, len(metric.DataPoints))
	for i, dp := range metric.DataPoints {
		times[i] = dp.Timestamp
		values[i] = dp.Value
	}

	graph := chart.Chart{
		Title:  "Database Connections",
		Width:  chartWidth,
		Height: chartHeight,
		TitleStyle: chart.Style{
			FontColor: chartText,
			FontSize:  14,
		},
		Background: baseChartStyle(),
		Canvas:     baseChartStyle(),
		XAxis: chart.XAxis{
			Style: chart.Style{
				FontColor: chartText,
				FontSize:  11,
			},
			ValueFormatter: chart.TimeValueFormatterWithFormat("Jan 02"),
		},
		YAxis: chart.YAxis{
			Style: chart.Style{
				FontColor: chartText,
				FontSize:  11,
			},
			ValueFormatter: func(v interface{}) string {
				return fmt.Sprintf("%.0f", v.(float64))
			},
		},
		Series: []chart.Series{
			chart.TimeSeries{
				Name:    "Connections",
				XValues: times,
				YValues: values,
				Style: chart.Style{
					StrokeColor: chartGreen,
					StrokeWidth: 2,
				},
			},
		},
	}

	return renderChartToImage(graph)
}

// RenderThroughputChart renders a combined read+write throughput chart (KB/s).
func RenderThroughputChart(readMetric, writeMetric cwTypes.TimeSeriesMetric) (image.Image, error) {
	if len(readMetric.DataPoints) < 2 || len(writeMetric.DataPoints) < 2 {
		return nil, fmt.Errorf("insufficient data points for throughput chart")
	}

	minLen := len(readMetric.DataPoints)
	if len(writeMetric.DataPoints) < minLen {
		minLen = len(writeMetric.DataPoints)
	}

	times := make([]time.Time, minLen)
	values := make([]float64, minLen)
	for i := 0; i < minLen; i++ {
		times[i] = readMetric.DataPoints[i].Timestamp
		values[i] = (readMetric.DataPoints[i].Value + writeMetric.DataPoints[i].Value) / 1024 // bytes/s to KB/s
	}

	graph := chart.Chart{
		Title:  "Total Throughput (KB/s)",
		Width:  chartWidth,
		Height: chartHeight,
		TitleStyle: chart.Style{
			FontColor: chartText,
			FontSize:  14,
		},
		Background: baseChartStyle(),
		Canvas:     baseChartStyle(),
		XAxis: chart.XAxis{
			Style: chart.Style{
				FontColor: chartText,
				FontSize:  11,
			},
			ValueFormatter: chart.TimeValueFormatterWithFormat("Jan 02"),
		},
		YAxis: chart.YAxis{
			Style: chart.Style{
				FontColor: chartText,
				FontSize:  11,
			},
			ValueFormatter: func(v interface{}) string {
				return fmt.Sprintf("%.1f", v.(float64))
			},
		},
		Series: []chart.Series{
			chart.TimeSeries{
				Name:    "Throughput",
				XValues: times,
				YValues: values,
				Style: chart.Style{
					StrokeColor: chartYellow,
					StrokeWidth: 2,
				},
			},
		},
	}

	return renderChartToImage(graph)
}
