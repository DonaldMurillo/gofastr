# GoFastr external pilot program

The v1 adoption gate requires evidence from at least one real application owned
by someone other than GoFastr's author. A tutorial clone or an author-maintained
demo does not count.

## Candidate and scope

Choose a CRUD-heavy application with three or more related entities, real auth
and authorization needs, and at least one workflow beyond generated CRUD. The
pilot should be small enough to deploy within two weeks but real enough that its
owner intends to keep using it.

The pilot owner and a GoFastr maintainer agree on:

- the application, deployment target, and data-sensitivity constraints;
- the blueprint-owned surface and the hand-written customization;
- one representative framework upgrade during the pilot;
- where defects and design friction will be recorded.

## Required evidence

The pilot is successful only when all of these are recorded in a tracking issue:

- A non-author owns the application repository and confirms it serves a real
  workflow.
- `gofastr generate --from gofastr.yml` produces an inspectable starting point.
- The app builds, tests, and deploys without a private framework fork.
- SQL, REST, OpenAPI, MCP, and UI surfaces are exercised or consciously scoped
  out with a reason.
- Auth, ownership/tenant scoping, and one application-specific permission are
  verified end to end.
- The owner completes one GoFastr minor-version upgrade using the documented
  upgrade path.
- Blocking defects, workarounds, time-to-first-deploy, and owner feedback are
  captured—even when the result is negative.

## Feedback record

Use this compact format in the tracking issue:

```text
App / owner:
Pilot dates:
Blueprint commit:
First deploy duration:
Surfaces exercised / excluded:
Upgrade from / to:
Blockers and workarounds:
Would the owner continue using GoFastr? Why?
Follow-up issues:
```

## v1 decision

The pilot satisfies the external-adoption gate only after the non-author owner
confirms continued use and all blocking issues are closed or explicitly scoped
out. Maintainers link the evidence issue from `ROADMAP.md`; this document alone
is a path to evidence, not evidence of adoption.
