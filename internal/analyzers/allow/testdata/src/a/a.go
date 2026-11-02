package a

import "encoding/json"

type v struct{ A int }

func unmarked(b []byte) {
	var x v
	_ = json.Unmarshal(b, &x) // want `json.Unmarshal error discarded`
}

func trailing(b []byte) {
	var x v
	_ = json.Unmarshal(b, &x) //gofastr:allow(discardeddecode) best-effort render of bytes already validated by the caller
}

// A trailing marker covers its own line only: code before the comment
// means it is not a stand-alone marker, so the next line still reports.
// (Mutating standsAlone's "nothing before the comment" check survived
// every test until this case, 2026-11-02 guard bash.)
func trailingThenNext(b []byte) {
	var x v
	_ = json.Unmarshal(b, &x) //gofastr:allow(discardeddecode) best-effort render of bytes already validated by the caller
	_ = json.Unmarshal(b, &x) // want `json.Unmarshal error discarded`
}

func standalone(b []byte) {
	var x v
	//gofastr:allow(discardeddecode) best-effort render of bytes already validated by the caller
	_ = json.Unmarshal(b, &x)
}

func bareMarker(b []byte) {
	var x v
	//gofastr:allow(discardeddecode)
	_ = json.Unmarshal(b, &x) // want `json.Unmarshal error discarded`
}

// Separator punctuation is not a reason: the contracts pipeline trims
// the same run and calls this marker reasonless.
func punctuationOnly(b []byte) {
	var x v
	//gofastr:allow(discardeddecode) --
	_ = json.Unmarshal(b, &x) // want `json.Unmarshal error discarded`
}

func otherRule(b []byte) {
	var x v
	_ = json.Unmarshal(b, &x) //gofastr:allow(mapwriter) unrelated marker // want `json.Unmarshal error discarded`
}

// I20: an inline block marker before code covers its own line only; the
// next line is not silenced.
func inlineBlock(b []byte) {
	var x v
	/* gofastr:allow(discardeddecode) best-effort render of validated bytes */ _ = json.Unmarshal(b, &x)
	_ = json.Unmarshal(b, &x) // want `json.Unmarshal error discarded`
}

// A block marker on a line of its own covers the next line, like the
// line-comment form.
func standaloneBlock(b []byte) {
	var x v
	/* gofastr:allow(discardeddecode) best-effort render of validated bytes */
	_ = json.Unmarshal(b, &x)
}

// A block marker whose only text after the parenthesis is the closing
// delimiter has no reason, and silences nothing.
func bareBlock(b []byte) {
	var x v
	/* gofastr:allow(discardeddecode) */
	_ = json.Unmarshal(b, &x) // want `json.Unmarshal error discarded`
}
