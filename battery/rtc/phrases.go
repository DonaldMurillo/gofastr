package rtc

import "github.com/DonaldMurillo/gofastr/core/i18n"

var (
	reasonInvalidJoin    = i18n.Define("rtc.invalid_join", "invalid join")
	reasonTryAgain       = i18n.Define("rtc.try_again", "try again")
	reasonSignalerClosed = i18n.Define("rtc.signaler_closed", "signaler closed")
	reasonRoomFull       = i18n.Define("rtc.room_full", "room full")
	reasonUserRequired   = i18n.Define("rtc.user_required", "sign-in required")
)
