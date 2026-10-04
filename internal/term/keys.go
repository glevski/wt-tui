package term

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// KeyKind names a key press; printable characters are KeyRune, control
// combinations KeyCtrl with the letter in Rune.
type KeyKind int

const (
	KeyRune KeyKind = iota
	KeyCtrl
	KeyEnter
	KeyCtrlEnter // needs a terminal that tells Ctrl+Enter from Enter (kitty protocol, xterm modifyOtherKeys)
	KeyEsc
	KeyTab
	KeyBackTab
	KeyBackspace
	KeyDelete
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPgUp
	KeyPgDn
	KeyF5
	KeyKittyReply // the terminal's answer to the keyboard-protocol query: it supports it
	KeyUnknown
)

// Key is one decoded key press.
type Key struct {
	Kind KeyKind
	Rune rune // the character for KeyRune, the lowercase letter for KeyCtrl
}

// Ctrl is the KeyCtrl press of letter c.
func Ctrl(c rune) Key { return Key{Kind: KeyCtrl, Rune: c} }

// Char is the KeyRune press of r.
func Char(r rune) Key { return Key{Kind: KeyRune, Rune: r} }

// ParseKeys decodes a chunk of terminal input. One read can carry several
// keys (a fast typist, a paste), so the whole chunk is parsed. A lone
// trailing ESC is the Esc key; escape sequences the UI has no use for are
// skipped whole.
func ParseKeys(b []byte) []Key {
	var keys []Key
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == 0x1b:
			k, n := parseEscape(b[i:])
			if k.Kind != KeyUnknown {
				keys = append(keys, k)
			}
			i += n
		case c == '\r' || c == '\n':
			keys = append(keys, Key{Kind: KeyEnter})
			i++
		case c == '\t':
			keys = append(keys, Key{Kind: KeyTab})
			i++
		case c == 0x7f || c == 0x08:
			keys = append(keys, Key{Kind: KeyBackspace})
			i++
		case c < 0x20:
			if c != 0 {
				keys = append(keys, Ctrl(rune('a'+c-1)))
			}
			i++
		default:
			r, size := utf8.DecodeRune(b[i:])
			if r != utf8.RuneError || size > 1 {
				keys = append(keys, Char(r))
			}
			i += size
		}
	}
	return keys
}

// parseEscape decodes the sequence at b[0] == ESC and returns the key with
// the number of bytes consumed.
func parseEscape(b []byte) (Key, int) {
	if len(b) == 1 {
		return Key{Kind: KeyEsc}, 1
	}
	switch b[1] {
	case '[':
		// CSI: parameters, then a final byte in 0x40..0x7e.
		j := 2
		for j < len(b) && (b[j] < 0x40 || b[j] > 0x7e) {
			j++
		}
		if j >= len(b) {
			return Key{Kind: KeyUnknown}, len(b)
		}
		params := string(b[2:j])
		final := b[j]
		return csiKey(params, final), j + 1
	case 'O':
		// SS3: arrows and Home/End in application-cursor mode.
		if len(b) < 3 {
			return Key{Kind: KeyEsc}, 1
		}
		switch b[2] {
		case 'A':
			return Key{Kind: KeyUp}, 3
		case 'B':
			return Key{Kind: KeyDown}, 3
		case 'C':
			return Key{Kind: KeyRight}, 3
		case 'D':
			return Key{Kind: KeyLeft}, 3
		case 'H':
			return Key{Kind: KeyHome}, 3
		case 'F':
			return Key{Kind: KeyEnd}, 3
		}
		return Key{Kind: KeyUnknown}, 3
	}
	// ESC followed by anything else is an Alt-combination (or a stray Esc
	// typed just before another key); neither has a binding.
	_, size := utf8.DecodeRune(b[1:])
	return Key{Kind: KeyUnknown}, 1 + size
}

func csiKey(params string, final byte) Key {
	fields := strings.Split(params, ";")
	first := fields[0]
	switch final {
	case 'u':
		if strings.HasPrefix(first, "?") {
			// CSI ? <flags> u: the reply to our CSI ? u query.
			return Key{Kind: KeyKittyReply}
		}
		// kitty keyboard protocol: CSI <codepoint> ; <modifiers> u.
		code, _ := strconv.Atoi(first)
		mods := 0
		if len(fields) > 1 {
			mods, _ = strconv.Atoi(fields[1])
		}
		return modifiedKey(code, mods)
	case '~':
		if first == "27" && len(fields) == 3 {
			// xterm modifyOtherKeys: CSI 27 ; <modifiers> ; <codepoint> ~.
			mods, _ := strconv.Atoi(fields[1])
			code, _ := strconv.Atoi(fields[2])
			return modifiedKey(code, mods)
		}
	}
	// Modifier parameters on the other sequences (ESC [ 1 ; 5 A for
	// Ctrl-Up) are ignored: the key counts, the modifier does not.
	switch final {
	case 'A':
		return Key{Kind: KeyUp}
	case 'B':
		return Key{Kind: KeyDown}
	case 'C':
		return Key{Kind: KeyRight}
	case 'D':
		return Key{Kind: KeyLeft}
	case 'H':
		return Key{Kind: KeyHome}
	case 'F':
		return Key{Kind: KeyEnd}
	case 'Z':
		return Key{Kind: KeyBackTab}
	case '~':
		switch first {
		case "1", "7":
			return Key{Kind: KeyHome}
		case "4", "8":
			return Key{Kind: KeyEnd}
		case "3":
			return Key{Kind: KeyDelete}
		case "5":
			return Key{Kind: KeyPgUp}
		case "6":
			return Key{Kind: KeyPgDn}
		case "15":
			return Key{Kind: KeyF5}
		}
	}
	return Key{Kind: KeyUnknown}
}

// modifiedKey decodes a key reported with its modifier bits (1 + shift 1,
// alt 2, ctrl 4, super 8), the encoding both the kitty protocol and
// xterm's modifyOtherKeys share.
func modifiedKey(code, mods int) Key {
	if mods > 0 {
		mods--
	}
	ctrl, shift := mods&4 != 0, mods&1 != 0
	switch code {
	case 13:
		if ctrl {
			return Key{Kind: KeyCtrlEnter}
		}
		return Key{Kind: KeyEnter}
	case 27:
		return Key{Kind: KeyEsc}
	case 9:
		if shift {
			return Key{Kind: KeyBackTab}
		}
		return Key{Kind: KeyTab}
	case 127, 8:
		return Key{Kind: KeyBackspace}
	}
	if ctrl && code >= 'a' && code <= 'z' {
		return Ctrl(rune(code))
	}
	if ctrl && code >= 'A' && code <= 'Z' {
		return Ctrl(rune(code + 'a' - 'A'))
	}
	if mods&^1 == 0 && code >= 0x20 && code != 0x7f {
		// Shift alone still types the character the terminal reports.
		return Char(rune(code))
	}
	return Key{Kind: KeyUnknown}
}
