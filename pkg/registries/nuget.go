package registries

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
)

func parseXML(c *Configuration, content []byte) {
	preparePatterns()
	decoder := xml.NewDecoder(bytes.NewReader(content))
	stack := []string{}
	rootSeen, closed := false, false
	mappingName := ""
	tokens := 0
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
			if closed {
				c.fail("invalid_xml", "invalid")
				return
			}
			if node.Name.Space != "" {
				c.fail("unsupported_xml_namespace", "unsupported")
				return
			}
			name := strings.ToLower(node.Name.Local)
			attrs := map[string]string{}
			for _, a := range node.Attr {
				if a.Name.Space != "" || a.Name.Local == "xmlns" {
					c.fail("unsupported_xml_namespace", "unsupported")
					return
				}
				key := strings.ToLower(a.Name.Local)
				// NuGet stores attributes case-insensitively, but validates required
				// attributes through XElement.Attribute with their exact spelling.
				supportedSection := len(stack) >= 2 && (stack[1] == "packagesources" || stack[1] == "disabledpackagesources" || stack[1] == "packagesourcemapping")
				if supportedSection && (key == "key" || key == "value" || key == "pattern") && key != a.Name.Local {
					c.fail("unsupported_xml_case", "unsupported")
					return
				}
				if _, exists := attrs[key]; exists {
					c.fail("invalid_xml", "invalid")
					return
				}
				attrs[key] = a.Value
			}
			stack = append(stack, name)
			if len(stack) > MaxXMLDepth {
				c.fail("xml_depth_limit", "incomplete")
				return
			}
			if len(stack) == 1 {
				if name != "configuration" {
					c.fail("unsupported_xml_root", "unsupported")
					return
				}
				rootSeen = true
				if len(attrs) > 0 {
					c.omit("unsupported_xml_attribute")
				}
				continue
			}
			if len(stack) == 2 {
				canonical := map[string]string{"packagesources": "packageSources", "disabledpackagesources": "disabledPackageSources", "packagesourcemapping": "packageSourceMapping"}[name]
				if canonical != "" && canonical != node.Name.Local {
					c.fail("unsupported_xml_case", "unsupported")
					return
				}
			}
			if len(stack) < 3 {
				continue
			}
			section := stack[1]
			if section != "packagesources" && section != "disabledpackagesources" && section != "packagesourcemapping" {
				continue
			}
			if len(stack) == 3 {
				if section == "packagesourcemapping" && name == "packagesource" {
					mappingName = attrs["key"]
					c.add(qualify(c, Declaration{Section: "packageSourceMapping", Operation: "map", Semantics: "declared", Name: label(mappingName, "name")}))
					if extraAttrs(attrs, "key") {
						c.omit("unsupported_xml_attribute")
					}
					continue
				}
				observeNuGet(c, section, name, attrs)
			} else if len(stack) == 4 && section == "packagesourcemapping" && stack[2] == "packagesource" && name == "package" {
				c.add(qualify(c, Declaration{Section: "packageSourceMapping", Operation: "map", Semantics: "declared", Name: label(mappingName, "name"), Pattern: label(attrs["pattern"], "pattern")}))
				if extraAttrs(attrs, "pattern") {
					c.omit("unsupported_xml_attribute")
				}
			} else {
				c.omit("unsupported_xml_element")
			}
		case xml.EndElement:
			if len(stack) == 0 {
				c.fail("invalid_xml", "invalid")
				return
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
			if len(stack) >= 2 && (stack[1] == "packagesources" || stack[1] == "disabledpackagesources" || stack[1] == "packagesourcemapping") && strings.TrimSpace(string(node)) != "" {
				c.omit("unsupported_xml_text")
			}
		}
	}
	if !rootSeen || !closed || len(stack) != 0 {
		c.fail("invalid_xml", "invalid")
	}
}
func extraAttrs(attrs map[string]string, allowed ...string) bool {
	for key := range attrs {
		found := false
		for _, a := range allowed {
			if key == a {
				found = true
				break
			}
		}
		if !found {
			return true
		}
	}
	return false
}
func observeNuGet(c *Configuration, section, name string, attrs map[string]string) {
	sectionName := map[string]string{"packagesources": "packageSources", "disabledpackagesources": "disabledPackageSources", "packagesourcemapping": "packageSourceMapping"}[section]
	d := Declaration{Section: sectionName, Operation: name, Semantics: "declared"}
	switch name {
	case "clear":
		if len(attrs) > 0 {
			c.omit("unsupported_xml_attribute")
		}
	case "remove":
		d.Name = label(attrs["key"], "name")
		d.Semantics = "unsupported"
		c.omit("unsupported_remove_semantics")
		if extraAttrs(attrs, "key") {
			c.omit("unsupported_xml_attribute")
		}
	case "add":
		if section == "packagesourcemapping" {
			c.omit("unsupported_xml_element")
			return
		}
		d.Name = label(attrs["key"], "name")
		if section == "packagesources" {
			d.Endpoint = endpoint(attrs["value"])
			if extraAttrs(attrs, "key", "value", "protocolversion", "allowinsecureconnections", "disabletlscertificatevalidation") {
				c.omit("unsupported_xml_attribute")
			}
		} else {
			switch strings.ToLower(attrs["value"]) {
			case "true":
				v := true
				d.Disabled = &v
			case "false":
				v := false
				d.Disabled = &v
			default:
				d.Semantics = "unsupported"
				if variable.MatchString(attrs["value"]) {
					c.omit("unresolved_disabled_value")
				} else {
					c.omit("invalid_disabled_value")
				}
			}
			if extraAttrs(attrs, "key", "value") {
				c.omit("unsupported_xml_attribute")
			}
		}
	default:
		c.omit("unsupported_xml_element")
		return
	}
	c.add(qualify(c, d))
}
