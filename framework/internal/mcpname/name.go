// Package mcpname formats the names of entity CRUD and transition tools.
package mcpname

// EntityTool returns the registered name for an entity action.
func EntityTool(namespace, entity, action string) string {
	if namespace == "" {
		return entity + "_" + action
	}
	return namespace + "." + entity + "." + action
}
