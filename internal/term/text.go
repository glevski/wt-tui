package term

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const esc = 0x1b

// Reset is the SGR sequence that clears every attribute.
const Reset = "\x1b[0m"

// scanEscape returns the length of the escape sequence starting at s[i]
// (which is ESC) and whether it is an SGR color sequence — the only kind
// worth keeping; cursor movement, erases and OSC titles are dropped.
func scanEscape(s string, i int) (n int, sgr bool) {
	if i+1 >= len(s) {
		return 1, false
	}
	switch s[i+1] {
	case '[':
		j := i + 2
		for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
			j++
		}
		if j >= len(s) {
			return len(s) - i, false
		}
		return j + 1 - i, s[j] == 'm'
	case ']':
		for j := i + 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1 - i, false
			}
			if s[j] == esc && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2 - i, false
			}
		}
		return len(s) - i, false
	case 'O', '(', ')':
		if i+2 < len(s) {
			return 3, false
		}
		return 2, false
	}
	return 2, false
}

// RuneWidth is the number of terminal columns a rune occupies: 0 for
// combining marks and zero-width code points, 2 for East Asian wide and
// fullwidth characters (and the common emoji blocks), 1 otherwise.
func RuneWidth(r rune) int {
	switch {
	case r == 0:
		return 0
	case r < 0x20 || r == 0x7f:
		return 0
	case r < 0x300:
		return 1
	case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r):
		return 0
	case r == 0x200b || (r >= 0xfe00 && r <= 0xfe0f) || r == 0x2060:
		return 0
	case r >= 0x1100 && r <= 0x115f,
		r >= 0x2e80 && r <= 0x303e,
		r >= 0x3041 && r <= 0x33ff,
		r >= 0x3400 && r <= 0x4dbf,
		r >= 0x4e00 && r <= 0x9fff,
		r >= 0xa000 && r <= 0xa4cf,
		r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff,
		r >= 0xfe30 && r <= 0xfe4f,
		r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6,
		r >= 0x1f300 && r <= 0x1f64f,
		r >= 0x1f680 && r <= 0x1f6ff,
		r >= 0x1f900 && r <= 0x1f9ff,
		r >= 0x20000 && r <= 0x3fffd:
		return 2
	}
	return 1
}

// Width is the number of columns s takes on screen, escape sequences
// excluded.
func Width(s string) int {
	w := 0
	for i := 0; i < len(s); {
		if s[i] == esc {
			n, _ := scanEscape(s, i)
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		w += RuneWidth(r)
		i += size
	}
	return w
}

// Strip removes every escape sequence from s.
func Strip(s string) string {
	if strings.IndexByte(s, esc) < 0 {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == esc {
			n, _ := scanEscape(s, i)
			i += n
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// Cut returns the part of s that falls into the columns [skip, skip+width),
// keeping its color sequences — the horizontal window a pane shows of a
// line. A wide character straddling either edge becomes a space. The
// result ends with a reset whenever it carried colors, so nothing bleeds
// into what is drawn next.
func Cut(s string, skip, width int) string {
	if width <= 0 {
		return ""
	}
	var b strings.Builder
	col, colored := 0, false
	end := skip + width
	for i := 0; i < len(s); {
		if s[i] == esc {
			n, sgr := scanEscape(s, i)
			if sgr {
				b.WriteString(s[i : i+n])
				colored = true
			}
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		rw := RuneWidth(r)
		if rw == 0 {
			// Combining marks ride on the character before them.
			if col > skip && col <= end {
				b.WriteRune(r)
			}
			continue
		}
		if col+rw <= skip {
			col += rw
			continue
		}
		if col >= end {
			break
		}
		if col < skip || col+rw > end {
			b.WriteByte(' ')
		} else {
			b.WriteRune(r)
		}
		col += rw
	}
	if colored {
		b.WriteString(Reset)
	}
	return b.String()
}

// Truncate fits s into width columns, marking a cut with an ellipsis.
func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if Width(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return Cut(s, 0, width-1) + "…"
}

// PadRight pads s with spaces to exactly width columns, cutting it when it
// is longer.
func PadRight(s string, width int) string {
	w := Width(s)
	switch {
	case w == width:
		return s
	case w > width:
		return Cut(s, 0, width)
	}
	return s + strings.Repeat(" ", width-w)
}

// PadLeft right-aligns s in width columns.
func PadLeft(s string, width int) string {
	w := Width(s)
	if w >= width {
		return s
	}
	return strings.Repeat(" ", width-w) + s
}

// Printable makes a line of program output safe to draw: tabs expand to
// 8-column stops, other control characters show as ^X the way less draws
// them, and color sequences pass through untouched.
func Printable(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return s
	}
	var b strings.Builder
	col := 0
	for i := 0; i < len(s); {
		if s[i] == esc {
			n, sgr := scanEscape(s, i)
			if sgr {
				b.WriteString(s[i : i+n])
			}
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == '\t':
			n := 8 - col%8
			b.WriteString(strings.Repeat(" ", n))
			col += n
		case r < 0x20:
			b.WriteByte('^')
			b.WriteByte(byte(r) + 0x40)
			col += 2
		case r == 0x7f:
			b.WriteString("^?")
			col += 2
		default:
			b.WriteRune(r)
			col += RuneWidth(r)
		}
	}
	return b.String()
}
