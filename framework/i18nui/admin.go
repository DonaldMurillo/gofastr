package i18nui

// The admin back office (battery/admin): its shell, dashboard and ops
// pages. The entity screens it draws use the entity*.go blocks; see
// entity.go for how the blocks merge into Defaults and AllKeys.

// The shell: brand, sidebar, toolbar, palette and account menu.
const (
	KeyAdminTitle        Key = "ui.admin.title"        // "Admin"
	KeyAdminBrandSub     Key = "ui.admin.brandSub"     // "Back office"
	KeyAdminNav          Key = "ui.admin.nav"          // "Admin navigation"
	KeyAdminSidebar      Key = "ui.admin.sidebar"      // "Admin sidebar"
	KeyAdminToolbar      Key = "ui.admin.toolbar"      // "Admin toolbar"
	KeyAdminDashboard    Key = "ui.admin.dashboard"    // "Dashboard"
	KeyAdminSystemNav    Key = "ui.admin.systemNav"    // "System"
	KeyAdminEntities     Key = "ui.admin.entities"     // "Entities"
	KeyAdminSearch       Key = "ui.admin.search"       // "Search or jump to…"
	KeyAdminPaletteNew   Key = "ui.admin.paletteNew"   // "New {entity}"
	KeyAdminPalettePage  Key = "ui.admin.palettePage"  // "Page"
	KeyAdminPaletteEmpty Key = "ui.admin.paletteEmpty" // "No matches."
	KeyAdminAccount      Key = "ui.admin.account"      // "Account"
	KeyAdminSignedInAs   Key = "ui.admin.signedInAs"   // "Signed in as {name}"
	KeyAdminYourRoles    Key = "ui.admin.yourRoles"    // "Roles: {roles}"
)

// The account page: the signed-in user's own settings.
const (
	KeyAdminAccountSettings Key = "ui.admin.accountSettings" // "Account settings"
	KeyAdminAccountSub      Key = "ui.admin.accountSub"      // "Your profile, how the admin looks to you, and your password."
	KeyAdminProfile         Key = "ui.admin.profile"         // "Profile"
	KeyAdminProfileSub      Key = "ui.admin.profileSub"      // "Who you are signed in as."
	KeyAdminName            Key = "ui.admin.name"            // "Name"
	KeyAdminEmail           Key = "ui.admin.email"           // "Email"
	KeyAdminVerified        Key = "ui.admin.verified"        // "Verified"
	KeyAdminUnverified      Key = "ui.admin.unverified"      // "Unverified"
	KeyAdminAppearance      Key = "ui.admin.appearance"      // "Appearance"
	KeyAdminAppearanceSub   Key = "ui.admin.appearanceSub"   // "Applies in this browser."
	KeyAdminThemeLabel      Key = "ui.admin.themeLabel"      // "Theme"
	KeyAdminSecurity        Key = "ui.admin.security"        // "Password"
	KeyAdminSecuritySub     Key = "ui.admin.securitySub"     // "Changing it signs you out everywhere else."
	KeyAdminCurrentPassword Key = "ui.admin.currentPassword" // "Current password"
	KeyAdminNewPassword     Key = "ui.admin.newPassword"     // "New password"
	KeyAdminNewPasswordHelp Key = "ui.admin.newPasswordHelp" // "At least {min} characters."
	KeyAdminConfirmPassword Key = "ui.admin.confirmPassword" // "Confirm new password"
	KeyAdminChangePassword  Key = "ui.admin.changePassword"  // "Change password"
	KeyAdminPasswordChanged Key = "ui.admin.passwordChanged" // "Password changed"
	KeyAdminNoPassword      Key = "ui.admin.noPassword"      // "Your account signs in without a password. Use "Forgot password" on the sign-in page to set one."
)

// Answers every admin form shares.
const (
	KeyAdminRefused  Key = "ui.admin.refused"  // "You may not do that."
	KeyAdminFailed   Key = "ui.admin.failed"   // "That did not work. Check the server logs for details."
	KeyAdminBadInput Key = "ui.admin.badInput" // "Fill in every field the form asks for."
	KeyAdminDone     Key = "ui.admin.done"     // "Done."
)

// The dashboard.
const (
	KeyAdminDashboardSub Key = "ui.admin.dashboardSub" // "Everything this admin manages, at a glance."
	KeyAdminCardNew      Key = "ui.admin.cardNew"      // "New"
	KeyAdminCountMany    Key = "ui.admin.countMany"    // "{count}+"
	KeyAdminFailedJobs   Key = "ui.admin.failedJobs"   // "Failed jobs"
	KeyAdminNoFailedJobs Key = "ui.admin.noFailedJobs" // "No failed jobs."
	KeyAdminRecent       Key = "ui.admin.recent"       // "Recent activity"
	KeyAdminNoActivity   Key = "ui.admin.noActivity"   // "No activity yet."
	KeyAdminViewAll      Key = "ui.admin.viewAll"      // "View all"

	// The Needs attention panel and one watched view's heading in it.
	KeyAdminAttention      Key = "ui.admin.attention"      // "Needs attention"
	KeyAdminAttentionClear Key = "ui.admin.attentionClear" // "Nothing needs attention."
	KeyAdminAttentionList  Key = "ui.admin.attentionList"  // "{entity} · {view}"

	// One recent-activity line and its parts: who did what to which
	// record, and how long ago.
	KeyAdminActivityLine      Key = "ui.admin.activityLine"      // "{actor} {verb} {record}"
	KeyAdminActivityBulkLine  Key = "ui.admin.activityBulkLine"  // "{actor} {verb} {count} {entity} in bulk"
	KeyAdminVerbCreate        Key = "ui.admin.verbCreate"        // "created"
	KeyAdminVerbUpdate        Key = "ui.admin.verbUpdate"        // "updated"
	KeyAdminVerbDelete        Key = "ui.admin.verbDelete"        // "deleted"
	KeyAdminVerbRestore       Key = "ui.admin.verbRestore"       // "restored"
	KeyAdminVerbPurge         Key = "ui.admin.verbPurge"         // "purged"
	KeyAdminVerbBulk          Key = "ui.admin.verbBulk"          // "ran a bulk action on"
	KeyAdminVerbStateOverride Key = "ui.admin.verbStateOverride" // "overrode the state of"
	KeyAdminVerbTransition    Key = "ui.admin.verbTransition"    // "changed the state of"
	KeyAdminUpdatedAgo        Key = "ui.admin.updatedAgo"        // "Updated {ago}"
)

// Table columns on the ops pages.
const (
	KeyAdminColID         Key = "ui.admin.colID"         // "ID"
	KeyAdminColType       Key = "ui.admin.colType"       // "Type"
	KeyAdminColAttempts   Key = "ui.admin.colAttempts"   // "Attempts"
	KeyAdminColPriority   Key = "ui.admin.colPriority"   // "Priority"
	KeyAdminColCreated    Key = "ui.admin.colCreated"    // "Created"
	KeyAdminColScheduled  Key = "ui.admin.colScheduled"  // "Scheduled"
	KeyAdminColActions    Key = "ui.admin.colActions"    // "Actions"
	KeyAdminColTime       Key = "ui.admin.colTime"       // "Time"
	KeyAdminColEntity     Key = "ui.admin.colEntity"     // "Entity"
	KeyAdminColOperation  Key = "ui.admin.colOperation"  // "Operation"
	KeyAdminColRecord     Key = "ui.admin.colRecord"     // "Record"
	KeyAdminColChanges    Key = "ui.admin.colChanges"    // "Changes"
	KeyAdminColActor      Key = "ui.admin.colActor"      // "Actor"
	KeyAdminColUser       Key = "ui.admin.colUser"       // "User"
	KeyAdminColRoles      Key = "ui.admin.colRoles"      // "Roles"
	KeyAdminColModule     Key = "ui.admin.colModule"     // "Module"
	KeyAdminColTrust      Key = "ui.admin.colTrust"      // "Trust"
	KeyAdminColState      Key = "ui.admin.colState"      // "State"
	KeyAdminColGeneration Key = "ui.admin.colGeneration" // "Generation"
	KeyAdminColRestarts   Key = "ui.admin.colRestarts"   // "Restarts"
	KeyAdminColRoutes     Key = "ui.admin.colRoutes"     // "Routes / tools"
	KeyAdminColLastExit   Key = "ui.admin.colLastExit"   // "Last exit"
)

// The jobs page.
const (
	KeyAdminQueue            Key = "ui.admin.queue"            // "Jobs"
	KeyAdminQueueSub         Key = "ui.admin.queueSub"         // "Background jobs by status."
	KeyAdminQueueStatus      Key = "ui.admin.queueStatus"      // "Status"
	KeyAdminQueueAll         Key = "ui.admin.queueAll"         // "All"
	KeyAdminQueuePending     Key = "ui.admin.queuePending"     // "Pending"
	KeyAdminQueueClaimed     Key = "ui.admin.queueClaimed"     // "Running"
	KeyAdminQueueFailed      Key = "ui.admin.queueFailed"      // "Failed"
	KeyAdminQueueLoadFailed  Key = "ui.admin.queueLoadFailed"  // "Could not load jobs. Check the server logs for details."
	KeyAdminQueueEmpty       Key = "ui.admin.queueEmpty"       // "No jobs"
	KeyAdminQueueEmptyDesc   Key = "ui.admin.queueEmptyDesc"   // "No jobs match this filter."
	KeyAdminReplay           Key = "ui.admin.replay"           // "Replay"
	KeyAdminReplayAll        Key = "ui.admin.replayAll"        // "Replay all"
	KeyAdminReplayAllConfirm Key = "ui.admin.replayAllConfirm" // "Replay every failed job?"
	KeyAdminReplayed         Key = "ui.admin.replayed"         // "Job queued again."
	KeyAdminReplayedAll      Key = "ui.admin.replayedAll"      // "Failed jobs queued again."
)

// The audit log.
const (
	KeyAdminAudit           Key = "ui.admin.audit"           // "Audit log"
	KeyAdminAuditSub        Key = "ui.admin.auditSub"        // "Who changed what, newest first."
	KeyAdminAuditLoadFailed Key = "ui.admin.auditLoadFailed" // "Could not load audit rows. Check the server logs for details."
	KeyAdminAuditEmpty      Key = "ui.admin.auditEmpty"      // "No audit entries"
	KeyAdminAuditEmptyDesc  Key = "ui.admin.auditEmptyDesc"  // "Audit events will appear here."
	KeyAdminAuditSystem     Key = "ui.admin.auditSystem"     // "System"
	KeyAdminAuditFrom       Key = "ui.admin.auditFrom"       // "From"
	KeyAdminAuditTo         Key = "ui.admin.auditTo"         // "To"
	KeyAdminAuditAnyEntity  Key = "ui.admin.auditAnyEntity"  // "Any entity"
	KeyAdminAuditAnyOp      Key = "ui.admin.auditAnyOp"      // "Any operation"
	KeyAdminAuditBadFilter  Key = "ui.admin.auditBadFilter"  // "\"{param}\" was ignored. It is not a value the filter accepts."

	KeyAdminAuditRole          Key = "ui.admin.auditRole"          // "Role"
	KeyAdminAuditUser          Key = "ui.admin.auditUser"          // "User"
	KeyAdminAuditGranted       Key = "ui.admin.auditGranted"       // "Granted {permission}"
	KeyAdminAuditRevoked       Key = "ui.admin.auditRevoked"       // "Revoked {permission}"
	KeyAdminAuditGrantRefused  Key = "ui.admin.auditGrantRefused"  // "Refused to grant {permission}"
	KeyAdminAuditRevokeRefused Key = "ui.admin.auditRevokeRefused" // "Refused to revoke {permission}"
	KeyAdminAuditAssignRefused Key = "ui.admin.auditAssignRefused" // "Refused to assign {role}"
	KeyAdminAuditNoRoles       Key = "ui.admin.auditNoRoles"       // "No roles"
	KeyAdminAuditBulkRecord    Key = "ui.admin.auditBulkRecord"    // "{count} {entity}"
	KeyAdminAuditBulkDeleted   Key = "ui.admin.auditBulkDeleted"   // "Deleted"
	KeyAdminAuditBulkRestored  Key = "ui.admin.auditBulkRestored"  // "Restored"
	KeyAdminAuditBulkUpdated   Key = "ui.admin.auditBulkUpdated"   // "Updated"
	KeyAdminAuditBulkSkipped   Key = "ui.admin.auditBulkSkipped"   // "{count} skipped"
	KeyAdminAuditBulkFailed    Key = "ui.admin.auditBulkFailed"    // "{count} failed"
)

// Roles and user roles.
const (
	KeyAdminRoles             Key = "ui.admin.roles"             // "Roles"
	KeyAdminRolesSub          Key = "ui.admin.rolesSub"          // "What each role may do."
	KeyAdminUserRoles         Key = "ui.admin.userRoles"         // "User roles"
	KeyAdminUserRolesSub      Key = "ui.admin.userRolesSub"      // "Which roles each user holds."
	KeyAdminNoRoles           Key = "ui.admin.noRoles"           // "No roles"
	KeyAdminNoRolesDesc       Key = "ui.admin.noRolesDesc"       // "Define roles in the app's access policy."
	KeyAdminNoUsers           Key = "ui.admin.noUsers"           // "No users"
	KeyAdminUsersLoadFailed   Key = "ui.admin.usersLoadFailed"   // "Could not load users. Check the server logs for details."
	KeyAdminPermission        Key = "ui.admin.permission"        // "Permission"
	KeyAdminNewRole           Key = "ui.admin.newRole"           // "New role"
	KeyAdminAddRole           Key = "ui.admin.addRole"           // "Add a role"
	KeyAdminGrant             Key = "ui.admin.grant"             // "Grant"
	KeyAdminRevoke            Key = "ui.admin.revoke"            // "Revoke"
	KeyAdminGrantCell         Key = "ui.admin.grantCell"         // "{role} holds {permission}"
	KeyAdminHoldsEvery        Key = "ui.admin.holdsEvery"        // "{role} holds every permission"
	KeyAdminSavePermissions   Key = "ui.admin.savePermissions"   // "Save permissions"
	KeyAdminPermissionsSaved  Key = "ui.admin.permissionsSaved"  // "Permissions saved."
	KeyAdminNoPermissions     Key = "ui.admin.noPermissions"     // "No permissions to grant"
	KeyAdminNoPermissionsDesc Key = "ui.admin.noPermissionsDesc" // "Every role holds every permission. Register the app's capabilities on its policy to grant them one by one."
	KeyAdminUndeclared        Key = "ui.admin.undeclared"        // "Not declared"
	KeyAdminSaveRoles         Key = "ui.admin.saveRoles"         // "Save roles"
	KeyAdminEditRoles         Key = "ui.admin.editRoles"         // "Edit roles"
	KeyAdminGranted           Key = "ui.admin.granted"           // "Permission granted."
	KeyAdminRolesSaved        Key = "ui.admin.rolesSaved"        // "Roles saved."
	KeyAdminGrantRefused      Key = "ui.admin.grantRefused"      // "You can only grant or revoke a permission you hold."
	KeyAdminAssignRefused     Key = "ui.admin.assignRefused"     // "You cannot assign a role above your own."
	KeyAdminUnknownCapability Key = "ui.admin.unknownCapability" // "That permission is not one the app declares."
)

// Process modules.
const (
	KeyAdminModules        Key = "ui.admin.modules"        // "Modules"
	KeyAdminModulesSub     Key = "ui.admin.modulesSub"     // "Process modules and their lifecycle."
	KeyAdminNoModules      Key = "ui.admin.noModules"      // "No process modules registered."
	KeyAdminEnable         Key = "ui.admin.enable"         // "Enable"
	KeyAdminDisable        Key = "ui.admin.disable"        // "Disable"
	KeyAdminDisableConfirm Key = "ui.admin.disableConfirm" // "Disable {module}? It drains and stops serving."
	KeyAdminBump           Key = "ui.admin.bump"           // "Bump generation"
	KeyAdminCapability     Key = "ui.admin.capability"     // "Capability"
	KeyAdminRevokeConfirm  Key = "ui.admin.revokeConfirm"  // "Revoke this capability from {module}? Its generation bumps and the child restarts."
	KeyAdminModuleEnabled  Key = "ui.admin.moduleEnabled"  // "Module enabled."
	KeyAdminModuleDisabled Key = "ui.admin.moduleDisabled" // "Module disabled."
	KeyAdminModuleBumped   Key = "ui.admin.moduleBumped"   // "Generation bumped."
	KeyAdminModuleRevoked  Key = "ui.admin.moduleRevoked"  // "Capability revoked."
	KeyAdminModuleFailed   Key = "ui.admin.moduleFailed"   // "The module did not accept that change. Check the server logs for details."
	KeyAdminModuleRefused  Key = "ui.admin.moduleRefused"  // "You may not manage process modules."
	KeyAdminServing        Key = "ui.admin.serving"        // "Serving"
	KeyAdminServes404      Key = "ui.admin.serves404"      // "Serves 404"
	KeyAdminServes503      Key = "ui.admin.serves503"      // "Serves 503"
	KeyAdminCircuitOpen    Key = "ui.admin.circuitOpen"    // "Circuit open"
	KeyAdminLeaseFailing   Key = "ui.admin.leaseFailing"   // "Lease failing"
)

var adminDefaults = map[Key]string{
	KeyAdminTitle:              "Admin",
	KeyAdminBrandSub:           "Back office",
	KeyAdminNav:                "Admin navigation",
	KeyAdminSidebar:            "Admin sidebar",
	KeyAdminToolbar:            "Admin toolbar",
	KeyAdminDashboard:          "Dashboard",
	KeyAdminSystemNav:          "System",
	KeyAdminEntities:           "Entities",
	KeyAdminSearch:             "Search or jump to…",
	KeyAdminPaletteNew:         "New {entity}",
	KeyAdminPalettePage:        "Page",
	KeyAdminPaletteEmpty:       "No matches.",
	KeyAdminAccount:            "Account",
	KeyAdminSignedInAs:         "Signed in as {name}",
	KeyAdminYourRoles:          "Roles: {roles}",
	KeyAdminAccountSettings:    "Account settings",
	KeyAdminAccountSub:         "Your profile, how the admin looks to you, and your password.",
	KeyAdminProfile:            "Profile",
	KeyAdminProfileSub:         "Who you are signed in as.",
	KeyAdminName:               "Name",
	KeyAdminEmail:              "Email",
	KeyAdminVerified:           "Verified",
	KeyAdminUnverified:         "Unverified",
	KeyAdminAppearance:         "Appearance",
	KeyAdminAppearanceSub:      "Applies in this browser.",
	KeyAdminThemeLabel:         "Theme",
	KeyAdminSecurity:           "Password",
	KeyAdminSecuritySub:        "Changing it signs you out everywhere else.",
	KeyAdminCurrentPassword:    "Current password", // not-a-secret: UI label
	KeyAdminNewPassword:        "New password",     // not-a-secret: UI label
	KeyAdminNewPasswordHelp:    "At least {min} characters.",
	KeyAdminConfirmPassword:    "Confirm new password", // not-a-secret: UI label
	KeyAdminChangePassword:     "Change password",      // not-a-secret: UI label
	KeyAdminPasswordChanged:    "Password changed",
	KeyAdminNoPassword:         "Your account signs in without a password. Use \"Forgot password\" on the sign-in page to set one.", // not-a-secret: UI label
	KeyAdminRefused:            "You may not do that.",
	KeyAdminFailed:             "That did not work. Check the server logs for details.",
	KeyAdminBadInput:           "Fill in every field the form asks for.",
	KeyAdminDone:               "Done.",
	KeyAdminDashboardSub:       "Everything this admin manages, at a glance.",
	KeyAdminCardNew:            "New",
	KeyAdminCountMany:          "{count}+",
	KeyAdminFailedJobs:         "Failed jobs",
	KeyAdminNoFailedJobs:       "No failed jobs.",
	KeyAdminRecent:             "Recent activity",
	KeyAdminNoActivity:         "No activity yet.",
	KeyAdminViewAll:            "View all",
	KeyAdminAttention:          "Needs attention",
	KeyAdminAttentionClear:     "Nothing needs attention.",
	KeyAdminAttentionList:      "{entity} · {view}",
	KeyAdminActivityLine:       "{actor} {verb} {record}",
	KeyAdminActivityBulkLine:   "{actor} {verb} {count} {entity} in bulk",
	KeyAdminVerbCreate:         "created",
	KeyAdminVerbUpdate:         "updated",
	KeyAdminVerbDelete:         "deleted",
	KeyAdminVerbRestore:        "restored",
	KeyAdminVerbPurge:          "purged",
	KeyAdminVerbBulk:           "ran a bulk action on",
	KeyAdminVerbStateOverride:  "overrode the state of",
	KeyAdminVerbTransition:     "changed the state of",
	KeyAdminUpdatedAgo:         "Updated {ago}",
	KeyAdminColID:              "ID",
	KeyAdminColType:            "Type",
	KeyAdminColAttempts:        "Attempts",
	KeyAdminColPriority:        "Priority",
	KeyAdminColCreated:         "Created",
	KeyAdminColScheduled:       "Scheduled",
	KeyAdminColActions:         "Actions",
	KeyAdminColTime:            "Time",
	KeyAdminColEntity:          "Entity",
	KeyAdminColOperation:       "Operation",
	KeyAdminColRecord:          "Record",
	KeyAdminColChanges:         "Changes",
	KeyAdminColActor:           "Actor",
	KeyAdminColUser:            "User",
	KeyAdminColRoles:           "Roles",
	KeyAdminColModule:          "Module",
	KeyAdminColTrust:           "Trust",
	KeyAdminColState:           "State",
	KeyAdminColGeneration:      "Generation",
	KeyAdminColRestarts:        "Restarts",
	KeyAdminColRoutes:          "Routes / tools",
	KeyAdminColLastExit:        "Last exit",
	KeyAdminQueue:              "Jobs",
	KeyAdminQueueSub:           "Background jobs by status.",
	KeyAdminQueueStatus:        "Status",
	KeyAdminQueueAll:           "All",
	KeyAdminQueuePending:       "Pending",
	KeyAdminQueueClaimed:       "Running",
	KeyAdminQueueFailed:        "Failed",
	KeyAdminQueueLoadFailed:    "Could not load jobs. Check the server logs for details.",
	KeyAdminQueueEmpty:         "No jobs",
	KeyAdminQueueEmptyDesc:     "No jobs match this filter.",
	KeyAdminReplay:             "Replay",
	KeyAdminReplayAll:          "Replay all",
	KeyAdminReplayAllConfirm:   "Replay every failed job?",
	KeyAdminReplayed:           "Job queued again.",
	KeyAdminReplayedAll:        "Failed jobs queued again.",
	KeyAdminAudit:              "Audit log",
	KeyAdminAuditFrom:          "From",
	KeyAdminAuditTo:            "To",
	KeyAdminAuditAnyEntity:     "Any entity",
	KeyAdminAuditAnyOp:         "Any operation",
	KeyAdminAuditBadFilter:     "\"{param}\" was ignored. It is not a value the filter accepts.",
	KeyAdminAuditRole:          "Role",
	KeyAdminAuditUser:          "User",
	KeyAdminAuditGranted:       "Granted {permission}",
	KeyAdminAuditRevoked:       "Revoked {permission}",
	KeyAdminAuditGrantRefused:  "Refused to grant {permission}",
	KeyAdminAuditRevokeRefused: "Refused to revoke {permission}",
	KeyAdminAuditAssignRefused: "Refused to assign {role}",
	KeyAdminAuditNoRoles:       "No roles",
	KeyAdminAuditBulkRecord:    "{count} {entity}",
	KeyAdminAuditBulkDeleted:   "Deleted",
	KeyAdminAuditBulkRestored:  "Restored",
	KeyAdminAuditBulkUpdated:   "Updated",
	KeyAdminAuditBulkSkipped:   "{count} skipped",
	KeyAdminAuditBulkFailed:    "{count} failed",
	KeyAdminAuditSub:           "Who changed what, newest first.",
	KeyAdminAuditLoadFailed:    "Could not load audit rows. Check the server logs for details.",
	KeyAdminAuditEmpty:         "No audit entries",
	KeyAdminAuditEmptyDesc:     "Audit events will appear here.",
	KeyAdminAuditSystem:        "System",
	KeyAdminRoles:              "Roles",
	KeyAdminRolesSub:           "What each role may do.",
	KeyAdminUserRoles:          "User roles",
	KeyAdminUserRolesSub:       "Which roles each user holds.",
	KeyAdminNoRoles:            "No roles",
	KeyAdminNoRolesDesc:        "Define roles in the app's access policy.",
	KeyAdminNoUsers:            "No users",
	KeyAdminUsersLoadFailed:    "Could not load users. Check the server logs for details.",
	KeyAdminPermission:         "Permission",
	KeyAdminNewRole:            "New role",
	KeyAdminAddRole:            "Add a role",
	KeyAdminGrant:              "Grant",
	KeyAdminRevoke:             "Revoke",
	KeyAdminGrantCell:          "{role} holds {permission}",
	KeyAdminHoldsEvery:         "{role} holds every permission",
	KeyAdminSavePermissions:    "Save permissions",
	KeyAdminPermissionsSaved:   "Permissions saved.",
	KeyAdminNoPermissions:      "No permissions to grant",
	KeyAdminNoPermissionsDesc:  "Every role holds every permission. Register the app's capabilities on its policy to grant them one by one.",
	KeyAdminUndeclared:         "Not declared",
	KeyAdminSaveRoles:          "Save roles",
	KeyAdminEditRoles:          "Edit roles",
	KeyAdminGranted:            "Permission granted.",
	KeyAdminRolesSaved:         "Roles saved.",
	KeyAdminGrantRefused:       "You can only grant or revoke a permission you hold.",
	KeyAdminAssignRefused:      "You cannot assign a role above your own.",
	KeyAdminUnknownCapability:  "That permission is not one the app declares.",
	KeyAdminModules:            "Modules",
	KeyAdminModulesSub:         "Process modules and their lifecycle.",
	KeyAdminNoModules:          "No process modules registered.",
	KeyAdminEnable:             "Enable",
	KeyAdminDisable:            "Disable",
	KeyAdminDisableConfirm:     "Disable {module}? It drains and stops serving.",
	KeyAdminBump:               "Bump generation",
	KeyAdminCapability:         "Capability",
	KeyAdminRevokeConfirm:      "Revoke this capability from {module}? Its generation bumps and the child restarts.",
	KeyAdminModuleEnabled:      "Module enabled.",
	KeyAdminModuleDisabled:     "Module disabled.",
	KeyAdminModuleBumped:       "Generation bumped.",
	KeyAdminModuleRevoked:      "Capability revoked.",
	KeyAdminModuleFailed:       "The module did not accept that change. Check the server logs for details.",
	KeyAdminModuleRefused:      "You may not manage process modules.",
	KeyAdminServing:            "Serving",
	KeyAdminServes404:          "Serves 404",
	KeyAdminServes503:          "Serves 503",
	KeyAdminCircuitOpen:        "Circuit open",
	KeyAdminLeaseFailing:       "Lease failing",
}
