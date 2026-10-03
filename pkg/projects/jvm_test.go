package projects

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func TestMavenMultiModuleAndProperties(t *testing.T) {
	d := ParseJVM("services/pom.xml", []byte(`<project xmlns="http://maven.apache.org/POM/4.0.0">
 <modelVersion>4.0.0</modelVersion><groupId>example</groupId><artifactId>suite</artifactId><version>1</version><packaging>pom</packaging>
 <properties><java.version>21</java.version><maven.compiler.release>${java.version}</maven.compiler.release><component>api</component></properties>
 <modules><module>${component}</module><module>../shared</module><module>${external}</module><module>../../escape</module></modules>
 <build><plugins><plugin><artifactId>maven-compiler-plugin</artifactId><configuration><jdkToolchain><version>21</version></jdkToolchain></configuration></plugin><plugin><groupId>org.openapitools</groupId><artifactId>openapi-generator-maven-plugin</artifactId><version>7.0</version></plugin></plugins></build>
 <dependencies><dependency><groupId>example</groupId><artifactId>lib</artifactId><version>${external.version}</version></dependency></dependencies>
 <profiles><profile><id>extras</id><properties><component>extra</component></properties><modules><module>${component}</module></modules></profile></profiles>
 </project>`))
	if len(d.Projects) != 1 || len(d.Diagnostics) != 0 {
		t.Fatalf("parse: %+v", d)
	}
	p := d.Projects[0]
	if p.Root != "services" || p.ID != "services/pom.xml" || p.Kind != "maven" {
		t.Fatalf("identity: %+v", p)
	}
	if len(p.References) != 5 {
		t.Fatalf("references: %+v", p.References)
	}
	for i, want := range []struct{ target, state string }{{"services/api/pom.xml", "declared"}, {"shared/pom.xml", "declared"}, {"", "unresolved"}, {"", "unresolved"}, {"services/extra/pom.xml", "conditional"}} {
		if p.References[i].Target != want.target || p.References[i].State != want.state {
			t.Errorf("ref %d: %+v", i, p.References[i])
		}
	}
	if p.References[4].Condition != "Maven profile extras" {
		t.Fatalf("profile condition: %+v", p.References[4])
	}
	for _, want := range []Requirement{{Kind: "java-release", Value: "21", State: "declared"}, {Kind: "java-toolchain", Value: "21", State: "declared"}, {Kind: "code-generation", Value: "org.openapitools:openapi-generator-maven-plugin:7.0", State: "declared"}, {Kind: "maven-dependency", Value: "example:lib:${external.version}", State: "unresolved"}} {
		found := false
		for _, r := range p.Requirements {
			if r.Kind == want.Kind && r.Value == want.Value && r.State == want.State {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %+v in %+v", want, p.Requirements)
		}
	}
}

func TestMavenParentResolution(t *testing.T) {
	for _, tt := range []struct {
		name, relative, target string
		refs                   int
	}{{"default", "", "pom.xml", 1}, {"explicit", "<relativePath>../../parent/pom.xml</relativePath>", "", 1}, {"disabled", "<relativePath />", "", 0}, {"local", "<relativePath>../root</relativePath>", "root/pom.xml", 1}} {
		t.Run(tt.name, func(t *testing.T) {
			d := ParseJVM("child/pom.xml", []byte(`<project><parent><groupId>x</groupId><artifactId>root</artifactId><version>1</version>`+tt.relative+`</parent><artifactId>child</artifactId></project>`))
			refs := d.Projects[0].References
			if len(refs) != tt.refs {
				t.Fatalf("refs: %+v", refs)
			}
			if len(refs) > 0 && refs[0].Target != tt.target {
				t.Errorf("target: %+v", refs[0])
			}
		})
	}
}

func TestMavenSystemPathUsesOnlyConfinedLiteralOrBasedirPaths(t *testing.T) {
	doc := ParseJVM("modules/service/pom.xml", []byte(`<project>
  <properties><local.lib>lib/known.jar</local.lib></properties>
  <dependencies>
    <dependency><groupId>local</groupId><artifactId>known</artifactId><scope>system</scope><systemPath>${basedir}/lib/known.jar</systemPath></dependency>
    <dependency><groupId>local</groupId><artifactId>other</artifactId><scope>system</scope><systemPath>${local.lib}</systemPath></dependency>
		<dependency><groupId>local</groupId><artifactId>escaped</artifactId><scope>system</scope><systemPath>../../../outside.jar</systemPath></dependency>
    <dependency><groupId>local</groupId><artifactId>dynamic</artifactId><scope>system</scope><systemPath>${external.lib}</systemPath></dependency>
    <dependency><groupId>local</groupId><artifactId>ordinary</artifactId><scope>compile</scope><systemPath>lib/ignored.jar</systemPath></dependency>
  </dependencies>
</project>`))
	if len(doc.Projects) != 1 {
		t.Fatalf("project: %+v", doc)
	}
	refs := doc.Projects[0].References
	if len(refs) != 4 {
		t.Fatalf("artifact references: %+v", refs)
	}
	if refs[0].Target != "modules/service/lib/known.jar" || refs[0].State != "declared" || refs[0].Value != "${basedir}/lib/known.jar" {
		t.Fatalf("basedir target: %+v", refs[0])
	}
	if refs[1].Target != "modules/service/lib/known.jar" || refs[1].State != "declared" {
		t.Fatalf("static property target: %+v", refs[1])
	}
	if refs[2].Target != "" || refs[2].State != "unresolved" || refs[3].Target != "" || refs[3].State != "unresolved" {
		t.Fatalf("unsafe paths resolved: %+v", refs)
	}
	encoded, err := json.Marshal(doc)
	if err != nil || strings.Contains(string(encoded), `"scope"`) {
		t.Fatalf("internal Maven scope escaped its report: %s err=%v", encoded, err)
	}
}

func TestMavenDependencyScopeIsPrivateAndOnlyApplicationDependenciesAreTagged(t *testing.T) {
	doc := ParseJVM("pom.xml", []byte(`<project>
  <dependencies>
    <dependency><groupId>example</groupId><artifactId>same</artifactId><scope>test</scope></dependency>
    <dependency><groupId>example</groupId><artifactId>same</artifactId><scope>compile</scope></dependency>
  </dependencies>
  <dependencyManagement><dependencies><dependency><groupId>example</groupId><artifactId>managed</artifactId><scope>test</scope></dependency></dependencies></dependencyManagement>
  <build><plugins><plugin><dependencies><dependency><groupId>example</groupId><artifactId>plugin-only</artifactId><scope>test</scope></dependency></dependencies></plugin></plugins></build>
  <profiles><profile><dependencies><dependency><groupId>example</groupId><artifactId>profile-only</artifactId><scope>test</scope></dependency></dependencies></profile></profiles>
</project>`))
	if len(doc.Projects) != 1 {
		t.Fatalf("project: %+v", doc)
	}
	var same []Requirement
	profileSeen := false
	for _, req := range doc.Projects[0].Requirements {
		if req.Kind == "maven-dependency" && strings.Contains(req.Value, ":same") {
			same = append(same, req)
		}
		if strings.Contains(req.Value, "managed") || strings.Contains(req.Value, "plugin-only") {
			t.Fatalf("non-direct Maven declaration was parsed as an app dependency: %+v", req)
		}
		if strings.Contains(req.Value, "profile-only") {
			profileSeen = true
			if req.Scope != "test" || req.Condition == "" {
				t.Fatalf("profile dependency lost scope or profile condition: %+v", req)
			}
		}
	}
	if len(same) != 2 || same[0].Scope != "test" || same[1].Scope != "compile" || !profileSeen {
		t.Fatalf("direct duplicate coordinates lost scope or identity: %+v", same)
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"Scope"`) || strings.Contains(string(encoded), `"scope"`) {
		t.Fatalf("private scope metadata changed public JSON: %s", encoded)
	}
}

func TestMavenPluginSystemDependencyRetainsLocalArtifact(t *testing.T) {
	doc := ParseJVM("pom.xml", []byte(`<project><build><plugins><plugin><artifactId>generator</artifactId><dependencies><dependency><groupId>local</groupId><artifactId>generator-data</artifactId><scope>system</scope><systemPath>${basedir}/lib/generator-data.jar</systemPath></dependency></dependencies></plugin></plugins></build></project>`))
	if len(doc.Projects) != 1 || len(doc.Projects[0].References) != 1 {
		t.Fatalf("plugin system dependency was not retained: %+v", doc)
	}
	ref := doc.Projects[0].References[0]
	if ref.Kind != "local-artifact" || ref.Target != "lib/generator-data.jar" || ref.State != "declared" {
		t.Fatalf("plugin artifact path was not resolved: %+v", ref)
	}
}

func TestMavenRejectsMalformedAndForeignXML(t *testing.T) {
	for _, input := range []string{`<project>`, `<project/><project/>`, `<!DOCTYPE project [<!ENTITY x SYSTEM "file:///etc/passwd">]><project>&x;</project>`, strings.Repeat("<x>", 65) + strings.Repeat("</x>", 65), `<project xmlns="urn:other"><modules><module>wrong</module></modules></project>`} {
		d := ParseJVM("pom.xml", []byte(input))
		if len(d.Diagnostics) != 1 || len(d.Projects) != 0 {
			t.Errorf("unexpected acceptance: %+v", d)
		}
	}
	d := ParseJVM("pom.xml", []byte(`<project xmlns="http://maven.apache.org/POM/4.0.0" xmlns:foreign="urn:other"><modules><foreign:module>wrong</foreign:module><module>right</module></modules></project>`))
	if len(d.Projects[0].References) != 1 || d.Projects[0].References[0].Target != "right/pom.xml" {
		t.Fatalf("foreign namespace: %+v", d)
	}
}

func TestMavenSingleByteXMLCharsets(t *testing.T) {
	for _, tc := range []struct {
		name, encoding string
	}{
		{"latin1", "ISO-8859-1"},
		{"ascii", "US-ASCII"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`<?xml version="1.0" encoding="` + tc.encoding + `"?><project><artifactId>caf`)
			body = append(body, 0xe9)
			body = append(body, []byte(`</artifactId></project>`)...)
			if tc.name == "ascii" {
				body = []byte(`<?xml version="1.0" encoding="US-ASCII"?><project><artifactId>cafe</artifactId></project>`)
			}
			doc := ParseJVM("pom.xml", body)
			if len(doc.Projects) != 1 || len(doc.Diagnostics) != 0 || doc.Projects[0].Requirements[0].Value != "café" && tc.name == "latin1" {
				t.Fatalf("single-byte POM rejected or corrupted: %+v", doc)
			}
		})
	}
	bad := ParseJVM("pom.xml", []byte("<?xml version=\"1.0\" encoding=\"US-ASCII\"?><project><artifactId>caf\xe9</artifactId></project>"))
	if len(bad.Projects) != 0 || len(bad.Diagnostics) == 0 {
		t.Fatalf("invalid US-ASCII accepted: %+v", bad)
	}
}

func TestMavenPropertyCyclesAndUnsafePaths(t *testing.T) {
	d := ParseJVM("pom.xml", []byte(`<project><properties><a>${b}</a><b>${a}</b></properties><modules><module>${a}</module><module>/absolute</module><module>C:\outside</module><module>../escape</module></modules></project>`))
	for _, r := range d.Projects[0].References {
		if r.State != "unresolved" || r.Target != "" {
			t.Fatalf("unsafe resolution: %+v", r)
		}
	}
}

func TestGradleLiteralIncludesAndComments(t *testing.T) {
	input := `// include(":fake")
/* include(":also-fake") */
val example = "include(':quoted')"
include(":app", ":services:api")
include ':legacy'
if (enabled) {
 include(":conditional")
}
include(dynamicName)
include(":prefix" + suffix)
`
	d := ParseJVM("platform/settings.gradle.kts", []byte(input))
	if len(d.Projects) != 0 || len(d.References) != 3 {
		t.Fatalf("unexpected observations: %+v", d)
	}
	for i, target := range []string{"platform/app", "platform/services/api", "platform/legacy"} {
		if d.References[i].Target != target || d.References[i].State != "conditional" {
			t.Errorf("reference: %+v", d.References[i])
		}
	}
	if d.Requirements[0].State != "unresolved" {
		t.Fatalf("script treated as evaluated: %+v", d)
	}
}

func TestGradleUnknownSyntaxDoesNotGuess(t *testing.T) {
	for _, input := range []string{`val example = """include(":fake")"""`, "/* unterminated", `val regex = /include(":fake")/`} {
		d := ParseJVM("settings.gradle", []byte(input))
		if len(d.References) != 0 || len(d.Diagnostics) == 0 {
			t.Fatalf("unexpected parse: %+v", d)
		}
	}
	d := ParseJVM("build.gradle", []byte("sourceCompatibility = '17'\ntargetCompatibility = 17\n"))
	if len(d.Projects) != 1 || len(d.Projects[0].Requirements) != 3 {
		t.Fatalf("build requirements: %+v", d)
	}
}

func TestGradlePropertiesDoNotExposeCredentials(t *testing.T) {
	input := []byte("distributionUrl=https\\://user:password@example.test/gradle-8.10.2-bin.zip?token=secret\nregistryPassword=secret\norg.gradle.jvmargs=-Dpassword=secret\n")
	d := ParseJVM("gradle/wrapper/gradle-wrapper.properties", input)
	if len(d.Requirements) != 1 || d.Requirements[0].Value != "8.10.2" {
		t.Fatalf("wrapper: %+v", d)
	}
	for _, r := range d.Requirements {
		if strings.Contains(r.Value, "secret") || strings.Contains(r.Value, "password") {
			t.Fatalf("credential leaked: %+v", r)
		}
	}
}

func TestMavenToolchainsNamespace(t *testing.T) {
	d := ParseJVM("toolchains.xml", []byte(`<toolchains xmlns="http://maven.apache.org/TOOLCHAINS/1.1.0"><toolchain><type>jdk</type><provides><version>21</version></provides><configuration><jdkHome>/private/machine/path</jdkHome></configuration></toolchain></toolchains>`))
	if len(d.Requirements) != 1 || d.Requirements[0].Value != "21" {
		t.Fatalf("toolchain: %+v", d)
	}
}

func TestJVMUnsupportedManifest(t *testing.T) {
	if IsJVM("README.md") {
		t.Fatal("readme supported")
	}
	d := ParseJVM("README.md", []byte("# example"))
	if len(d.Projects)+len(d.Requirements)+len(d.Diagnostics) != 0 {
		t.Fatalf("unexpected document: %+v", d)
	}
}

func TestGradleProjectDirOverrides(t *testing.T) {
	for _, tt := range []struct{ script, target, state string }{
		{"include(':api')\nproject(':api').projectDir = file('components/api')\n", "suite/components/api", "conditional"},
		{"include(':api')\nproject(':api').projectDir = file(rootDirFromEnvironment)\n", "", "unresolved"},
		{"include(':api')\nproject(':api').projectDir = file('../../outside')\n", "", "unresolved"},
		{"include(':api')\nif (custom) {\nproject(':api').projectDir = file('custom')\n}\n", "", "unresolved"},
	} {
		d := ParseJVM("suite/settings.gradle", []byte(tt.script))
		if len(d.References) != 1 || d.References[0].Target != tt.target || d.References[0].State != tt.state {
			t.Errorf("script %q: %+v", tt.script, d)
		}
	}
}

func TestGradleRootProjectName(t *testing.T) {
	// settings.gradle: rootProject.name read for various syntaxes
	for _, tt := range []struct {
		name    string
		script  string
		wantReq string // expected gradle-root-name value, or "" for none
	}{
		{"groovy single-quote", "rootProject.name = 'spring-petclinic'\n", "spring-petclinic"},
		{"groovy double-quote", `rootProject.name = "my-project"` + "\n", "my-project"},
		{"kotlin kts", "rootProject.name = \"my-app\"\n", "my-app"},
		{"with include", "rootProject.name = 'app'\ninclude(':lib')\n", "app"},
		{"dynamic ignored", "rootProject.name = someVar\n", ""},
		{"nested ignored", "allprojects {\nrootProject.name = 'inner'\n}\n", ""},
	} {
		d := ParseJVM("settings.gradle", []byte(tt.script))
		var got string
		for _, r := range d.Requirements {
			if r.Kind == "gradle-root-name" {
				got = r.Value
				break
			}
		}
		if got != tt.wantReq {
			t.Errorf("%s: got gradle-root-name=%q, want %q", tt.name, got, tt.wantReq)
		}
	}
	// build.gradle: rootProject.name is not a build-gradle token, should not be emitted
	d := ParseJVM("build.gradle", []byte("rootProject.name = 'build'\n"))
	for _, r := range d.Requirements {
		if r.Kind == "gradle-root-name" {
			t.Errorf("build.gradle emitted gradle-root-name unexpectedly: %+v", r)
		}
	}
}

func TestGradleToolchainLiterals(t *testing.T) {
	d := ParseJVM("build.gradle.kts", []byte(`java {
 toolchain {
  languageVersion = JavaLanguageVersion.of(21)
 }
}
// languageVersion = JavaLanguageVersion.of(8)
`))
	if len(d.Projects) != 1 || len(d.Projects[0].Requirements) != 2 {
		t.Fatalf("toolchain: %+v", d)
	}
	r := d.Projects[0].Requirements[1]
	if r.Kind != "java-toolchain" || r.Value != "21" || r.State != "conditional" {
		t.Fatalf("toolchain: %+v", r)
	}
	d = ParseJVM("build.gradle.kts", []byte(`java { toolchain { languageVersion = JavaLanguageVersion.of(21) + custom } }`))
	if len(d.Projects[0].Requirements) != 1 {
		t.Fatalf("dynamic toolchain: %+v", d)
	}
}

func TestGradleIgnoresMultilineCallsAndNestedComments(t *testing.T) {
	d := ParseJVM("settings.gradle.kts", []byte(`/* outer /* nested */
include(":comment")
*/
customFunction(
include(":argument")
)
include(":actual")
`))
	if len(d.References) != 1 || d.References[0].Value != ":actual" {
		t.Fatalf("false declarations: %+v", d)
	}
}

func TestMavenPropertyExpansionBoundedBeforeAllocation(t *testing.T) {
	properties := map[string]string{"payload": strings.Repeat("x", 65535)}
	expression := strings.Repeat("${payload}", 3000)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	value, resolved := jvmResolve(expression, properties)
	runtime.ReadMemStats(&after)
	if resolved || value != "" {
		t.Fatal("oversized expansion was accepted")
	}
	// A build-then-check implementation allocates about 196 MiB for this
	// 30 KiB expression. Allow ample runtime overhead while rejecting that bug.
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 4<<20 {
		t.Fatalf("expansion allocated %d bytes before rejecting the value", allocated)
	}
	if _, ok := jvmResolve(strings.Repeat("x", 65537), nil); ok {
		t.Fatal("oversized initial value accepted")
	}
	if value, ok := jvmResolve("${exact}", map[string]string{"exact": strings.Repeat("x", 65536)}); !ok || len(value) != 65536 {
		t.Fatal("exact limit rejected")
	}
	if value, ok := jvmResolve("prefix-${suffix}", map[string]string{"suffix": "value"}); !ok || value != "prefix-value" {
		t.Fatalf("ordinary replacement: %q, %v", value, ok)
	}
}

func TestJVMEmptyModuleDeclarationsAreDiagnostics(t *testing.T) {
	for _, input := range []struct{ name, content string }{{"pom.xml", "<project><modules><module/></modules></project>"}, {"settings.gradle", "include('')"}} {
		d := ParseJVM(input.name, []byte(input.content))
		if len(d.Diagnostics) != 1 {
			t.Fatalf("empty module not diagnosed: %+v", d)
		}
		if len(d.References) != 0 {
			t.Fatalf("empty configuration reference: %+v", d)
		}
		for _, p := range d.Projects {
			if len(p.References) != 0 {
				t.Fatalf("empty project reference: %+v", p)
			}
		}
	}
}

func TestJVMAggregateExpansionBudget(t *testing.T) {
	content := `<project><properties><large>` + strings.Repeat("x", 60000) + `</large></properties><modules>` + strings.Repeat(`<module>${large}</module>`, 10000) + `</modules></project>`
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	d := ParseJVM("pom.xml", []byte(content))
	runtime.ReadMemStats(&after)
	if len(d.Diagnostics) != 1 || d.Diagnostics[0].Code != "declaration-budget-exceeded" {
		t.Fatalf("budget diagnostic: %+v", d.Diagnostics)
	}
	if len(d.Projects) != 1 || len(d.Projects[0].References) > 18 {
		t.Fatalf("unbounded expanded references: %d", len(d.Projects[0].References))
	}
	total := 0
	for _, ref := range d.Projects[0].References {
		total += len(ref.Kind) + len(ref.Value) + len(ref.Target) + len(ref.State) + len(ref.Evidence) + len(ref.Condition)
	}
	if total > 1<<20 {
		t.Fatalf("retained %d observation bytes", total)
	}
	// The unsafe implementation expands this small input to roughly 600 MiB.
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 32<<20 {
		t.Fatalf("aggregate expansion allocated %d bytes", allocated)
	}
}

func TestJVMObservationCountBudget(t *testing.T) {
	for _, input := range []struct{ name, content string }{
		{"pom.xml", `<project><modules>` + strings.Repeat(`<module>a</module>`, 5000) + `</modules></project>`},
		{"settings.gradle", "include(" + strings.Repeat("':a',", 4999) + "':a')"},
		{"gradle.properties", strings.Repeat("org.gradle.java.installations.auto-download=true\n", 5000)},
	} {
		d := ParseJVM(input.name, []byte(input.content))
		count := len(d.References) + len(d.Requirements)
		for _, p := range d.Projects {
			count += len(p.Requirements) + len(p.References)
		}
		if count > 4096 || len(d.Diagnostics) != 1 || d.Diagnostics[0].Code != "declaration-budget-exceeded" {
			t.Fatalf("%s: count=%d diagnostics=%+v", input.name, count, d.Diagnostics)
		}
	}
}

func TestMaven41DeclarationsAndSubprojects(t *testing.T) {
	d := ParseJVM("suite/pom.xml", []byte(`<project xmlns="http://maven.apache.org/POM/4.1.0" xmlns:foreign="urn:foreign">
 <modelVersion>4.1.0</modelVersion><parent><groupId>example</groupId><artifactId>parent</artifactId></parent>
 <artifactId>suite</artifactId><properties><child>api</child><maven.compiler.release>21</maven.compiler.release><foreign:maven.compiler.target>8</foreign:maven.compiler.target></properties>
 <subprojects><subproject>${child}</subproject><foreign:subproject>foreign</foreign:subproject><subproject xmlns="http://maven.apache.org/POM/4.0.0">mixed-namespace</subproject></subprojects>
 <profiles><profile><id>optional</id><subprojects><subproject>extra</subproject></subprojects></profile></profiles>
 </project>`))
	if len(d.Diagnostics) != 0 || len(d.Projects) != 1 {
		t.Fatalf("Maven4.1: %+v", d)
	}
	p := d.Projects[0]
	if len(p.References) != 3 {
		t.Fatalf("references: %+v", p.References)
	}
	for _, ref := range p.References {
		switch ref.Target {
		case "suite/api/pom.xml":
			if ref.State != "declared" {
				t.Fatalf("literal subproject: %+v", ref)
			}
		case "suite/extra/pom.xml":
			if ref.State != "conditional" || ref.Condition != "Maven profile optional" {
				t.Fatalf("profile subproject: %+v", ref)
			}
		case "pom.xml":
			if ref.Kind != "parent" || ref.State != "conditional" || !strings.Contains(ref.Condition, "reactor") {
				t.Fatalf("unevaluated parent lookup: %+v", ref)
			}
		default:
			t.Fatalf("foreign namespace contributed reference: %+v", ref)
		}
	}
	release := false
	for _, req := range p.Requirements {
		if req.Kind == "java-release" && req.Value == "21" {
			release = true
		}
		if req.Kind == "java-target" {
			t.Fatalf("foreign property observed: %+v", req)
		}
	}
	if !release {
		t.Fatal("Maven4.1 compiler declaration absent")
	}
	unsupported := ParseJVM("pom.xml", []byte(`<project xmlns="http://maven.apache.org/POM/4.2.0"><subprojects><subproject>not-yet-supported</subproject></subprojects></project>`))
	if len(unsupported.Projects) != 0 || len(unsupported.Diagnostics) != 1 || unsupported.Diagnostics[0].Code != "unsupported_manifest" {
		t.Fatalf("newer namespace silently accepted: %+v", unsupported)
	}
}

func TestMavenUnsupportedEncodingDiagnostic(t *testing.T) {
	d := ParseJVM("pom.xml", []byte("<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><project><artifactId>caf\xe9</artifactId></project>"))
	if len(d.Projects) != 1 || len(d.Diagnostics) != 0 {
		t.Fatalf("ISO-8859-1 rejected: %+v", d)
	}
	found := false
	for _, req := range d.Projects[0].Requirements {
		if req.Kind == "maven-artifactId" && req.Value == "café" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ISO-8859-1 text corrupted: %+v", d.Projects[0].Requirements)
	}
}
