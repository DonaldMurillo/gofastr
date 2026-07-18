package uinodev1

import (
	"bytes"
	"encoding/json"
)

// strictDecode unmarshals raw into dst with DisallowUnknownFields. This is
// the load-bearing control that makes data-fui-*, on*, and arbitrary
// attacker-supplied prop keys UNREPRESENTABLE: no prop struct has a field
// named "data-fui-rpc" / "onclick" / etc., so the decoder rejects them.
//
// We use a fresh json.Decoder per call rather than json.Unmarshal directly
// because json.Unmarshal does not expose DisallowUnknownFields.
func strictDecode(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// componentDecoders maps each closed-enum component to a typed prop
// decoder. Every entry here corresponds to one Component constant. The
// validator looks up a decoder by component name and rejects any name
// not present — that is the closed enum check.
//
// To add a component (rare; design §9 lists the v1 set): add the
// Component constant, define a prop struct that implements Props, add
// it here, and add tests for its invariants.
var componentDecoders = map[Component]func(json.RawMessage) (Props, error){
	CompStack:      decodeAs[StackProps],
	CompCluster:    decodeAs[ClusterProps],
	CompGrid:       decodeAs[GridProps],
	CompSection:    decodeAs[SectionProps],
	CompCard:       decodeAs[CardProps],
	CompDivider:    decodeAs[DividerProps],
	CompHeading:    decodeAs[HeadingProps],
	CompParagraph:  decodeAs[ParagraphProps],
	CompText:       decodeAs[TextProps],
	CompStrong:     decodeAs[StrongProps],
	CompEm:         decodeAs[EmProps],
	CompCode:       decodeAs[CodeProps],
	CompSmall:      decodeAs[SmallProps],
	CompBadge:      decodeAs[BadgeProps],
	CompDetailList: decodeAs[DetailListProps],
	CompKeyValue:   decodeAs[KeyValueProps],
	CompStatCard:   decodeAs[StatCardProps],
	CompDataTable:  decodeAs[DataTableProps],
	CompButton:     decodeAs[ButtonProps],
	CompLink:       decodeAs[LinkProps],
	CompImage:      decodeAs[ImageProps],
}

// decodeAs is the generic per-component decoder. T must be a prop struct
// that implements Props; the generic instantiation gives us type safety
// (no map[string]any anywhere) while keeping the dispatch table compact.
func decodeAs[T Props](raw json.RawMessage) (Props, error) {
	var p T
	if err := strictDecode(raw, &p); err != nil {
		return nil, err
	}
	return p, nil
}
