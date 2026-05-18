package ui

import (
	"strings"
	"testing"
)

func TestTimePickerRequiresName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("TimePicker without Name should panic")
		}
	}()
	TimePicker(TimePickerConfig{Label: "x"})
}

func TestTimePickerRequiresLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("TimePicker without Label should panic")
		}
	}()
	TimePicker(TimePickerConfig{Name: "x"})
}

func TestTimePickerEmitsTimeInput(t *testing.T) {
	h := string(TimePicker(TimePickerConfig{
		Name: "wake", Label: "Wake at", Value: "07:30",
	}))
	if !strings.Contains(h, `type="time"`) {
		t.Errorf("expected type=time:\n%s", h)
	}
	if !strings.Contains(h, `value="07:30"`) {
		t.Errorf("expected initial Value:\n%s", h)
	}
}

func TestTimePickerMinMaxStep(t *testing.T) {
	h := string(TimePicker(TimePickerConfig{
		Name: "x", Label: "x", Min: "09:00", Max: "17:00", Step: 900,
	}))
	if !strings.Contains(h, `min="09:00"`) {
		t.Errorf("expected min:\n%s", h)
	}
	if !strings.Contains(h, `max="17:00"`) {
		t.Errorf("expected max:\n%s", h)
	}
	if !strings.Contains(h, `step="900"`) {
		t.Errorf("expected step=900:\n%s", h)
	}
}

func TestTimePickerLabelAssociation(t *testing.T) {
	h := string(TimePicker(TimePickerConfig{Name: "wake", Label: "Wake at"}))
	if !strings.Contains(h, `for="wake"`) {
		t.Errorf("label[for] should match Name:\n%s", h)
	}
	if !strings.Contains(h, `aria-label="Wake at"`) {
		t.Errorf("input should carry aria-label:\n%s", h)
	}
}

func TestTimePickerErrorState(t *testing.T) {
	h := string(TimePicker(TimePickerConfig{
		Name: "x", Label: "x", Error: "Too late",
	}))
	if !strings.Contains(h, "is-error") {
		t.Errorf("Error should add .is-error wrapper class:\n%s", h)
	}
	if !strings.Contains(h, `aria-invalid="true"`) {
		t.Errorf("Error should mark aria-invalid:\n%s", h)
	}
}
