// GoFastr runtime module, sequenced WebSocket client
//
// The browser half of core/stream.StateChannel's contracts (#375,
// #375's ordering rule and #377's reconnect generations). Loaded on
// demand via __gofastr.loadModule('ws'); there is no DOM marker, an
// application calls the API directly:
//
//   const ws = __gofastr.connectWebSocket(url, { onMessage, ... })
//   const reduce = __gofastr.createSequencedReducer(init, apply)
//
// Nothing here logs. Close reasons, message payloads, and credentials
// never reach console output; the only close classification that
// leaves this module is a bounded reason class.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  // createSequencedReducer(initialState, apply) returns reduce(envelope)
  // that applies ONLY envelopes whose `sequence` is strictly greater
  // than the last applied one. A reconnect snapshot captured before a
  // live mutation therefore cannot resurrect the state that mutation
  // replaced: sequence <= applied is rejected, whatever its payload.
  // apply(state, envelope, sequence) returns the next state; the
  // reducer returns { applied, state, appliedSequence }.
  // appliedSequence starts at -1, "nothing applied": a channel whose
  // source has never mutated sends its hydration snapshot at sequence
  // 0, and that snapshot must apply.
  NS.createSequencedReducer = (initialState, apply) => {
    let state = initialState;
    let appliedSequence = -1;
    return (envelope) => {
      const seq = envelope ? Number(envelope.sequence) : NaN;
      if (!Number.isFinite(seq) || seq <= appliedSequence) {
        return { applied: false, state, appliedSequence };
      }
      appliedSequence = seq;
      state = apply(state, envelope, seq);
      return { applied: true, state, appliedSequence };
    };
  };

  // connectWebSocket(url, opts) manages ONE WebSocket with reconnect
  // and per-reconnect GENERATIONS. A socket that reconnects is a new
  // generation; generation ids are distinct per reconnect so the
  // application can tell "the transport came back" from "the work I
  // started on the old transport is still valid". A new generation
  // invalidates only generation-bound work — which work survives is
  // the application's decision, and a healthy application protocol
  // can ride across a transient reconnect without teardown.
  //
  // opts (all optional):
  //   onGenerationStart({generation, resumedAfterSequence}) - socket open
  //   onHydrated({generation, snapshotSequence})            - handle.hydrated(seq, gen)
  //   onGenerationEnd({generation, reasonClass})            - socket gone
  //   onMessage({data, raw, generation})                    - data: parsed JSON or null
  //   reconnect: false disables reconnect (default true)
  //
  // Hooks are idempotent per generation: onGenerationStart fires once
  // per open, onGenerationEnd once per close (error and close both
  // firing still ends the generation once), handle.hydrated() and
  // handle.resyncComplete() only act once per generation.
  //
  // reasonClass is one of 'closed' (clean close), 'error' (transport
  // failure or refused connect), 'stop' (handle.close()). The raw
  // close reason and code are never exposed or recorded.
  //
  // Transport connected, state hydrated, protocol resynchronized, and
  // application ready are DISTINCT states: phase moves
  // connecting → open (onGenerationStart) → hydrated
  // (handle.hydrated) → resynced (handle.resyncComplete), and where a
  // protocol stalls is the application's call. WebSocket recovery
  // proves nothing about media protocols layered on top (WebRTC and
  // friends): those must resynchronize through these hooks, not assume
  // a live socket means a live session.
  NS.connectWebSocket = (url, opts) => {
    const o = opts || {};
    const status = { generation: 0, phase: 'connecting', reasonClass: '', attempts: 0, lastSequence: 0 };
    let generation = 0;
    let socket = null;
    let retryTimer = 0;
    let userClosed = false;
    let endedGeneration = 0;
    let hydratedGeneration = 0;
    let resyncGeneration = 0;

    const hook = (name, info) => {
      if (Object.prototype.hasOwnProperty.call(o, name) && typeof o[name] === 'function') { try { o[name](info); } catch (_) {} }
    };

    const endGeneration = (reasonClass) => {
      if (endedGeneration === generation) return;
      endedGeneration = generation;
      status.phase = 'closed';
      status.reasonClass = reasonClass;
      hook('onGenerationEnd', { generation, reasonClass });
      if (!userClosed && o.reconnect !== false) {
        status.attempts += 1;
        retryTimer = setTimeout(open, Math.min(1000 * 2 ** (status.attempts - 1), 30000));
      }
    };

    const open = () => {
      if (userClosed) return;
      generation += 1;
      status.generation = generation;
      status.phase = 'connecting';
      status.reasonClass = '';
      let ws = null;
      try { ws = new WebSocket(url); } catch (_) { endGeneration('error'); return; }
      socket = ws;
      ws.onopen = () => {
        status.attempts = 0;
        status.phase = 'open';
        hook('onGenerationStart', { generation, resumedAfterSequence: status.lastSequence });
      };
      ws.onmessage = (ev) => {
        let data = null;
        try { data = JSON.parse(ev.data); } catch (_) { data = null; }
        const seq = data ? Number(data.sequence) : NaN;
        if (Number.isFinite(seq) && seq > status.lastSequence) status.lastSequence = seq;
        hook('onMessage', { data, raw: ev.data, generation });
      };
      ws.onerror = () => { /* classified by the onclose that follows */ };
      ws.onclose = (ev) => {
        if (socket !== ws) return;
        socket = null;
        endGeneration(ev && ev.wasClean ? 'closed' : 'error');
      };
    };

    const handle = {
      // Live status object, mutated in place (same shape as sseStatus).
      status,
      send: (data) => {
        if (!socket || socket.readyState !== 1) return false;
        socket.send(typeof data === 'string' ? data : JSON.stringify(data));
        return true;
      },
      // The application applied the reconnect snapshot; snapshotSequence
      // defaults to the highest sequence observed. Once per generation,
      // and only for the CURRENT generation: pass the generation the
      // snapshot came from (onGenerationStart / onMessage carry it) so
      // an async apply from generation 1 finishing after generation 2
      // opened cannot mark generation 2 hydrated.
      hydrated: (snapshotSequence, forGeneration) => {
        if (forGeneration !== undefined && forGeneration !== generation) return false;
        if (status.phase !== 'open') return false;
        if (hydratedGeneration === generation) return false;
        hydratedGeneration = generation;
        const seq = Number.isFinite(Number(snapshotSequence)) ? Number(snapshotSequence) : status.lastSequence;
        if (seq > status.lastSequence) status.lastSequence = seq;
        status.phase = 'hydrated';
        hook('onHydrated', { generation, snapshotSequence: seq });
        return true;
      },
      // The application finished resynchronizing its protocol (media,
      // presence, whatever) on this generation. Once per generation,
      // only after hydrated, and only for the generation named (same
      // rule as hydrated): the phases are open → hydrated → resynced,
      // never out of order.
      resyncComplete: (forGeneration) => {
        if (forGeneration !== undefined && forGeneration !== generation) return false;
        if (status.phase !== 'hydrated') return false;
        if (resyncGeneration === generation) return false;
        resyncGeneration = generation;
        status.phase = 'resynced';
        return true;
      },
      close: () => {
        if (userClosed) return;
        userClosed = true;
        clearTimeout(retryTimer);
        const s = socket;
        socket = null;
        endGeneration('stop');
        status.phase = 'stopped';
        if (s) { try { s.close(); } catch (_) {} }
      },
    };

    open();
    return handle;
  };

  (NS.loadedModules ||= {}).ws = true;
})();
