// Package term is the thin terminal layer under the UI: it opens /dev/tty,
// switches it into raw mode, reports its size, parses key presses and
// measures and cuts ANSI-colored text so it fits a pane. Rendering on the
// tty directly keeps stdout clean for the shell wrapper that captures the
// jump script.
package term

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// ErrNoTTY is returned when there is no terminal to interact with.
var ErrNoTTY = errors.New("no interactive terminal")

// Terminal is the controlling tty, in raw mode between Raw and Restore.
type Terminal struct {
	tty     *os.File
	restore func()
}

// Open opens the controlling terminal for reading keys and writing frames.
func Open() (*Terminal, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, ErrNoTTY
	}
	return &Terminal{tty: tty}, nil
}

// File is the underlying tty, for handing it to a child process (the pager)
// as its stdin/stdout.
func (t *Terminal) File() *os.File { return t.tty }

// Close restores the terminal mode and closes the tty.
func (t *Terminal) Close() {
	t.Restore()
	t.tty.Close()
}

// Raw puts the terminal into raw mode: keys arrive one at a time, nothing is
// echoed and Ctrl-C is an ordinary key, so the UI always gets to restore the
// screen on its way out.
func (t *Terminal) Raw() error {
	if t.restore != nil {
		return nil
	}
	restore, err := makeRaw(t.tty.Fd())
	if err != nil {
		return ErrNoTTY
	}
	t.restore = restore
	return nil
}

// Restore undoes Raw; safe to call more than once.
func (t *Terminal) Restore() {
	if t.restore != nil {
		t.restore()
		t.restore = nil
	}
}

// Size reports the terminal's columns and rows.
func (t *Terminal) Size() (cols, rows int, err error) {
	var ws struct{ Row, Col, X, Y uint16 }
	if err := ioctl(t.tty.Fd(), syscall.TIOCGWINSZ, unsafe.Pointer(&ws)); err != nil {
		return 0, 0, err
	}
	if ws.Col == 0 || ws.Row == 0 {
		return 80, 24, nil
	}
	return int(ws.Col), int(ws.Row), nil
}

// Read reads one chunk of input bytes from the terminal.
func (t *Terminal) Read(buf []byte) (int, error) { return t.tty.Read(buf) }

// Write writes raw output to the terminal.
func (t *Terminal) Write(s string) { _, _ = t.tty.WriteString(s) }

// Screen control sequences: the alternate screen keeps the shell's scrollback
// intact, the synchronized-output pair makes a frame appear at once on
// terminals that support it (others ignore it). Entering the screen also
// asks the terminal to tell Ctrl+Enter from Enter: the kitty keyboard
// protocol's "disambiguate" mode (pushed on the alternate screen's own
// stack and popped before leaving it) and xterm's modifyOtherKeys; a
// terminal without either ignores the request and Ctrl+Enter stays Enter.
const (
	EnterScreen = "\x1b[?1049h\x1b[?25l\x1b[>1u\x1b[>4;2m"
	LeaveScreen = "\x1b[>4;0m\x1b[<u\x1b[?25h\x1b[?1049l"
	BeginFrame  = "\x1b[?2026h\x1b[H"
	EndFrame    = "\x1b[?2026l"
)

// makeRaw mirrors cfmakeraw minus output processing: input is unbuffered
// and unechoed, flow control and signal keys are plain bytes. Returns the
// function that restores the previous state.
func makeRaw(fd uintptr) (func(), error) {
	var old syscall.Termios
	if err := ioctl(fd, ioctlGetTermios, unsafe.Pointer(&old)); err != nil {
		return nil, err
	}
	raw := old
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if err := ioctl(fd, ioctlSetTermios, unsafe.Pointer(&raw)); err != nil {
		return nil, err
	}
	return func() { _ = ioctl(fd, ioctlSetTermios, unsafe.Pointer(&old)) }, nil
}

func ioctl(fd, req uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}
