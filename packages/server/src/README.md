# Server layout

Layers, outermost first. Dependencies point downward only — a repository never
imports a service, and a service never imports a route.

| Directory | Holds | May import |
|---|---|---|
| `apis/` | Fastify route handlers, one file per resource | services, repositories, constants, types |
| `services/` | Business logic: spec validation, executor, run pipeline, browser, notifier, task registry | repositories, utils, config, constants, types |
| `repositories/` | Every SQL statement, and the SQLite connection | utils, config, types |
| `config/` | Env parsing with fail-fast validation | utils |
| `constants/` | Runtime values shared across layers | `@breckr/shared` (types only) |
| `utils/` | Dependency-free helpers: mutex, timeout, paths, JSON | nothing local |
| `types/` | Server-local types (`TaskDefinition`, `Logger`, …) | `@breckr/shared` |

## How a task becomes a run

Tasks are user data, stored in SQLite as a declarative `TaskSpec` and authored
from the dashboard. Two services turn one into something executable:

- **`spec.service`** validates a spec. Pure — no database, no browser, no
  config — which is what makes its whole rejection table testable. It is the
  only thing standing between a typo in a form and a monitor that silently
  never fires, so every rejection names the offending field.
- **`executor.service`** compiles a stored task into a `ResolvedTask`: the spec
  is *interpreted*, never evaluated. No user string reaches a function
  constructor or a `vm`.

`ResolvedTask` is the seam. Because the executor emits exactly the shape the
file-based registry used to produce, the runner, the browser mutex, the notifier
and the edge-trigger state machine did not change at all when tasks moved out of
files and into the database.

`registry.service` owns the live cron entries and is mutable at run time —
`register` / `reschedule` / `unregister` — because a task saved at 10:05 has to
start firing without a restart. node-cron cannot swap an expression on a live
handle, so `reschedule` destroys and rebuilds.

## Conventions

**Relative imports carry a `.ts` extension.** `tsc` rewrites them to `.js` on
emit (`rewriteRelativeImportExtensions`). This is deliberate: Node's native type
stripping resolves specifiers literally and will not map `.js` onto a `.ts`
file, so writing `.js` — the usual `nodenext` convention — would break
`npm run dev`, which runs the sources directly.

**Shared types are imported with `import type`.** `@breckr/shared` publishes no
runtime entry point at all, because Node refuses to strip types inside
`node_modules`. A value import from it fails at build, which is the intent.

**Tests run serially** (`--test-concurrency=1`). They share one SQLite file, and
concurrent processes racing to create the schema is a real source of flakes.

## Testing

```bash
npm test
```

Tests live beside the code they cover as `*.test.ts` and are excluded from the
build by `tsconfig.build.json`. They use a separate database (`DB_PATH` is
overridden by the `test` script) and never touch `data/monitor.db`.

The runner takes its notifier as an injected dependency so the edge-trigger
state machine can be driven through every delivery outcome — ESM bindings cannot
be monkey-patched the way CommonJS exports could.
