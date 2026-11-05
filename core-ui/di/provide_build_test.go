package di

import "testing"

func TestProvideRefusesBuildingType(t *testing.T) {
	type service struct{ name string }
	for _, direct := range []bool{false, true} {
		t.Run(map[bool]string{false: "factory", true: "value"}[direct], func(t *testing.T) {
			container := NewContainer()
			started := make(chan struct{})
			release := make(chan struct{})
			original := &service{name: "original"}
			if err := container.Provide(func() *service {
				close(started)
				<-release
				return original
			}); err != nil {
				t.Fatal(err)
			}
			finished := make(chan error, 1)
			var first *service
			go func() { finished <- container.Resolve(&first) }()
			<-started
			var replacement any = func() *service { return &service{name: "replacement"} }
			if direct {
				replacement = &service{name: "replacement"}
			}
			err := container.Provide(replacement)
			close(release)
			if err == nil {
				t.Error("Provide accepted a replacement while the type was building")
			}
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
			var second *service
			if err := container.Resolve(&second); err != nil {
				t.Fatal(err)
			}
			if first != original || second != original {
				t.Fatalf("singleton identity changed: first=%p second=%p want=%p", first, second, original)
			}
		})
	}
}
