package registries

import (
	"net"
	"net/netip"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

var patternOnce sync.Once
var variable, feedName, npmScope, packagePattern, dnsLabel, xmlDeclaration *regexp.Regexp

func preparePatterns() {
	patternOnce.Do(func() {
		variable = regexp.MustCompile(`\$|%[A-Za-z_][A-Za-z0-9_]*%`)
		feedName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._ -]{0,127}$`)
		npmScope = regexp.MustCompile(`^@[a-z0-9][a-z0-9._-]{0,127}$`)
		packagePattern = regexp.MustCompile(`^(\*|[A-Za-z0-9][A-Za-z0-9._-]{0,127}\*?)$`)
		dnsLabel = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
		xmlDeclaration = regexp.MustCompile(`^<\?xml[ \t\r\n]+version[ \t\r\n]*=[ \t\r\n]*(?:"1\.0"|'1\.0')(?:[ \t\r\n]+encoding[ \t\r\n]*=[ \t\r\n]*(?:"[Uu][Tt][Ff]-8"|'[Uu][Tt][Ff]-8'))?(?:[ \t\r\n]+standalone[ \t\r\n]*=[ \t\r\n]*(?:"(?:yes|no)"|'(?:yes|no)'))?[ \t\r\n]*\?>$`)
	})
}

// MatchPath identifies supported basenames, not whether a configuration is used.
func MatchPath(filename string) (string, bool) {
	switch {
	case strings.EqualFold(path.Base(filename), "nuget.config"):
		return "nuget", true
	case path.Base(filename) == ".npmrc":
		return "npm", true
	case path.Base(filename) == "settings.xml":
		return "maven", true
	case (path.Base(filename) == "config" || path.Base(filename) == "config.toml") && path.Base(path.Dir(filename)) == ".cargo":
		return "cargo", true
	default:
		return "", false
	}
}

func supportedConfigurations() []string {
	return []string{
		"cargo_dot_cargo_config_basename_exact",
		"cargo_dot_cargo_config_toml_basename_exact",
		"maven_settings_xml_basename_exact",
		"nuget_config_basename_case_insensitive",
		"npmrc_basename_exact",
	}
}
func safePath(value string) bool {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) || path.IsAbs(value) || path.Clean(value) != value || value == ".." || strings.HasPrefix(value, "../") || strings.Contains(value, "\\") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return false
		}
	}
	return true
}
func label(value, kind string) *Label {
	preparePatterns()
	l := &Label{Status: "omitted"}
	if variable.MatchString(value) {
		l.Status = "unresolved"
		return l
	}
	pattern := feedName
	if kind == "scope" {
		pattern = npmScope
	}
	if kind == "pattern" {
		pattern = packagePattern
	}
	if len(value) <= 130 && pattern.MatchString(value) {
		l.Status = "qualified_identifier"
		l.Value = value
	}
	return l
}
func endpoint(value string) *Endpoint {
	preparePatterns()
	e := &Endpoint{Status: "invalid"}
	if len(value) == 0 || len(value) > 8192 || !utf8.ValidString(value) {
		return e
	}
	if variable.MatchString(value) {
		e.Status = "unresolved"
		return e
	}
	for _, r := range value {
		if r <= 32 || r == 127 {
			return e
		}
	}
	if strings.HasPrefix(value, "\\\\") || strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") || len(value) > 2 && value[1] == ':' && (value[2] == '\\' || value[2] == '/') {
		e.Status = "local_path"
		return e
	}
	u, err := url.Parse(value)
	if err != nil {
		return e
	}
	if u.Scheme == "" && u.Host == "" && !strings.ContainsAny(value, ":\\") {
		e.Status = "local_path"
		return e
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		e.Status = "unsupported"
		return e
	}
	if u.Opaque != "" || u.Host == "" || strings.Contains(u.Host, "%") || strings.Contains(value, "\\") {
		return e
	}
	host := u.Hostname()
	if host == "" || len(host) > 253 {
		return e
	}
	port := u.Port()
	if strings.HasSuffix(u.Host, ":") {
		return e
	}
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return e
		}
		port = strconv.Itoa(n)
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Zone() != "" {
			return e
		}
		host = ip.String()
		if ip.Is6() {
			host = "[" + host + "]"
		}
	} else {
		if strings.ContainsAny(host, ":[]") {
			return e
		}
		dns := strings.TrimSuffix(host, ".")
		if dns == "" {
			return e
		}
		allNumeric := true
		for _, part := range strings.Split(dns, ".") {
			if !dnsLabel.MatchString(part) {
				return e
			}
			if strings.Trim(part, "0123456789") != "" {
				allNumeric = false
			}
		}
		if allNumeric {
			return e
		}
		host = strings.ToLower(host)
	}
	if port != "" {
		host = net.JoinHostPort(strings.Trim(host, "[]"), port)
	}
	e.Status = "origin"
	e.Origin = scheme + "://" + host
	return e
}
func qualify(c *Configuration, d Declaration) Declaration {
	for _, l := range []*Label{d.Name, d.Scope, d.Pattern} {
		if l != nil && l.Status != "qualified_identifier" {
			c.omit("identifier_" + l.Status)
		}
	}
	if d.Endpoint != nil && d.Endpoint.Status != "origin" && d.Endpoint.Status != "local_path" {
		c.omit("endpoint_" + d.Endpoint.Status)
	}
	return d
}
