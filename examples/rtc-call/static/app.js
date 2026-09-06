// RTC call browser glue: the one script the room page loads.
//
// Responsibilities, and nothing else:
//   - the rtc runtime module (__gofastr.connectRoom): rooms, one
//     RTCPeerConnection per remote peer, perfect negotiation, the
//     negotiated 'chat' data channel, reconnect generations;
//   - getUserMedia: camera and microphone out to every peer.
//
// The server rendered every element this script touches. It flips
// `hidden`, sets textContent and srcObject, and clones the two
// templates (#call-tile, #call-chat-line). No innerHTML; the only
// element creation is template.content.cloneNode(true).
//
// window.__call is the bounded, metadata-safe debug surface: phase,
// peer ids, connection states, chat lines and statuses received. No
// SDP, no candidates, no credentials, no close reasons.
(() => {
  'use strict';

  const root = document.getElementById('call-root');
  if (!root) return;
  const cfg = {
    room: root.dataset.callRoom || '',
    wsPath: root.dataset.callWs || '',
  };
  if (!cfg.room || !cfg.wsPath) return;

  const byId = (id) => document.getElementById(id);
  const grid = byId('call-grid');
  const tileTpl = byId('call-tile');
  const lineTpl = byId('call-chat-line');
  if (!grid || !tileTpl || !lineTpl) return;

  // Bounded diagnostics + test surface. Arrays cap so a long call
  // cannot grow them without bound.
  const CAP = 64;
  const push = (arr, v) => { arr.push(v); if (arr.length > CAP) arr.shift(); };
  window.__call = {
    phase: 'boot',
    peers: [],       // current peer ids
    states: {},      // peer id -> connectionState
    chat: [],        // {name, text} received
    statuses: [],    // {id, status} received
    media: 'idle',
    muted: false,
  };
  const dbg = window.__call;

  let room = null;
  let localStream = null;
  let audioTrack = null;
  const tiles = new Map(); // peer id -> tile element

  function wsURL() {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    return proto + '//' + window.location.host + cfg.wsPath + '?room=' + encodeURIComponent(cfg.room);
  }

  // ── Live region rendering ─────────────────────────────────────
  // Every widget was server-rendered; this only flips `hidden` and
  // sets textContent, except the two template clones below.

  function trackPeers() {
    dbg.peers = room ? Array.from(room.peers.keys()) : [];
  }

  function tileFor(id) { return tiles.get(id) || null; }

  // The server-rendered notice: the only surface that tells a person
  // why nothing happened (camera refused, room full). Empty text hides it.
  function notice(text) {
    const box = byId('call-notice');
    const slot = byId('call-notice-text');
    if (!box || !slot) return;
    slot.textContent = text;
    box.hidden = !text;
  }

  function addTile(peer) {
    const clone = tileTpl.content.cloneNode(true);
    const tile = clone.firstElementChild;
    tile.setAttribute('data-peer', peer.id);
    const name = tile.querySelector('[data-call-name]');
    if (name) name.textContent = peer.name || peer.id;
    grid.appendChild(clone);
    tiles.set(peer.id, tile);
  }

  function removeTile(id) {
    const tile = tiles.get(id);
    if (tile) { tile.remove(); tiles.delete(id); }
  }

  function setMutedPill(tile, muted) {
    if (!tile) return;
    const on = tile.querySelector('[data-call-pill="muted-on"]');
    const off = tile.querySelector('[data-call-pill="muted-off"]');
    if (on) on.hidden = !muted;
    if (off) off.hidden = !!muted;
  }

  function addChatLine(name, text) {
    const clone = lineTpl.content.cloneNode(true);
    const row = clone.firstElementChild;
    const who = row.querySelector('[data-call-chat-name]');
    const what = row.querySelector('[data-call-chat-text]');
    if (who) who.textContent = name;
    if (what) what.textContent = text;
    const list = byId('call-chat');
    if (list) {
      list.appendChild(clone);
      // Bound the list the same way dbg arrays are bounded: drop the
      // oldest row, never rebuild the list.
      while (list.children.length > 200) list.firstElementChild.remove();
    }
  }

  // ── The room ──────────────────────────────────────────────────

  function connect() {
    room = window.__gofastr.connectRoom(wsURL(), {
      channels: ['chat'],
      onPeer(peer) {
        addTile(peer);
        trackPeers();
      },
      onPeerLeave(peer) {
        removeTile(peer.id);
        delete dbg.states[peer.id];
        trackPeers();
      },
      onTrack(peer, ev) {
        const tile = tileFor(peer.id);
        if (!tile) return;
        const video = tile.querySelector('[data-call-video]');
        if (video && ev.streams && ev.streams[0]) {
          video.srcObject = ev.streams[0];
          video.hidden = false;
          video.play().catch(() => {}); // autoplay refused: the frame is still there
        }
      },
      onStatus(peer, status) {
        push(dbg.statuses, { id: peer.id, status: status });
        if (status && typeof status.muted === 'boolean') setMutedPill(tileFor(peer.id), status.muted);
      },
      onMessage(peer, ch, ev) {
        if (ch.label !== 'chat') return;
        const text = String(ev.data);
        push(dbg.chat, { name: peer.name || peer.id, text: text });
        addChatLine(peer.name || peer.id, text);
      },
      onPeerState(peer, state) {
        dbg.states[peer.id] = state;
      },
      onPhase(phase) {
        dbg.phase = phase;
        // The controls come alive with the room: until then a share
        // has nothing to publish to and a chat submit nothing to ride.
        if (phase === 'hydrated') {
          ['call-share', 'call-chat-input', 'call-chat-send'].forEach((id) => { const el = byId(id); if (el) el.disabled = false; });
        }
        // A refusal is final: the module does not retry it, so say why.
        if (phase === 'closed:refused') {
          notice(room && room.status.refused === 409 ? 'This room is full.' : 'You cannot join this room.');
        }
      },
    });
  }

  // ── Controls ──────────────────────────────────────────────────

  byId('call-share').addEventListener('click', () => {
    if (localStream) return;
    navigator.mediaDevices.getUserMedia({ video: { width: 640, height: 480 }, audio: true })
      .then((stream) => {
        if (!room) {
          // The module failed after the button was enabled: release
          // the devices rather than hold a camera nobody can see.
          stream.getTracks().forEach((t) => t.stop());
          notice('The call is not connected; camera released.');
          return;
        }
        localStream = stream;
        audioTrack = stream.getAudioTracks()[0] || null;
        const local = byId('call-local');
        if (local) { local.srcObject = stream; local.hidden = false; }
        dbg.media = 'sharing';
        notice('');
        const mute = byId('call-mute');
        if (mute) mute.disabled = false;
        stream.getTracks().forEach((t) => room.addTrack(t, stream));
      })
      .catch((err) => {
        const name = (err && err.name) || 'Error';
        dbg.media = 'denied:' + name;
        notice('Camera or microphone unavailable (' + name + '). Check the browser permission and try again.');
      });
  });

  byId('call-mute').addEventListener('click', () => {
    if (!audioTrack) return;
    dbg.muted = !dbg.muted;
    audioTrack.enabled = !dbg.muted;
    const mute = byId('call-mute');
    if (mute) mute.textContent = dbg.muted ? 'Unmute' : 'Mute';
    room.setStatus({ muted: dbg.muted });
  });

  // Leave is a server-rendered link, not a handler: this script is
  // document-scoped, so the runtime loads the lobby as a real
  // document and the teardown closes the socket and the tracks.

  const chatForm = byId('call-chat-form');
  if (chatForm) {
    chatForm.addEventListener('submit', (ev) => {
      ev.preventDefault();
      const input = byId('call-chat-input');
      if (!input || !input.value.trim() || !room) return;
      const sent = room.send('chat', input.value);
      if (sent > 0) {
        // Your own line, or the transcript reads as a send that failed.
        addChatLine((room.self && room.self.name) || 'You', input.value);
        input.value = '';
        notice('');
      } else {
        notice('Nobody is connected yet; your message was not sent.'); // keep the text
      }
    });
  }

  window.__gofastr.loadModule('rtc').then(() => {
    dbg.phase = 'connecting';
    connect();
  }).catch(() => { dbg.phase = 'rtc-module-failed'; });
})();
