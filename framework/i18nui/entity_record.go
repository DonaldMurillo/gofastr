package i18nui

// The entity record's chrome. See entity.go.
const (
	KeyEntityCreate     Key = "ui.entity.create"     // "Create {entity}"
	KeyEntitySave       Key = "ui.entity.save"       // "Save"
	KeyEntityCreatedOn  Key = "ui.entity.createdOn"  // "Created {date}"
	KeyEntityUpdatedOn  Key = "ui.entity.updatedOn"  // "Updated {date}"
	KeyEntityDetails    Key = "ui.entity.details"    // "Details"
	KeyEntityID         Key = "ui.entity.id"         // "ID"
	KeyEntitySaved      Key = "ui.entity.saved"      // "Saved"
	KeyEntityCreated    Key = "ui.entity.created"    // "{entity} created"
	KeyEntitySaveFailed Key = "ui.entity.saveFailed" // "Could not save."
	KeyEntityMoved      Key = "ui.entity.moved"      // "{entity} updated"
	KeyEntityMoveFailed Key = "ui.entity.moveFailed" // "Could not {action}."
	KeyEntityActionRan  Key = "ui.entity.actionRan"  // "Ran {action}."
	// A danger move's or action's confirm, before it runs.
	KeyEntityMoveConfirmTitle   Key = "ui.entity.moveConfirmTitle"   // "{action} this {entity}?"
	KeyEntityMoveConfirm        Key = "ui.entity.moveConfirm"        // "It moves from {from} to {to}."
	KeyEntityMoveAccept         Key = "ui.entity.moveAccept"         // "{action} {entity}"
	KeyEntityActionConfirmTitle Key = "ui.entity.actionConfirmTitle" // "{action}?"
	KeyEntityActionConfirm      Key = "ui.entity.actionConfirm"      // "It runs on {title}."
	KeyEntityTabEdit            Key = "ui.entity.tabEdit"            // "Edit"
	KeyEntityTabRelated         Key = "ui.entity.tabRelated"         // "Related"
	KeyEntityTabActivity        Key = "ui.entity.tabActivity"        // "Activity"
	KeyEntityRecordSections     Key = "ui.entity.recordSections"     // "Sections"
	KeyEntitySet                Key = "ui.entity.set"                // "Set"
	KeyEntityNotSet             Key = "ui.entity.notSet"             // "Not set"
	KeyEntityKeepValue          Key = "ui.entity.keepValue"          // "Leave blank to keep the current value."
	KeyEntityNoActivity         Key = "ui.entity.noActivity"         // "No activity yet"
	// A relation picker: its search placeholder and the note after a
	// cut-off list.
	KeyEntityCreateAnother    Key = "ui.entity.createAnother"    // "Create another"
	KeyEntityCopyAPIURL       Key = "ui.entity.copyAPIURL"       // "Copy API URL"
	KeyEntityPickerSearch     Key = "ui.entity.pickerSearch"     // "Search {entities}…"
	KeyEntityPickerMore       Key = "ui.entity.pickerMore"       // "Showing the first {n}. Type to find others."
	KeyEntityReadOnly         Key = "ui.entity.readOnly"         // "This {entity} is read-only."
	KeyEntityLeaveGuard       Key = "ui.entity.leaveGuard"       // "You have unsaved changes. Leaving now discards them."
	KeyEntityLeaveGuardTitle  Key = "ui.entity.leaveGuardTitle"  // "Discard unsaved changes?"
	KeyEntityLeaveGuardAccept Key = "ui.entity.leaveGuardAccept" // "Discard changes"
	KeyEntityUnchanged        Key = "ui.entity.unchanged"        // "— unchanged —"
	KeyEntityReplace          Key = "ui.entity.replace"          // "Replace"
	KeyEntityReplaceHint      Key = "ui.entity.replaceHint"      // "Saving the form keeps it. Type a new value and choose Replace to change it."
	KeyEntityNewValue         Key = "ui.entity.newValue"         // "New value"
	KeyEntityTabApi           Key = "ui.entity.tabApi"           // "API"
	KeyEntityApiJsonTitle     Key = "ui.entity.apiJsonTitle"     // "This record as the API returns it"
	KeyEntityApiRestTitle     Key = "ui.entity.apiRestTitle"     // "REST"
	KeyEntityApiRestOff       Key = "ui.entity.apiRestOff"       // "This entity is not exposed over REST."
	KeyEntityApiMcpTitle      Key = "ui.entity.apiMcpTitle"      // "MCP tools"
	KeyEntityApiMcpOff        Key = "ui.entity.apiMcpOff"        // "This entity is not exposed over MCP."
	KeyEntityApiResource      Key = "ui.entity.apiResource"      // "Resource"
	KeyEntityApiRecord        Key = "ui.entity.apiRecord"        // "This record"
	KeyEntityApiIndex         Key = "ui.entity.apiIndex"         // "API index (llm.md)"
	KeyEntityOverride         Key = "ui.entity.override"         // "Override status"
	KeyEntityOverrideState    Key = "ui.entity.overrideState"    // "New status"
	KeyEntityOverrideReason   Key = "ui.entity.overrideReason"   // "Reason"
	KeyEntityOverrideHelp     Key = "ui.entity.overrideHelp"     // "Required, at most 500 characters. Recorded in the audit log."
	KeyEntityOverrideDone     Key = "ui.entity.overrideDone"     // "Status overridden"
	KeyEntityOverrideFailed   Key = "ui.entity.overrideFailed"   // "Could not override the status."
	KeyEntityOverrideTitle    Key = "ui.entity.overrideTitle"    // "Override the status of this {entity}?"
	KeyEntityOverrideConfirm  Key = "ui.entity.overrideConfirm"  // "The change skips the workflow and is recorded in the audit log with your reason."
	KeyEntityOverrideBlank    Key = "ui.entity.overrideBlank"    // "A reason is required."
	KeyEntityOverrideLong     Key = "ui.entity.overrideLong"     // "The reason is longer than 500 characters."
	KeyEntityOverrideStateBad Key = "ui.entity.overrideStateBad" // "That status is not one this entity declares."
	KeyEntityOverrideNoAudit  Key = "ui.entity.overrideNoAudit"  // "A status override needs an audit log on this entity."
	KeyEntityOverrideDenied   Key = "ui.entity.overrideDenied"   // "You may not override the status of this {entity}."

	// One Activity tab entry's headline: who did what to this record.
	KeyEntityActivityCreated  Key = "ui.entity.activityCreated"  // "{actor} created this {entity}"
	KeyEntityActivityUpdated  Key = "ui.entity.activityUpdated"  // "{actor} made changes"
	KeyEntityActivitySaved    Key = "ui.entity.activitySaved"    // "{actor} saved this {entity}"
	KeyEntityActivityDeleted  Key = "ui.entity.activityDeleted"  // "{actor} deleted this {entity}"
	KeyEntityActivityRestored Key = "ui.entity.activityRestored" // "{actor} restored this {entity}"
	KeyEntityActivityMoved    Key = "ui.entity.activityMoved"    // "{actor} ran {action}"
	KeyEntityActivityOverride Key = "ui.entity.activityOverride" // "{actor} overrode the status"
	KeyEntityActivityOther    Key = "ui.entity.activityOther"    // "{actor}: {action}"
	KeyEntityActivitySystem   Key = "ui.entity.activitySystem"   // "System"
	KeyEntityActivityReason   Key = "ui.entity.activityReason"   // "Reason: {reason}"
)

var entityRecordDefaults = map[Key]string{
	KeyEntityCreate:             "Create {entity}",
	KeyEntitySave:               "Save",
	KeyEntityCreatedOn:          "Created {date}",
	KeyEntityUpdatedOn:          "Updated {date}",
	KeyEntityDetails:            "Details",
	KeyEntityID:                 "ID",
	KeyEntitySaved:              "Saved",
	KeyEntityCreated:            "{entity} created",
	KeyEntitySaveFailed:         "Could not save.",
	KeyEntityMoved:              "{entity} updated",
	KeyEntityMoveFailed:         "Could not {action}.",
	KeyEntityActionRan:          "Ran {action}.",
	KeyEntityMoveConfirmTitle:   "{action} this {entity}?",
	KeyEntityMoveConfirm:        "It moves from {from} to {to}.",
	KeyEntityMoveAccept:         "{action} {entity}",
	KeyEntityActionConfirmTitle: "{action}?",
	KeyEntityActionConfirm:      "It runs on {title}.",
	KeyEntityTabEdit:            "Edit",
	KeyEntityTabRelated:         "Related",
	KeyEntityTabActivity:        "Activity",
	KeyEntityRecordSections:     "Sections",
	KeyEntitySet:                "Set",
	KeyEntityNotSet:             "Not set",
	KeyEntityKeepValue:          "Leave blank to keep the current value.",
	KeyEntityNoActivity:         "No activity yet",
	KeyEntityPickerSearch:       "Search {entities}…",
	KeyEntityCreateAnother:      "Create another",
	KeyEntityCopyAPIURL:         "Copy API URL",
	KeyEntityPickerMore:         "Showing the first {n}. Type to find others.",
	KeyEntityReadOnly:           "This {entity} is read-only.",
	KeyEntityLeaveGuard:         "You have unsaved changes. Leaving now discards them.",
	KeyEntityLeaveGuardTitle:    "Discard unsaved changes?",
	KeyEntityLeaveGuardAccept:   "Discard changes",
	KeyEntityUnchanged:          "— unchanged —",
	KeyEntityReplace:            "Replace",
	KeyEntityReplaceHint:        "Saving the form keeps it. Type a new value and choose Replace to change it.",
	KeyEntityNewValue:           "New value",
	KeyEntityTabApi:             "API",
	KeyEntityApiJsonTitle:       "This record as the API returns it",
	KeyEntityApiRestTitle:       "REST",
	KeyEntityApiRestOff:         "This entity is not exposed over REST.",
	KeyEntityApiMcpTitle:        "MCP tools",
	KeyEntityApiMcpOff:          "This entity is not exposed over MCP.",
	KeyEntityApiResource:        "Resource",
	KeyEntityApiRecord:          "This record",
	KeyEntityApiIndex:           "API index (llm.md)",
	KeyEntityOverride:           "Override status",
	KeyEntityOverrideState:      "New status",
	KeyEntityOverrideReason:     "Reason",
	KeyEntityOverrideHelp:       "Required, at most 500 characters. Recorded in the audit log.",
	KeyEntityOverrideDone:       "Status overridden",
	KeyEntityOverrideFailed:     "Could not override the status.",
	KeyEntityOverrideTitle:      "Override the status of this {entity}?",
	KeyEntityOverrideConfirm:    "The change skips the workflow and is recorded in the audit log with your reason.",
	KeyEntityOverrideBlank:      "A reason is required.",
	KeyEntityOverrideLong:       "The reason is longer than 500 characters.",
	KeyEntityOverrideStateBad:   "That status is not one this entity declares.",
	KeyEntityOverrideNoAudit:    "A status override needs an audit log on this entity.",
	KeyEntityOverrideDenied:     "You may not override the status of this {entity}.",

	KeyEntityActivityCreated:  "{actor} created this {entity}",
	KeyEntityActivityUpdated:  "{actor} made changes",
	KeyEntityActivitySaved:    "{actor} saved this {entity}",
	KeyEntityActivityDeleted:  "{actor} deleted this {entity}",
	KeyEntityActivityRestored: "{actor} restored this {entity}",
	KeyEntityActivityMoved:    "{actor} ran {action}",
	KeyEntityActivityOverride: "{actor} overrode the status",
	KeyEntityActivityOther:    "{actor}: {action}",
	KeyEntityActivitySystem:   "System",
	KeyEntityActivityReason:   "Reason: {reason}",
}
