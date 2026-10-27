package icons

import "strings"

// Category is a curated group of icons. ID is the stable filter value;
// Label is the human-readable name.
type Category struct {
	ID    string
	Label string
}

// Categories returns all icon categories in display order. The returned slice
// belongs to the caller. Each canonical icon has exactly one category.
func Categories() []Category {
	result := make([]Category, 0, len(categoryGroups))
	for _, group := range categoryGroups {
		result = append(result, group.Category)
	}
	return result
}

// CategoryFor returns an icon's category, resolving compatibility aliases.
// Unknown names return an empty Category and false.
func CategoryFor(name string) (Category, bool) {
	if canonical, ok := aliases[name]; ok {
		name = canonical
	}
	category, ok := categoryByName[name]
	return category, ok
}

// Categories describe our catalog, independently of upstream SVG provenance.
// Keep assignments explicit so a new drawing cannot silently fall into a
// catch-all category. Tests require complete, non-overlapping coverage.
var categoryGroups = []struct {
	Category
	names string
}{
	//gofastr:allow(rawtext) English developer-catalog taxonomy; hosts may translate this metadata label
	{Category{"actions", "Actions"}, `
		copy crop delete download download-cloud edit edit-3 edit-square
		filter link link-2 maximize maximize-2 minimize minimize-2 minus minus-circle
		minus-square mouse-pointer paperclip plus plus-circle plus-square save
		scissors search settings settings-gear share share-2 toggle-left toggle-right
		tool trash trash-simple upload upload-cloud zoom-in zoom-out`},
	//gofastr:allow(rawtext) English developer-catalog taxonomy; hosts may translate this metadata label
	{Category{"arrows", "Arrows"}, `
		arrow-down arrow-down-circle arrow-down-left arrow-down-right arrow-left
		arrow-left-circle arrow-right arrow-right-circle arrow-up arrow-up-circle
		arrow-up-left arrow-up-right chevron-down chevron-left chevron-right chevron-up
		chevrons-down chevrons-left chevrons-right chevrons-up chevrons-up-down
		corner-down-left corner-down-right corner-left-down corner-left-up
		corner-right-down corner-right-up corner-up-left corner-up-right external-link
		log-in log-out move refresh-ccw refresh-cw repeat rotate-ccw rotate-cw shuffle`},
	//gofastr:allow(rawtext) English developer-catalog taxonomy; hosts may translate this metadata label
	{Category{"brands", "Brands"}, `
		chrome codepen codesandbox dribbble facebook figma framer github gitlab
		instagram linkedin pocket slack trello twitch twitter youtube`},
	//gofastr:allow(rawtext) English developer-catalog taxonomy; hosts may translate this metadata label
	{Category{"commerce", "Commerce"}, `
		award briefcase credit-card dollar-sign gift package percent receipt
		shopping-bag shopping-cart tag truck`},
	//gofastr:allow(rawtext) English developer-catalog taxonomy; hosts may translate this metadata label
	{Category{"communication", "Communication"}, `
		at-sign inbox mail message-circle message-square phone phone-call
		phone-forwarded phone-incoming phone-missed phone-off phone-outgoing rss
		send voicemail`},
	//gofastr:allow(rawtext) English developer-catalog taxonomy; hosts may translate this metadata label
	{Category{"data", "Data and charts"}, `
		activity bar-chart bar-chart-2 database pie-chart table trending-down trending-up`},
	//gofastr:allow(rawtext) English developer-catalog taxonomy; hosts may translate this metadata label
	{Category{"development", "Development"}, `
		code command git-branch git-commit git-merge git-pull-request terminal`},
	//gofastr:allow(rawtext) English developer-catalog taxonomy; hosts may translate this metadata label
	{Category{"devices", "Devices and connectivity"}, `
		airplay battery battery-charging bluetooth cast cpu hard-drive monitor power
		printer server smartphone tablet tv wifi wifi-off`},
	//gofastr:allow(rawtext) English developer-catalog taxonomy; hosts may translate this metadata label
	{Category{"editor", "Editor"}, `
		align-center align-justify align-left align-right bold feather hash italic
		pen-tool type underline`},
	//gofastr:allow(rawtext) English developer-catalog taxonomy; hosts may translate this metadata label
	{Category{"files", "Files"}, `
		archive book book-open bookmark box clipboard file file-minus file-plus
		file-text folder folder-minus folder-plus`},
	//gofastr:allow(rawtext) English developer-catalog taxonomy; hosts may translate this metadata label
	{Category{"layout", "Layout"}, `
		columns grid layers layout list menu more-horizontal more-vertical sidebar`},
	//gofastr:allow(rawtext) English developer-catalog taxonomy; hosts may translate this metadata label
	{Category{"media", "Media"}, `
		aperture camera camera-off disc fast-forward film headphones image mic mic-off
		music pause pause-circle play play-circle radio rewind skip-back skip-forward
		speaker stop-circle video video-off volume volume-1 volume-2 volume-x`},
	//gofastr:allow(rawtext) English developer-catalog taxonomy; hosts may translate this metadata label
	{Category{"nature", "Nature and weather"}, `
		cloud cloud-drizzle cloud-lightning cloud-off cloud-rain cloud-snow coffee
		droplet flame moon sun sunrise sunset thermometer umbrella wind zap zap-off`},
	//gofastr:allow(rawtext) English developer-catalog taxonomy; hosts may translate this metadata label
	{Category{"people", "People"}, `
		frown heart meh smile thumbs-down thumbs-up user user-check user-minus
		user-plus user-x users`},
	//gofastr:allow(rawtext) English developer-catalog taxonomy; hosts may translate this metadata label
	{Category{"places", "Places and travel"}, `
		anchor compass crosshair flag globe home map map-pin navigation navigation-2 target`},
	//gofastr:allow(rawtext) English developer-catalog taxonomy; hosts may translate this metadata label
	{Category{"security", "Security"}, `key lock shield shield-off unlock`},
	//gofastr:allow(rawtext) English developer-catalog taxonomy; hosts may translate this metadata label
	{Category{"shapes", "Shapes and symbols"}, `
		circle diamond divide divide-circle divide-square hexagon octagon slash square star triangle`},
	//gofastr:allow(rawtext) English developer-catalog taxonomy; hosts may translate this metadata label
	{Category{"status", "Status and alerts"}, `
		alert-circle bell bell-off check check-circle check-square close danger eye
		eye-off help-circle info life-buoy loader warning x-circle x-octagon x-square`},
	//gofastr:allow(rawtext) English developer-catalog taxonomy; hosts may translate this metadata label
	{Category{"time", "Time"}, `calendar clock watch`},
}

var categoryByName = func() map[string]Category {
	result := make(map[string]Category, len(names))
	for _, group := range categoryGroups {
		for _, name := range strings.Fields(group.names) {
			result[name] = group.Category
		}
	}
	return result
}()
