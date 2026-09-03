package isolation

import "testing"

func TestListenAddrHonoursPortShapes(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "off")
	cases := []struct{ port, fallback, want string }{
		{"", ":3090", ":3090"},                        // unset: fallback as given
		{"8088", ":3090", ":8088"},                    // PaaS bare port
		{"localhost:8123", ":3090", "localhost:8123"}, // gofastr dev host:port
		{"", "localhost:8080", "localhost:8080"},
	}
	for _, c := range cases {
		t.Setenv("PORT", c.port)
		got, err := ListenAddr(".", c.fallback)
		if err != nil {
			t.Fatalf("PORT=%q: %v", c.port, err)
		}
		if got != c.want {
			t.Errorf("PORT=%q fallback=%q: got %q, want %q", c.port, c.fallback, got, c.want)
		}
	}
}
