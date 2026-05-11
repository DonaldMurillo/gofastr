package html

// ARIA role constants for landmark, widget, and document structure roles.
// See https://www.w3.org/TR/wai-aria-1.2/#role_definitions

// Landmark roles.
const (
	RoleBanner        = "banner"
	RoleNavigation    = "navigation"
	RoleMain          = "main"
	RoleContentinfo   = "contentinfo"
	RoleComplementary = "complementary"
	RoleSearch        = "search"
	RoleForm          = "form"
	RoleRegion        = "region"
)

// Live region roles.
const (
	RoleDialog      = "dialog"
	RoleAlert       = "alert"
	RoleAlertDialog = "alertdialog"
	RoleStatus      = "status"
	RoleLog         = "log"
	RoleMarquee     = "marquee"
	RoleTimer       = "timer"
)

// Widget roles.
const (
	RoleButton   = "button"
	RoleLink     = "link"
	RoleCheckbox = "checkbox"
	RoleRadio    = "radio"
	RoleTab      = "tab"
	RoleTabList  = "tablist"
	RoleTabPanel = "tabpanel"
)

// Grid and table roles.
const (
	RoleGrid     = "grid"
	RoleGridCell = "gridcell"
	RoleRow      = "row"
	RoleRowGroup = "rowgroup"
	RoleTable    = "table"
)

// List and menu roles.
const (
	RoleList     = "list"
	RoleListItem = "listitem"
	RoleListbox  = "listbox"
	RoleOption   = "option"
	RoleMenu     = "menu"
	RoleMenuItem = "menuitem"
)
