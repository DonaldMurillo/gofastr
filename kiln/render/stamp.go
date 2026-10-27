package render

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// WorldStamp names a world by a short hash of its JSON form. Every
// rendered page carries the stamp of the world it was built from (on its
// reload script's URL), and the event stream's ready event carries the
// stamp the server holds, so a page that missed an edit between its
// render and its stream connecting can tell. Empty when the world does
// not encode, which turns the check off.
func WorldStamp(w *world.World) string {
	b, err := json.Marshal(w)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}
