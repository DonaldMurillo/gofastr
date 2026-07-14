# GoFastr application instructions — resilient responsive composition

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
  error states, interaction patterns, and shared tokens. Do not recreate or
  override those internals.
- The application owns information hierarchy, page rhythm, proportions,
  density, screen-specific layout, media placement, storytelling, and art
  direction.
- Product-specific composition is encouraged through GoFastr's approved APIs:
  layout components, tokenized `style.Contribute`, registered variants, and
  app-owned components registered with `registry.RegisterStyle`. Never ship raw
  unscoped CSS, inline styles, literal theme colors, or selectors that reach
  into a framework component's private structure.

## Visual-direction and layout-risk gate

Before selecting components, write a short `DESIGN.md` that states:

1. The primary user task and dominant decision for each required route.
2. Intended information density and the exact mobile priority order.
3. The composition model and how it transforms at the narrow viewport.
4. Expected short and long content regions, including the continuation plan
   after any short desktop rail ends.
5. The responsive contract for rows that combine variable-length content with
   trailing metadata, controls, badges, prices, timestamps, or identities.
6. The navigation or disclosure strategy for any mobile detail page expected to
   exceed roughly three viewport heights.
7. The three likeliest layout failures: boundary collisions, clipped trailing
   content, dead space from unequal columns, tiny metadata, awkward wrapping, or
   buried critical content.
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
- Protect every desktop column boundary with a real gutter and enough minimum
  width for its longest heading, label, value, and control. Adjacent metadata
  must never visually touch the next region.
- Do not place substantially different content lengths in one shared tall row
  merely to obtain columns. Define what occupies the space after the shorter
  rail ends: widen the primary flow, span later modules across the grid, or move
  related secondary modules into the available region. A large accidental blank
  column beside continuing content is a composition defect.
- Any row with a flexible title/body and trailing metadata needs an explicit
  narrow-screen transformation. At mobile width, stack the metadata on its own
  line or place it before the body; give flexible children `min-width: 0`, allow
  wrapping, and never depend on clipped overflow or a trailing nowrap value.
- Keep secondary text deliberately legible. Avoid tiny uppercase or monospaced
  metadata as a density shortcut, especially at 390px and in dark mode.
- Mobile is a designed composition, not desktop columns stacked mechanically.
  Put the user's urgent decision or action before long history and secondary
  metadata.
- Long mobile pages need orientation. Provide a compact section index or jump
  links near the status/decision summary, use clear section landmarks, and
  selectively collapse only genuinely archival detail when native disclosure
  preserves access without hiding the primary task. Do not solve length by
  shrinking type or removing operationally important content.
- Use realistic, domain-specific data and copy. Placeholder prose is a visual
  defect because it prevents credible hierarchy and density decisions.

## Completion gate

- Implement every route named in `EVAL_TASK.md`.
- Review the 390px and 1440px structures in both color schemes. Trace every
  region in reading order and revisit the three layout risks in `DESIGN.md`.
- Audit every flex/grid row that has content on both edges. At 390px, its title,
  body, actor, timestamp, badge, and action must all remain visible without
  clipping or horizontal scrolling.
- Audit long mobile routes for orientation, section reachability, readable
  metadata, and primary-task priority.
- Audit the full desktop page below the first viewport. If one column ends early,
  verify that later content deliberately reuses or spans the open space.
- Identify the three weakest composition decisions from the rendered structure
  and revise them before finishing.
- Run `go test ./...` and leave the app runnable with `go run .` using the `PORT`
  environment variable generated by the scaffold.
