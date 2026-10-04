//go:build windows && amd64

package windows

import (
	"errors"
	"reflect"
	"testing"
)

func TestReadShellItemArrayUsesCountAndItemSlots(t *testing.T) {
	var calls []string
	var released []uintptr
	api := shellItemArrayAPI{
		getCount: func(_ uintptr, count *uint32) error {
			calls = append(calls, "GetCount")
			*count = 2
			return nil
		},
		getItemAt: func(_ uintptr, index uint32, item *uintptr) error {
			calls = append(calls, "GetItemAt")
			*item = uintptr(index + 100)
			return nil
		},
		release: func(item uintptr) { released = append(released, item) },
		path:    func(item uintptr) (string, error) { return map[uintptr]string{100: "a.txt", 101: "b.txt"}[item], nil },
	}

	got, err := readShellItemArray(7, api)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.txt", "b.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	if want := []string{"GetCount", "GetItemAt", "GetItemAt"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("array calls = %v, want %v", calls, want)
	}
	if want := []uintptr{100, 101}; !reflect.DeepEqual(released, want) {
		t.Fatalf("released items = %v, want %v", released, want)
	}
}

func TestShellItemArrayVTableSlots(t *testing.T) {
	if shellItemArrayGetCountSlot != 7 || shellItemArrayGetItemAtSlot != 8 {
		t.Fatalf("IShellItemArray slots = (%d, %d), want (7, 8)", shellItemArrayGetCountSlot, shellItemArrayGetItemAtSlot)
	}
}

func TestReadShellItemArrayReturnsGetCountError(t *testing.T) {
	wantErr := errors.New("GetCount failed")
	called := 0
	api := shellItemArrayAPI{
		getCount: func(_ uintptr, _ *uint32) error {
			called++
			return wantErr
		},
		getItemAt: func(uintptr, uint32, *uintptr) error {
			t.Fatal("retrieved an item before GetCount succeeded")
			return nil
		},
		release: func(uintptr) { t.Fatal("released an item before obtaining the count") },
		path:    func(uintptr) (string, error) { t.Fatal("resolved an item before obtaining the count"); return "", nil },
	}
	got, err := readShellItemArray(7, api)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped %v", err, wantErr)
	}
	if got != nil || called != 1 {
		t.Fatalf("result = %v, calls = %d; want nil and one GetCount", got, called)
	}
}
