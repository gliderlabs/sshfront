package internal

import (
	"encoding/binary"
	"syscall"
	"unsafe"
)

//// winsize is borrowed from https://github.com/creack/termios/blob/master/win/win.go

// winsize stores the Heighty and Width of a terminal.
type winsize struct {
	Height uint16
	Width  uint16
	x      uint16 // unused
	y      uint16 // unused
}

func setWinsize(fd uintptr, width int, height int) {
	ws := &winsize{Width: uint16(width), Height: uint16(height)}
	syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCSWINSZ), uintptr(unsafe.Pointer(ws)))
}

//// Helpers below are borrowed from go.crypto circa 2011:

// parsePtyRequest parses the payload of the pty-req message and extracts the
// dimensions of the terminal. See RFC 4254, section 6.2.
func parsePtyRequest(s []byte) (width, height int, ok bool) {
	_, s, ok = parseString(s)
	if !ok {
		return
	}
	width32, s, ok := parseUint32(s)
	if !ok {
		return
	}
	height32, _, ok := parseUint32(s)
	width = int(width32)
	height = int(height32)
	if width < 1 {
		ok = false
	}
	if height < 1 {
		ok = false
	}
	return
}

func parseWinchRequest(s []byte) (width, height int, ok bool) {
	width32, s, ok := parseUint32(s)
	if !ok {
		return
	}
	height32, s, ok := parseUint32(s)
	if !ok {
		return
	}

	width = int(width32)
	height = int(height32)
	if width < 1 {
		ok = false
	}
	if height < 1 {
		ok = false
	}
	return
}

func parseString(in []byte) (out string, rest []byte, ok bool) {
	if len(in) < 4 {
		return
	}
	length := binary.BigEndian.Uint32(in)
	if uint32(len(in)) < 4+length {
		return
	}
	out = string(in[4 : 4+length])
	rest = in[4+length:]
	ok = true
	return
}

func parseUint32(in []byte) (uint32, []byte, bool) {
	if len(in) < 4 {
		return 0, nil, false
	}
	return binary.BigEndian.Uint32(in), in[4:], true
}
