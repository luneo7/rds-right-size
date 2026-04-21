package export

import (
	"image/color"

	"github.com/wcharczuk/go-chart/v2/drawing"
)

// Light theme color palette for PNG export.
var (
	// Backgrounds
	bgWhite   = color.RGBA{255, 255, 255, 255}
	bgLight   = color.RGBA{248, 250, 252, 255} // Very light gray for cards
	bgSection = color.RGBA{241, 245, 249, 255} // Slightly darker for section headers

	// Text
	textDark   = color.RGBA{15, 23, 42, 255}   // Near-black for primary text
	textMedium = color.RGBA{71, 85, 105, 255}   // Slate-500 for secondary text
	textLight  = color.RGBA{148, 163, 184, 255} // Slate-400 for muted text
	textWhite  = color.RGBA{255, 255, 255, 255}

	// Semantic colors
	colorPurple   = color.RGBA{124, 58, 237, 255} // Primary / branding
	colorGreen    = color.RGBA{16, 185, 129, 255}  // Success / downscale
	colorRed      = color.RGBA{239, 68, 68, 255}   // Danger / upscale
	colorAmber    = color.RGBA{245, 158, 11, 255}   // Warning / terminate
	colorBlue     = color.RGBA{59, 130, 246, 255}   // Info / memory
	colorCyan     = color.RGBA{6, 182, 212, 255}    // Accent
	colorYellow   = color.RGBA{234, 179, 8, 255}    // Projected / throughput

	// Borders
	borderLight = color.RGBA{226, 232, 240, 255} // Slate-200
	borderMedium = color.RGBA{203, 213, 225, 255} // Slate-300

	// Chart-specific drawing colors (for go-chart)
	chartRed    = drawing.Color{R: 239, G: 68, B: 68, A: 255}
	chartYellow = drawing.Color{R: 234, G: 179, B: 8, A: 255}
	chartBlue   = drawing.Color{R: 59, G: 130, B: 246, A: 255}
	chartGreen  = drawing.Color{R: 16, G: 185, B: 129, A: 255}
	chartGrid   = drawing.Color{R: 226, G: 232, B: 240, A: 255}
	chartText   = drawing.Color{R: 71, G: 85, B: 105, A: 255}
	chartBg     = drawing.Color{R: 255, G: 255, B: 255, A: 255}
)
