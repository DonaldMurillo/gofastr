---
name: eval-report
description: The short report format for any eval result in this repo. Load it whenever you report on an eval: after running primitives-eval or any harness under evals/, when asked "how did the eval go", "is it working", "what did the runs show", or when summarising runs you or a subagent just finished. Four parts, in a fixed order, under 150 words.
---

# Eval report

The reader wants a decision, not a lab notebook. Write these four parts,
in this order, with these headings. Nothing before them, nothing after.

## 1. Is the eval working?

One line: **Yes**, **Partly** or **No**, then one sentence saying why.
This is about the eval itself, not the thing it tests. "Partly: the
grader matched on wording, so one correct fix scored zero." If the
answer is No, stop here and say what has to be fixed first.

## 2. What is working, and how much

At most four lines, or one small table. Each line is a claim with its
number. Always give the fraction with its total, never a bare
percentage: "fixed 13 of 30", not "43%". Say what is not working in the
same list, with the same kind of number.

## 3. What I think

Two or three sentences, in the first person. Say whether the direction
is right, and the one thing that would change your mind. Take a side.

## 4. Next steps

At most three, each one line, each starting with a verb. Put the one you
would do first at the top. If a step needs the reader's decision, say
so on that line.

## Rules

- Under 150 words in total, not counting a table.
- Plain words. No method, no list of what was run, no caveats section.
  If the evidence is thin, say so in one clause in part 1 or part 3.
- Cost goes in part 1 as a clause ("30 runs, $3"), nowhere else.
- No file paths unless a next step needs one.
- If the reader asks for detail, give it then, on the one thing asked.
