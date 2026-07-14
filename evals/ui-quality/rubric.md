# GoFastr visual quality rubric

Judge the rendered product, not the framework, source code, or presumed effort.
All screenshots in a bundle belong to one candidate and cover the same product
at multiple routes, viewports, and color schemes.

The target is the refinement associated with the strongest shadcn/ui full-page
blocks: disciplined typography, coherent spacing, responsive behavior,
accessible-looking interaction states, component finish, and production-ready
composition. Merely reproducing a shadcn dashboard skeleton is not sufficient.
Product specificity and page-level authorship matter equally.

Score each dimension from 0 to 10 using the whole screenshot bundle:

1. **Hierarchy** — purpose, dominant element, primary action, and reading order
   are immediately apparent without every region having equal visual weight.
2. **Composition** — page proportions, alignment, whitespace, section rhythm,
   grouping, and any asymmetry feel intentional; cards and elevation are used
   only where they clarify interaction or containment.
3. **Typography** — type scale, weight, measure, labels, data, and code create
   structure and remain legible; typography is not merely default component text.
4. **Product specificity** — the interface visibly belongs to the scenario and
   would not work unchanged for an unrelated SaaS product.
5. **Density** — information density fits the task. Dense tools are efficient
   rather than cramped; spacious pages remain purposeful rather than empty.
6. **Component polish** — controls, surfaces, charts, tables, media, icons,
   borders, radii, and states form one finished system with no obvious defaults,
   awkward wrapping, placeholder feel, or inconsistent treatments.
7. **Responsive intent** — mobile has its own priorities, navigation, grouping,
   and reading flow. It must not look like desktop columns mechanically stacked.
8. **Theme coherence** — light and dark modes both preserve hierarchy, contrast,
   restraint, and brand character instead of looking like token inversions.

Calibration:

- **9–10:** publication-quality and unusually resolved; credible alongside the
  best shadcn blocks while also being distinctly tailored to this product.
- **8–8.9:** polished and production-ready with a few identifiable weaknesses.
- **7–7.9:** competent design-system assembly, but generic or under-resolved.
- **5–6.9:** usable yet visibly template-like, repetitive, or inconsistent.
- **0–4.9:** broken, incoherent, substantially unfinished, or visually unusable.

Do not reward decorative complexity. Penalize generic AI patterns when they are
not justified by content: equal card grids, stat rows as default introductions,
centered gradient heroes, decorative pills, nested cards, excessive rounding,
and uniform section widths or weights.

Your written feedback must identify concrete visible evidence. Never infer source
quality, implementation architecture, accessibility-tree behavior, or hidden
interactions from screenshots.
