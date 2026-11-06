package config_test

import (
	"strconv"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/config"
)

func TestLoadIntHonorsTargetWidth(t *testing.T) {
	maximum := uint64(^uint(0) >> 1)
	for _, value := range []string{strconv.FormatUint(maximum+1, 10), strconv.FormatUint(maximum+19, 10), "-" + strconv.FormatUint(maximum+2, 10)} {
		var settings struct {
			Limit int `config:"LIMIT"`
		}
		if err := config.Load(&settings, config.MapSource{"LIMIT": value}); err == nil {
			t.Errorf("LIMIT=%s silently bound as %d on a %d-bit target", value, settings.Limit, strconv.IntSize)
		}
	}
	for _, value := range []string{strconv.FormatUint(maximum, 10), "-" + strconv.FormatUint(maximum+1, 10), "0", "31337"} {
		var settings struct {
			Limit int `config:"LIMIT"`
		}
		if err := config.Load(&settings, config.MapSource{"LIMIT": value}); err != nil || strconv.Itoa(settings.Limit) != value {
			t.Errorf("in-range LIMIT=%s: got %d, error %v", value, settings.Limit, err)
		}
	}
	var wide struct {
		Limit int64 `config:"LIMIT"`
	}
	if err := config.Load(&wide, config.MapSource{"LIMIT": "4000000001"}); err != nil || wide.Limit != 4000000001 {
		t.Fatalf("int64 must remain 64-bit: got %d, error %v", wide.Limit, err)
	}
}
