package treehash

// textStats mirrors Git's convert.c gather_stats. Line-ending normalization
// decisions must match Git byte for byte, so this intentionally reproduces
// Git's counting rules rather than a generic "is text" heuristic.
type textStats struct {
	nul, loneCR, loneLF, crlf, printable, nonprintable int64
}

// statsGatherer accumulates textStats over a stream. A carriage return at a
// chunk boundary is held until the next byte is known.
type statsGatherer struct {
	stats     textStats
	pendingCR bool
	last      byte
	seen      bool
}

func (g *statsGatherer) write(p []byte) {
	for i := 0; i < len(p); i++ {
		c := p[i]
		if g.pendingCR {
			g.pendingCR = false
			if c == '\n' {
				g.stats.crlf++
				g.last, g.seen = c, true
				continue
			}
			g.stats.loneCR++
		}
		g.last, g.seen = c, true
		switch {
		case c == '\r':
			g.pendingCR = true
		case c == '\n':
			g.stats.loneLF++
		case c == 127:
			g.stats.nonprintable++
		case c < 32:
			switch c {
			case '\b', '\t', 0x1b, 0x0c:
				g.stats.printable++
			case 0:
				g.stats.nul++
				g.stats.nonprintable++
			default:
				g.stats.nonprintable++
			}
		default:
			g.stats.printable++
		}
	}
}

func (g *statsGatherer) finish() textStats {
	if g.pendingCR {
		g.pendingCR = false
		g.stats.loneCR++
	}
	// Git does not count a trailing DOS end-of-file marker as nonprintable.
	if g.seen && g.last == 0x1a {
		g.stats.nonprintable--
	}
	return g.stats
}

// isBinary mirrors Git's convert_is_binary.
func (s textStats) isBinary() bool {
	return s.loneCR > 0 || s.nul > 0 || (s.printable>>7) < s.nonprintable
}

// crlfAction is the clean-direction ("to Git") subset of Git's
// convert_crlf_action that affects object content.
type crlfAction int

const (
	crlfBinary crlfAction = iota // no conversion
	crlfText                     // convert CRLF to LF unconditionally
	crlfAuto                     // convert CRLF to LF unless the content is binary
)

// crlfStripper removes a carriage return only when it is immediately followed
// by a line feed, which is what Git's crlf_to_git does.
type crlfStripper struct {
	pendingCR bool
}

func (s *crlfStripper) apply(dst, p []byte) []byte {
	for _, c := range p {
		if s.pendingCR {
			s.pendingCR = false
			if c != '\n' {
				dst = append(dst, '\r')
			}
		}
		if c == '\r' {
			s.pendingCR = true
			continue
		}
		dst = append(dst, c)
	}
	return dst
}

func (s *crlfStripper) flush(dst []byte) []byte {
	if s.pendingCR {
		s.pendingCR = false
		dst = append(dst, '\r')
	}
	return dst
}
