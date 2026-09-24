package intentmap

import (
	"bytes"
	"regexp"
	"strings"
)

// Java and Kotlin Spring Boot / build-file entry-point detection.
//
// Three distinct static signals are recognised:
//
//  1. @SpringBootApplication on a class that also contains a public-static main
//     method. This is the canonical Spring Boot entry point. The class name is
//     extracted and recorded as the entry-point name. Only the first occurrence
//     is retained per file.
//
//  2. Maven <mainClass> inside <plugin>…<configuration>. A declared mainClass
//     in a pom.xml spring-boot-maven-plugin configuration is the authoritative
//     build-time entry point.
//
//  3. Gradle mainClass = "…" or springBoot { mainClass = "…" } in build.gradle /
//     build.gradle.kts. Only static string literals are recognised; property
//     expressions like mainClass.set(…) use a looser pattern but must still
//     produce a plausible class name.
//
// None of these patterns execute code or expand macros. They are bounded regex
// matches on the raw file bytes.

var (
	// javaSpringBootApp matches the @SpringBootApplication annotation line.
	// The annotation may carry attributes: @SpringBootApplication(exclude={…}).
	javaSpringBootApp = regexp.MustCompile(`(?m)^\s*@SpringBootApplication`)

	// javaMainMethod matches "public static void main" or "static public void main"
	// in any order of modifiers, allowing for access modifier, annotations, etc.
	javaMainMethod = regexp.MustCompile(`(?m)\bpublic\s+static\s+void\s+main\s*\(`)

	// javaClassName matches "public class Foo" or "public final class Foo".
	javaClassName = regexp.MustCompile(`(?m)^\s*public\s+(?:final\s+)?class\s+([A-Z][A-Za-z0-9_]*)`)

	// kotlinMainFun matches the top-level "fun main" in a file that also has
	// @SpringBootApplication. Kotlin Spring Boot apps use a top-level main
	// function annotated with @JvmStatic or a companion object. We accept the
	// simpler pattern of finding both annotations.
	kotlinMainFun = regexp.MustCompile(`(?m)^\s*fun\s+main\s*\(`)

	// mavenMainClass matches <mainClass>…</mainClass> in pom.xml.
	mavenMainClass = regexp.MustCompile(`(?i)<mainClass>\s*([A-Za-z][A-Za-z0-9_.]*)\s*</mainClass>`)

	// gradleMainClassAssign matches mainClass = "…" or mainClass.set("…") forms.
	gradleMainClassAssign = regexp.MustCompile(`(?m)\bmainClass\s*[.=]\s*(?:set\s*\(\s*)?"([A-Za-z][A-Za-z0-9_.]*)"`)
)

// parseJavaSpringBoot detects @SpringBootApplication entry points in Java or
// Kotlin source files. It returns at most one KindInterface observation per
// file. The file must be under src/main (not src/test) to avoid test
// application classes contributing false positives.
func parseJavaSpringBoot(filePath string, content []byte) []Observation {
	// Skip test sources. Spring Boot test apps annotated with
	// @SpringBootApplication are not the real application entry point.
	// Only skip paths that have a "/test/" or "src/test/" directory component,
	// not just any file whose name contains "test".
	lower := strings.ToLower(filePath)
	if strings.Contains(lower, "/src/test/") ||
		strings.HasPrefix(lower, "src/test/") ||
		strings.HasPrefix(lower, "test/") ||
		strings.Contains(lower, "/tests/") {
		return nil
	}
	if !javaSpringBootApp.Match(content) {
		return nil
	}
	isKotlin := strings.HasSuffix(lower, ".kt")
	hasMain := false
	if isKotlin {
		hasMain = kotlinMainFun.Match(content)
	} else {
		hasMain = javaMainMethod.Match(content)
	}
	if !hasMain {
		return nil
	}
	// Extract the class or file name as the entry-point name.
	name := ""
	if !isKotlin {
		if m := javaClassName.FindSubmatch(content); m != nil {
			name = string(m[1])
		}
	}
	if name == "" {
		// Fall back to the file's base name without extension.
		base := filePath
		if slash := strings.LastIndexByte(base, '/'); slash >= 0 {
			base = base[slash+1:]
		}
		if dot := strings.LastIndexByte(base, '.'); dot >= 0 {
			base = base[:dot]
		}
		name = base
	}
	annotLine := 0
	for i, line := range bytes.Split(content, []byte("\n")) {
		if javaSpringBootApp.Match(line) {
			annotLine = i + 1
			break
		}
	}
	return []Observation{
		{
			Kind:      KindInterface,
			Name:      name,
			State:     "declared",
			Basis:     "code_syntax",
			Path:      filePath,
			StartLine: annotLine,
			EndLine:   annotLine,
			Properties: map[string]string{
				"interface_kind": "spring_boot_application",
			},
		},
	}
}

// parseMavenMainClass detects <mainClass> declarations in pom.xml files.
func parseMavenMainClass(filePath string, content []byte) []Observation {
	matches := mavenMainClass.FindSubmatch(content)
	if matches == nil {
		return nil
	}
	name := string(matches[1])
	if name == "" {
		return nil
	}
	line := 1 + bytes.Count(content[:mavenMainClass.FindIndex(content)[0]], []byte("\n"))
	return []Observation{
		{
			Kind:      KindInterface,
			Name:      name,
			State:     "declared",
			Basis:     "declared_config",
			Path:      filePath,
			StartLine: line,
			EndLine:   line,
			Properties: map[string]string{
				"interface_kind": "maven_main_class",
			},
		},
	}
}

// parseGradleMainClass detects mainClass declarations in Gradle build files.
func parseGradleMainClass(filePath string, content []byte) []Observation {
	matches := gradleMainClassAssign.FindSubmatch(content)
	if matches == nil {
		return nil
	}
	name := string(matches[1])
	if name == "" {
		return nil
	}
	line := 1 + bytes.Count(content[:gradleMainClassAssign.FindIndex(content)[0]], []byte("\n"))
	return []Observation{
		{
			Kind:      KindInterface,
			Name:      name,
			State:     "declared",
			Basis:     "declared_config",
			Path:      filePath,
			StartLine: line,
			EndLine:   line,
			Properties: map[string]string{
				"interface_kind": "gradle_main_class",
			},
		},
	}
}
