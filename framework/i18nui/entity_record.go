package i18nui

// The entity record's chrome. See entity.go.
const (
	KeyEntityCreate      Key = "ui.entity.create"      // "Create {entity}"
	KeyEntitySave        Key = "ui.entity.save"        // "Save changes"
	KeyEntitySaved       Key = "ui.entity.saved"       // "Saved"
	KeyEntityCreated     Key = "ui.entity.created"     // "{entity} created"
	KeyEntitySaveFailed  Key = "ui.entity.saveFailed"  // "Could not save."
	KeyEntityMoved       Key = "ui.entity.moved"       // "{entity} updated"
	KeyEntityMoveFailed  Key = "ui.entity.moveFailed"  // "Could not {action}."
	KeyEntityTabEdit     Key = "ui.entity.tabEdit"     // "Edit"
	KeyEntityTabRelated  Key = "ui.entity.tabRelated"  // "Related"
	KeyEntityTabActivity Key = "ui.entity.tabActivity" // "Activity"
	KeyEntitySet         Key = "ui.entity.set"         // "Set"
	KeyEntityNotSet      Key = "ui.entity.notSet"      // "Not set"
	KeyEntityKeepValue   Key = "ui.entity.keepValue"   // "Leave blank to keep the current value."
	KeyEntityNoActivity  Key = "ui.entity.noActivity"  // "No activity yet"
)

var entityRecordDefaults = map[Key]string{
	KeyEntityCreate:      "Create {entity}",
	KeyEntitySave:        "Save changes",
	KeyEntitySaved:       "Saved",
	KeyEntityCreated:     "{entity} created",
	KeyEntitySaveFailed:  "Could not save.",
	KeyEntityMoved:       "{entity} updated",
	KeyEntityMoveFailed:  "Could not {action}.",
	KeyEntityTabEdit:     "Edit",
	KeyEntityTabRelated:  "Related",
	KeyEntityTabActivity: "Activity",
	KeyEntitySet:         "Set",
	KeyEntityNotSet:      "Not set",
	KeyEntityKeepValue:   "Leave blank to keep the current value.",
	KeyEntityNoActivity:  "No activity yet",
}
