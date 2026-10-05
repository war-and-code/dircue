package discovery

// ClassifyCandidate returns the existing filename-only candidate classification
// for a path. A nil result means the filename did not identify one of the
// discovery candidate kinds. This does not read or validate file contents.
func ClassifyCandidate(filename string) *Candidate {
	_, _, candidate := classify(filename)
	return candidate
}
