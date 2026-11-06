package config

import (
	"reflect"
	"testing"
)

func TestIntegerBindingHonorsWidth(t *testing.T) {
	for _, text := range []string{"2147483648", "3000000000", "4000000001", "-2147483649"} {
		var destination int32
		if err := setIntegerField(reflect.ValueOf(&destination).Elem(), text, "LIMIT"); err == nil {
			t.Errorf("32-bit integer %s truncated to %d", text, destination)
		}
	}
	for _, text := range []string{"2147483647", "-2147483648", "0"} {
		var destination int32
		if err := setIntegerField(reflect.ValueOf(&destination).Elem(), text, "LIMIT"); err != nil {
			t.Errorf("in-range 32-bit integer %s refused: %v", text, err)
		}
	}
}
