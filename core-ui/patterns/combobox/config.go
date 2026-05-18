package combobox

// Config configures a combobox.
type Config struct {
	// ID is the input element id; the listbox id is `<ID>-listbox`.
	// Required.
	ID string

	// Name is the form-submit name on the input. Required.
	Name string

	// Label is the visible <label> text associated with the input.
	// Required.
	Label string

	// RPCPath is the search endpoint. Required. POST with form-encoded
	// body whose first field is `<Name>=<query>`.
	RPCPath string

	// SignalName is the data-fui-rpc-signal value used to swap the
	// listbox HTML on every search response. Required.
	SignalName string

	// DebounceMs is the input debounce window. Default 250.
	DebounceMs int

	// Placeholder is the input placeholder text.
	Placeholder string

	// EmptyHTML is HTML rendered into the listbox at first paint when
	// no options are server-rendered. Pass an empty string to start
	// with an empty hidden listbox (the typical case — the listbox
	// auto-opens once the user types and the server returns options).
	EmptyHTML string

	// LabelHidden visually hides the label (still announced to AT).
	// Use when the surrounding context makes the label redundant.
	LabelHidden bool

	// Class is added to the wrapper's class list.
	Class string
}
