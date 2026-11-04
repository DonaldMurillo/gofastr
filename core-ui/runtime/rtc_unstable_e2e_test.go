package runtime

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestRTCReconnectRebuildsPendingOffer(t *testing.T) {
	env := rtcSetup(t)
	rtcPollTrue(t, env.a, `__room.peers.get('bravo')?.channels.chat?.readyState==='open'`, 15*time.Second)
	rtcPollTrue(t, env.b, `__room.peers.get('alpha')?.channels.chat?.readyState==='open'`, 15*time.Second)
	rtcFront(t, env.a, chromedp.Evaluate(`window.__old=__room.peers.get('bravo').pc;
window.__generation=__room.status.generation;
const remote=__old.setRemoteDescription.bind(__old);
__old.setRemoteDescription=description=>{if(description.type==='answer'){window.__lostAnswer=true;return Promise.resolve()}return remote(description)};
__old.restartIce();`, nil))
	rtcPollTrue(t, env.a, `window.__lostAnswer && __old.signalingState==='have-local-offer' && __old.connectionState==='connected'`, 15*time.Second)
	if !env.room.kill("alpha") {
		t.Fatal("no alpha socket to reconnect")
	}
	rtcPollTrue(t, env.a, `__room.status.generation>__generation && __room.status.phase==='hydrated'`, 20*time.Second)
	rtcPollTrue(t, env.a, `__room.peers.get('bravo')?.pc!==__old`, 5*time.Second)
	rtcPollTrue(t, env.a, `__room.peers.get('bravo')?.channels.chat?.readyState==='open'`, 15*time.Second)
	rtcFront(t, env.b, chromedp.Evaluate(`__room.send('chat','after-pending-offer')`, nil))
	rtcPollTrue(t, env.a, `__rtc.chanMsgs.includes('bravo:chat:after-pending-offer')`, 10*time.Second)
}
