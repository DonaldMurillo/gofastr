// Remote assist browser glue: the one script the session pages load.
//
// Responsibilities, and nothing else:
//   - the WebSocket signaling client (runtime module 'ws':
//     connectWebSocket + createSequencedReducer), which owns ordering
//     and reconnect generations;
//   - the WebRTC call: getUserMedia (video only, never audio), offer /
//     answer / trickle ICE relayed through the socket, remote video.
//
// Commands (send instruction, acknowledge) go through ordinary form
// POSTs enhanced to fetch, so the page works before this script loads
// and the server keeps one mutation path. Live state arrives on the
// socket; nothing here polls.
//
// window.__assist is the bounded, metadata-safe debug surface: phases,
// envelope types, sequences, connection states. No SDP, no camera
// frames, no credentials, no close reasons.
(() => {
  'use strict';

  const root = document.getElementById('assist-root');
  if (!root) return;
  const cfg = {
    session: root.dataset.assistSession || '',
    role: root.dataset.assistRole || '',
    wsPath: root.dataset.assistWs || '',
  };
  if (!cfg.session || !cfg.role || !cfg.wsPath) return;

  const byId = (id) => document.getElementById(id);
  const show = (el, on) => { if (el) el.hidden = !on; };

  // Bounded diagnostics + test surface.
  window.__assist = {
    role: cfg.role,
    generations: 0,
    phase: 'boot',
    media: 'idle',
    applied: [],      // envelope type + sequence, in apply order
    signaling: 'none',
    pcState: 'new',
  };
  const dbg = window.__assist;
  const note = (k, v) => { dbg[k] = v; };

  // A stable per-tab client id: reconnects reuse it, so the server's
  // WSConfig.ConnectionID (session-role-client) correlates generations.
  const clientKey = '__assist_client_' + cfg.session;
  let clientId = null;
  try { clientId = sessionStorage.getItem(clientKey); } catch (_) { /* private mode */ }
  if (!clientId) {
    clientId = String(Math.random().toString(36).slice(2, 10));
    try { sessionStorage.setItem(clientKey, clientId); } catch (_) { /* ignore */ }
  }

  // ── Live region rendering ─────────────────────────────────────
  // The server rendered every widget; this only flips `hidden` and
  // sets textContent on elements with stable ids.

  function renderInstruction(text) {
    const el = byId('assist-instruction-text');
    if (el) el.textContent = text === '' ? 'No instruction yet.' : text;
  }

  function renderState(state) {
    renderInstruction(state.instruction || '');
    flip('assist-pill-op', !!state.operatorOnline);
    flip('assist-pill-media', !!state.mediaUp);
    flip('assist-pill-ack', !!state.acked);
    const inv = byId('assist-invocation');
    if (inv) {
      inv.textContent = state.invocation
        ? 'Last command: agent invocation ' + state.invocation.slice(0, 8) + '.'
        : 'Last command: manual or page button.';
    }
  }

  // flip shows exactly one of a pillPair's two StatusPills.
  function flip(idBase, on) {
    show(byId(idBase + '-on'), on);
    show(byId(idBase + '-off'), !on);
  }

  // ── The sequenced protocol ────────────────────────────────────

  const initialState = { instruction: '', invocation: '', acked: false, mediaUp: false, operatorOnline: false, supportOnline: false };

  function apply(state, env) {
    const next = Object.assign({}, state);
    switch (env.type) {
      case 'snapshot': {
        const p = env.payload || {};
        next.instruction = p.instruction || '';
        next.invocation = p.invocation || '';
        next.acked = !!p.acked;
        next.mediaUp = !!p.mediaUp;
        next.operatorOnline = !!p.operatorOnline;
        next.supportOnline = !!p.supportOnline;
        break;
      }
      case 'instruction':
        next.instruction = (env.payload && env.payload.instruction) || '';
        next.invocation = (env.payload && env.payload.ref) || ''; // support only; absent for the operator
        next.acked = false;
        break;
      case 'clear':
        next.instruction = '';
        next.invocation = (env.payload && env.payload.ref) || '';
        next.acked = false;
        break;
      case 'ack':
        next.acked = true;
        break;
      case 'presence':
        if (env.payload && env.payload.role === 'operator') next.operatorOnline = !!env.payload.online;
        if (env.payload && env.payload.role === 'support') next.supportOnline = !!env.payload.online;
        break;
      case 'media':
        next.mediaUp = !!(env.payload && env.payload.mediaUp);
        break;
      default:
        break; // signal envelopes feed the media layer, not page state
    }
    return next;
  }

  // Created once the 'ws' module is loaded: the factory lives there.
  let reduce = null;

  let sock = null;
  let pc = null;
  let localStream = null;

  function wsURL() {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    return proto + '//' + window.location.host + cfg.wsPath + '?client=' + clientId;
  }

  function send(obj) {
    if (!sock) return false;
    return sock.send(obj);
  }

  function startSocket() {
    sock = window.__gofastr.connectWebSocket(wsURL(), {
      onGenerationStart(info) {
        note('phase', 'open');
        dbg.generations = info.generation;
      },
      onMessage(info) {
        const env = info.data;
        if (!env) return;
        if (env.type === 'signal') {
          onSignal(env.payload || {});
          return;
        }
        const res = reduce(env);
        if (res.applied) {
          if (dbg.applied.length < 64) dbg.applied.push(env.type + ':' + env.sequence);
          renderState(res.state);
        }
        // A snapshot the reducer refused is one that is not newer than
        // the state already on the page: hydration is satisfied either
        // way. (In this app every disconnect bumps the version through
        // presence, so a reconnect snapshot is always newer; the rule
        // matters for a protocol whose disconnects are silent.) Bind
        // the call to the generation the message came from.
        if (env.type === 'snapshot' && sock.hydrated(env.sequence, info.generation)) {
          note('phase', 'hydrated');
          // A live socket proves nothing about media: renegotiate the
          // peer through the fresh generation (operator re-offers).
          if (cfg.role === 'operator' && localStream) sendOffer();
        }
      },
      onGenerationEnd(info) {
        note('phase', 'closed:' + info.reasonClass);
      },
    });
  }

  // ── Commands: enhance the server-rendered forms to fetch ──────

  function enhanceForm(id) {
    const form = byId(id);
    if (!form) return;
    form.addEventListener('submit', (ev) => {
      ev.preventDefault();
      const data = new URLSearchParams(new FormData(form));
      window.fetch(form.action, {
        method: 'POST',
        body: data,
        credentials: 'same-origin',
      }).then((res) => { if (!res.ok) note('phase', 'command-rejected:' + res.status); })
        .catch(() => { note('phase', 'command-unreachable'); });
    });
  }
  enhanceForm('assist-manual-form');
  enhanceForm('assist-clear-form');
  enhanceForm('assist-ack-form');

  // ── WebRTC: camera out (operator), camera in (support) ────────
  //
  // iceServers is empty on purpose: peers on the same network reach
  // each other through host candidates, and this example runs its
  // browser test on one machine. README covers STUN/TURN for real
  // deployments. No audio track is ever requested.

  function newPC() {
    const pcx = new RTCPeerConnection({ iceServers: [] });
    pcx.onicecandidate = (ev) => {
      if (ev.candidate) {
        send({ kind: 'signal', to: peerRole(), type: 'ice', data: { candidate: ev.candidate.toJSON() } });
      }
    };
    // ontrack fires while setRemoteDescription is still resolving, so
    // the handler must exist before the offer is applied.
    pcx.ontrack = (ev) => {
      const remoteVideo = byId('assist-remote');
      if (remoteVideo && ev.streams && ev.streams[0]) {
        remoteVideo.srcObject = ev.streams[0];
        remoteVideo.hidden = false;
      }
    };
    pcx.onconnectionstatechange = () => {
      note('pcState', pcx.connectionState);
      const live = pcx.connectionState === 'connected';
      const failed = pcx.connectionState === 'failed';
      if (cfg.role === 'support') {
        const noteEl = byId('assist-media-note');
        if (noteEl && live) noteEl.textContent = 'Receiving the operator\u2019s camera.';
        if (noteEl && failed) noteEl.textContent = 'The media path failed. Ask the operator to share again.';
      }
      if (live || failed) send({ kind: 'media', up: live });
    };
    return pcx;
  }

  function peerRole() { return cfg.role === 'operator' ? 'support' : 'operator'; }

  function shareCamera() {
    const btn = byId('assist-share');
    if (btn) {
      btn.addEventListener('click', () => {
        if (localStream) return;
        const constraints = { video: { width: 640, height: 480 }, audio: false };
        navigator.mediaDevices.getUserMedia(constraints).then((stream) => {
          localStream = stream;
          const local = byId('assist-local');
          if (local) { local.srcObject = stream; local.hidden = false; }
          note('media', 'sharing');
          flip('assist-pill-media', true);
          ensurePC();
          localStream.getTracks().forEach((t) => pc.addTrack(t, localStream));
          sendOffer();
        }).catch((err) => { note('media', 'denied:' + (err && err.name)); });
      });
    }
  }

  function ensurePC() {
    if (!pc) pc = newPC();
    return pc;
  }

  function sendOffer() {
    ensurePC();
    note('signaling', 'offering');
    pc.createOffer().then((offer) => pc.setLocalDescription(offer)).then(() => {
      send({ kind: 'signal', to: peerRole(), type: 'offer', data: { sdp: pc.localDescription } });
    }).catch(() => { note('signaling', 'offer-failed'); });
  }

  function onSignal(sig) {
    if (!sig || !sig.data) return;
    note('signaling', sig.type);
    if (sig.type === 'offer') {
      ensurePC();
      const remote = new RTCSessionDescription(sig.data.sdp);
      pc.setRemoteDescription(remote).then(() => pc.createAnswer())
        .then((answer) => pc.setLocalDescription(answer)).then(() => {
        send({ kind: 'signal', to: peerRole(), type: 'answer', data: { sdp: pc.localDescription } });
      }).catch(() => { note('signaling', 'answer-failed'); });
      return;
    }
    if (sig.type === 'answer') {
      if (!pc) return;
      pc.setRemoteDescription(new RTCSessionDescription(sig.data.sdp)).catch(() => {
        note('signaling', 'answer-stale');
      });
      return;
    }
    if (sig.type === 'ice') {
      ensurePC();
      pc.addIceCandidate(new RTCIceCandidate(sig.data.candidate)).catch(() => {
        note('signaling', 'ice-stale'); // a candidate that outlived its generation
      });
    }
  }

  // Bridge diagnostics on the support console: did the browser even
  // register the tools? WithBridgeDebug exposes the bounded state.
  function reportBridge() {
    const el = byId('assist-bridge');
    if (!el) return;
    const tick = () => {
      const st = window.__gofastrWebMCP;
      if (st && st.attempted > 0) {
        el.textContent = st.registered + ' of ' + st.attempted + ' tools registered'
          + (st.supported ? '' : ' (WebMCP unsupported in this browser)');
        return;
      }
      setTimeout(tick, 250);
    };
    tick();
  }

  shareCamera();
  reportBridge();
  window.__gofastr.loadModule('ws').then(() => {
    reduce = window.__gofastr.createSequencedReducer(initialState, apply);
    note('phase', 'connecting');
    startSocket();
  }).catch(() => { note('phase', 'ws-module-failed'); });
})();
