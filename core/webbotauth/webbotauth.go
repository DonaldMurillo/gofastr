// Package webbotauth verifies inbound Web Bot Auth requests: RFC 9421
// HTTP Message Signatures under the profile of
// draft-meunier-webbotauth-httpsig-protocol-02 (18 August 2026), the
// IETF Web Bot Auth working-group draft. The draft is moving: it was
// renamed and reorganized in mid-2026 and Signature-Agent semantics
// changed between revisions. This package is experimental and tracks
// the draft deliberately; the pin above is the exact revision the
// verification rules implement.
//
// Verification is Ed25519 only (alg "ed25519", OKP keys). That is the
// algorithm every reference implementation in the draft's registry
// uses; RSA/ECDSA verification is out of scope until the draft
// stabilizes.
//
// The profile enforced per signature (draft section 5.2):
//
//   - tag MUST be "web-bot-auth"
//   - created and expires MUST be present and currently valid
//   - keyid MUST be the base64url JWK thumbprint of the signing key
//   - @authority or @target-uri MUST be covered
//   - the Signature-Agent member keyed to the signature label MUST be
//     covered (the legacy bare-string header is accepted and treated
//     as that single member, per draft section 5.2.1)
//
// Outcomes follow draft Appendix C.1: verified, invalid (failed a
// check), or unverified (not enough information to decide, e.g.
// discovery failed or the key is unknown). Only a verified outcome
// yields an Agent identity; policy on the other two is the caller's.
package webbotauth

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Agent is the identity established by a verified signature: the
// resolved directory URL (the draft's identifier, query and fragment
// discarded) plus the key thumbprint that verified the request.
type Agent struct {
	URL   string
	KeyID string
}

// Outcome classifies a verification run (draft Appendix C.1).
type Outcome uint8

const (
	// OutcomeUnverified: not enough information to decide (no
	// signature, discovery failed, unknown key).
	OutcomeUnverified Outcome = iota
	// OutcomeInvalid: a presented signature failed a profile or
	OutcomeInvalid
	// OutcomeVerified: a signature validated against a key the agent's
	// directory provided.
	OutcomeVerified
)

// maxClockSkewSeconds is how far in the future a created parameter may
// be without failing: signers and verifiers rarely share a clock
// source.
const maxClockSkewSeconds int64 = 60

func (o Outcome) String() string {
	switch o {
	case OutcomeVerified:
		return "verified"
	case OutcomeInvalid:
		return "invalid"
	default:
		return "unverified"
	}
}

// Result is one request's verification outcome.
type Result struct {
	Outcome Outcome
	// Retryable marks an outcome the caller reached for its own
	// reasons rather than the request's -- currently only
	// ErrResolverBusy. Require mode answers 503 rather than 403 for
	// these, because 403 tells a correctly-signed agent to go fix
	// credentials that are fine.
	Retryable bool
	Agent     *Agent // non-nil only for OutcomeVerified
	Label     string // signature label that produced the outcome
	Reason    string // human-readable, for logs
}

// Verifier verifies inbound signed requests. Construct with New and
// install with Middleware. Safe for concurrent use.
type Verifier struct {
	resolver *directoryResolver
	require  bool
	log      *slog.Logger
	now      func() time.Time
}

// New builds a Verifier. A nil logger silences the observe-mode logs.
func New(require bool, log *slog.Logger) *Verifier {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	v := &Verifier{require: require, log: log, now: time.Now}
	v.resolver = newDirectoryResolver(log)
	return v
}

// Middleware returns the verification middleware. In observe mode (the
// default, require == false) it never blocks: it annotates the request
// context with the verified identity and logs. In require mode a
// request that is not verified is refused with 403 and an
// Accept-Signature field asking for this profile (draft section 5.3).
// The default is observe on purpose: a verification bug in
// draft-tracking middleware must not be able to take an app's traffic
// down.
// maxSignaturesPerRequest caps how many Signature-Input members one
// request may present. Real senders sign once, occasionally twice during
// a key rotation; the cap exists so a sender cannot buy N outbound
// discovery round-trips with one cheap request.
const maxSignaturesPerRequest = 4

func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res := v.VerifyRequest(r)
		switch res.Outcome {
		case OutcomeVerified:
			r = r.WithContext(WithAgent(r.Context(), res.Agent))
			v.log.Debug("webbotauth: verified agent",
				"agent", res.Agent.URL, "keyid", res.Agent.KeyID, "label", res.Label)
		case OutcomeInvalid:
			v.log.Warn("webbotauth: invalid signature",
				"reason", res.Reason, "label", res.Label)
		default:
			v.log.Debug("webbotauth: unverified request", "reason", res.Reason)
		}
		if v.require && res.Outcome != OutcomeVerified && !isPublicDiscoveryPath(r.URL.Path) {
			if res.Retryable {
				// Our backpressure, not their signature. 403 would tell a
				// correctly-signed agent to fix credentials that are fine.
				w.Header().Set("Retry-After", "1")
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprintf(w, `{"title":"Web Bot Auth verification unavailable","status":503,"detail":%q}`+"\n", res.Reason)
				return
			}
			w.Header().Set("Accept-Signature", `wba=("@authority" "signature-agent");created;expires;keyid;tag="web-bot-auth";alg="ed25519"`)
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(w, `{"title":"Web Bot Auth signature required","status":403,"detail":%q}`+"\n", res.Reason)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isPublicDiscoveryPath reports whether a path must stay reachable
// without a verified signature. The site's own key directory is the
// only such path: a remote verifier fetches it to check the signature
// on requests we send, and refusing it in require mode means nobody can
// ever reach the state require mode demands.
func isPublicDiscoveryPath(path string) bool {
	return path == directoryWellKnown
}

// VerifyRequest runs the full verification pass over r. Multiple
// signatures are validated independently; the first verified signature
// attributes the request. If none verifies, any invalid signature is
// reported (before falling back to unverified).
func (v *Verifier) VerifyRequest(r *http.Request) Result {
	now := v.now()
	inputRaw := combineFieldValues(r.Header.Values("Signature-Input"))
	sigRaw := combineFieldValues(r.Header.Values("Signature"))
	if strings.TrimSpace(inputRaw) == "" {
		if strings.TrimSpace(sigRaw) == "" {
			return Result{Outcome: OutcomeUnverified, Reason: "no signature on request"}
		}
		return Result{Outcome: OutcomeInvalid, Reason: "Signature present without Signature-Input"}
	}
	inputs, err := parseSFDictionary(inputRaw)
	if err != nil {
		return Result{Outcome: OutcomeInvalid, Reason: "malformed Signature-Input"}
	}
	sigs, err := parseSFDictionary(sigRaw)
	if err != nil {
		return Result{Outcome: OutcomeInvalid, Reason: "malformed Signature"}
	}
	// One request can name as many Signature-Input members as it likes,
	// and each can cost a DNS check plus a directory fetch. The resolver
	// coalesces identical identifiers, which bounds nothing for a sender
	// naming distinct hosts on purpose, so the count is capped here.
	if len(inputs.members) > maxSignaturesPerRequest {
		return Result{Outcome: OutcomeInvalid,
			Reason: fmt.Sprintf("Signature-Input carries %d members, over the %d limit",
				len(inputs.members), maxSignaturesPerRequest)}
	}
	var firstInvalid *Result
	var lastReason string
	var retryable bool
	for _, m := range inputs.members {
		res := v.verifyOne(r, inputs, sigs, m, now)
		if res.Outcome == OutcomeVerified {
			return res
		}
		if res.Reason != "" {
			lastReason = res.Reason
		}
		// The aggregate below builds a fresh Result from the reason
		// string alone, so anything else the member carried has to be
		// carried explicitly or it is silently dropped.
		if res.Retryable {
			retryable = true
		}
		if res.Outcome == OutcomeInvalid && firstInvalid == nil {
			cp := res
			firstInvalid = &cp
		}
	}
	if firstInvalid != nil {
		return *firstInvalid
	}
	if lastReason == "" {
		lastReason = "no verifiable web-bot-auth signature"
	}
	return Result{Outcome: OutcomeUnverified, Reason: lastReason, Retryable: retryable}
}

// verifyOne validates the Signature-Input member under label against
// the message. The returned outcome follows the C.1 triad; a signature
// that is merely not-this-profile (wrong tag, unsupported algorithm) is
// skipped as unverified rather than marked invalid.
func (v *Verifier) verifyOne(r *http.Request, inputs, sigs *sfDictionary, m sfMember, now time.Time) Result {
	label := m.key
	if m.list == nil {
		return Result{Outcome: OutcomeInvalid, Label: label, Reason: "Signature-Input member is not an inner list"}
	}
	covered, params := m.list.items, m.list.params

	// Signature value for this label.
	sigEntry, err := signatureValue(sigs, label)
	if err != nil {
		return Result{Outcome: OutcomeInvalid, Label: label, Reason: err.Error()}
	}

	// Profile gates (draft 5.2 / 5.4).
	tagParam, ok := params.get("tag")
	if !ok || tagParam.typ != sfString || tagParam.str != "web-bot-auth" {
		return Result{Outcome: OutcomeUnverified, Label: label, Reason: "signature tag is not web-bot-auth; not a Web Bot Auth signature"}
	}
	created, ok := intParam(params, "created")
	if !ok {
		return Result{Outcome: OutcomeInvalid, Label: label, Reason: "missing created parameter"}
	}
	if now.Unix() < created-maxClockSkewSeconds {
		return Result{Outcome: OutcomeInvalid, Label: label, Reason: fmt.Sprintf("created %d is in the future", created)}
	}
	expires, ok := intParam(params, "expires")
	if !ok {
		return Result{Outcome: OutcomeInvalid, Label: label, Reason: "missing expires parameter"}
	}
	if now.Unix() >= expires {
		return Result{Outcome: OutcomeInvalid, Label: label, Reason: fmt.Sprintf("signature expired at %d (now %d)", expires, now.Unix())}
	}
	keyidItem, ok := params.get("keyid")
	if !ok || keyidItem.typ != sfString || keyidItem.str == "" {
		return Result{Outcome: OutcomeInvalid, Label: label, Reason: "missing keyid parameter"}
	}
	keyid := keyidItem.str
	if alg, ok := params.get("alg"); ok && (alg.typ != sfString || alg.str != "ed25519") {
		return Result{Outcome: OutcomeUnverified, Label: label, Reason: "unsupported signature algorithm; only ed25519 is verified"}
	}

	// Covered-component requirements.
	var hasAuthority bool
	for _, c := range covered {
		if c.typ != sfString {
			continue
		}
		if c.str == "@authority" || c.str == "@target-uri" {
			hasAuthority = true
		}
	}
	if !hasAuthority {
		return Result{Outcome: OutcomeInvalid, Label: label, Reason: "signature does not cover @authority or @target-uri"}
	}

	// Signature-Agent: the signature must cover the member whose URL
	// resolves its key (draft 5.2.1). The draft's normative text says
	// "the member keyed to the signature label", but its own E.1.1 and
	// E.2.1 vectors key the member differently from the label
	// (member agent2, label sig2) and bind them by the covered
	// component instead — the covered bytes are what the signature
	// actually protects. Rule: prefer the covered member keyed to this
	// signature's label; otherwise a single covered member is used; two
	// or more covered members with neither keyed to the label is
	// ambiguous and refuses attribution.
	var coveredAgentKeys []string
	var coveredBareAgent bool
	for _, c := range covered {
		if c.typ != sfString || c.str != "signature-agent" {
			continue
		}
		if len(c.p.list) == 0 {
			coveredBareAgent = true
			continue
		}
		if k, ok := c.p.get("key"); ok && k.typ == sfString && len(c.p.list) == 1 {
			coveredAgentKeys = append(coveredAgentKeys, k.str)
		}
	}
	memberKey := label
	switch {
	case slices.Contains(coveredAgentKeys, label):
		// The draft's D.2 shape: the member keyed to this label.
	case len(coveredAgentKeys) == 1 && !coveredBareAgent:
		// The draft's own E.1.1/E.2.1 vectors: a single covered member
		// keyed differently from the label.
		memberKey = coveredAgentKeys[0]
	case len(coveredAgentKeys) == 0 && coveredBareAgent:
		// Legacy bare-string header (draft E.1.2/E.2.2).
	default:
		return Result{Outcome: OutcomeInvalid, Label: label,
			Reason: "signature does not cover a single identifiable Signature-Agent member"}
	}
	if coveredBareAgent && len(coveredAgentKeys) > 0 {
		return Result{Outcome: OutcomeInvalid, Label: label,
			Reason: "signature covers both bare and keyed Signature-Agent components"}
	}
	agentHeader := combineFieldValues(r.Header.Values("Signature-Agent"))
	ref, _, err := agentRefFor(r.Context(), agentHeader, memberKey)
	if err != nil {
		if errors.Is(err, ErrResolverBusy) {
			// Not a verdict on the signature: we declined to look. The
			// package's own contract calls this unverified -- not enough
			// information to decide -- and the caller should retry.
			return Result{Outcome: OutcomeUnverified, Label: label,
				Reason: err.Error(), Retryable: true}
		}
		return Result{Outcome: OutcomeInvalid, Label: label, Reason: err.Error()}
	}

	// Signature base (RFC 9421 section 2.5). Any unresolvable covered
	// component fails the signature.
	reqCtx := newRequestCtx(r)
	base, err := buildSignatureBase(reqCtx, covered, params)
	if err != nil {
		return Result{Outcome: OutcomeInvalid, Label: label, Reason: "signature base: " + err.Error()}
	}

	// Key resolution: (identifier URL, keyid) lookup.
	set, err := v.resolver.resolve(r.Context(), ref)
	if err != nil {
		// Deliberately no Retryable here: nothing in the fetch path
		// produces a busy sentinel today, so a check would be dead code.
		// If one is ever added — the fetch semaphore in directory.go is
		// the obvious place — this site must match it the way the
		// Signature-Agent path above matches ErrResolverBusy, or a
		// saturated fetch budget will silently answer 403 in require
		// mode.
		return Result{Outcome: OutcomeUnverified, Label: label, Reason: err.Error()}
	}
	key := set.selectKey(keyid, now)
	if key == nil {
		return Result{Outcome: OutcomeUnverified, Label: label,
			Reason: fmt.Sprintf("key %s not provided by %s", keyid, ref.identifier)}
	}
	if len(sigEntry) != ed25519.SignatureSize {
		return Result{Outcome: OutcomeInvalid, Label: label, Reason: "signature value is not 64 bytes"}
	}
	if !ed25519.Verify(key.pub, []byte(base), sigEntry) {
		return Result{Outcome: OutcomeInvalid, Label: label, Reason: "signature verification failed"}
	}
	return Result{Outcome: OutcomeVerified, Label: label,
		Agent: &Agent{URL: ref.identifier, KeyID: keyid}}
}

// signatureValue extracts the Byte Sequence signature for label.
func signatureValue(sigs *sfDictionary, label string) ([]byte, error) {
	m := sigs.get(label)
	if m == nil || m.item == nil {
		return nil, fmt.Errorf("no Signature value for label %q", label)
	}
	if m.item.typ != sfBytes {
		return nil, fmt.Errorf("Signature value for label %q is not a byte sequence", label)
	}
	return m.item.bs, nil
}

// intParam returns an integer signature parameter.
func intParam(p sfParams, key string) (int64, bool) {
	v, ok := p.get(key)
	if !ok || v.typ != sfInt {
		return 0, false
	}
	return v.i, true
}

// agentRefFor resolves the Signature-Agent header to this signature
// label's discovery reference. legacy is true when the header is the
// deprecated bare-string form.
func agentRefFor(ctx context.Context, header, label string) (*agentRef, bool, error) {
	if strings.TrimSpace(header) == "" {
		return nil, false, fmt.Errorf("signed request carries no Signature-Agent header")
	}
	if strings.HasPrefix(strings.TrimLeft(header, " \t"), `"`) {
		// Legacy sf-string form (draft E.1.2/E.2.2): a bare String
		// Item, treated as a single directory-type member keyed to
		// the covering signature's label.
		it, err := parseSFItem(header)
		if err != nil || it.typ != sfString {
			return nil, false, fmt.Errorf("malformed legacy Signature-Agent header")
		}
		ref, err := parseAgentRef(ctx, it.str, discoveryDirectory)
		if err != nil {
			// %w, not %v: stringifying here breaks the chain, and a
			// saturated resolver would read as an invalid signature.
			return nil, false, fmt.Errorf("legacy Signature-Agent: %w", err)
		}
		return ref, true, nil
	}
	dict, err := parseSFDictionary(header)
	if err != nil {
		return nil, false, fmt.Errorf("malformed Signature-Agent header")
	}
	m := dict.get(label)
	if m == nil || m.item == nil {
		return nil, false, fmt.Errorf("Signature-Agent has no member for label %q", label)
	}
	if m.item.typ != sfString {
		return nil, false, fmt.Errorf("Signature-Agent member %q is not a string item", label)
	}
	typ := discoveryDirectory
	if t, ok := m.item.p.get("type"); ok {
		if t.typ != sfToken {
			return nil, false, fmt.Errorf("Signature-Agent member %q has a non-token type parameter", label)
		}
		// Unknown types MUST be ignored (draft 5.2.1); cimd is not
		// implemented by this verifier.
		switch t.str {
		case "directory", "jwks_uri":
			typ = discoveryType(t.str)
		default:
			return nil, false, fmt.Errorf("Signature-Agent member %q uses unsupported discovery type %q", label, t.str)
		}
	}
	ref, err := parseAgentRef(ctx, m.item.str, typ)
	if err != nil {
		return nil, false, err
	}
	return ref, false, nil
}

// ── Request context ─────────────────────────────────────────────────

type ctxKey struct{}

// WithAgent returns a context carrying a verified agent identity.
func WithAgent(ctx context.Context, a *Agent) context.Context {
	return context.WithValue(ctx, ctxKey{}, a)
}

// AgentFromContext returns the verified agent identity, or nil for
// unverified traffic.
func AgentFromContext(ctx context.Context) *Agent {
	a, _ := ctx.Value(ctxKey{}).(*Agent)
	return a
}
