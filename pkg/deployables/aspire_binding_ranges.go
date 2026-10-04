package deployables

// aspireBindingRanges indexes the two names and two statement boundaries
// inspected by Aspire's conservative binding checks. Its storage and build
// time are linear in the already bounded token stream.
type aspireBindingRanges struct {
	projectsPrefix               []uint32
	distributedApplicationPrefix []uint32
	nextSemicolon                []uint32
	nextNamespaceBoundary        []uint32
}

func newAspireBindingRanges(tokens []csToken) aspireBindingRanges {
	n := len(tokens)
	ranges := aspireBindingRanges{
		projectsPrefix:               make([]uint32, n+1),
		distributedApplicationPrefix: make([]uint32, n+1),
		nextSemicolon:                make([]uint32, n+1),
		nextNamespaceBoundary:        make([]uint32, n+1),
	}
	// n is the exclusive end when no delimiter remains. The arrays use uint32
	// because lexer input is capped at maxAspireCSharpTokens.
	ranges.nextSemicolon[n] = uint32(n)
	ranges.nextNamespaceBoundary[n] = uint32(n)
	for i, token := range tokens {
		ranges.projectsPrefix[i+1] = ranges.projectsPrefix[i]
		ranges.distributedApplicationPrefix[i+1] = ranges.distributedApplicationPrefix[i]
		if token.text == "Projects" {
			ranges.projectsPrefix[i+1]++
		}
		if token.text == "DistributedApplication" {
			ranges.distributedApplicationPrefix[i+1]++
		}
	}
	for i := n - 1; i >= 0; i-- {
		ranges.nextSemicolon[i] = ranges.nextSemicolon[i+1]
		ranges.nextNamespaceBoundary[i] = ranges.nextNamespaceBoundary[i+1]
		if tokens[i].text == ";" {
			ranges.nextSemicolon[i] = uint32(i)
		}
		if tokens[i].text == ";" || tokens[i].text == "{" {
			ranges.nextNamespaceBoundary[i] = uint32(i)
		}
	}
	return ranges
}

func (ranges aspireBindingRanges) containsProjectsBeforeSemicolon(start int) bool {
	return ranges.containsBefore(start, ranges.projectsPrefix, ranges.nextSemicolon)
}

func (ranges aspireBindingRanges) containsDistributedApplicationBeforeSemicolon(start int) bool {
	return ranges.containsBefore(start, ranges.distributedApplicationPrefix, ranges.nextSemicolon)
}

func (ranges aspireBindingRanges) containsProjectsBeforeNamespaceBoundary(start int) bool {
	return ranges.containsBefore(start, ranges.projectsPrefix, ranges.nextNamespaceBoundary)
}

// start is the using/namespace keyword token. The searched range begins at
// the following token and excludes the first applicable delimiter.
func (ranges aspireBindingRanges) containsBefore(start int, prefix, nextBoundary []uint32) bool {
	if start < 0 || start+1 >= len(prefix) || len(nextBoundary) != len(prefix) {
		return false
	}
	from := start + 1
	to := int(nextBoundary[from])
	if to > len(prefix)-1 {
		to = len(prefix) - 1
	}
	return to > from && prefix[to] != prefix[from]
}
