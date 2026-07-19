---
name: plain-words
description: Write GoFastr-facing copy in plain, concrete words. Auto-load before writing or editing ANY user-facing sentence — taglines, README prose, site/UI text, headlines, docs, changelogs, marketing. Kills jargon, marketing adjectives, abstract nouns, and AI-slop phrasing. Triggers on writing/editing copy, tagline, hero, headline, lede, positioning, README prose, landing page, UI microcopy.
---

# Plain words

You keep reaching for jargon and marketing words. Stop. This is the filter you run every sentence through before it ships.

## The one test

Before you write or keep a user-facing sentence, ask:

**Would a working Go developer say this out loud to a coworker?**

If it sounds like a landing page, a keynote, or a pitch deck — delete it and write what the thing actually does. If you can't say it at a desk with a straight face, it's wrong.

## Banned — never use these words or phrases

first-class, first-party, bolt-on, add-on, seamless(ly), powerful, robust, blazing(ly), lightning-fast, unlock, leverage, empower, enable (as filler), elevate, delight, craft(ed), journey, world-class, battle-tested, enterprise-grade, cutting-edge, next-gen, revolutionary, game-changer, supercharge, effortless, "out of the box" (as praise), "under the hood", "at the table", "wired for", "runs through", "baked in", introspect, **surface** (as a noun meaning API/tools/pages), projection, authoritative, paradigm, ecosystem, holistic, turnkey, native (as buzzword), rich (as praise), "just works", "so you can focus on what matters", "the way X should be", "not a bolt-on / not an afterthought".

If one shows up, delete the sentence and start over with the concrete thing.

## Rules

1. **Name the real thing.** "an agent can call `posts_list`" — not "agents get a tool surface".
2. **Plain verbs:** get, write, read, run, call, add, change, delete. Not: leverage, enable, empower, provide, deliver.
3. **Show it.** A command, a file path, a route, a flag beats any adjective. Prefer a two-line code block over a sentence praising the code.
4. **One idea per sentence. Cut every word that isn't load-bearing.** Long, comma-spliced sentences are where jargon hides.
5. **No praise adjectives.** Facts don't need selling. Delete "powerful", "simple", "elegant", "clean".
6. **No metaphors.** Say the literal thing. ("on-ramp", "batteries", "at the table" — cut.)
7. **Every claim must be checkable.** If you can't point to a file, route, or behavior, don't write it.

## After you write

Re-read once. Strike every banned word. Replace every abstraction with the concrete thing it stands for. If the sentence still reads like copywriting, it is — say it the boring way instead. Boring and true beats clever and vague, every time.
