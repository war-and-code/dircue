package deployables

// actions_workdir.go: GitHub Actions working-directory precedence and
// checkout-path detection for issue #48.
//
// This file extends the github-actions deployable observer to record:
//   - Effective working-directory per step, applying the precedence chain:
//     step > job defaults.run > workflow defaults.run > GITHUB_WORKSPACE (implicit).
//   - Whether an expression blocks fallback to a literal at a lower precedence.
//   - Preceding declared-self checkout paths (actions/checkout with path:)
//     that let the observer qualify workspace-relative paths.
//   - Job `needs` references for issue #11.
//
// All observations are static declarations. No scripts are run, no environment
// variables are expanded, and no network fetches occur.

import (
	"regexp"
	"strings"
)

// workdirPrec records the resolved working-directory declaration for a step,
// with the precedence level that provided it and whether an expression at any
// higher-precedence level blocked fallback to a literal.
type workdirPrec struct {
	// dir is the literal directory value, or "" if only expressions were found.
	dir string
	// basis is the field name that provided the value: "step", "job-default",
	// "workflow-default", or "workspace" (implicit GITHUB_WORKSPACE root).
	basis string
	// expressionBlocked is true when an expression at a higher precedence level
	// prevented resolving a lower-precedence literal.
	expressionBlocked bool
}

// resolveWorkdir applies the GitHub Actions working-directory precedence chain:
//
//  1. Step-level `working-directory`.
//  2. Job-level `defaults.run.working-directory`.
//  3. Workflow-level `defaults.run.working-directory`.
//  4. Implicit GITHUB_WORKSPACE (recorded as "workspace" basis, no dir value).
//
// An expression at any level blocks descent to lower-precedence literals.
func resolveWorkdir(
	stepWD, stepWDDynamic bool, stepWDVal string,
	jobWD, jobWDDynamic bool, jobWDVal string,
	wfWD, wfWDDynamic bool, wfWDVal string,
) workdirPrec {
	// Step level.
	if stepWD {
		if stepWDDynamic {
			return workdirPrec{basis: "step", expressionBlocked: true}
		}
		return workdirPrec{dir: stepWDVal, basis: "step"}
	}
	// Job default level.
	if jobWD {
		if jobWDDynamic {
			return workdirPrec{basis: "job-default", expressionBlocked: true}
		}
		return workdirPrec{dir: jobWDVal, basis: "job-default"}
	}
	// Workflow default level.
	if wfWD {
		if wfWDDynamic {
			return workdirPrec{basis: "workflow-default", expressionBlocked: true}
		}
		return workdirPrec{dir: wfWDVal, basis: "workflow-default"}
	}
	// Implicit GITHUB_WORKSPACE.
	return workdirPrec{basis: "workspace"}
}

// checkoutPath extracts the literal `path` input from an `actions/checkout`
// step and reports whether the step checks out another repository. A
// `repository` input other than `${{ github.repository }}`, including any other
// expression, counts as another repository. It returns ok=false for
// expressions, missing values, or non-checkout steps.
func checkoutPath(step map[interface{}]interface{}) (p string, otherRepository, ok bool) {
	usesVal, found := stringValue(step, "uses")
	// Match actions/checkout@* (any version tag or SHA).
	if !found || !strings.HasPrefix(usesVal, "actions/checkout@") {
		return "", false, false
	}
	withMap, found := object(step, "with")
	if !found {
		return "", false, false
	}
	p, found = stringValue(withMap, "path")
	if !found || dynamic(p) {
		return "", false, false
	}
	if repository, set := stringValue(withMap, "repository"); set && strings.TrimSpace(repository) != "" {
		otherRepository = !selfRepositoryExpr.MatchString(strings.TrimSpace(repository))
	}
	return p, otherRepository, true
}

var selfRepositoryExpr = regexp.MustCompile(`^\$\{\{\s*github\.repository\s*\}\}$`)

// jobDefaultsWorkdir returns the declared literal working-directory from a
// job's `defaults.run` block, and whether it contains an expression.
func jobDefaultsWorkdir(job map[interface{}]interface{}) (val string, present bool, isDynamic bool) {
	defaults, ok := object(job, "defaults")
	if !ok {
		return "", false, false
	}
	run, ok := object(defaults, "run")
	if !ok {
		return "", false, false
	}
	wd, ok := stringValue(run, "working-directory")
	if !ok {
		return "", false, false
	}
	return wd, true, dynamic(wd)
}

// workflowDefaultsWorkdir returns the declared literal working-directory from
// the workflow-level `defaults.run` block, and whether it contains an
// expression.
func workflowDefaultsWorkdir(doc map[interface{}]interface{}) (val string, present bool, isDynamic bool) {
	return jobDefaultsWorkdir(doc) // identical field path
}
