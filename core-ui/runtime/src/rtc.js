// GoFastr runtime module, WebRTC rooms
//
// The browser half of the rtc signaling protocol (server package
// battery/rtc): one room of peers over the 'ws' module's sequenced
// WebSocket. One RTCPeerConnection per remote
// peer, perfect negotiation, trickle ICE, negotiated data channels,
// per-peer status documents, and renegotiation across WebSocket
// generations. Media never touches the signaling server; SDP and
// candidates cross it as opaque, addressed signal frames.
//
// Loaded on demand via __gofastr.loadModule('rtc'); there is no DOM
// marker, an application calls the API directly:
//
//   const room = __gofastr.connectRoom(url, opts)
//
// The 'ws' module is loaded inside a one-time init before the first
// socket opens. Nothing here logs: SDP, candidates, credentials,
// status bodies, and close reasons never reach console output.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  let wsInit = null;
  const wsReady = () => (wsInit ||= NS.loadModule('ws'));

  // connectRoom(url, opts) joins the room at url. The wire is the rtc
  // protocol: snapshot/join/leave/status envelopes reduced through
  // createSequencedReducer, plus addressed, transient 'signal'
  // envelopes that are never reduced (they are per-pair negotiation
  // state, like the relayed frames of the remote-assist example).
  //
  // opts (all optional):
  //   iceServers  extra RTCIceServer entries, appended AFTER the list
  //               the server's snapshot carried
  //   rtc         extra RTCConfiguration fields merged in
  //   reconnect   false disables the ws module's reconnect
  //   channels    data channels; entry i gets id i, created
  //               negotiated:true on BOTH sides (no ondatachannel).
  //               A string is a reliable ordered channel by that
  //               label; an object {label, ...RTCDataChannelInit}
  //               sets the rest (ordered:false, maxRetransmits:0 for
  //               telemetry that must never queue behind old frames)
  //   onSnapshot({room, self, peers})   after each hydration
  //   onPeer(peer) / onPeerLeave(peer)  peer objects come and go
  //   onTrack(peer, ev)                 RTCTrackEvent
  //   onChannel(peer, ch)               a data channel opened
  //   onMessage(peer, ch, ev)           MessageEvent on a channel
  //   onPeerState(peer, state)          pc.connectionState changes
  //   onStatus(peer, status)            remote status document changed
  //   onPhase(phase)   'connecting'|'open'|'hydrated'|'closed:<class>'
  //                    ('closed:refused' when the server accepted the
  //                    socket only to refuse the join; the HTTP-like
  //                    status is on room.status.refused and nothing
  //                    retries)
  //
  // room.self is the PeerInfo or null before hydration; room.peers is
  // the Map<id, peer>; room.status is {generation, phase, peers,
  // refused}, mutated in place. room.send passes strings and binary
  // (ArrayBuffer, a view, Blob) through and JSON-encodes the rest;
  // room.replaceTrack swaps a sender's track with no renegotiation and
  // keeps the module's own list current for connections built later. peer = { id, role, name, user, order, status,
  // pc, polite, channels, state }. polite describes the REMOTE side:
  // the peer with the greater join order is the polite one (ties
  // break on the greater id), so this side defers in negotiation
  // exactly when peer.polite is false.
  //
  // Perfect negotiation is the W3C pattern per peer: makingOffer,
  // ignoreOffer, isSettingRemoteAnswerPending; the impolite side
  // ignores a colliding offer, the polite side lets
  // setRemoteDescription(offer) roll its local offer back implicitly,
  // addIceCandidate rejections are swallowed (a candidate that
  // outlived its offer is noise, and nothing here may log it).
  //
  // A locally-polite pc that has never negotiated (remoteDescription
  // null) does not offer from onnegotiationneeded; Chrome re-fires it
  // after the impolite side's offer is answered. (A round-1 probe
  // measured an endless empty-offer loop after a first-offer rollback
  // on that day's code; on Chrome 152 a plain rollback of a first
  // offer converges too. The rebuild below is belt and braces on a pc
  // with no transport to lose, kept because the glare test exercises
  // it and it costs nothing.) A 1500 ms fallback per pc covers an impolite
  // side with nothing to negotiate, and fires only while the pc is
  // stable and no remote offer has been seen. If the two first offers
  // still cross (the fallback and the impolite offer in flight at
  // once), the polite side does not roll back: it discards the
  // never-negotiated pc and answers from a fresh one, which has no
  // transport to break. Mid-call collisions roll back as the pattern
  // says. Offers are also burst-limited per pc: more than twelve in
  // five seconds is a loop, and the pc is rebuilt to leave it.
  //
  // Generations: a reconnect is a new WebSocket generation; peers
  // still present in the reconnect snapshot keep their pc while it is
  // connected or connecting (no renegotiation storm), anything else
  // is recreated with tracks and channels re-added. A reconnect that
  // lands under a NEW self id (the server mints one per socket when
  // the host sets no Join.PeerID) rebuilds every pc: each remote saw
  // this side leave and rebuilt its own, and a kept pc on this side
  // would face a fresh one it never negotiated with. After hydration
  // the remembered status is resent and the transport marked
  // resynchronized. On 'failed' the pc restarts ICE once per
  // generation, with the ICE list the latest snapshot or 'iceServers'
  // event carried (applied to live pcs via setConfiguration, so the
  // restart allocates with a credential that has not expired).
  NS.connectRoom = (url, opts) => {
    const o = opts || {};
    const status = { generation: 0, phase: 'connecting', peers: 0, refused: 0 };
    const peers = new Map();
    const roster = new Map();
    const nst = new WeakMap();
    const tracks = [];
    const chans = o.channels || [];
    let self = null, iceList = null, ws = null, reduce = null, statusDoc = null, dead = false, reborn = false;

    const hook = (name, a, b, c) => {
      if (Object.prototype.hasOwnProperty.call(o, name) && typeof o[name] === 'function') { try { o[name](a, b, c); } catch (_) {} }
    };
    const phase = (p) => { status.phase = p; hook('onPhase', p); };

    const iceCfg = () => Object.assign({ iceServers: (iceList || []).concat(o.iceServers || []) }, o.rtc);

    // Greater order is polite; equal order breaks on the greater id.
    const remotePolite = (info) => !!self && (info.order > self.order || (info.order === self.order && info.id > self.id));

    const send = (obj) => { if (ws) ws.send(obj); };
    const sig = (peer, type, data) => send({ kind: 'signal', to: peer.id, type: type, data: data });

    const newPC = (peer) => {
      const pc = new RTCPeerConnection(iceCfg());
      const st = { offer: false, ans: false, ice: false, timer: 0, remote: false, win: 0, n: 0, rechan: 0 };
      nst.set(pc, st);
      peer.pc = pc;
      peer.state = pc.connectionState;
      peer.channels = {};
      const offer = () => {
        const now = Date.now();
        if (now - st.win > 5000) { st.win = now; st.n = 0; }
        if (++st.n > 12) { closePC(peer); newPC(peer); return; }
        st.offer = true;
        pc.setLocalDescription().then(() => sig(peer, 'offer', { sdp: pc.localDescription }), () => {}).finally(() => { st.offer = false; });
      };
      pc.onnegotiationneeded = () => {
        // The local side is polite when the remote is not (peer.polite
        // describes the remote). A locally-polite pc that has never
        // negotiated (remoteDescription null) does not offer yet: the
        // initial glare would mean rollback, which wedges Chrome's
        // first ICE gathering (see the header). Chrome re-fires this
        // event once the impolite side's offer is answered and
        // signaling is stable again. One fallback timer per pc: if no
        // remote description has arrived when it fires, the impolite
        // side had nothing to negotiate and the polite side offers.
        if (!peer.polite && !pc.remoteDescription) {
          if (!st.timer) {
            st.timer = setTimeout(() => {
              st.timer = 0;
              if (!st.remote && !pc.remoteDescription && pc.signalingState === 'stable') offer();
            }, 1500);
          }
          return;
        }
        offer();
      };
      pc.onicecandidate = (ev) => { if (ev.candidate) sig(peer, 'ice', { candidate: ev.candidate.toJSON() }); };
      pc.ontrack = (ev) => hook('onTrack', peer, ev);
      pc.onconnectionstatechange = () => {
        peer.state = pc.connectionState;
        hook('onPeerState', peer, pc.connectionState);
        if (pc.connectionState === 'failed' && !st.ice) { st.ice = true; pc.restartIce(); }
      };
      for (let i = 0; i < tracks.length; i++) { try { pc.addTrack(tracks[i].track, tracks[i].stream); } catch (_) {} }
      for (let i = 0; i < chans.length; i++) mkChan(peer, pc, st, i);
    };

    // One negotiated channel on a pc. A channel dies with its SCTP
    // transport: when the remote rebuilt its pc after our reconnect,
    // its offer restarts ICE and DTLS on our kept pc, the old
    // association closes, and the pc stays connected with dead channel
    // objects. Recreate on the live pc (same negotiated id), bounded
    // per pc so a transport that is really gone cannot spin this.
    const mkChan = (peer, pc, st, i) => {
      const spec = typeof chans[i] === 'string' ? { label: chans[i] } : chans[i];
      const ch = pc.createDataChannel(spec.label, Object.assign({}, spec, { negotiated: true, id: i }));
      try { ch.binaryType = 'arraybuffer'; } catch (_) {} // the spec default since 2024; Firefox lagged on blob
      peer.channels[spec.label] = ch;
      ch.onopen = () => hook('onChannel', peer, ch);
      ch.onmessage = (ev) => hook('onMessage', peer, ch, ev);
      ch.onclose = () => {
        if (dead || peer.pc !== pc || pc.connectionState === 'closed' || peer.channels[spec.label] !== ch) return;
        if (++st.rechan > chans.length * 4) return;
        mkChan(peer, pc, st, i);
      };
    };

    // Close one peer's pc, disarming its fallback timer with it (the
    // timer must not fire an offer out of a closed pc).
    const closePC = (peer) => {
      const pc = peer.pc;
      if (!pc) return;
      const st = nst.get(pc);
      if (st && st.timer) { clearTimeout(st.timer); st.timer = 0; }
      try { pc.close(); } catch (_) {}
    };

    // A join for an id already present is a peer that reconnected
    // (a replaced socket, or a leave the lossy lane dropped): its old
    // pc belongs to a browser context that no longer exists, so it is
    // dropped and rebuilt rather than leaked beside a second one.
    const makePeer = (info) => {
      if (peers.has(info.id)) dropPeer(info.id);
      const peer = {
        id: info.id, role: info.role || '', name: info.name || '', user: info.user || '',
        order: info.order, status: info.status || null, pc: null, polite: false, channels: {}, state: 'new',
      };
      peer.polite = remotePolite(info);
      peers.set(info.id, peer);
      status.peers = peers.size;
      newPC(peer);
      hook('onPeer', peer);
      if (peer.status) hook('onStatus', peer, peer.status);
    };
    const dropPeer = (id) => {
      const peer = peers.get(id);
      if (!peer) return;
      peers.delete(id);
      status.peers = peers.size;
      closePC(peer);
      hook('onPeerLeave', peer);
    };

    // One inbound signal frame, already addressed to this peer.
    const onSignal = (p) => {
      const peer = peers.get(p.from);
      if (!peer || !peer.pc || !p.data) return;
      let pc = peer.pc, st = nst.get(pc);
      const polite = !peer.polite;
      if (p.type === 'offer' || p.type === 'answer') {
        if (!p.data.sdp) return;
        st.remote = true;
        // W3C perfect negotiation: on a colliding offer (this side is
        // mid-offer, or not stable) the impolite side ignores it and
        // the polite side yields. ready reads the flag a still-pending
        // setRemoteDescription(answer) left (st.ans): an offer that
        // arrives during it is chained behind it on a connection that
        // is stable by then, so it is no collision. Writing this
        // frame's own type into the flag before the check turned that
        // offer into one and dropped it for good.
        const ready = !st.offer && (pc.signalingState === 'stable' || st.ans);
        if (!polite && p.type === 'offer' && !ready) return;
        // The polite side yields by rollback only on a pc that has
        // negotiated before. A first-offer collision discards the pc
        // and answers from a fresh one (see the header for why).
        if (p.type === 'offer' && !ready && !pc.remoteDescription) {
          closePC(peer);
          newPC(peer);
          pc = peer.pc;
          st = nst.get(pc);
          st.remote = true;
        }
        st.ans = p.type === 'answer';
        pc.setRemoteDescription(p.data.sdp).then(() => {
          if (st.timer) { clearTimeout(st.timer); st.timer = 0; }
          if (p.type === 'offer') {
            return pc.setLocalDescription().then(() => sig(peer, 'answer', { sdp: pc.localDescription }));
          }
        }, () => {}).finally(() => { st.ans = false; });
        return;
      }
      if (p.type === 'ice' && p.data.candidate) pc.addIceCandidate(p.data.candidate).catch(() => {});
    };

    // Reconcile the live peer objects against a fresh snapshot roster:
    // new peers get a pc, gone peers are closed, kept peers keep their
    // pc only while it is connected or connecting.
    const reconcile = () => {
      roster.forEach((info, id) => {
        const peer = peers.get(id);
        if (!peer) { makePeer(info); return; }
        peer.role = info.role || '';
        peer.name = info.name || '';
        peer.user = info.user || '';
        peer.order = info.order;
        peer.polite = remotePolite(info);
        if (info.status && JSON.stringify(info.status) !== JSON.stringify(peer.status)) {
          peer.status = info.status;
          hook('onStatus', peer, peer.status);
        }
        const cs = peer.pc ? peer.pc.connectionState : 'closed';
        const st = peer.pc ? nst.get(peer.pc) : null;
        if (st) { st.ice = false; st.rechan = 0; } // one restartIce, fresh channel budget, per generation
        if (reborn || (cs !== 'connected' && cs !== 'connecting')) {
          closePC(peer);
          newPC(peer);
        }
      });
      peers.forEach((peer, id) => { if (!roster.has(id)) dropPeer(id); });
      reborn = false;
    };

    // A refreshed ICE list (every snapshot, and the server's half-TTL
    // TURN credential push) reaches the live connections. A pc reads
    // its servers once, at construction; without this a kept pc would
    // restart ICE with a credential that has expired.
    const applyIce = () => {
      const cfg = iceCfg();
      peers.forEach((peer) => {
        const pc = peer.pc;
        if (!pc || pc.signalingState === 'closed' || typeof pc.setConfiguration !== 'function') return;
        try { pc.setConfiguration(cfg); } catch (_) {}
      });
    };

    const apply = (state, env) => {
      const p = env.payload || {};
      if (env.type === 'snapshot') {
        roster.clear();
        const was = self && self.id;
        self = p.self || null;
        reborn = !!(was && self && self.id !== was);
        iceList = p.iceServers || [];
        applyIce();
        const list = p.peers || [];
        for (let i = 0; i < list.length; i++) roster.set(list[i].id, list[i]);
      } else if (env.type === 'iceServers') {
        iceList = p.iceServers || [];
        applyIce();
      } else if (env.type === 'join') {
        roster.set(p.id, p);
      } else if (env.type === 'leave') {
        roster.delete(p.id);
      } else if (env.type === 'status') {
        const info = roster.get(p.id);
        if (info) info.status = p.status;
      }
      return state;
    };

    const afterHydrate = () => {
      phase('hydrated');
      if (statusDoc) send({ kind: 'status', data: statusDoc });
      ws.resyncComplete();
    };

    const onMessage = (info) => {
      const env = info.data;
      if (dead || !env || typeof env.type !== 'string') return;
      if (env.type === 'signal') { onSignal(env.payload || {}); return; }
      const res = reduce(env);
      if (env.type === 'snapshot') {
        if (res.applied) {
          reconcile();
          hook('onSnapshot', { room: (env.payload || {}).room, self: self, peers: peers });
        }
        // A snapshot the reducer refused is one not newer than the
        // state already applied; the transport is still hydrated.
        if (ws.hydrated(env.sequence, info.generation)) afterHydrate();
        return;
      }
      if (!res.applied) return;
      const p = env.payload || {};
      if (env.type === 'join') makePeer(p);
      else if (env.type === 'leave') dropPeer(p.id);
      else if (env.type === 'status') {
        const peer = peers.get(p.id);
        if (peer) { peer.status = p.status; hook('onStatus', peer, p.status); }
      }
    };

    const start = () => {
      reduce = NS.createSequencedReducer(roster, apply);
      ws = NS.connectWebSocket(url, {
        reconnect: o.reconnect,
        onGenerationStart: (i) => { status.generation = i.generation; phase('open'); },
        onMessage: onMessage,
        onGenerationEnd: (i) => {
          status.generation = i.generation;
          if (i.reasonClass === 'refused') status.refused = ws.status.refused;
          phase('closed:' + i.reasonClass);
        },
      });
    };

    const handle = {
      status: status,
      peers: peers,
      get self() { return self; },
      addTrack: (track, stream) => {
        if (!track || tracks.some((t) => t.track === track)) return;
        tracks.push({ track: track, stream: stream });
        peers.forEach((peer) => { if (peer.pc) { try { peer.pc.addTrack(track, stream); } catch (_) {} } });
      },
      removeTrack: (track) => {
        for (let i = tracks.length - 1; i >= 0; i--) if (tracks[i].track === track) tracks.splice(i, 1);
        peers.forEach((peer) => {
          if (!peer.pc) return;
          const senders = peer.pc.getSenders();
          for (let i = 0; i < senders.length; i++) if (senders[i].track === track) peer.pc.removeTrack(senders[i]);
        });
      },
      // A sender swap needs no renegotiation (camera switch, screen
      // share). The list new connections are built from is updated
      // too; track surgery through peer.pc alone left it stale.
      replaceTrack: (oldTrack, newTrack) => {
        // replaceTrack(t, null) is the spec's "stop sending on this
        // sender"; a null entry would make the next connection's
        // addTrack throw, so the entry goes rather than turning null.
        for (let i = tracks.length - 1; i >= 0; i--) {
          if (tracks[i].track !== oldTrack) continue;
          if (newTrack) tracks[i].track = newTrack; else tracks.splice(i, 1);
        }
        const swaps = [];
        peers.forEach((peer) => {
          if (!peer.pc) return;
          const senders = peer.pc.getSenders();
          for (let i = 0; i < senders.length; i++) if (senders[i].track === oldTrack) swaps.push(senders[i].replaceTrack(newTrack));
        });
        return Promise.all(swaps).then(() => undefined);
      },
      send: (label, data) => {
        let n = 0;
        const raw = typeof data === 'string' || data instanceof ArrayBuffer || ArrayBuffer.isView(data) || (typeof Blob !== 'undefined' && data instanceof Blob);
        const body = raw ? data : JSON.stringify(data);
        peers.forEach((peer) => {
          if (!Object.prototype.hasOwnProperty.call(peer.channels, label)) return;
          const ch = peer.channels[label];
          if (ch && ch.readyState === 'open') { try { ch.send(body); n += 1; } catch (_) {} }
        });
        return n;
      },
      setStatus: (obj) => { statusDoc = obj; if (self) self.status = obj; send({ kind: 'status', data: obj }); },
      close: () => {
        if (dead) return;
        dead = true;
        if (ws) ws.close();
        peers.forEach((peer) => dropPeer(peer.id));
      },
    };

    wsReady().then(() => { if (!dead) start(); }, () => phase('closed:error'));
    return handle;
  };

  (NS.loadedModules ||= {}).rtc = true;
})();
