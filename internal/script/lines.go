package script

import "bytes"

// The output assembler turns a terminal's raw bytes into lines of text for
// output triggers: escape sequences dropped, \r redraws reduced to their
// last state, and the alternate screen (full-screen programs) told apart.
// It reads the bytes on its own goroutine, never holding up the terminal.

// maxLine is where a long line is cut.
const maxLine = 4 << 10

type line struct {
	text string
	alt  bool // printed on the alternate screen
}

type escState int

const (
	escNone   escState = iota
	escStart           // after ESC
	escCSI             // ESC [
	escStr             // OSC, DCS, SOS, PM, APC: until BEL or ESC \
	escStrEsc          // ESC inside a string
)

type assembler struct {
	cur   []byte
	dirty bool // cur has bytes since the last line or flush
	shown bool // cur was flushed: its newline doesn't hand it out again
	cr    bool // a \r came: the next byte starts the line over
	alt   bool
	esc   escState
	csi   []byte
}

func (a *assembler) feed(p []byte) []line {
	var out []line
	for _, c := range p {
		switch a.esc {
		case escStart:
			switch c {
			case '[':
				a.esc, a.csi = escCSI, a.csi[:0]
			case ']', 'P', 'X', '^', '_':
				a.esc = escStr
			default:
				a.esc = escNone // a two-byte escape such as ESC 7
			}
			continue
		case escCSI:
			if c >= 0x40 && c <= 0x7e {
				a.csiDone(c)
				a.esc = escNone
			} else if len(a.csi) < 32 {
				a.csi = append(a.csi, c)
			}
			continue
		case escStr:
			switch c {
			case 0x07:
				a.esc = escNone
			case 0x1b:
				a.esc = escStrEsc
			}
			continue
		case escStrEsc:
			if c == '\\' {
				a.esc = escNone
			} else {
				a.esc = escStr
			}
			continue
		}
		switch c {
		case 0x1b:
			a.esc = escStart
		case '\n':
			if a.dirty || !a.shown {
				out = append(out, line{text: string(a.cur), alt: a.alt})
			}
			a.cur, a.dirty, a.cr, a.shown = a.cur[:0], false, false, false
		case '\r':
			a.cr = true
		case '\b':
			if len(a.cur) > 0 {
				a.cur = a.cur[:len(a.cur)-1]
			}
		case '\t':
			a.put(' ')
		default:
			if c >= 0x20 || c >= 0x80 {
				a.put(c)
			}
		}
	}
	return out
}

func (a *assembler) put(c byte) {
	if a.cr {
		a.cur, a.cr = a.cur[:0], false
	}
	if len(a.cur) < maxLine {
		a.cur = append(a.cur, c)
	}
	a.dirty = true
}

// csiDone acts on a finished CSI: only the alternate screen modes matter.
func (a *assembler) csiDone(final byte) {
	if final != 'h' && final != 'l' || len(a.csi) == 0 || a.csi[0] != '?' {
		return
	}
	for _, mode := range bytes.Split(a.csi[1:], []byte{';'}) {
		switch string(mode) {
		case "47", "1047", "1049":
			a.alt = final == 'h'
		}
	}
}

// flush hands out a line that never got its newline (a prompt, "Listening
// on :8080"), once, when the output has gone quiet.
func (a *assembler) flush() (line, bool) {
	if !a.dirty || len(a.cur) == 0 {
		return line{}, false
	}
	a.dirty, a.shown = false, true
	return line{text: string(a.cur), alt: a.alt}, true
}
