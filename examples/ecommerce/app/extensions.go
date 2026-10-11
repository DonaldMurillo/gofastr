package main

import "github.com/DonaldMurillo/gofastr/framework/entityui"

// appExtensions holds the entityui extensions this app adds beside its
// screens. Everything starts empty: a view whose filter depends on the
// caller, a field kind, a record tab or an action each lands here, keyed
// by entity. See 'gofastr docs blueprints' ("Extending the screens") and
// the framework/entityui package doc for each extension point.
//
//	var appExtensions = entityui.Extensions{
//		Entities: map[string]entityui.Extension{
//			"invoices": {
//				Views: map[string]entityui.ViewFunc{
//					"overdue": {Filter: overdueFilter},
//				},
//			},
//		},
//	}
var appExtensions = entityui.Extensions{}
