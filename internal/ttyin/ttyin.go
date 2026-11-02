// Package ttyin keeps escape sequences whole across reads: Bubble Tea v1 parses
// each read alone, so a split mouse report leaks as keys like alt+[ and "<65;116;36M".
package ttyin

import "io"

// File is the terminal input; *os.File satisfies it. Bubble Tea and cancelreader
// need Fd and Name to keep raw mode and cancelable reads.
type File interface {
	io.ReadWriteCloser
	Fd() uintptr
	Name() string
}

// maxHeld caps a held tail so malformed input can't stall the reader.
const maxHeld = 64

// Reader wraps a terminal so no read ends inside an escape sequence; it holds
// the partial tail until the rest arrives.
type Reader struct {
	File
	held []byte
}

// New wraps f.
func New(f File) *Reader { return &Reader{File: f} }

func (r *Reader) Read(p []byte) (int, error) {
	if len(p) <= len(r.held) {
		n := copy(p, r.held)
		r.held = r.held[n:]
		return n, nil
	}
	n := copy(p, r.held)
	continuing := n > 0
	r.held = r.held[:0]
	m, err := r.File.Read(p[n:])
	n += m
	if err != nil {
		return n, err
	}
	if cut := incompleteTail(p[:n]); cut < n && n-cut <= maxHeld && !isKeypress(p[cut:n], cut, n < len(p), continuing) {
		r.held = append(r.held, p[cut:n]...)
		return cut, nil
	}
	return n, nil
}

// isKeypress reports whether an unfinished tail is really Esc or Alt+[: alone in
// its read, or a bare ESC ending a short read (reports are written whole).
func isKeypress(tail []byte, at int, short, continuing bool) bool {
	if continuing {
		return false
	}
	return (at == 0 && len(tail) <= 2) || (len(tail) == 1 && short)
}

// incompleteTail returns where an unfinished trailing ESC or CSI sequence
// starts in b, or len(b) when b ends on a sequence boundary.
func incompleteTail(b []byte) int {
	i := len(b) - 1
	for i >= 0 && b[i] != 0x1b {
		i--
	}
	if i < 0 {
		return len(b)
	}
	t := b[i:]
	if len(t) == 1 {
		return i
	}
	if t[1] != '[' {
		return len(b)
	}
	// CSI: parameter and intermediate bytes (0x20-0x3f) until a final byte.
	for _, c := range t[2:] {
		if c < 0x20 || c > 0x3f {
			return len(b)
		}
	}
	return i
}
