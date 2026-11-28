package desktopcmd

import (
	"bytes"
	"encoding/xml"
	"strings"
)

type infoPlistValues struct {
	Name, DisplayName, Identifier, Executable string
	ShortVersion, Version                     string
	Scheme                                    string
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func renderInfoPlist(v infoPlistValues) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n<dict>\n")
	pair := func(k, s string) {
		b.WriteString("\t<key>" + xmlEscape(k) + "</key>\n")
		b.WriteString("\t<string>" + xmlEscape(s) + "</string>\n")
	}
	pair("CFBundleName", v.Name)
	pair("CFBundleDisplayName", v.DisplayName)
	pair("CFBundleIdentifier", v.Identifier)
	pair("CFBundleExecutable", v.Executable)
	pair("CFBundlePackageType", "APPL")
	pair("CFBundleIconFile", "icon")
	pair("CFBundleShortVersionString", v.ShortVersion)
	pair("CFBundleVersion", v.Version)
	pair("LSMinimumSystemVersion", "12.0")
	b.WriteString("\t<key>NSHighResolutionCapable</key>\n\t<true/>\n")
	b.WriteString("\t<key>NSAppTransportSecurity</key>\n\t<dict>\n")
	b.WriteString("\t\t<key>NSAllowsLocalNetworking</key>\n\t\t<true/>\n")
	b.WriteString("\t</dict>\n")
	pair("LSApplicationCategoryType", "public.app-category.productivity")
	if v.Scheme != "" {
		b.WriteString("\t<key>CFBundleURLTypes</key>\n\t<array>\n\t\t<dict>\n")
		b.WriteString("\t\t\t<key>CFBundleURLName</key>\n\t\t\t<string>" + xmlEscape(v.Identifier) + "</string>\n")
		b.WriteString("\t\t\t<key>CFBundleURLSchemes</key>\n\t\t\t<array>\n")
		b.WriteString("\t\t\t\t<string>" + xmlEscape(v.Scheme) + "</string>\n")
		b.WriteString("\t\t\t</array>\n\t\t</dict>\n\t</array>\n")
	}
	b.WriteString("</dict>\n</plist>\n")
	return []byte(b.String())
}
