package config

import "testing"

func TestExplicitEmptyResetsString(t *testing.T) {
	c := struct{ Token string }{Token: "old-secret"}
	if err := Load(&c, MapSource{"TOKEN": ""}); err != nil {
		t.Fatal(err)
	}
	if c.Token != "" {
		t.Fatalf("retained %q", c.Token)
	}
}
func TestEmptyNumericSettingIsInvalid(t *testing.T) {
	c := struct{ Port int }{Port: 8080}
	if Load(&c, MapSource{"PORT": ""}) == nil {
		t.Fatal("explicit empty integer was silently accepted")
	}
}
func TestDocumentedValidationTags(t *testing.T) {
	for _, src := range []MapSource{{"PORT": "0", "MODE": "info"}, {"PORT": "80", "MODE": "bad"}} {
		c := struct {
			Port int    `validate:"min=1,max=65535"`
			Mode string `validate:"oneof=info debug"`
		}{}
		if Load(&c, src) == nil {
			t.Fatalf("accepted %v", src)
		}
	}
}

func TestValidationTagsCheckUnboundDefaults(t *testing.T) {
	c := struct {
		Port int `validate:"min=1"`
	}{}
	if e := Load(&c, MapSource{}); e == nil {
		t.Fatal("unset port zero bypassed minimum")
	}
	c.Port = 80
	if e := Load(&c, MapSource{}); e != nil {
		t.Fatal(e)
	}
}
