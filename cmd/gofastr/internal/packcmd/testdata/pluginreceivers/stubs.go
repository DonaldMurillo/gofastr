package pluginreceivers

import "github.com/DonaldMurillo/gofastr/framework"

type adminPlugin struct{ enabled bool }

func (*adminPlugin) Init(*framework.App) error { return nil }
func (*adminPlugin) Name() string              { return "admin" }

type reportPlugin[T any] struct{}

func (reportPlugin[T]) Init(*framework.App) error { return nil }
func (reportPlugin[T]) Name() string              { return "report" }

type exportPlugin[K, V any] struct{}

func (*exportPlugin[K, V]) Init(*framework.App) error { return nil }
func (*exportPlugin[K, V]) Name() string              { return "export" }
