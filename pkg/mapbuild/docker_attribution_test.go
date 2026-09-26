package mapbuild

import (
	"context"
	"testing"

	"github.com/war-and-code/dircue/pkg/deployables"
	"github.com/war-and-code/dircue/pkg/mapdoc"
)

// attributeRootDockerfile runs real Dockerfile and POM text through the
// deployables observer and addDeployables, so reference sorting and parsing
// are exercised together. It returns the component name the root Dockerfile
// builds, or "" when it builds none.
func attributeRootDockerfile(t *testing.T, dockerfile string, extra map[string]string) string {
	t.Helper()
	files := map[string]string{
		"pom.xml":        `<project><modelVersion>4.0.0</modelVersion><groupId>ex</groupId><artifactId>parent</artifactId><version>1</version><packaging>pom</packaging><modules><module>webapp</module></modules></project>`,
		"webapp/pom.xml": `<project><modelVersion>4.0.0</modelVersion><groupId>ex</groupId><artifactId>webapp</artifactId><version>1</version><packaging>war</packaging><build><finalName>ROOT</finalName></build></project>`,
		"Dockerfile":     dockerfile,
	}
	for name, body := range extra {
		files[name] = body
	}
	var candidates []deployables.Candidate
	for name, body := range files {
		content := []byte(body)
		candidates = append(candidates, deployables.Candidate{Path: name, Size: int64(len(content)), Read: func(context.Context, int64) ([]byte, int64, error) {
			return content, int64(len(content)), nil
		}})
	}
	report, err := deployables.Observe(context.Background(), candidates, deployables.Options{})
	if err != nil {
		t.Fatal(err)
	}
	doc := mapdoc.New()
	parent := mapdoc.NewNode(mapdoc.NodeComponent, []string{".", "pom.xml"}, "maven")
	parent.Name = "parent"
	parent.Properties = map[string]string{"root": ".", "ecosystem": "maven"}
	webapp := mapdoc.NewNode(mapdoc.NodeComponent, []string{"webapp", "webapp/pom.xml"}, "maven")
	webapp.Name = "webapp"
	webapp.Properties = map[string]string{"root": "webapp", "ecosystem": "maven"}
	doc.Nodes = append(doc.Nodes, parent, webapp)
	if _, ok := extra["package.json"]; ok {
		node := mapdoc.NewNode(mapdoc.NodeComponent, []string{".", "package.json"}, "npm")
		node.Name = "(root)"
		node.Properties = map[string]string{"root": ".", "ecosystem": "npm"}
		doc.Nodes = append(doc.Nodes, node)
	}
	names := map[string]string{}
	for _, n := range doc.Nodes {
		names[n.ID] = n.Name
	}
	addDeployables(&doc, report)
	dockerID := mapdoc.NewNode(mapdoc.NodeDeployable, []string{"Dockerfile"}, "dockerfile:container_build:(root)").ID
	for _, edge := range doc.Edges {
		if edge.From == dockerID && edge.Type == mapdoc.EdgeBuilds && len(edge.Coverage.Reasons) == 1 && edge.Coverage.Reasons[0] == "dockerfile_copy_source_matches_maven_archive" {
			return names[edge.To]
		}
	}
	return ""
}

const stagedBuild = "FROM maven:3.9 AS build\nWORKDIR /src\nCOPY . .\nRUN mvn package\n"

func TestDockerFinalStageIsTheLastFromNotTheLastSortedImage(t *testing.T) {
	// References are sorted by kind and value before mapbuild sees them, so a
	// final image whose name sorts before "maven" once lost the attribution.
	for _, image := range []string{"tomcat:10", "jetty:12", "eclipse-temurin:17", "amazoncorretto:21"} {
		t.Run(image, func(t *testing.T) {
			dockerfile := stagedBuild + "FROM " + image + "\nCOPY --from=build /src/webapp/target/ROOT.war /app/ROOT.war\n"
			if got := attributeRootDockerfile(t, dockerfile, nil); got != "webapp" {
				t.Fatalf("final stage %s: attributed %q, want webapp", image, got)
			}
		})
	}
}

func TestDockerAttributionFailsClosedOnUnvettedWrites(t *testing.T) {
	final := "FROM tomcat:10\nCOPY --from=build /src/webapp/target/ROOT.war /usr/local/tomcat/webapps/ROOT.war\n"
	for _, test := range []struct{ name, dockerfile string }{
		{"COPY from context overwrites the built archive", stagedBuild + "COPY vendor/ROOT.war /src/webapp/target/ROOT.war\n" + final},
		{"COPY --from overwrites the built archive", "FROM alpine:3 AS fetch\nRUN wget -O /ROOT.war https://example.invalid/ROOT.war\n" + stagedBuild + "COPY --from=fetch /ROOT.war /src/webapp/target/ROOT.war\n" + final},
		{"COPY into an ancestor directory", stagedBuild + "COPY vendor /src/webapp\n" + final},
		{"COPY with an unreadable source", stagedBuild + "COPY ${ARTIFACT} /src/webapp/target/\n" + final},
		{"semicolon continuation line", "FROM maven:3.9 AS build\nWORKDIR /src\nCOPY . .\nRUN mvn package \\\n    ; curl -o /src/webapp/target/ROOT.war https://example.invalid/ROOT.war\n" + final},
		{"or continuation line", "FROM maven:3.9 AS build\nWORKDIR /src\nCOPY . .\nRUN mvn package \\\n    || curl -o /src/webapp/target/ROOT.war https://example.invalid/ROOT.war\n" + final},
		{"comment inside continuation", "FROM maven:3.9 AS build\nWORKDIR /src\nCOPY . .\nRUN mvn package \\\n# fetch instead\n    && curl -o /src/webapp/target/ROOT.war https://example.invalid/ROOT.war\n" + final},
		{"background job", stagedBuild + "RUN mvn -q validate & curl -o /src/webapp/target/ROOT.war https://example.invalid/ROOT.war\n" + final},
		{"Maven plugin goal downloads the archive", "FROM maven:3.9 AS build\nWORKDIR /src\nCOPY . .\nRUN mvn dependency:copy -Dartifact=com.example:webapp:1:war -DoutputDirectory=/src/webapp/target\n" + final},
		{"Maven plugin goal on a continuation line", "FROM maven:3.9 AS build\nWORKDIR /src\nCOPY . .\nRUN mvn \\\n    dependency:copy -Dartifact=com.example:webapp:1:war -DoutputDirectory=/src/webapp/target\n" + final},
		{"final-stage COPY replaces the archive", stagedBuild + final + "COPY vendor/ROOT.war /usr/local/tomcat/webapps/\n"},
		{"plugin goal smuggled through an inline assignment", "FROM maven:3.9 AS build\nWORKDIR /src\nCOPY . .\nRUN GOAL=\"dependency:copy\" mvn $GOAL -DoutputDirectory=/src/webapp/target/\n" + final},
		{"plugin goal smuggled through an ARG default", "FROM maven:3.9 AS build\nARG GOAL=dependency:copy\nWORKDIR /src\nCOPY . .\nRUN mvn ${GOAL} -DoutputDirectory=/src/webapp/target/\n" + final},
		{"plugin goal smuggled through an ENV value", "FROM maven:3.9 AS build\nENV GOALS=\"clean dependency:copy\"\nWORKDIR /src\nCOPY . .\nRUN mvn $GOALS -DoutputDirectory=/src/webapp/target/\n" + final},
		{"variable glued to a plugin goal", "FROM maven:3.9 AS build\nWORKDIR /src\nCOPY . .\nRUN mvn $PLUGIN:copy -DoutputDirectory=/src/webapp/target/\n" + final},
		{"unknown bare word passed to Maven", "FROM maven:3.9 AS build\nWORKDIR /src\nCOPY . .\nRUN mvn dependency package\n" + final},
		{"directory source with a trailing slash", stagedBuild + "COPY vendor.d/ /src/webapp/target/\n" + final},
		{"directory-like source without an extension allowlist", stagedBuild + "COPY vendor.d /src/webapp/target/\n" + final},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := attributeRootDockerfile(t, test.dockerfile, nil); got != "" {
				t.Fatalf("attributed %q; want no archive attribution", got)
			}
		})
	}
}

func TestDockerAttributionKeepsKnownBuildSteps(t *testing.T) {
	final := "FROM tomcat:10\nCOPY --from=build /src/webapp/target/ROOT.war /usr/local/tomcat/webapps/\n"
	for _, test := range []struct{ name, dockerfile string }{
		{"OS packages between context copy and build", "FROM maven:3.9 AS build\nWORKDIR /src\nCOPY . .\nRUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends git\nRUN mvn package\n" + final},
		{"Maven options and variables", "FROM maven:3.9 AS build\nWORKDIR /src\nCOPY . .\nRUN mvn -B -pl org.example:webapp -am $MVN_ARGS clean package\n" + final},
		{"Maven options on continuation lines", "FROM maven:3.9 AS build\nWORKDIR /src\nCOPY . .\nRUN mvn clean package \\\n    -DskipTests \\\n    -B\n" + final},
		{"unrelated file copied beside the archive", stagedBuild + "COPY settings.xml /src/webapp/\n" + final},
		{"static ARG default with lifecycle phases", "FROM maven:3.9 AS build\nARG MVN_ARGS='clean install -DskipTests'\nARG MVN_SETTINGS=\"-s /usr/share/maven/ref/settings-docker.xml\"\nWORKDIR /src\nCOPY . .\nRUN mvn $MVN_SETTINGS $MVN_ARGS\n" + final},
		{"ampersand inside a continued URL argument", "FROM maven:3.9 AS build\nWORKDIR /src\nCOPY . .\nRUN mvn package \\\n    -Dsite.url=https://example.invalid/?a=1&b=2\n" + final},
		{"later final-stage copy into a subdirectory", stagedBuild + "FROM tomcat:10\nRUN mkdir -p /opt/app\nCOPY --from=build /src/webapp/target/ROOT.war /opt/app\nCOPY conf /opt/app/conf\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := attributeRootDockerfile(t, test.dockerfile, nil); got != "webapp" {
				t.Fatalf("attributed %q; want webapp", got)
			}
		})
	}
}

func TestDockerAttributionIgnoresOtherEcosystemsAtArchiveRoot(t *testing.T) {
	// A package.json beside the WAR module's pom.xml is a separate component.
	// It must not make the Maven archive's owner ambiguous.
	extra := map[string]string{
		"webapp/pom.xml": `<project><modelVersion>4.0.0</modelVersion><groupId>ex</groupId><artifactId>webapp</artifactId><version>1</version><packaging>war</packaging><build><finalName>ROOT</finalName></build></project>`,
		"package.json":   `{"name":"ui","version":"1.0.0"}`,
	}
	// Place the WAR at the repository root for this case.
	extra["pom.xml"] = `<project><modelVersion>4.0.0</modelVersion><groupId>ex</groupId><artifactId>parent</artifactId><version>1</version><packaging>war</packaging><build><finalName>app</finalName></build></project>`
	got := attributeRootDockerfile(t, "FROM tomcat:10\nCOPY target/app.war /usr/local/tomcat/webapps/ROOT.war\n", extra)
	if got != "parent" {
		t.Fatalf("attributed %q; want the root Maven component", got)
	}
}
