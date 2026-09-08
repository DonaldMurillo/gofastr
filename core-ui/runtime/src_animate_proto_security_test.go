package runtime

import (
	"strings"
	"testing"
)

// Pins: reserved keys (__proto__/constructor/prototype) never reach the
// runtime's shared registry writes — animate guards the slot creation
// inline, the toast timer registry is keyed by Map. (2026-09-06
// adversarial pass, round 5.)
// Property: reserved keys (__proto__/constructor/prototype) never reach the runtime's shared
// registry writes — the kernel guards setSignal and both seed loops with isReservedSignalKey
// (runtime.js:617/647/823/1463); the animate and toast slot-creation writes bypass it.
// Surfaces: src/animate.js::wire — G._signals[name] = slot;
// src/toasts.js::_initToasts — NS._toastTimers[id] = rec.
// Finding: [animate-signal-proto-write] data-fui-animate-signal="__proto__" re-parents the
// shared signal store via the __proto__ setter (the own-prop read at wire() does not stop the
// write); the kernel's seed-merge then writes into the planted prototype and signals go
// permanently dead client-side. [toasts-proto-write] data-fui-toast-id="__proto__" re-parents
// the toast timer registry the same way; the for-in cleanup in _initToasts then mis-enumerates
// inherited keys and clearTimeout cancels the planted toast's own timer.
// Fix direction: run the same reserved-key skip the kernel seed loops use (isReservedSignalKey)
// before both slot-creation writes (or key the toast registry by Map).

// TestAnimateRedReservedKeyWrite pins the animate module's slot-creation
// write to the shared signal store: the attribute-borne signal name must be
// rejected as a reserved key before `G._signals[name] = slot`. Acceptance is
// the same approximation TestSeedLoopsSkipReservedKeys uses for the kernel's
// seed loops: a helper call (isReservedSignalKey) or an inline check naming
// all three reserved keys, before the assignment.
func TestAnimateRedReservedKeyWrite(t *testing.T) {
	src := readSrc(t, "src/animate.js")
	start := strings.Index(src, "const name = el.getAttribute('data-fui-animate-signal');")
	if start < 0 {
		t.Fatalf("setup broken: could not locate wire()'s signal-name read in src/animate.js")
	}
	endRel := strings.Index(src[start:], "// Avoid double-wiring")
	if endRel < 0 {
		t.Fatalf("setup broken: could not locate '// Avoid double-wiring' after the signal-name read in src/animate.js")
	}
	body := src[start : start+endRel]
	write := strings.Index(body, "G._signals[name] = slot")
	if write < 0 {
		// A Map re-keying of the store would change the write spelling; that
		// is secure, anything else is surface drift.
		if strings.Contains(body, "G._signals.set(") {
			return
		}
		t.Fatalf("setup broken: could not locate the G._signals[name] = slot creation write in src/animate.js wire()")
	}
	region := body[:write]
	hasHelper := strings.Contains(region, "isReservedSignalKey(")
	hasInline := strings.Contains(region, "__proto__") &&
		strings.Contains(region, "constructor") &&
		strings.Contains(region, "prototype")
	if !hasHelper && !hasInline {
		t.Errorf("SECURITY: [animate-signal-proto-write] wire() creates G._signals[name] with no reserved-key guard — data-fui-animate-signal=\"__proto__\" re-parents the shared signal store via the __proto__ setter, the kernel's seed-merge then writes into the planted prototype, and signals go permanently dead client-side (the kernel guards setSignal and both seed loops with isReservedSignalKey; this write bypasses it). Region:\n%s", region)
	}
}

// TestToastsRedReservedKeyWrite pins the toast module's timer-registry
// write: the attribute-borne toast id must be rejected as a reserved key
// (or the registry keyed by Map) before `NS._toastTimers[id] = rec`.
// Acceptance mirrors TestAnimateRedReservedKeyWrite: helper call or inline
// check naming all three reserved keys before the write, or a Map .set(
// spelling.
func TestToastsRedReservedKeyWrite(t *testing.T) {
	src := readSrc(t, "src/toasts.js")
	start := strings.Index(src, "const id = item.getAttribute('data-fui-toast-id');")
	if start < 0 {
		t.Fatalf("setup broken: could not locate _initToasts' id read in src/toasts.js")
	}
	endRel := strings.Index(src[start:], "// Cancel timers")
	if endRel < 0 {
		t.Fatalf("setup broken: could not locate '// Cancel timers' after the id read in src/toasts.js")
	}
	body := src[start : start+endRel]
	write := strings.Index(body, "NS._toastTimers[id] = rec")
	if write < 0 {
		if strings.Contains(body, "NS._toastTimers.set(") {
			return // Map-keyed registry: plain string keys, no re-parenting
		}
		t.Fatalf("setup broken: could not locate the NS._toastTimers[id] = rec write in src/toasts.js _initToasts()")
	}
	region := body[:write]
	hasHelper := strings.Contains(region, "isReservedSignalKey(")
	hasInline := strings.Contains(region, "__proto__") &&
		strings.Contains(region, "constructor") &&
		strings.Contains(region, "prototype")
	if !hasHelper && !hasInline {
		t.Errorf("SECURITY: [toasts-proto-write] _initToasts writes NS._toastTimers[id] with no reserved-key guard — data-fui-toast-id=\"__proto__\" re-parents the registry via the __proto__ setter, the for-in cleanup then mis-enumerates inherited keys and clearTimeout cancels the planted toast's own timer. Region:\n%s", region)
	}
}
