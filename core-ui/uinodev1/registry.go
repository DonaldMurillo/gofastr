package uinodev1

import (
	"bytes"
	"encoding/json"
)

func strictDecode(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

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

func decodeAs[T Props](raw json.RawMessage) (Props, error) {
	var p T
	if err := strictDecode(raw, &p); err != nil {
		return nil, err
	}
	return p, nil
}
