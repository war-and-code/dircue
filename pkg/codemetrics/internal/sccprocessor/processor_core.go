// Package processor contains the upstream scc counting engine subset used by dircue.
package processor

import (
	"regexp"
	"runtime"
	"sync"
)

var Version = "4.1.0"

// Flags set via the CLI which control how the output is displayed

// Files indicates if there should be file output or not when formatting
var Files = false

// Languages indicates if the command line should print out the supported languages
var Languages = false

// Verbose enables verbose logging output
var Verbose = false

// Debug enables debug logging output
var Debug = false

// Trace enables trace logging output which is extremely verbose
var Trace = false

// Duplicates enables duplicate file detection
var Duplicates = false

// MinifiedGenerated enables minified/generated file detection
var MinifiedGenerated = false

// IgnoreMinifiedGenerate printing counts for minified/generated files
var IgnoreMinifiedGenerate = false

// MinifiedGeneratedLineByteLength number of bytes per average line to determine file is minified/generated
var MinifiedGeneratedLineByteLength = 255

// Minified enables minified file detection
var Minified = false

// IgnoreMinified ignore printing counts for minified files
var IgnoreMinified = false

// Generated enables generated file detection
var Generated = false

// GeneratedMarkers defines head markers for generated file detection
var GeneratedMarkers []string

// IgnoreGenerated ignore printing counts for generated files
var IgnoreGenerated = false

// Complexity toggles complexity calculation
var Complexity = false

// Cognitive toggles cognitive (nesting-weighted) complexity calculation
var Cognitive = false

// More enables wider output with more information in formatter
var More = false

// Cocomo toggles the COCOMO calculation
var Cocomo = false

// SLOCCountFormat prints a more SLOCCount like COCOMO calculation
var SLOCCountFormat = false

// CocomoProjectType allows the flipping between project types which impacts the calculation
var CocomoProjectType = "organic"

// Size toggles the Size calculation
var Size = false

// Draw horizontal borders between sections.
var HBorder = false

// SizeUnit determines what size calculation is used for megabytes
var SizeUnit = "si"

// Ci indicates if running inside a CI so to disable box drawing characters
var Ci = false

// GitIgnore disables .gitignore checks
var GitIgnore = false

// GitModuleIgnore disables .gitmodules checks
var GitModuleIgnore = false

// Ignore disables ignore file checks
var Ignore = false

// SccIgnore disables sccignore file checks
var SccIgnore = false

// CountIgnore should we count ignore files?
var CountIgnore = false

// CountUnsupported when set counts files scc does not recognise under an
// "Unknown" category, treating them as plain text. See issue #464.
var CountUnsupported = false

// IgnoreFiles are paths to additional ignore files supplied via --ignore-file.
// They are applied as a low priority base layer in the order supplied so a later
// file can override an earlier one, and any in-tree .gitignore/.ignore/.sccignore
// discovered while walking overrides all of them.
var IgnoreFiles = []string{}

// DisableCheckBinary toggles checking for binary files using NUL bytes
var DisableCheckBinary = false

// UlocMode toggles checking for binary files using NUL bytes
var UlocMode = false

// Percent toggles checking for binary files using NUL bytes
var Percent = false

// MaxMean sets the calculation of the max and mean line length
var MaxMean = false

// Dryness toggles checking for binary files using NUL bytes
var Dryness = false

// SortBy sets which column output in formatter should be sorted by
var SortBy = ""

// Exclude is a regular expression which is used to exclude files from being processed
var Exclude = []string{}

// CountAs is a rule for mapping known or new extensions to other rules
var CountAs = ""

// Format sets the output format of the formatter
var Format = ""

// FormatMulti is a rule for defining multiple output formats
var FormatMulti = ""

// SQLProject is used to store the name for the SQL insert formats but is optional
var SQLProject = ""

// RemapUnknown allows remapping of unknown files with a string to search the content for
var RemapUnknown = ""

// RemapAll allows remapping of all files with a string to search the content for
var RemapAll = ""

type MatchEngine int

const (
	// MatchGlob is the default. The pattern is a glob ('*' and '?') translated
	// to an anchored regex and matched as a full match against the path.
	MatchGlob MatchEngine = iota
	// MatchRegex treats the pattern as a raw (unanchored) RE2 regex. Opt in
	// with the re: prefix.
	MatchRegex
)

// CountRule is the typed, library-facing form of a --count-as-pattern rule.
// It matches files by their path and relabels them to a new named category
// whose counting rules are cloned from an existing base language.
type CountRule struct {
	Engine       MatchEngine // MatchGlob (the default) or MatchRegex
	Pattern      string      // glob or regex source
	Name         string      // new category display name
	BaseLanguage string      // existing language whose counting rules are cloned
}

// CountRules is the typed input set either directly by library users or by the
// CLI after parsing CountAsPattern. Setup happens in setupCountRules.
var CountRules []CountRule

// CountAsPattern holds the raw repeatable --count-as-pattern flag values. Each
// is parsed into a CountRule at setup. Library users may set CountRules directly.
var CountAsPattern []string

// compiledCountRule is the runtime form scanned by newFileJob
type compiledCountRule struct {
	re   *regexp.Regexp
	name string
}

var compiledCountRules []compiledCountRule

// CurrencySymbol allows setting the currency symbol for cocomo project cost estimation
var CurrencySymbol = ""

// FileOutput sets the file that output should be written to
var FileOutput = ""

// PathDenyList sets the paths that should be skipped
var PathDenyList = []string{}

// FileListQueueSize is the queue of files found and ready to be read into memory
var FileListQueueSize = runtime.NumCPU()

// FileProcessJobWorkers is the number of workers that process the file collecting stats
var FileProcessJobWorkers = runtime.NumCPU() * 4

// FileListJobWorkers is the number of workers that turn a path the walker found
// into a FileJob, which is a stat and a language lookup each
var FileListJobWorkers = runtime.NumCPU()

// FileSummaryJobQueueSize is the queue used to hold processed file statistics before formatting
var FileSummaryJobQueueSize = runtime.NumCPU()

// DirectoryWalkerJobWorkers is the number of workers which will walk the directory tree
var DirectoryWalkerJobWorkers = 8

// AllowListExtensions is a list of extensions which are allowed to be processed
var AllowListExtensions = []string{}

// ExcludeListExtensions is a list of extensions which should be ignored
var ExcludeListExtensions = []string{}

// ExcludeFilename is a list of filenames which should be ignored
var ExcludeFilename = []string{}

// AverageWage is the average wage in dollars used for the COCOMO cost estimate
var AverageWage int64 = 56286

// Overhead is the overhead multiplier for corporate overhead (facilities, equipment, accounting, etc.)
var Overhead float64 = 2.4

// EAF is the effort adjustment factor derived from the cost drivers, i.e. 1.0 if rated nominal
var EAF float64 = 1.0

// Locomo toggles the LOCOMO (LLM Output COst MOdel) calculation
var Locomo = false

// CostComparison enables both COCOMO and LOCOMO output for side-by-side comparison
var CostComparison = false

// LocomoPresetName is the LLM model preset for pricing and throughput defaults
var LocomoPresetName = "medium"

// LocomoInputPrice is the cost per 1M input tokens (overrides preset)
var LocomoInputPrice float64
var LocomoInputPriceSet = false

// LocomoOutputPrice is the cost per 1M output tokens (overrides preset)
var LocomoOutputPrice float64
var LocomoOutputPriceSet = false

// LocomoTPS is the output tokens per second (overrides preset)
var LocomoTPS float64
var LocomoTPSSet = false

// LocomoReviewMinutesPerLine is the human review time per line of code in minutes
var LocomoReviewMinutesPerLine float64 = 0.01

// LocomoConfig is the power-user config string "tokensPerLine,baseInputPerLine,complexityWeight,iterations,iterationWeight"
var LocomoConfig = ""

// LocomoTokensPerLine is the average number of output tokens per line of code
var LocomoTokensPerLine float64 = 10

// LocomoBaseInputPerLine is the base number of input tokens per output line
var LocomoBaseInputPerLine float64 = 20

// LocomoComplexityWeight is the scaling weight applied to sqrt(complexity density) for input tokens
var LocomoComplexityWeight float64 = 5

// LocomoIterations is the base number of iteration/retry attempts
var LocomoIterations float64 = 1.5

// LocomoIterationWeight is the scaling weight for complexity-driven retries
var LocomoIterationWeight float64 = 2

// LocomoCyclesOverride is the user-supplied iteration factor override (--locomo-cycles)
var LocomoCyclesOverride float64

// LocomoCyclesSet indicates whether --locomo-cycles was explicitly set
var LocomoCyclesSet = false

// GcFileCount is the number of files to process before turning the GC back on
var GcFileCount = 10000
var gcPercent = -1

// NoLarge if set true will ignore files over a certain number of lines or bytes
var NoLarge = false

// IncludeSymLinks if set true will count symlink files
var IncludeSymLinks = false

// LargeLineCount number of lines before being counted as a large file based on https://github.com/pinpt/ripsrc/blob/master/ripsrc/fileinfo/fileinfo.go#L44
var LargeLineCount int64 = 40000

// LargeByteCount number of bytes before being counted as a large file based on https://github.com/pinpt/ripsrc/blob/master/ripsrc/fileinfo/fileinfo.go#L44
var LargeByteCount int64 = 1000000

// Hotspots toggles the hotspots git-history report
var Hotspots = false

// Coupling toggles the change-coupling git-history report (file pairs that
// change together)
var Coupling = false

// CouplingFor, when non-empty, switches the coupling report to the
// file-oriented "blast radius" view for the given path: what tends to change
// when that file changes.
var CouplingFor = ""

// CouplingWeighted ranks coupling by degree × the pair's (smaller) file
// complexity instead of raw co-change, so pairs of genuinely complex files
// outrank generated/data-file churn. Implies Coupling. Honours the Cognitive
// global for its complexity source, matching --hotspots.
var CouplingWeighted = false

// ByAuthor toggles the author-rollup git-history report
var ByAuthor = false

// Timeline selects an over-time view. With ByAuthor, runs the author
// timeline report (plan 04); alone, runs the languages-over-time report
// (plan 05). With Hotspots set, the combination errors out.
var Timeline = false

// HistoryBuckets is the time-bucket resolution for the timeline reports.
// Wired to --buckets in main.go; default 60.
var HistoryBuckets = 60

// FoldAuthors enables the name+domain identity folding fallback applied
// after the mailmap. Toggled off via --no-fold-authors.
var FoldAuthors = true

// DirFilePaths is not set via flags but by arguments following the flags for file or directory to process
var DirFilePaths = []string{}

// ExtensionToLanguage is loaded from the JSON that is in constants.go
var ExtensionToLanguage = map[string][]string{}

// ShebangLookup loaded from the JSON in constants.go contains shebang lookups
var ShebangLookup = map[string][]string{}

// FilenameToLanguage similar to ExtensionToLanguage loaded from the JSON in constants.go
var FilenameToLanguage = map[string]string{}

// LanguageFeatures contains the processed languages from processLanguageFeature
var LanguageFeatures = map[string]LanguageFeature{}

// LanguageFeaturesMutex is the shared mutex used to control getting and setting of language features
// used rather than sync.Map because it turned out to be marginally faster
var LanguageFeaturesMutex = sync.Mutex{}

// Start time in milli seconds in case we want the total time
var startTimeMilli = makeTimestampMilli()

// ConfigureGc needs to be set outside of ProcessConstants because it should only be enabled in command line
// mode https://github.com/boyter/scc/issues/32

var isLazy bool

func ConfigureLazy(lazy bool) { isLazy = lazy }
func ProcessConstants() {
	clear(ExtensionToLanguage)
	clear(FilenameToLanguage)
	clear(ShebangLookup)
	for name, value := range languageDatabase {
		for _, ext := range value.Extensions {
			ExtensionToLanguage[ext] = append(ExtensionToLanguage[ext], name)
		}
		for _, fname := range value.FileNames {
			FilenameToLanguage[fname] = name
		}
		if len(value.SheBangs) != 0 {
			ShebangLookup[name] = value.SheBangs
		}
	}
	if !isLazy {
		for name, value := range languageDatabase {
			processLanguageFeature(name, value)
		}
	}
}
func LoadLanguageFeature(name string) {
	if !isLazy {
		return
	}
	LanguageFeaturesMutex.Lock()
	_, ok := LanguageFeatures[name]
	LanguageFeaturesMutex.Unlock()
	if ok {
		return
	}
	value, ok := languageDatabase[name]
	if !ok {
		return
	}
	processLanguageFeature(name, value)
}

func caseSpellings(tokens []string, caseInsensitive bool) []string {
	if !caseInsensitive {
		return tokens
	}

	const maxLetters = 8

	spellings := make([]string, 0, len(tokens))
	for _, token := range tokens {
		letters := 0
		for i := 0; i < len(token); i++ {
			if token[i] != asciiLower(token[i]) || token[i] != asciiUpper(token[i]) {
				letters++
			}
		}

		if letters == 0 || letters > maxLetters {
			spellings = append(spellings, token)
			continue
		}

		variants := []string{""}
		for i := 0; i < len(token); i++ {
			lower, upper := asciiLower(token[i]), asciiUpper(token[i])
			next := make([]string, 0, len(variants)*2)
			for _, prefix := range variants {
				next = append(next, prefix+string(lower))
				if upper != lower {
					next = append(next, prefix+string(upper))
				}
			}
			variants = next
		}
		spellings = append(spellings, variants...)
	}

	return spellings
}

func asciiLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + 'a' - 'A'
	}

	return b
}

func asciiUpper(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - 'a' + 'A'
	}

	return b
}

func processLanguageFeature(name string, value Language) {
	complexityTrie := &Trie{}
	slCommentTrie := &Trie{}
	mlCommentTrie := &Trie{}
	stringTrie := &Trie{}
	tokenTrie := &Trie{}
	keywordBytes := make([][]byte, 0, len(value.Keywords))
	postfixExcludes := make([][]byte, 0, len(value.ComplexityChecksPostfixExcludes))

	complexityMask := byte(0)
	singleLineCommentMask := byte(0)
	multiLineCommentMask := byte(0)
	stringMask := byte(0)
	processMask := byte(0)

	for _, v := range value.ComplexityChecks {
		complexityMask |= v[0]
		complexityTrie.Insert(TComplexity, []byte(v))
		if !Complexity {
			tokenTrie.Insert(TComplexity, []byte(v))
		}
	}
	if !Complexity {
		processMask |= complexityMask
	}

	for _, v := range value.ComplexityChecksPostfix {
		if !Complexity {
			tokenTrie.Insert(TComplexityPostfix, []byte(v))
			processMask |= v[0]
		}
	}

	for _, v := range value.ComplexityChecksPostfixExcludes {
		postfixExcludes = append(postfixExcludes, []byte(v))
	}

	for _, v := range caseSpellings(value.LineComment, value.CaseInsensitive) {
		singleLineCommentMask |= v[0]
		slCommentTrie.Insert(TSlcomment, []byte(v))
		tokenTrie.Insert(TSlcomment, []byte(v))
	}
	processMask |= singleLineCommentMask

	for _, v := range value.MultiLine {
		multiLineCommentMask |= v[0][0]
		mlCommentTrie.InsertClose(TMlcomment, []byte(v[0]), []byte(v[1]))
		tokenTrie.InsertClose(TMlcomment, []byte(v[0]), []byte(v[1]))
	}
	processMask |= multiLineCommentMask

	for _, v := range value.Quotes {
		stringMask |= v.Start[0]
		stringTrie.InsertClose(TString, []byte(v.Start), []byte(v.End))
		tokenTrie.InsertClose(TString, []byte(v.Start), []byte(v.End))
	}
	processMask |= stringMask

	for _, v := range value.Keywords {
		keywordBytes = append(keywordBytes, []byte(v))
	}

	// Compile any regex heuristics used to disambiguate shared extensions such
	// as .h between C / C++ / Objective-C. The patterns are validated at
	// generation time (scripts/include.go) so MustCompile is safe here, but we
	// guard with Compile anyway to honour the no-panics policy. Each pattern's
	// necessary literals are pre-converted to bytes so guessByHeuristics can
	// cheaply skip running the regex when none of them appear in the content.
	heuristics := make([]CompiledHeuristic, 0, len(value.Heuristics))
	for _, v := range value.Heuristics {
		re, err := regexp.Compile(v.Pattern)
		if err != nil {
			printWarnF("failed to compile heuristic %q for language %s: %v", v.Pattern, name, err)
			continue
		}
		literals := make([][]byte, 0, len(v.Literals))
		for _, l := range v.Literals {
			literals = append(literals, []byte(l))
		}
		heuristics = append(heuristics, CompiledHeuristic{Re: re, Literals: literals, Anchored: v.Anchored})
	}

	// A line comment spelled as a word ends where the word does, which costs a
	// check on every token that matches. Almost no language has one, so work out
	// here whether this one does and let the hot loop skip the check entirely.
	escape := byte('\\')
	if len(value.Escape) != 0 {
		escape = value.Escape[0]
	}

	// The exact set of bytes the counting loop has to stop on, which is every
	// byte that opens a token plus the newline and the nul. Built here once so
	// the loop pays a single load and branch per byte instead of a mask test
	// that nearly always passes followed by a failed trie walk.
	tokenFirst := newTokenFirst()
	for i := range tokenTrie.Table {
		if tokenTrie.Table[i] != nil {
			tokenFirst[i] = true
		}
	}

	wordComments := false
	for _, token := range value.LineComment {
		if len(token) > 1 && isIdentifierContinue(token[len(token)-1]) {
			wordComments = true
			break
		}
	}

	LanguageFeaturesMutex.Lock()
	LanguageFeatures[name] = LanguageFeature{
		Complexity:            complexityTrie,
		MultiLineComments:     mlCommentTrie,
		MultiLine:             value.MultiLine,
		SingleLineComments:    slCommentTrie,
		LineComment:           value.LineComment,
		Strings:               stringTrie,
		Tokens:                tokenTrie,
		Nested:                value.NestedMultiLine,
		LineSplice:            value.LineSplice,
		WordComments:          wordComments,
		CommentIsWord:         value.CommentIsWord,
		Escape:                escape,
		PostfixExcludes:       postfixExcludes,
		ComplexityCheckMask:   complexityMask,
		MultiLineCommentMask:  multiLineCommentMask,
		SingleLineCommentMask: singleLineCommentMask,
		StringCheckMask:       stringMask,
		ProcessMask:           processMask,
		TokenFirst:            tokenFirst,
		Keywords:              value.Keywords,
		KeywordBytes:          keywordBytes,
		Heuristics:            heuristics,
		Quotes:                value.Quotes,
	}
	LanguageFeaturesMutex.Unlock()
}

// LanguageDatabase returns a copy of the upstream language database.
func LanguageDatabase() map[string]Language {
	out := make(map[string]Language, len(languageDatabase))
	for name, lang := range languageDatabase {
		out[name] = lang
	}
	return out
}
