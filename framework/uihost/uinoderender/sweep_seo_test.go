package uinoderender

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/uinodev1"
)

func TestSweepSeoUnknownComponentErrors(t *testing.T) {
	h, err := New(context.Background(), nil).Render(tree(uinodev1.Node{Component: uinodev1.Component("marquee")}))
	if err == nil || h != "" {
		t.Fatalf("unknown component: html %q, err %v; want no html and an error", h, err)
	}
}

func TestSweepSeoNilTreeErrors(t *testing.T) {
	if _, err := New(context.Background(), nil).Render(nil); err == nil {
		t.Fatal("a nil tree rendered without an error")
	}
}

func TestSweepSeoChildErrorRejectsWholeTree(t *testing.T) {
	root := uinodev1.Node{
		Component: uinodev1.CompStack,
		Props:     uinodev1.StackProps{Direction: "vertical"},
		Children: []uinodev1.Node{
			node(uinodev1.CompParagraph, uinodev1.ParagraphProps{Text: "kept?"}),
			{Component: uinodev1.CompButton, Props: uinodev1.ButtonProps{Label: "Go"}, ActionRef: "ghost"},
		},
	}
	h, err := New(context.Background(), staticResolver("")).Render(tree(root))
	if err == nil || h != "" {
		t.Fatalf("a failing child: html %q, err %v; want no html and an error", h, err)
	}
}

func TestSweepSeoEmptyOrBlankActionRefErrors(t *testing.T) {
	always := func(string) (string, bool) { return "/m/x", true }
	blank := func(string) (string, bool) { return "", true }
	refused := func(string) (string, bool) { return "/m/x", false }
	for name, c := range map[string]struct {
		ref     string
		resolve ActionResolver
	}{"empty ref": {"", always}, "blank url": {"save", blank}, "refused": {"save", refused}} {
		_, err := New(context.Background(), c.resolve).Render(tree(uinodev1.Node{
			Component: uinodev1.CompButton, Props: uinodev1.ButtonProps{Label: "Save"}, ActionRef: c.ref,
		}))
		if err == nil {
			t.Errorf("%s: rendered a button with no URL", name)
		}
	}
}

func TestSweepSeoDataTableDropsExtraCells(t *testing.T) {
	h, err := New(context.Background(), nil).Render(tree(node(uinodev1.CompDataTable, uinodev1.DataTableProps{
		Columns: []uinodev1.DataColumn{{Key: "a", Label: "A"}},
		Rows:    []uinodev1.DataRow{{Cells: []uinodev1.DataCell{{Text: "first"}, {Text: "overflow"}}}},
	})))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(h), ">first<") || strings.Contains(string(h), "overflow") {
		t.Fatalf("want the first cell only:\n%s", h)
	}
}

func TestSweepSeoKeyValueKeyIsTheLabel(t *testing.T) {
	h, err := New(context.Background(), nil).Render(tree(node(uinodev1.CompKeyValue, uinodev1.KeyValueProps{
		Items: []uinodev1.KeyValueItem{{Key: "Kterm", Value: "Vvalue"}},
	})))
	if err != nil {
		t.Fatal(err)
	}
	if k, v := strings.Index(string(h), ">Kterm<"), strings.Index(string(h), ">Vvalue<"); k < 0 || v < k {
		t.Fatalf("the key is not the row's term:\n%s", h)
	}
}

func TestSweepSeoGridColumnsMapToMin(t *testing.T) {
	for cols, want := range map[int]string{0: "", -1: "", 1: "60rem", 3: "18rem", 5: "10rem", 6: "8rem", 12: "8rem"} {
		if got := gridMinForColumns(cols); got != want {
			t.Errorf("gridMinForColumns(%d) = %q, want %q", cols, got, want)
		}
	}
}

func TestSweepSeoCardAndTrendMapping(t *testing.T) {
	if cardVariant("flat") == cardVariant("high") || cardVariant("low") != cardVariant("") {
		t.Error("card elevation: flat must differ from the elevated default that low, high and empty share")
	}
	if statDirection("up") == statDirection("") || statDirection("down") == statDirection("") || statDirection("flat") != statDirection("") {
		t.Error("stat trend: up and down must differ from the flat default")
	}
}
