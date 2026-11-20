// Package webbotauth verifies inbound Web Bot Auth requests: RFC 9421
// HTTP Message Signatures under the profile of
// draft-meunier-webbotauth-httpsig-protocol-02 (18 August 2026), Ed25519
// only. Each request is verified, invalid or unverified (draft Appendix
// C.1); only a verified one yields an Agent.
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

// Agent is a verified signer: its resolved directory URL, which the
// signer chooses, and the thumbprint of the key that verified.
type Agent struct {
	URL   string
	KeyID string
}

// Outcome classifies a verification run (draft Appendix C.1).
type Outcome uint8

const (
	// OutcomeUnverified: not enough information to decide.
	OutcomeUnverified Outcome = iota
	// OutcomeInvalid: a presented signature failed a check.
	OutcomeInvalid
	// OutcomeVerified: a signature verified against its directory's key.
	OutcomeVerified
)

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
	// Retryable marks an outcome caused by ErrResolverBusy, not the request.
	Retryable bool
	Agent     *Agent // non-nil only for OutcomeVerified
	Label     string // signature label that produced the outcome
	Reason    string // human-readable, for logs
}

// Verifier verifies inbound signed requests. Construct with New and
// install with Middleware. Safe for concurrent use.
type Verifier struct {
	resolver    *directoryResolver
	require     bool
	log         *slog.Logger
	now         func() time.Time
	maxValidity time.Duration
}

// DefaultMaxValidity is the longest expires - created window accepted
// when none is set.
const DefaultMaxValidity = 5 * time.Minute

// Option configures a Verifier at New.
type Option func(*Verifier)

// WithMaxValidity sets the longest expires - created window a signature
// may carry; zero means DefaultMaxValidity, and a negative d panics.
func WithMaxValidity(d time.Duration) Option {
	if d < 0 {
		panic("webbotauth: WithMaxValidity: negative duration")
	}
	return func(v *Verifier) { v.maxValidity = d }
}

// New builds a Verifier. A nil logger silences the observe-mode logs.
func New(require bool, log *slog.Logger, opts ...Option) *Verifier {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	v := &Verifier{require: require, log: log, now: time.Now}
	for _, o := range opts {
		o(v)
	}
	v.resolver = newDirectoryResolver(log)
	return v
}

// MaxValidity reports the longest expires - created window this
// Verifier accepts.
func (v *Verifier) MaxValidity() time.Duration {
	if v.maxValidity == 0 {
		return DefaultMaxValidity
	}
	return v.maxValidity
}

func (v *Verifier) validityCap() int64 {
	return int64(v.MaxValidity() / time.Second)
}

const maxSignaturesPerRequest = 4

// Middleware verifies each request. Without require it never blocks and
// puts a verified Agent on the context; with require an unverified
// request gets 403 and Accept-Signature, or 503 when Retryable, except on
// the key directory path.
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
				w.Header().Set("Retry-After", "1")
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusServiceUnavailable)
				//gofastr:allow(rawtext) RFC 9457 problem details for a signing agent, protocol text like the Accept-Signature header
				fmt.Fprintf(w, `{"title":"Web Bot Auth verification unavailable","status":503,"detail":%q}`+"\n", res.Reason)
				return
			}
			w.Header().Set("Accept-Signature", `wba=("@authority" "signature-agent");created;expires;keyid;tag="web-bot-auth";alg="ed25519"`)
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusForbidden)
			//gofastr:allow(rawtext) RFC 9457 problem details for a signing agent, protocol text like the Accept-Signature header
			fmt.Fprintf(w, `{"title":"Web Bot Auth signature required","status":403,"detail":%q}`+"\n", res.Reason)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isPublicDiscoveryPath(path string) bool {
	return path == directoryWellKnown
}

// VerifyRequest returns the first verified signature's result, else the
// first invalid one's, else unverified.
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

func (v *Verifier) verifyOne(r *http.Request, inputs, sigs *sfDictionary, m sfMember, now time.Time) Result {
	label := m.key
	if m.list == nil {
		return Result{Outcome: OutcomeInvalid, Label: label, Reason: "Signature-Input member is not an inner list"}
	}
	covered, params := m.list.items, m.list.params

	sigEntry, err := signatureValue(sigs, label)
	if err != nil {
		return Result{Outcome: OutcomeInvalid, Label: label, Reason: err.Error()}
	}

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
	if limit := v.validityCap(); expires-created > limit {
		return Result{Outcome: OutcomeInvalid, Label: label, Reason: fmt.Sprintf("signature validity window %ds exceeds the %ds limit", expires-created, limit)}
	}
	keyidItem, ok := params.get("keyid")
	if !ok || keyidItem.typ != sfString || keyidItem.str == "" {
		return Result{Outcome: OutcomeInvalid, Label: label, Reason: "missing keyid parameter"}
	}
	keyid := keyidItem.str
	if alg, ok := params.get("alg"); ok && (alg.typ != sfString || alg.str != "ed25519") {
		return Result{Outcome: OutcomeUnverified, Label: label, Reason: "unsupported signature algorithm; only ed25519 is verified"}
	}

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
	case len(coveredAgentKeys) == 1 && !coveredBareAgent:
		memberKey = coveredAgentKeys[0]
	case len(coveredAgentKeys) == 0 && coveredBareAgent:
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
			return Result{Outcome: OutcomeUnverified, Label: label,
				Reason: err.Error(), Retryable: true}
		}
		return Result{Outcome: OutcomeInvalid, Label: label, Reason: err.Error()}
	}

	reqCtx := newRequestCtx(r)
	base, err := buildSignatureBase(reqCtx, covered, params)
	if err != nil {
		return Result{Outcome: OutcomeInvalid, Label: label, Reason: "signature base: " + err.Error()}
	}

	set, err := v.resolver.resolve(r.Context(), ref)
	if err != nil {
		return Result{Outcome: OutcomeUnverified, Label: label, Reason: err.Error(),
			Retryable: errors.Is(err, ErrResolverBusy)}
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

func intParam(p sfParams, key string) (int64, bool) {
	v, ok := p.get(key)
	if !ok || v.typ != sfInt {
		return 0, false
	}
	return v.i, true
}

func agentRefFor(ctx context.Context, header, label string) (*agentRef, bool, error) {
	if strings.TrimSpace(header) == "" {
		return nil, false, fmt.Errorf("signed request carries no Signature-Agent header")
	}
	if strings.HasPrefix(strings.TrimLeft(header, " \t"), `"`) {
		it, err := parseSFItem(header)
		if err != nil || it.typ != sfString {
			return nil, false, fmt.Errorf("malformed legacy Signature-Agent header")
		}
		ref, err := parseAgentRef(ctx, it.str, discoveryDirectory)
		if err != nil {
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
