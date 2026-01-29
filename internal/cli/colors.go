// Package cli provides shared CLI output utilities for Athena commands.
package cli

import (
	"os"

	"golang.org/x/term"
)

// Color and style ANSI codes
const (
	// Styles
	Reset     = "\033[0m"
	Bold      = "\033[1m"
	Dim       = "\033[2m"
	Italic    = "\033[3m"
	Underline = "\033[4m"

	// Standard colors (foreground)
	Black   = "\033[30m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[37m"

	// Bright/light colors (foreground)
	BrightBlack   = "\033[90m" // Gray
	BrightRed     = "\033[91m"
	BrightGreen   = "\033[92m"
	BrightYellow  = "\033[93m"
	BrightBlue    = "\033[94m"
	BrightMagenta = "\033[95m"
	BrightCyan    = "\033[96m"
	BrightWhite   = "\033[97m"

	// Aliases for readability
	Gray = BrightBlack
)

// colorsEnabled caches whether colors should be used
var colorsEnabled *bool

// ColorsEnabled returns true if the terminal supports colors.
// Checks if stdout is a terminal and NO_COLOR env var is not set.
func ColorsEnabled() bool {
	if colorsEnabled != nil {
		return *colorsEnabled
	}

	enabled := term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("NO_COLOR") == ""
	colorsEnabled = &enabled
	return enabled
}

// ForceColors enables or disables colors regardless of terminal detection.
func ForceColors(enabled bool) {
	colorsEnabled = &enabled
}

// Style applies a style/color only if colors are enabled.
func Style(code string) string {
	if ColorsEnabled() {
		return code
	}
	return ""
}

// Styled wraps text with a style code and reset.
func Styled(text, code string) string {
	if !ColorsEnabled() {
		return text
	}
	return code + text + Reset
}

// Convenience functions for common styles

func Bolden(text string) string {
	return Styled(text, Bold)
}

func Dimmed(text string) string {
	return Styled(text, Dim)
}

func Italicize(text string) string {
	return Styled(text, Italic)
}

// Color functions

func RedText(text string) string {
	return Styled(text, Red)
}

func GreenText(text string) string {
	return Styled(text, Green)
}

func YellowText(text string) string {
	return Styled(text, Yellow)
}

func BlueText(text string) string {
	return Styled(text, Blue)
}

func MagentaText(text string) string {
	return Styled(text, Magenta)
}

func CyanText(text string) string {
	return Styled(text, Cyan)
}

func WhiteText(text string) string {
	return Styled(text, White)
}

func GrayText(text string) string {
	return Styled(text, Gray)
}

// Combined styles

func BoldWhite(text string) string {
	return Styled(text, Bold+White)
}

func BoldCyan(text string) string {
	return Styled(text, Bold+Cyan)
}

func BoldYellow(text string) string {
	return Styled(text, Bold+Yellow)
}

func BoldGreen(text string) string {
	return Styled(text, Bold+Green)
}

func BoldRed(text string) string {
	return Styled(text, Bold+Red)
}

func BoldBlue(text string) string {
	return Styled(text, Bold+Blue)
}

func BoldMagenta(text string) string {
	return Styled(text, Bold+Magenta)
}
