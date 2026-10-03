package registries

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
)

const (
	mavenSettingsNS10 = "http://maven.apache.org/SETTINGS/1.0.0"
	mavenSettingsNS11 = "http://maven.apache.org/SETTINGS/1.1.0"
	mavenSettingsNS12 = "http://maven.apache.org/SETTINGS/1.2.0"
	xsiNamespace      = "http://www.w3.org/2001/XMLSchema-instance"
)

type mavenEntry struct {
	fields       map[string]string
	counts       map[string]int
	duplicate    bool
	repositories []mavenRepositoryEntry
}

type mavenRepositoryEntry struct {
	section string
	entry   *mavenEntry
}

func newMavenEntry() *mavenEntry {
	return &mavenEntry{fields: map[string]string{}, counts: map[string]int{}}
}

type mavenFrame struct {
	name  string
	entry *mavenEntry
}

// parseMavenSettings retains only bounded, allowlisted declaration fields.
// It deliberately ignores servers, proxies, localRepository and properties.
func parseMavenSettings(c *Configuration, content []byte) {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	stack := make([]mavenFrame, 0, 8)
	rootSeen, closed := false, false
	rootNS := ""
	tokens := 0
	var capture strings.Builder
	captureField := ""
	captureOverflow := false
	captureNested := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			c.fail("invalid_xml", "invalid")
			return
		}
		tokens++
		if tokens > MaxXMLTokens {
			c.fail("xml_token_limit", "incomplete")
			return
		}
		switch node := token.(type) {
		case xml.Directive:
			c.fail("unsupported_xml_directive", "unsupported")
			return
		case xml.ProcInst:
			if node.Target != "xml" || rootSeen {
				c.fail("unsupported_xml_instruction", "unsupported")
				return
			}
			end := bytes.Index(content, []byte("?>"))
			if tokens != 1 || end < 0 || end > 512 || !xmlDeclaration.Match(content[:end+2]) {
				c.fail("unsupported_xml_declaration", "unsupported")
				return
			}
		case xml.StartElement:
			if captureField != "" {
				captureNested = true
			}
			if closed {
				c.fail("invalid_xml", "invalid")
				return
			}
			if len(stack) == 0 {
				if rootSeen || node.Name.Local != "settings" || !supportedMavenNamespace(node.Name.Space) {
					c.fail("unsupported_xml_root", "unsupported")
					return
				}
				rootSeen = true
				rootNS = node.Name.Space
			} else if node.Name.Space != rootNS {
				c.fail("unsupported_xml_namespace", "unsupported")
				return
			}
			if len(stack)+1 > MaxXMLDepth {
				c.fail("xml_depth_limit", "incomplete")
				return
			}
			if len(node.Attr) > 0 {
				for _, attr := range node.Attr {
					if attr.Name.Space != "xmlns" && attr.Name.Local != "xmlns" && !(attr.Name.Space == xsiNamespace && attr.Name.Local == "schemaLocation") {
						c.omit("unsupported_xml_attribute")
						break
					}
				}
			}
			name := node.Name.Local
			frame := mavenFrame{name: name}
			pathNames := appendFrameNames(stack, name)
			switch strings.Join(pathNames, "/") {
			case "settings/mirrors/mirror", "settings/profiles/profile", "settings/profiles/profile/repositories/repository", "settings/profiles/profile/pluginRepositories/pluginRepository":
				frame.entry = newMavenEntry()
			}
			if isMavenCapturePath(pathNames) {
				capture.Reset()
				captureField = name
				captureOverflow = false
				captureNested = false
			}
			if len(stack) == 1 && name != "mirrors" && name != "profiles" && name != "activeProfiles" && name != "localRepository" && name != "interactiveMode" && name != "offline" && name != "pluginGroups" && name != "servers" && name != "proxies" {
				c.omit("unsupported_maven_section")
			}
			if len(stack) == 1 && (name == "servers" || name == "proxies" || name == "localRepository" || name == "pluginGroups") {
				c.omit("unsupported_maven_section")
			}
			if len(stack) == 2 && stack[1].name == "profiles" && name != "profile" {
				c.omit("unsupported_maven_profile_entry")
			}
			if len(stack) == 3 && stack[1].name == "profiles" && stack[2].name == "profile" && name != "id" && name != "activation" && name != "properties" && name != "repositories" && name != "pluginRepositories" {
				c.omit("unsupported_maven_profile_entry")
			}
			if len(stack) == 3 && stack[1].name == "profiles" && stack[2].name == "profile" && (name == "activation" || name == "properties") {
				c.omit("unsupported_maven_profile_entry")
			}
			if (len(stack) == 3 && stack[1].name == "mirrors" && stack[2].name == "mirror" && name != "id" && name != "mirrorOf" && name != "url") ||
				(len(stack) == 5 && stack[1].name == "profiles" && stack[4].name == "repository" && name != "id" && name != "url") ||
				(len(stack) == 5 && stack[1].name == "profiles" && stack[4].name == "pluginRepository" && name != "id" && name != "url") {
				c.omit("unsupported_maven_registry_field")
			}
			stack = append(stack, frame)
		case xml.EndElement:
			if len(stack) == 0 || stack[len(stack)-1].name != node.Name.Local || node.Name.Space != rootNS {
				c.fail("invalid_xml", "invalid")
				return
			}
			pathNames := frameNames(stack)
			if captureField == node.Name.Local && isMavenCapturePath(pathNames) {
				text := strings.TrimSpace(capture.String())
				if captureOverflow {
					c.omit("maven_text_limit")
				} else if captureNested {
					c.omit("unsupported_maven_nested_field")
				} else if len(stack) >= 2 {
					parent := &stack[len(stack)-2]
					if parent.entry != nil {
						parent.entry.counts[node.Name.Local]++
						if parent.entry.counts[node.Name.Local] > 1 {
							parent.entry.duplicate = true
						} else {
							parent.entry.fields[node.Name.Local] = text
						}
					} else if node.Name.Local == "activeProfile" {
						c.add(qualify(c, Declaration{Section: "activeProfiles", Operation: "add", Semantics: "declared", Applicability: "unresolved", Name: label(text, "name")}))
					}
				}
				captureField = ""
			}
			frame := stack[len(stack)-1]
			if frame.entry != nil {
				switch {
				case len(pathNames) == 3 && pathNames[1] == "mirrors" && pathNames[2] == "mirror":
					emitMavenMirror(c, frame.entry)
				case len(pathNames) == 3 && pathNames[1] == "profiles" && pathNames[2] == "profile":
					if frame.entry.duplicate {
						c.omit("duplicate_maven_field")
					} else {
						id := frame.entry.fields["id"]
						c.add(qualify(c, Declaration{Section: "profiles", Operation: "add", Semantics: "declared", Applicability: "unresolved", Name: label(id, "name")}))
					}
					profileID := frame.entry.fields["id"]
					if frame.entry.duplicate {
						profileID = ""
					}
					for _, repo := range frame.entry.repositories {
						emitMavenRepository(c, repo.entry, repo.section, profileID)
					}
				case len(pathNames) == 5 && pathNames[1] == "profiles" && pathNames[2] == "profile" && pathNames[3] == "repositories":
					queueMavenRepository(c, stack, frame.entry, "repositories")
				case len(pathNames) == 5 && pathNames[1] == "profiles" && pathNames[2] == "profile" && pathNames[3] == "pluginRepositories":
					queueMavenRepository(c, stack, frame.entry, "pluginRepositories")
				}
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				closed = true
			}
		case xml.CharData:
			if len(stack) == 0 && strings.TrimSpace(string(node)) != "" {
				c.fail("invalid_xml", "invalid")
				return
			}
			if captureField != "" {
				if capture.Len()+len(node) > MaxMavenTextBytes {
					captureOverflow = true
				} else if !captureOverflow {
					capture.Write(node)
				}
			}
		}
	}
	if !rootSeen || !closed || len(stack) != 0 {
		c.fail("invalid_xml", "invalid")
	}
}

func supportedMavenNamespace(uri string) bool {
	return uri == "" || uri == mavenSettingsNS10 || uri == mavenSettingsNS11 || uri == mavenSettingsNS12
}

func appendFrameNames(stack []mavenFrame, name string) []string {
	names := make([]string, 0, len(stack)+1)
	for _, frame := range stack {
		names = append(names, frame.name)
	}
	return append(names, name)
}

func frameNames(stack []mavenFrame) []string {
	names := make([]string, 0, len(stack))
	for _, frame := range stack {
		names = append(names, frame.name)
	}
	return names
}

func isMavenCapturePath(p []string) bool {
	joined := strings.Join(p, "/")
	switch joined {
	case "settings/mirrors/mirror/id", "settings/mirrors/mirror/mirrorOf", "settings/mirrors/mirror/url",
		"settings/profiles/profile/id", "settings/profiles/profile/repositories/repository/id", "settings/profiles/profile/repositories/repository/url",
		"settings/profiles/profile/pluginRepositories/pluginRepository/id", "settings/profiles/profile/pluginRepositories/pluginRepository/url",
		"settings/activeProfiles/activeProfile":
		return true
	default:
		return false
	}
}

func emitMavenMirror(c *Configuration, entry *mavenEntry) {
	if entry.duplicate {
		c.omit("duplicate_maven_field")
		return
	}
	d := Declaration{Section: "mirrors", Operation: "add", Semantics: "declared", Applicability: "unresolved", Name: label(entry.fields["id"], "name"), Pattern: label(entry.fields["mirrorOf"], "pattern"), Endpoint: endpoint(entry.fields["url"])}
	c.add(qualify(c, d))
}

func queueMavenRepository(c *Configuration, stack []mavenFrame, entry *mavenEntry, section string) {
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].name == "profile" && stack[i].entry != nil {
			stack[i].entry.repositories = append(stack[i].entry.repositories, mavenRepositoryEntry{section: section, entry: entry})
			return
		}
	}
	c.omit("unsupported_maven_repository_scope")
}

func emitMavenRepository(c *Configuration, entry *mavenEntry, section, profileID string) {
	if entry.duplicate {
		c.omit("duplicate_maven_field")
		return
	}
	d := Declaration{Section: section, Operation: "add", Semantics: "declared", Applicability: "unresolved", Name: label(entry.fields["id"], "name"), Endpoint: endpoint(entry.fields["url"])}
	if profileID != "" {
		d.Scope = label(profileID, "name")
	}
	c.add(qualify(c, d))
}
