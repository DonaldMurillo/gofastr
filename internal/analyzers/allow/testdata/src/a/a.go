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

func otherRule(b []byte) {
	var x v
	_ = json.Unmarshal(b, &x) //gofastr:allow(mapwriter) unrelated marker // want `json.Unmarshal error discarded`
}
