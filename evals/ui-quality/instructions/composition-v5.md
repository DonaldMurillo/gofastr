# GoFastr application instructions — component-safe visual authorship

Build the requested application as a complete, runnable GoFastr app. Correctness
is necessary, but component compliance alone is not completion: the result must
look authored for this product at both mobile and desktop sizes.

## Architecture and ownership

- SSR-first. Use normal links for page navigation and server-driven islands for
  in-page state. Do not add feature-specific JavaScript.
- In-page anchor links to SSR sections are appropriate for navigating a long
  document. Use islands only when server-owned state actually changes.
- Survey `framework/ui`, `core-ui/app`, and `core-ui/patterns` before building,
  but time-box the survey and move to implementation once the required
  primitives are identified.
- The framework owns reusable component behavior, component internals, focus and
  error states, interaction patterns, shared tokens, and the rendered root of
  every framework component. Prefer its header, navigation, disclosure,
  timeline, status, link, button, and avatar primitives over custom equivalents.
- The application owns information hierarchy, page rhythm, proportions,
  density, screen-specific layout, storytelling, and art direction.
- Product-specific composition is encouraged through GoFastr's approved APIs:
  layout components, tokenized `style.Contribute`, registered variants, and
  app-owned components registered with `registry.RegisterStyle`. Never ship raw
  unscoped CSS, inline styles, literal theme colors, or selectors that reach
  into a framework component's private structure.
- App-owned styles must target app-owned classes or attributes. Do not use broad
  element selectors such as `a`, `button`, `input`, or `h2 a` inside an app
  scope: framework components may be nested there, and app CSS is intentionally
  loaded after component CSS. Style a text link through an app-owned class or a
  framework variant, never by restyling every anchor in the composition.
- Never target a framework `data-fui-comp` marker or framework class from app
  CSS, and never override a framework component root indirectly with a broader
  selector. If a framework component needs a new reusable treatment, add a
  registered variant upstream and select it through the component API.

## Visual-direction and layout-risk gate

Before selecting components, write a short `DESIGN.md` that states:

1. The primary user task and dominant decision for each route.
2. Intended information density and exact mobile priority order.
3. The composition model and narrow-screen transformation.
4. Which desktop regions are short versus long, and how later content reclaims
   the width after a short rail ends.
5. The mobile contract for rows with a flexible title and trailing metadata.
6. Which secondary mobile details remain inline, move later, or use native
   disclosure so the opening task is not buried.
7. The three likeliest visible failures: clipped metadata, microscopic labels,
   excessive mobile length, equal-height dead space, or unresolved header
   chrome.
8. Two generic visual patterns this product will deliberately avoid.
9. Typography, surface, border, radius, and elevation posture.

Then implement against that contract.

## Composition standards

- Establish grouping with hierarchy, alignment, separators, and whitespace
  before reaching for another card.
- Do not use a three-column feature grid unless the content truly contains three
  equal concepts. Do not use decorative pills, nested cards, or elevation as the
  default grouping mechanism.
- Do not introduce every dashboard with a row of equal-weight statistics.
- Vary section widths and visual weight according to purpose. Every screen needs
  an intentional focal point and reading path.
- Protect desktop column boundaries with real gutters and minimum widths. Do not
  let a short rail set the height of a much shorter neighboring hero and create
  blank space. Keep the opening rail compact, align tracks to their content, and
  place long supporting lists in later sections.
- Once a short desktop rail ends, long chronology or evidence must span the
  available canvas or use a balanced new grid. Do not leave an accidental blank
  column beside continuing content.
- Any row with a flexible title/body and trailing metadata needs an explicit
  mobile form: metadata on its own line before or after the title, flexible
  children with `min-width: 0`, natural wrapping, and no right-edge nowrap.
- At mobile width, use base body text for body copy and at least the equivalent
  of 0.875rem with comfortable line height for metadata. Do not use tiny
  uppercase, monospace, `xs`, or compressed `sm` text as a density shortcut.
- Keep framework header chrome resolved at 390px: use a concise mobile brand,
  preserve the primary navigation affordance, and never expose a raw underlined
  or visited-looking brand link.

## Mobile priority and length discipline

- Mobile is a designed composition, not every desktop region serialized. The
  opening should contain only the status/title, current impact, next decision or
  primary action, primary owner, and a compact key-signal summary.
- Do not place a full responder/member roster, complete historical record, or
  other exhaustive secondary list inside the mobile opening. Show the primary
  owner plus a count or presence summary, then put the complete list later or in
  a clearly labeled native disclosure.
- Long mobile pages need orientation, but the navigation must not become micro
  text. Use a two-column touch-sized section-link grid or a deliberately
  scrollable touch row with labels at least 0.875rem and targets roughly 44px
  high. A six-link inline text strip is a defect.
- Keep the primary timeline or workflow visible. Secondary operating facts,
  archival evidence, communication history, and related records may use native
  disclosure when that preserves access and substantially shortens the scan.
- Do not solve length by shrinking typography or removing operationally
  important content.
- Use realistic, domain-specific data and copy. Placeholder prose is a visual
  defect because it prevents credible hierarchy and density decisions.

## Completion gate

- Implement every route named in `EVAL_TASK.md`.
- Review the 390px and 1440px structures in light and dark. Trace the full page,
  not just the first viewport.
- At 390px, audit every edge-aligned row. Title, body, actor, timestamp, badge,
  and action must remain visible, readable, and wrapped onto intentional lines.
- Verify every visible framework control still owns its complete appearance:
  its label must remain inside its surface, its foreground/background contrast
  must be legible in both themes, and app-level selectors must not win over its
  component styles.
- Verify the mobile opening contains only the primary owner summary—not a full
  roster—and that section navigation is touch-sized rather than a tiny link
  strip.
- Verify mobile secondary text meets the stated legibility floor and that the
  page uses disclosure or prioritization instead of endless full-detail rows.
- At 1440px, verify the opening has no height-matched blank field and every long
  section deliberately spans or reuses the canvas after a short rail ends.
- Identify the three weakest composition decisions and revise them before
  finishing.
- Run `go test ./...` and leave the app runnable with `go run .` using the `PORT`
  environment variable generated by the scaffold.
