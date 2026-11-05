package app

import "testing"

func TestGroupPathsStayInsidePrefix(t *testing.T) {
	for _, test := range []struct{ prefix, path string }{
		{"/settings", "/legal"},
		{"/settings", "/settings-old/profile"},
		{"/settings", "/settings2"},
		{"/settings", "/"},
		{"/projects/{project}", "/projects/{other}/tasks"},
	} {
		t.Run(test.prefix+test.path, func(t *testing.T) {
			group := NewScreenGroup(test.prefix, nil)
			panicMsg(t, func() { group.Screen(NewScreen(test.path, NewStaticComponent("body")), nil) })
		})
	}
	for _, test := range []struct{ prefix, path, want string }{
		{"/settings", "/settings", "/settings"},
		{"/settings", "/settings/", "/settings/"},
		{"/settings", "profile", "/settings/profile"},
		{"/settings", "/settings/profile", "/settings/profile"},
		{"/", "/legal", "/legal"},
		{"", "/legal", "/legal"},
		{"/projects/{project}", "/projects/:project/tasks", "/projects/:project/tasks"},
		{"/projects/:project", "/projects/{project}/tasks", "/projects/{project}/tasks"},
	} {
		t.Run(test.prefix+test.path, func(t *testing.T) {
			group := NewScreenGroup(test.prefix, nil)
			screen := NewScreen(test.path, NewStaticComponent("body"))
			group.Screen(screen, nil)
			if screen.Path != test.want {
				t.Fatalf("path=%q, want %q", screen.Path, test.want)
			}
		})
	}
	child := NewScreenGroup("/settings", nil).SubGroup("advanced", nil)
	panicMsg(t, func() { child.Screen(NewScreen("/settings/legal", NewStaticComponent("body")), nil) })
}
