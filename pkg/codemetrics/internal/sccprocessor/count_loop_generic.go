// SPDX-License-Identifier: MIT

package processor

func countLoopGeneric(fileJob *FileJob, langFeatures LanguageFeature, bomSkip, endPoint int, currentState int64, endString []byte, endComments [][]byte, ignoreEscape bool) bool {
	//We want to track cognitive complexity nesting. Cognitive complexity
	//means we assign higher complexity to nested branch conditions, so
	//
	//if something:
	//    if otherthing:
	//
	//would be assigned a higher complexity than
	//
	//if something:
	//if otherthing:
	//
	//because the nested if requires more mental overhead. To do this we need to track
	//how nested each condition is when we hit it. We do this by counting the number of
	//whitespace characters are in front of the condition.
	//This is an appoximation, true for languages like Python, and probably true for anything
	//else. However, the benefit of this approach is that it's almost free from a CPU point of view
	//and the increase in spotting complex code, is genuinely useful.
	var indentStack []int
	lineStart := bomSkip
	needIndent := true

	content := fileJob.Content
	// Hoisted because neither changes for the life of the loop and both are
	// read on every byte of the file.
	byteType := fileJob.ContentByteType
	total := int(fileJob.Bytes)

	for index := bomSkip; index < total; index++ {
		curByte := content[index]

		if byteType != nil {
			byteType[index] = stateToByteType(currentState)
		} else if index < endPoint && isBlankRun[curByte] {
			// A run of spaces, tabs and carriage returns changes nothing: no
			// state moves, no line ends, and the only byte of it the loop has
			// anything to say about is the last. Walking it a byte at a time
			// costs the whole of the loop body for each one, so find the end of
			// the run and carry on from there. The newline is deliberately not
			// in the table, because a line ends on it.
			next := index + 1
			for next < endPoint && isBlankRun[content[next]] {
				next++
			}
			index = next
			curByte = content[index]
		}

		// Based on our current state determine if the state should change by checking
		// what the character is. The below is very CPU bound so need to be careful if
		// changing anything in here and profile/measure afterwards!
		// NB that the order of the if statements matters and has been set to what in benchmarks is most efficient
		if !isWhitespace(curByte) {

			// At the first non-whitespace byte of a code-bearing line, update the
			// indent stack so complexity tokens on this line are weighted by their
			// nesting depth. Lines that begin a comment must not move the stack;
			// lines inside a multiline comment/string never reach here in a
			// blank-derived state so they are excluded automatically.
			if Cognitive && needIndent && (currentState == SBlank || currentState == SMulticommentBlank) {
				if tokenType, _, _ := langFeatures.Tokens.Match(fileJob.Content[index:]); tokenType != TSlcomment && tokenType != TMlcomment {
					indent := index - lineStart
					for len(indentStack) > 0 && indent < indentStack[len(indentStack)-1] {
						indentStack = indentStack[:len(indentStack)-1]
					}
					if len(indentStack) == 0 || indent > indentStack[len(indentStack)-1] {
						indentStack = append(indentStack, indent)
					}
					nesting := len(indentStack) - 1
					if nesting < 0 {
						nesting = 0
					}
					fileJob.cognitiveNesting = nesting
					needIndent = false
				}
			}

			switch currentState {
			case SCode:
				index, currentState, endString, endComments, ignoreEscape = codeState(
					fileJob,
					index,
					endPoint,
					currentState,
					endString,
					endComments,
					langFeatures,
					&fileJob.Hash,
				)
			case SString:
				index, currentState = stringState(fileJob, index, endPoint, endString, currentState, ignoreEscape, langFeatures.Escape)
			case SDocString:
				// For a docstring we can either move into blank in which case we count it as a docstring
				// or back into code in which case it should be counted as code
				index, currentState = docStringState(fileJob, index, endPoint, endString, currentState)
			case SMulticomment, SMulticommentCode:
				index, currentState, endString, endComments = commentState(
					fileJob,
					index,
					endPoint,
					currentState,
					endComments,
					endString,
					langFeatures,
				)
			case SBlank, SMulticommentBlank:
				// From blank we can move into comment, move into a multiline comment
				// or move into code but we can only do one.
				index, currentState, endString, endComments, ignoreEscape = blankState(
					fileJob,
					index,
					currentState,
					endComments,
					endString,
					langFeatures,
				)
			}

			// Only a state above moves the index or marks the file binary, so
			// both of the checks that follow belong here rather than on the
			// whitespace the loop walked over to get to one.

			// We shouldn't normally need this, but unclosed strings or comments
			// might leave the index past the end of the file when we reach this
			// point.
			if index >= len(content) {
				return false
			}

			// Only check the first 10000 characters for null bytes indicating a binary file
			// and if we find it then we return otherwise carry on and ignore binary markers
			if index < 10000 && fileJob.Binary {
				return false
			}

			curByte = content[index]
		}

		// This means the end of processing the line so calculate the stats according to what state
		// we are currently in
		if curByte == '\n' || index >= endPoint {
			fileJob.Lines++
			if Cognitive {
				lineStart = index + 1
				needIndent = true
			}
			if fileJob.TrackComplexityLines {
				fileJob.ComplexityLine = append(fileJob.ComplexityLine, 0)
				if Cognitive {
					fileJob.CognitiveLine = append(fileJob.CognitiveLine, 0)
				}
			}

			// Whether this line hands its state to the next one changes for a
			// language that splices, so work out once whether it ends in a splice
			// and let resetLineState answer for both of the branches below.
			spliced := false
			if langFeatures.LineSplice {
				spliced = endsWithLineSplice(fileJob.Content, index)
			}

			if NoLarge && fileJob.Lines >= LargeLineCount {
				// Save memory by unsetting the content as we no longer require it
				fileJob.Content = nil
				return false
			}

			switch currentState {
			case SCode, SString, SCommentCode, SMulticommentCode:
				fileJob.Code++
				if langFeatures.LineSplice {
					currentState = resetLineState(currentState, spliced, ignoreEscape)
				} else {
					currentState = resetState(currentState)
				}
				if fileJob.Callback != nil {
					if !fileJob.Callback.ProcessLine(fileJob, fileJob.Lines, LINE_CODE) {
						return false
					}
				}
				if Trace {
					// Don't remove the outside if-statements, for performance
					printTraceF("%s line %d ended with state: %d: counted as code", fileJob.Location, fileJob.Lines, currentState)
				}
			case SComment, SMulticomment, SMulticommentBlank:
				fileJob.Comment++
				if langFeatures.LineSplice {
					currentState = resetLineState(currentState, spliced, ignoreEscape)
				} else {
					currentState = resetState(currentState)
				}
				if fileJob.Callback != nil {
					if !fileJob.Callback.ProcessLine(fileJob, fileJob.Lines, LINE_COMMENT) {
						return false
					}
				}
				if Trace {
					// Same as above
					printTraceF("%s line %d ended with state: %d: counted as comment", fileJob.Location, fileJob.Lines, currentState)
				}
			case SBlank:
				fileJob.Blank++
				if fileJob.Callback != nil {
					if !fileJob.Callback.ProcessLine(fileJob, fileJob.Lines, LINE_BLANK) {
						return false
					}
				}
				if Trace {
					// Same as above
					printTraceF("%s line %d ended with state: %d: counted as blank", fileJob.Location, fileJob.Lines, currentState)
				}
			case SDocString:
				fileJob.Comment++
				if fileJob.Callback != nil {
					if !fileJob.Callback.ProcessLine(fileJob, fileJob.Lines, LINE_COMMENT) {
						return false
					}
				}
				if Trace {
					// Same as above
					printTraceF("%s line %d ended with state: %d: counted as comment", fileJob.Location, fileJob.Lines, currentState)
				}
			}
		}
	}

	return true
}
