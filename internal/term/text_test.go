package term

import "testing"

func TestWidth(t *testing.T) {
	cases := map[string]int{
		"":                        0,
		"abc":                     3,
		"\x1b[32m+abc\x1b[m":      4,
		"日本":                      4,
		"é":                      1,
		"\x1b]0;title\x07x":       1,
		"tab\there":               7, // control chars take no width until Printable expands them
		"\x1b[1mdiff --git\x1b[m": 10,
	}
	for in, want := range cases {
		if got := Width(in); got != want {
			t.Errorf("Width(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestCut(t *testing.T) {
	cases := []struct {
		in          string
		skip, width int
		want        string
	}{
		{"abcdef", 0, 3, "abc"},
		{"abcdef", 2, 3, "cde"},
		{"abcdef", 5, 10, "f"},
		{"abcdef", 6, 10, ""},
		{"abc", 0, 0, ""},
		{"\x1b[31mabc\x1b[mdef", 1, 3, "\x1b[31mbc\x1b[md\x1b[0m"},
		{"日本語", 0, 3, "日 "},  // second wide char straddles the right edge
		{"日本語", 1, 3, " 本"},  // first wide char straddles the left edge
		{"日本語", 0, 6, "日本語"}, // exact fit
		{"aéb", 0, 2, "aé"},
		{"a\x1b[Kb", 0, 2, "ab"}, // non-SGR sequences are dropped
	}
	for _, c := range cases {
		if got := Cut(c.in, c.skip, c.width); got != c.want {
			t.Errorf("Cut(%q, %d, %d) = %q, want %q", c.in, c.skip, c.width, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in    string
		width int
		want  string
	}{
		{"abc", 5, "abc"},
		{"abc", 3, "abc"},
		{"abcdef", 4, "abc…"},
		{"abcdef", 1, "…"},
		{"abcdef", 0, ""},
		{"\x1b[36mfeature/auth\x1b[m", 8, "\x1b[36mfeature\x1b[0m…"},
	}
	for _, c := range cases {
		if got := Truncate(c.in, c.width); got != c.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", c.in, c.width, got, c.want)
		}
	}
}

func TestPad(t *testing.T) {
	if got := PadRight("ab", 4); got != "ab  " {
		t.Errorf("PadRight = %q", got)
	}
	if got := PadRight("abcdef", 4); got != "abcd" {
		t.Errorf("PadRight cut = %q", got)
	}
	if got := PadLeft("ab", 4); got != "  ab" {
		t.Errorf("PadLeft = %q", got)
	}
	if got := PadRight("\x1b[1mab\x1b[m", 3); Width(got) != 3 {
		t.Errorf("PadRight colored width = %d", Width(got))
	}
}

func TestPrintable(t *testing.T) {
	cases := map[string]string{
		"plain":              "plain",
		"a\tb":               "a       b",
		"abcdefgh\tb":        "abcdefgh        b",
		"\x1b[32m+\tx\x1b[m": "\x1b[32m+       x\x1b[m",
		"crlf\r":             "crlf^M",
		"del\x7f":            "del^?",
		"a\x1b[Kb":           "ab",
	}
	for in, want := range cases {
		if got := Printable(in); got != want {
			t.Errorf("Printable(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStrip(t *testing.T) {
	if got := Strip("\x1b[1mdiff\x1b[m --git \x1b]0;t\x07x"); got != "diff --git x" {
		t.Errorf("Strip = %q", got)
	}
}

func TestParseKeys(t *testing.T) {
	cases := []struct {
		in   string
		want []Key
	}{
		{"a", []Key{Char('a')}},
		{"ab", []Key{Char('a'), Char('b')}},
		{"\x1b", []Key{{Kind: KeyEsc}}},
		{"\x1b[A\x1b[B", []Key{{Kind: KeyUp}, {Kind: KeyDown}}},
		{"\x1bOA", []Key{{Kind: KeyUp}}},
		{"\x1b[1;5A", []Key{{Kind: KeyUp}}},
		{"\x1b[5~\x1b[6~", []Key{{Kind: KeyPgUp}, {Kind: KeyPgDn}}},
		{"\x1b[H\x1b[F\x1b[1~\x1b[4~", []Key{{Kind: KeyHome}, {Kind: KeyEnd}, {Kind: KeyHome}, {Kind: KeyEnd}}},
		{"\x1b[Z", []Key{{Kind: KeyBackTab}}},
		{"\x1b[15~", []Key{{Kind: KeyF5}}},
		{"\x1b[3~", []Key{{Kind: KeyDelete}}},
		{"\r\n\t", []Key{{Kind: KeyEnter}, {Kind: KeyEnter}, {Kind: KeyTab}}},
		{"\x7f\x08", []Key{{Kind: KeyBackspace}, {Kind: KeyBackspace}}},
		{"\x04\x15\x03", []Key{Ctrl('d'), Ctrl('u'), Ctrl('c')}},
		{"é日", []Key{Char('é'), Char('日')}},
		{"\x1b[13;5u", []Key{{Kind: KeyCtrlEnter}}},    // kitty: ctrl+enter
		{"\x1b[27;5;13~", []Key{{Kind: KeyCtrlEnter}}}, // xterm modifyOtherKeys: ctrl+enter
		{"\x1b[13u", []Key{{Kind: KeyEnter}}},
		{"\x1b[27u", []Key{{Kind: KeyEsc}}},
		{"\x1b[9;2u", []Key{{Kind: KeyBackTab}}},
		{"\x1b[99;5u", []Key{Ctrl('c')}},
		{"\x1b[27;5;115~", []Key{Ctrl('s')}},
		{"\x1b[97;2u", []Key{Char('a')}},
		{"\x1b[97;3u", nil},
		{"\x1b[127u", []Key{{Kind: KeyBackspace}}},
		{"\x1b[?1u", []Key{{Kind: KeyKittyReply}}}, // the terminal supports the protocol
		{"\x1b[?0u", []Key{{Kind: KeyKittyReply}}},
		{"\x1b1\x1b9", []Key{{Kind: KeyAlt, Rune: '1'}, {Kind: KeyAlt, Rune: '9'}}}, // alt+digit as ESC digit
		{"\x1b[50;3u", []Key{{Kind: KeyAlt, Rune: '2'}}},                            // alt+2 in the kitty encoding
		{"\x1bx", nil},      // alt-x: no binding
		{"\x1b[?1;2c", nil}, // a terminal reply: skipped whole
		{"\x1b[?1;2cq", []Key{Char('q')}},
	}
	for _, c := range cases {
		got := ParseKeys([]byte(c.in))
		if len(got) != len(c.want) {
			t.Errorf("ParseKeys(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("ParseKeys(%q)[%d] = %v, want %v", c.in, i, got[i], c.want[i])
			}
		}
	}
}
