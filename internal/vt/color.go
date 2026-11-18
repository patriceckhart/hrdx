package vt

import "sync"

// ANSI color values
const (
	Black Color = iota
	Red
	Green
	Yellow
	Blue
	Magenta
	Cyan
	LightGrey
	DarkGrey
	LightRed
	LightGreen
	LightYellow
	LightBlue
	LightMagenta
	LightCyan
	White
)

// Default colors are potentially distinct to allow for special behavior.
// For example, a transparent background. Otherwise, the simple case is to
// map default colors to another color.
const (
	DefaultFG Color = 1<<24 + iota
	DefaultBG
	DefaultCursor
)

// Color maps to the ANSI colors [0, 16) and the xterm colors [16, 256).
type Color uint32

// ANSI returns true if Color is within [0, 16).
func (c Color) ANSI() bool {
	return (c < 16)
}

// RGB is a 24-bit color, 0xRRGGBB.
type RGB uint32

// Palette holds the real colors a frontend draws with. The emulator uses it
// only to answer color queries (OSC 4, 10, 11): programs such as GitHub
// Copilot CLI derive their whole color scheme from those answers. Rendering
// keeps using default and indexed colors, so the frontend stays in charge.
type Palette struct {
	FG, BG RGB
	ANSI   [16]RGB
}

// DefaultPalette matches xterm.js defaults on a dark background. It is a
// safe answer when the real colors are unknown.
var DefaultPalette = Palette{
	FG: 0xe4e4e7,
	BG: 0x18181b,
	ANSI: [16]RGB{
		0x2e3436, 0xcc0000, 0x4e9a06, 0xc4a000, 0x3465a4, 0x75507b, 0x06989a, 0xd3d7cf,
		0x555753, 0xef2929, 0x8ae234, 0xfce94f, 0x729fcf, 0xad7fa8, 0x34e2e2, 0xeeeeec,
	},
}

var reportPalette struct {
	sync.RWMutex
	p Palette
}

func init() { reportPalette.p = DefaultPalette }

// SetReportPalette sets the colors every emulator reports from now on. Call
// it at startup once the frontend knows its colors.
func SetReportPalette(p Palette) {
	reportPalette.Lock()
	reportPalette.p = p
	reportPalette.Unlock()
}

// ReportPalette returns the colors emulators report.
func ReportPalette() Palette {
	reportPalette.RLock()
	defer reportPalette.RUnlock()
	return reportPalette.p
}

// rgbOf resolves an indexed or default color to the RGB value it is
// displayed as: the palette for defaults and ANSI 0 to 15, the xterm cube
// and gray ramp for 16 to 255, and direct colors as they are.
func rgbOf(c Color, p Palette) RGB {
	switch {
	case c == DefaultFG || c == DefaultCursor:
		return p.FG
	case c == DefaultBG:
		return p.BG
	case c < 16:
		return p.ANSI[c]
	case c < 232:
		levels := [6]RGB{0x00, 0x5f, 0x87, 0xaf, 0xd7, 0xff}
		n := c - 16
		return levels[n/36]<<16 | levels[n/6%6]<<8 | levels[n%6]
	case c < 256:
		gray := RGB(8 + 10*(c-232))
		return gray<<16 | gray<<8 | gray
	}
	return RGB(c & 0xffffff)
}
