package style

import (
	"strings"
	"testing"
)

// --- DefaultTheme ----------------------------------------------------------

func TestDefaultThemeHasAllTokens(t *testing.T) {
	theme := DefaultTheme()

	// Colors
	expectedColors := map[string]string{
		"primary": "#4F46E5", "secondary": "#6B7280", "danger": "#EF4444",
		"success": "#10B981", "warning": "#F59E0B", "info": "#3B82F6",
		"surface": "#FFFFFF", "background": "#F9FAFB", "text": "#1F2937",
		"text-muted": "#6B7280", "border": "#E5E7EB",
	}
	for k, v := range expectedColors {
		got, ok := theme.Colors[k]
		if !ok {
			t.Errorf("DefaultTheme missing color %q", k)
		} else if got != v {
			t.Errorf("DefaultTheme Colors[%q] = %q, want %q", k, got, v)
		}
	}

	// Spacing
	expectedSpacing := map[string]int{
		"xs": 2, "sm": 4, "md": 8, "lg": 16, "xl": 24, "2xl": 32, "3xl": 48,
	}
	for k, v := range expectedSpacing {
		got, ok := theme.Spacing[k]
		if !ok {
			t.Errorf("DefaultTheme missing spacing %q", k)
		} else if got != v {
			t.Errorf("DefaultTheme Spacing[%q] = %d, want %d", k, got, v)
		}
	}

	// Radii
	expectedRadii := map[string]int{
		"none": 0, "sm": 4, "md": 8, "lg": 12, "xl": 16, "full": 9999,
	}
	for k, v := range expectedRadii {
		got, ok := theme.Radii[k]
		if !ok {
			t.Errorf("DefaultTheme missing radius %q", k)
		} else if got != v {
			t.Errorf("DefaultTheme Radii[%q] = %d, want %d", k, got, v)
		}
	}

	// Fonts
	expectedFonts := map[string]string{
		"body":    "'Inter', system-ui, sans-serif",
		"heading": "'Inter', system-ui, sans-serif",
		"mono":    "'JetBrains Mono', monospace",
	}
	for k, v := range expectedFonts {
		got, ok := theme.Fonts[k]
		if !ok {
			t.Errorf("DefaultTheme missing font %q", k)
		} else if got != v {
			t.Errorf("DefaultTheme Fonts[%q] = %q, want %q", k, got, v)
		}
	}

	// Breakpoints
	expectedBreakpoints := map[string]int{
		"sm": 640, "md": 768, "lg": 1024, "xl": 1280, "2xl": 1536,
	}
	for k, v := range expectedBreakpoints {
		got, ok := theme.Breakpoints[k]
		if !ok {
			t.Errorf("DefaultTheme missing breakpoint %q", k)
		} else if got != v {
			t.Errorf("DefaultTheme Breakpoints[%q] = %d, want %d", k, got, v)
		}
	}

	if theme.Name != "default" {
		t.Errorf("DefaultTheme Name = %q, want %q", theme.Name, "default")
	}
}

// --- ResolveToken ----------------------------------------------------------

func TestResolveTokenColors(t *testing.T) {
	theme := DefaultTheme()

	val, ok := theme.ResolveToken("{colors.primary}")
	if !ok {
		t.Error("ResolveToken({colors.primary}) not found")
	}
	if val != "#4F46E5" {
		t.Errorf("ResolveToken({colors.primary}) = %q, want %q", val, "#4F46E5")
	}
}

func TestResolveTokenSpacing(t *testing.T) {
	theme := DefaultTheme()

	val, ok := theme.ResolveToken("{spacing.md}")
	if !ok {
		t.Error("ResolveToken({spacing.md}) not found")
	}
	if val != "8px" {
		t.Errorf("ResolveToken({spacing.md}) = %q, want %q", val, "8px")
	}
}

func TestResolveTokenRadii(t *testing.T) {
	theme := DefaultTheme()

	val, ok := theme.ResolveToken("{radii.lg}")
	if !ok {
		t.Error("ResolveToken({radii.lg}) not found")
	}
	if val != "12px" {
		t.Errorf("ResolveToken({radii.lg}) = %q, want %q", val, "12px")
	}
}

func TestResolveTokenFonts(t *testing.T) {
	theme := DefaultTheme()

	val, ok := theme.ResolveToken("{fonts.body}")
	if !ok {
		t.Error("ResolveToken({fonts.body}) not found")
	}
	if val != "'Inter', system-ui, sans-serif" {
		t.Errorf("ResolveToken({fonts.body}) = %q, want correct font", val)
	}
}

func TestResolveTokenUnknown(t *testing.T) {
	theme := DefaultTheme()

	val, ok := theme.ResolveToken("{colors.nonexistent}")
	if ok {
		t.Error("ResolveToken should return false for unknown token")
	}
	if val != "{colors.nonexistent}" {
		t.Errorf("ResolveToken unknown = %q, want original ref", val)
	}
}

func TestResolveTokenInvalidFormat(t *testing.T) {
	theme := DefaultTheme()

	_, ok := theme.ResolveToken("no-braces")
	if ok {
		t.Error("ResolveToken should return false for invalid format")
	}
}

// --- ResolveAll ------------------------------------------------------------

func TestResolveAllMultipleTokens(t *testing.T) {
	theme := DefaultTheme()

	input := "padding: {spacing.md}px; color: {colors.primary};"
	result := theme.ResolveAll(input)

	if !strings.Contains(result, "8") {
		t.Errorf("ResolveAll should resolve spacing.md to 8, got: %s", result)
	}
	if !strings.Contains(result, "#4F46E5") {
		t.Errorf("ResolveAll should resolve colors.primary, got: %s", result)
	}
}

func TestResolveAllWithUnresolved(t *testing.T) {
	theme := DefaultTheme()

	input := "color: {colors.primary}; unknown: {foo.bar};"
	result := theme.ResolveAll(input)

	if !strings.Contains(result, "#4F46E5") {
		t.Errorf("ResolveAll should resolve known token, got: %s", result)
	}
	if !strings.Contains(result, "{foo.bar}") {
		t.Errorf("ResolveAll should leave unknown token as-is, got: %s", result)
	}
}

// --- ResolveSpacing / ResolveColor / ResolveRadius -------------------------

func TestResolveSpacing(t *testing.T) {
	theme := DefaultTheme()

	got := theme.ResolveSpacing("md")
	if got != "8px" {
		t.Errorf("ResolveSpacing(md) = %q, want %q", got, "8px")
	}
}

func TestResolveSpacingUnknown(t *testing.T) {
	theme := DefaultTheme()

	got := theme.ResolveSpacing("unknown")
	if got != "0px" {
		t.Errorf("ResolveSpacing(unknown) = %q, want %q", got, "0px")
	}
}

func TestResolveColor(t *testing.T) {
	theme := DefaultTheme()

	got := theme.ResolveColor("primary")
	if got != "#4F46E5" {
		t.Errorf("ResolveColor(primary) = %q, want %q", got, "#4F46E5")
	}
}

func TestResolveColorUnknown(t *testing.T) {
	theme := DefaultTheme()

	got := theme.ResolveColor("nonexistent")
	if got != "" {
		t.Errorf("ResolveColor(nonexistent) = %q, want empty", got)
	}
}

func TestResolveRadius(t *testing.T) {
	theme := DefaultTheme()

	got := theme.ResolveRadius("lg")
	if got != "12px" {
		t.Errorf("ResolveRadius(lg) = %q, want %q", got, "12px")
	}
}

func TestResolveRadiusUnknown(t *testing.T) {
	theme := DefaultTheme()

	got := theme.ResolveRadius("unknown")
	if got != "0px" {
		t.Errorf("ResolveRadius(unknown) = %q, want %q", got, "0px")
	}
}

// --- CSSCustomProperties ---------------------------------------------------

func TestCSSCustomProperties(t *testing.T) {
	theme := DefaultTheme()
	css := theme.CSSCustomProperties()

	if !strings.HasPrefix(css, ":root {") {
		t.Error("CSSCustomProperties should start with :root {")
	}
	if !strings.Contains(css, "--color-primary: #4F46E5;") {
		t.Error("CSSCustomProperties should contain color-primary")
	}
	if !strings.Contains(css, "--spacing-md: 8px;") {
		t.Error("CSSCustomProperties should contain spacing-md")
	}
	if !strings.Contains(css, "--radii-lg: 12px;") {
		t.Error("CSSCustomProperties should contain radii-lg")
	}
	if !strings.Contains(css, "--font-body:") {
		t.Error("CSSCustomProperties should contain font-body")
	}
	if !strings.Contains(css, "--breakpoint-md: 768px;") {
		t.Error("CSSCustomProperties should contain breakpoint-md")
	}
	if !strings.HasSuffix(css, "}") {
		t.Error("CSSCustomProperties should end with }")
	}
}

// --- Classes ---------------------------------------------------------------

func TestClassesToAttr(t *testing.T) {
	c := Classes{"flex": true, "items-center": true, "hidden": false}
	attr := c.ToAttr()

	classVal, ok := attr["class"]
	if !ok {
		t.Fatal("ToAttr should have 'class' key")
	}
	if !strings.Contains(classVal, "flex") {
		t.Error("class should contain 'flex'")
	}
	if !strings.Contains(classVal, "items-center") {
		t.Error("class should contain 'items-center'")
	}
	if strings.Contains(classVal, "hidden") {
		t.Error("class should NOT contain 'hidden' (set to false)")
	}
}

func TestClassesToAttrEmpty(t *testing.T) {
	c := Classes{"hidden": false}
	attr := c.ToAttr()

	if _, ok := attr["class"]; ok {
		t.Error("ToAttr should not have 'class' key when no classes are active")
	}
}

func TestClassesString(t *testing.T) {
	c := Classes{"flex": true, "p-8": true, "hidden": false}
	s := c.String()

	if !strings.Contains(s, "flex") || !strings.Contains(s, "p-8") {
		t.Errorf("String() should contain active classes, got: %s", s)
	}
	if strings.Contains(s, "hidden") {
		t.Error("String() should not contain inactive classes")
	}
}

// --- Use -------------------------------------------------------------------

func TestUse(t *testing.T) {
	attrs := Use("card")
	if attrs["class"] != "comp-card" {
		t.Errorf("expected class=comp-card, got %v", attrs)
	}
}

func TestUseWith(t *testing.T) {
	attrs := UseWith("card", Classes{"highlighted": true, "hidden": false})
	s := attrs["class"]
	if !strings.Contains(s, "comp-card") {
		t.Errorf("expected comp-card in class, got %s", s)
	}
	if !strings.Contains(s, "highlighted") {
		t.Errorf("expected highlighted in class, got %s", s)
	}
	if strings.Contains(s, "hidden") {
		t.Errorf("should not include false classes, got %s", s)
	}
}

func TestComponentCSS(t *testing.T) {
	theme := DefaultTheme()
	theme.Components["card"] = StyleDef{
		"padding":          "{spacing.md} {spacing.lg}",
		"border-radius":    "{radii.lg}",
		"background-color": "{colors.surface}",
	}

	css := theme.ComponentCSS("card")
	if !strings.Contains(css, ".comp-card") {
		t.Errorf("expected .comp-card selector, got %s", css)
	}
	if !strings.Contains(css, "padding: 8px 16px") {
		t.Errorf("expected resolved spacing, got %s", css)
	}
	if !strings.Contains(css, "border-radius: 12px") {
		t.Errorf("expected resolved radii, got %s", css)
	}
	if !strings.Contains(css, "background-color: #FFFFFF") {
		t.Errorf("expected resolved color, got %s", css)
	}
}

func TestComponentCSSNotFound(t *testing.T) {
	theme := DefaultTheme()
	css := theme.ComponentCSS("nonexistent")
	if css != "" {
		t.Errorf("expected empty string for undefined component, got %s", css)
	}
}

func TestAllComponentCSS(t *testing.T) {
	theme := DefaultTheme()
	theme.Components["card"] = StyleDef{
		"padding": "{spacing.md}",
	}
	theme.Components["badge"] = StyleDef{
		"font-size": "{spacing.sm}px",
	}

	css := theme.AllComponentCSS()
	if !strings.Contains(css, ".comp-card") {
		t.Errorf("expected .comp-card, got %s", css)
	}
	if !strings.Contains(css, ".comp-badge") {
		t.Errorf("expected .comp-badge, got %s", css)
	}
}

// --- UtilityClass ----------------------------------------------------------

func TestUtilityClassSpacing(t *testing.T) {
	theme := DefaultTheme()

	got := theme.UtilityClass("p", "md")
	if got != "p-8" {
		t.Errorf("UtilityClass(p, md) = %q, want %q", got, "p-8")
	}
}

func TestUtilityClassRadius(t *testing.T) {
	theme := DefaultTheme()

	got := theme.UtilityClass("rounded", "lg")
	if got != "rounded-12" {
		t.Errorf("UtilityClass(rounded, lg) = %q, want %q", got, "rounded-12")
	}
}

func TestUtilityClassUnknownToken(t *testing.T) {
	theme := DefaultTheme()

	got := theme.UtilityClass("p", "unknown")
	if got != "p-unknown" {
		t.Errorf("UtilityClass(p, unknown) = %q, want %q", got, "p-unknown")
	}
}

// --- GenerateUtilityCSS ----------------------------------------------------

func TestGenerateUtilityCSSPadding(t *testing.T) {
	theme := DefaultTheme()
	css := GenerateUtilityCSS([]string{"p-md", "px-lg"}, theme)

	if !strings.Contains(css, ".p-md { padding: 8px; }") {
		t.Errorf("GenerateUtilityCSS should contain padding rule for p-md, got:\n%s", css)
	}
	if !strings.Contains(css, ".px-lg { padding-left: 16px; padding-right: 16px; }") {
		t.Errorf("GenerateUtilityCSS should contain padding rule for px-lg, got:\n%s", css)
	}
}

func TestGenerateUtilityCSSDisplay(t *testing.T) {
	theme := DefaultTheme()
	css := GenerateUtilityCSS([]string{"flex", "hidden"}, theme)

	if !strings.Contains(css, ".flex { display: flex; }") {
		t.Errorf("GenerateUtilityCSS should contain flex rule, got:\n%s", css)
	}
	if !strings.Contains(css, ".hidden { display: none; }") {
		t.Errorf("GenerateUtilityCSS should contain hidden rule, got:\n%s", css)
	}
}

func TestGenerateUtilityCSSFlexDirection(t *testing.T) {
	theme := DefaultTheme()
	css := GenerateUtilityCSS([]string{"flex-row", "flex-col"}, theme)

	if !strings.Contains(css, ".flex-row { flex-direction: row; }") {
		t.Errorf("GenerateUtilityCSS should contain flex-row, got:\n%s", css)
	}
	if !strings.Contains(css, ".flex-col { flex-direction: column; }") {
		t.Errorf("GenerateUtilityCSS should contain flex-col, got:\n%s", css)
	}
}

func TestGenerateUtilityCSSColors(t *testing.T) {
	theme := DefaultTheme()
	css := GenerateUtilityCSS([]string{"text-primary", "bg-danger"}, theme)

	if !strings.Contains(css, ".text-primary { color: #4F46E5; }") {
		t.Errorf("GenerateUtilityCSS should contain text-primary color, got:\n%s", css)
	}
	if !strings.Contains(css, ".bg-danger { background-color: #EF4444; }") {
		t.Errorf("GenerateUtilityCSS should contain bg-danger, got:\n%s", css)
	}
}

func TestGenerateUtilityCSSMargin(t *testing.T) {
	theme := DefaultTheme()
	css := GenerateUtilityCSS([]string{"m-sm", "my-lg"}, theme)

	if !strings.Contains(css, ".m-sm { margin: 4px; }") {
		t.Errorf("GenerateUtilityCSS should contain margin rule, got:\n%s", css)
	}
	if !strings.Contains(css, ".my-lg { margin-top: 16px; margin-bottom: 16px; }") {
		t.Errorf("GenerateUtilityCSS should contain my-lg, got:\n%s", css)
	}
}

func TestGenerateUtilityCSSGap(t *testing.T) {
	theme := DefaultTheme()
	css := GenerateUtilityCSS([]string{"gap-md", "gap-x-lg"}, theme)

	if !strings.Contains(css, ".gap-md { gap: 8px; }") {
		t.Errorf("GenerateUtilityCSS should contain gap-md, got:\n%s", css)
	}
	if !strings.Contains(css, ".gap-x-lg { column-gap: 16px; }") {
		t.Errorf("GenerateUtilityCSS should contain gap-x-lg, got:\n%s", css)
	}
}

func TestGenerateUtilityCSSFontSize(t *testing.T) {
	theme := DefaultTheme()
	css := GenerateUtilityCSS([]string{"text-lg", "text-sm"}, theme)

	if !strings.Contains(css, ".text-lg { font-size: 1.125rem; }") {
		t.Errorf("GenerateUtilityCSS should contain text-lg, got:\n%s", css)
	}
	if !strings.Contains(css, ".text-sm { font-size: 0.875rem; }") {
		t.Errorf("GenerateUtilityCSS should contain text-sm, got:\n%s", css)
	}
}

func TestGenerateUtilityCSSFontWeight(t *testing.T) {
	theme := DefaultTheme()
	css := GenerateUtilityCSS([]string{"font-bold", "font-medium"}, theme)

	if !strings.Contains(css, ".font-bold { font-weight: 700; }") {
		t.Errorf("GenerateUtilityCSS should contain font-bold, got:\n%s", css)
	}
	if !strings.Contains(css, ".font-medium { font-weight: 500; }") {
		t.Errorf("GenerateUtilityCSS should contain font-medium, got:\n%s", css)
	}
}

func TestGenerateUtilityCSSBorderRadius(t *testing.T) {
	theme := DefaultTheme()
	css := GenerateUtilityCSS([]string{"rounded-md", "rounded-full"}, theme)

	if !strings.Contains(css, ".rounded-md { border-radius: 8px; }") {
		t.Errorf("GenerateUtilityCSS should contain rounded-md, got:\n%s", css)
	}
	if !strings.Contains(css, ".rounded-full { border-radius: 9999px; }") {
		t.Errorf("GenerateUtilityCSS should contain rounded-full, got:\n%s", css)
	}
}

func TestGenerateUtilityCSSWidthHeight(t *testing.T) {
	theme := DefaultTheme()
	css := GenerateUtilityCSS([]string{"w-full", "h-full"}, theme)

	if !strings.Contains(css, ".w-full { width: 100%; }") {
		t.Errorf("GenerateUtilityCSS should contain w-full, got:\n%s", css)
	}
	if !strings.Contains(css, ".h-full { height: 100%; }") {
		t.Errorf("GenerateUtilityCSS should contain h-full, got:\n%s", css)
	}
}

func TestGenerateUtilityCSSOverflow(t *testing.T) {
	theme := DefaultTheme()
	css := GenerateUtilityCSS([]string{"overflow-hidden", "overflow-auto"}, theme)

	if !strings.Contains(css, ".overflow-hidden { overflow: hidden; }") {
		t.Errorf("GenerateUtilityCSS should contain overflow-hidden, got:\n%s", css)
	}
	if !strings.Contains(css, ".overflow-auto { overflow: auto; }") {
		t.Errorf("GenerateUtilityCSS should contain overflow-auto, got:\n%s", css)
	}
}

func TestGenerateUtilityCSSAlignJustify(t *testing.T) {
	theme := DefaultTheme()
	css := GenerateUtilityCSS([]string{"items-center", "justify-between"}, theme)

	if !strings.Contains(css, ".items-center { align-items: center; }") {
		t.Errorf("GenerateUtilityCSS should contain items-center, got:\n%s", css)
	}
	if !strings.Contains(css, ".justify-between { justify-content: space-between; }") {
		t.Errorf("GenerateUtilityCSS should contain justify-between, got:\n%s", css)
	}
}

func TestGenerateUtilityCSSBorder(t *testing.T) {
	theme := DefaultTheme()
	css := GenerateUtilityCSS([]string{"border", "border-primary"}, theme)

	if !strings.Contains(css, ".border { border-width: 1px; }") {
		t.Errorf("GenerateUtilityCSS should contain border, got:\n%s", css)
	}
	if !strings.Contains(css, ".border-primary { border-color: #4F46E5; }") {
		t.Errorf("GenerateUtilityCSS should contain border-primary, got:\n%s", css)
	}
}

// --- CSSExtractor ----------------------------------------------------------

func TestExtractFromHTML(t *testing.T) {
	extractor := NewCSSExtractor(DefaultTheme())
	html := `<div class="flex items-center"><span class="text-primary bold">Hello</span></div>`

	classes := extractor.ExtractFromHTML(html)

	seen := map[string]bool{}
	for _, c := range classes {
		seen[c] = true
	}
	for _, expected := range []string{"flex", "items-center", "text-primary", "bold"} {
		if !seen[expected] {
			t.Errorf("ExtractFromHTML missing class %q, got: %v", expected, classes)
		}
	}
}

func TestExtractFromHTMLNoClasses(t *testing.T) {
	extractor := NewCSSExtractor(DefaultTheme())
	html := `<div><span>Hello</span></div>`

	classes := extractor.ExtractFromHTML(html)
	if len(classes) != 0 {
		t.Errorf("ExtractFromHTML should return empty for HTML with no classes, got: %v", classes)
	}
}

func TestExtractFromHTMLDeduplicates(t *testing.T) {
	extractor := NewCSSExtractor(DefaultTheme())
	html := `<div class="flex"><span class="flex">dup</span></div>`

	classes := extractor.ExtractFromHTML(html)
	count := 0
	for _, c := range classes {
		if c == "flex" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("ExtractFromHTML should deduplicate, found %d occurrences of 'flex'", count)
	}
}

func TestGenerateCSSWithKnownStyles(t *testing.T) {
	extractor := NewCSSExtractor(DefaultTheme())
	extractor.Known["c-btn-primary"] = StyleDef{
		"padding":   "{spacing.sm} {spacing.lg}",
		"color":     "#FFFFFF",
		"font-size": "14px",
	}

	css := extractor.GenerateCSS([]string{"c-btn-primary"})

	if !strings.Contains(css, ".c-btn-primary {") {
		t.Errorf("GenerateCSS should contain .c-btn-primary, got:\n%s", css)
	}
	if !strings.Contains(css, "padding: 4px 16px") {
		t.Errorf("GenerateCSS should resolve tokens in padding, got:\n%s", css)
	}
	if !strings.Contains(css, "color: #FFFFFF") {
		t.Errorf("GenerateCSS should contain color, got:\n%s", css)
	}
}

func TestGenerateCSSWithUtilityClasses(t *testing.T) {
	extractor := NewCSSExtractor(DefaultTheme())

	css := extractor.GenerateCSS([]string{"flex", "p-md"})

	if !strings.Contains(css, ".flex { display: flex; }") {
		t.Errorf("GenerateCSS should resolve utility class flex, got:\n%s", css)
	}
	if !strings.Contains(css, ".p-md { padding: 8px; }") {
		t.Errorf("GenerateCSS should resolve utility class p-md, got:\n%s", css)
	}
}

func TestGenerateChunk(t *testing.T) {
	extractor := NewCSSExtractor(DefaultTheme())
	html := `<div class="flex items-center"><span class="text-primary">Hi</span></div>`

	css := extractor.GenerateChunk(html)

	if !strings.Contains(css, ".flex { display: flex; }") {
		t.Errorf("GenerateChunk should contain flex rule, got:\n%s", css)
	}
	if !strings.Contains(css, ".items-center { align-items: center; }") {
		t.Errorf("GenerateChunk should contain items-center rule, got:\n%s", css)
	}
	if !strings.Contains(css, ".text-primary { color: #4F46E5; }") {
		t.Errorf("GenerateChunk should contain text-primary rule, got:\n%s", css)
	}
}

// --- RouteGraph ------------------------------------------------------------

func TestRouteGraphPreloadManifest(t *testing.T) {
	graph := NewRouteGraph()
	graph.AddRoute("/", "home.css", []string{"/about", "/contact"})
	graph.AddRoute("/about", "about.css", []string{"/", "/contact"})
	graph.AddRoute("/contact", "contact.css", []string{"/"})

	manifest := graph.PreloadManifest()

	// Check home route
	home, ok := manifest["/"]
	if !ok {
		t.Fatal("manifest missing / route")
	}
	if home.CSS != "home.css" {
		t.Errorf("home CSS = %q, want %q", home.CSS, "home.css")
	}
	if len(home.Preload) != 2 {
		t.Fatalf("home Preload = %v, want 2 items", home.Preload)
	}
	preloadSet := map[string]bool{}
	for _, p := range home.Preload {
		preloadSet[p] = true
	}
	if !preloadSet["about.css"] || !preloadSet["contact.css"] {
		t.Errorf("home should preload about.css and contact.css, got: %v", home.Preload)
	}

	// Check about route
	about, ok := manifest["/about"]
	if !ok {
		t.Fatal("manifest missing /about route")
	}
	if about.CSS != "about.css" {
		t.Errorf("about CSS = %q, want %q", about.CSS, "about.css")
	}

	// Check contact route
	contact, ok := manifest["/contact"]
	if !ok {
		t.Fatal("manifest missing /contact route")
	}
	if contact.CSS != "contact.css" {
		t.Errorf("contact CSS = %q, want %q", contact.CSS, "contact.css")
	}
	if len(contact.Preload) != 1 || contact.Preload[0] != "home.css" {
		t.Errorf("contact should preload home.css, got: %v", contact.Preload)
	}
}

func TestRouteGraphPreloadManifestUnknownAdjacent(t *testing.T) {
	graph := NewRouteGraph()
	graph.AddRoute("/", "home.css", []string{"/nonexistent"})

	manifest := graph.PreloadManifest()

	home := manifest["/"]
	if len(home.Preload) != 0 {
		t.Errorf("home Preload should be empty for unknown adjacent, got: %v", home.Preload)
	}
}

// --- MergeThemes -----------------------------------------------------------

func TestMergeThemesOverlay(t *testing.T) {
	base := DefaultTheme()

	custom := Theme{
		Name: "custom",
		Colors: Colors{
			"primary": "#FF0000",
			"custom":  "#00FF00",
		},
		Spacing: Spacing{
			"md": 12,
		},
	}

	merged := MergeThemes(base, custom)

	if merged.Name != "custom" {
		t.Errorf("merged Name = %q, want %q", merged.Name, "custom")
	}
	if merged.Colors["primary"] != "#FF0000" {
		t.Errorf("merged primary = %q, want %q", merged.Colors["primary"], "#FF0000")
	}
	if merged.Colors["custom"] != "#00FF00" {
		t.Errorf("merged custom = %q, want %q", merged.Colors["custom"], "#00FF00")
	}
	if merged.Colors["secondary"] != "#6B7280" {
		t.Errorf("merged secondary should be from base = %q", merged.Colors["secondary"])
	}
	if merged.Spacing["md"] != 12 {
		t.Errorf("merged spacing md = %d, want 12", merged.Spacing["md"])
	}
	if merged.Spacing["sm"] != 4 {
		t.Errorf("merged spacing sm should be from base = %d", merged.Spacing["sm"])
	}
}

func TestMergeThemesEmptyCustom(t *testing.T) {
	base := DefaultTheme()
	custom := Theme{}

	merged := MergeThemes(base, custom)

	if merged.Name != "default" {
		t.Errorf("merged Name = %q, want %q", merged.Name, "default")
	}
	if len(merged.Colors) != len(base.Colors) {
		t.Errorf("merged should have same colors as base")
	}
}

func TestMergeThemesDoesNotMutate(t *testing.T) {
	base := DefaultTheme()
	custom := Theme{
		Colors: Colors{"primary": "#FF0000"},
	}

	MergeThemes(base, custom)

	if base.Colors["primary"] != "#4F46E5" {
		t.Error("MergeThemes should not mutate base theme")
	}
}
