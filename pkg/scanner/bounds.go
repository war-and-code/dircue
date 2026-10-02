package scanner

// GitStorageBounds returns the fixed retained-reader limits. Transient readers
// used while resolving deltas are not included in these limits.
func GitStorageBounds() (lanes, singleLaneReaders, concurrentLaneReaders int) {
	return maxGitObjectLanes, maxGitPackDescriptorsSingleLane, maxGitPackDescriptorsPerConcurrentLane
}

// FunctionReportLimit is the maximum retained functions across a structural
// report. It does not enable structural analysis in a map.
func FunctionReportLimit() int { return functionReportLimit }
