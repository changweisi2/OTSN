// Package ui renders otsn's minimal terminal output: flat colors,
// aligned tables, and human-readable sizes. Output degrades gracefully
// when not attached to a terminal or when NO_COLOR is set.
package ui

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Color is whether ANSI colors should be emitted.
var Color = os.Getenv("NO_COLOR") == "" && isTTY(os.Stdout)

// TTY is whether stdout is a terminal (drives progress rendering).
var TTY = isTTY(os.Stdout)

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func init() { enableVT() }

func paint(code, s string) string {
	if !Color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// Bold returns s in bold.
func Bold(s string) string { return paint("1", s) }

// Hi returns s in bright white.
func Hi(s string) string { return paint("97", s) }

// Cyan returns s in cyan.
func Cyan(s string) string { return paint("36", s) }

// Dim returns s in dim.
func Dim(s string) string { return paint("2", s) }

// Red returns s in red.
func Red(s string) string { return paint("31", s) }

// Yellow returns s in yellow.
func Yellow(s string) string { return paint("33", s) }

// Green returns s in green.
func Green(s string) string { return paint("32", s) }

// Title renders a section heading, e.g. "◈ scan complete".
func Title(s string) string { return Cyan("◈") + " " + Bold(s) }

// Abbrev shortens an absolute path by replacing the home directory with ~.
func Abbrev(p string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(os.PathSeparator)) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// FmtBytes renders a byte count as 1.2 GB, 340 MB, 12 KB, 5 B.
func FmtBytes(n int64) string {
	if n < 0 {
		return "-" + FmtBytes(-n)
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// FmtInt renders an integer with thousands separators.
func FmtInt(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		return "-" + s
	}
	return s
}

// Bar renders a horizontal bar of width cells for a fraction in [0,1].
func Bar(frac float64, width int) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	full := int(frac * float64(width))
	var b strings.Builder
	for i := 0; i < full; i++ {
		b.WriteString("█")
	}
	start := full
	if full < width {
		idx := int((frac*float64(width) - float64(full)) * 8)
		if idx > 0 {
			b.WriteRune(barPartial[idx-1])
			start = full + 1
		}
	}
	for i := start; i < width; i++ {
		b.WriteString("·")
	}
	return b.String()
}

// barPartial is the eighth-block glyph ladder, as runes (byte indexing a
// UTF-8 string literal would slice mid-rune).
var barPartial = []rune("▏▎▍▌▋▊▉")

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// runeWidth approximates terminal display width: wide CJK and emoji
// glyphs occupy two columns, everything else one.
func runeWidth(r rune) int {
	switch {
	case r >= 0x1100 && r <= 0x115F, // Hangul Jamo
		r == 0x2329, r == 0x232A, // angle brackets
		r >= 0x2E80 && r <= 0xA4CF && r != 0x303F, // CJK radicals .. Yi
		r >= 0xAC00 && r <= 0xD7A3,                // Hangul syllables
		r >= 0xF900 && r <= 0xFAFF,                // CJK compat ideographs
		r >= 0xFE10 && r <= 0xFE19,                // vertical forms
		r >= 0xFE30 && r <= 0xFE6F,                // CJK compat forms
		r >= 0xFF00 && r <= 0xFF60,                // fullwidth forms
		r >= 0xFFE0 && r <= 0xFFE6,                // fullwidth signs
		r >= 0x1F300 && r <= 0x1FAFF,              // emoji
		r >= 0x20000 && r <= 0x3FFFD:              // CJK extension
		return 2
	}
	return 1
}

func visibleLen(s string) int {
	n := 0
	for _, r := range ansiRe.ReplaceAllString(s, "") {
		n += runeWidth(r)
	}
	return n
}

func pad(s string, width int, right bool) string {
	n := width - visibleLen(s)
	if n < 0 {
		n = 0
	}
	if right {
		return strings.Repeat(" ", n) + s
	}
	return s + strings.Repeat(" ", n)
}

// Table renders an aligned box table; columns listed in right are right
// aligned. ANSI-colored cells are measured without their escape codes.
func Table(headers []string, rows [][]string, right map[int]bool) string {
	if len(headers) == 0 {
		return ""
	}
	w := make([]int, len(headers))
	for i, h := range headers {
		w[i] = visibleLen(h)
	}
	for _, r := range rows {
		for i, c := range r {
			if n := visibleLen(c); n > w[i] {
				w[i] = n
			}
		}
	}
	var b strings.Builder
	rule := func(left, mid, end string) {
		b.WriteString(left)
		for i, wd := range w {
			b.WriteString(strings.Repeat("─", wd+2))
			if i < len(w)-1 {
				b.WriteString(mid)
			}
		}
		b.WriteString(end + "\n")
	}
	rule("┌", "┬", "┐")
	b.WriteString("│")
	for i, h := range headers {
		b.WriteString(" " + pad(h, w[i], right[i]) + " │")
	}
	b.WriteString("\n")
	rule("├", "┼", "┤")
	for _, r := range rows {
		b.WriteString("│")
		for i, c := range r {
			b.WriteString(" " + pad(c, w[i], right[i]) + " │")
		}
		b.WriteString("\n")
	}
	rule("└", "┴", "┘")
	return b.String()
}

// Progress renders a single-line scan progress update on stderr.
func Progress(done int64) {
	if !TTY {
		return
	}
	fmt.Fprintf(os.Stderr, "\r\x1b[2K▸ scanning · %s entries", FmtInt(done))
}

// Done clears the progress line.
func Done() {
	if TTY {
		fmt.Fprint(os.Stderr, "\r\x1b[2K")
	}
}

// Warnf prints a yellow warning to stderr.
func Warnf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, paint("33", "otsn: "+format+"\n"), a...)
}
