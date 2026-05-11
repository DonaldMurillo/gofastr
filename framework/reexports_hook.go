package framework

import "github.com/gofastr/gofastr/framework/hook"

// Re-exports of framework/hook so existing callers using framework.X
// keep compiling after the hook package extraction.

type (
	HookType     = hook.HookType
	HookFunc     = hook.HookFunc
	HookRegistry = hook.HookRegistry
)

const (
	BeforeCreate = hook.BeforeCreate
	AfterCreate  = hook.AfterCreate
	BeforeUpdate = hook.BeforeUpdate
	AfterUpdate  = hook.AfterUpdate
	BeforeDelete = hook.BeforeDelete
	AfterDelete  = hook.AfterDelete
	BeforeList   = hook.BeforeList
	AfterList    = hook.AfterList
)

var NewHookRegistry = hook.NewHookRegistry
