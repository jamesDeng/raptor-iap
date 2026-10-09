# Raptor request progress with T3 WorkLog

This slice reuses the actual upstream T3 WorkLog presentation primitives. It does not run a T3 server. Raptor remains the browser authorization boundary; Gateway owns execution and durable progress.

The pinned upstream revision and original source hash are in `src/vendor/SOURCE.json`. The only component change is the `cn` import; upstream source and distributed bundle retain the MIT license. React and other bundled dependencies retain their generated license notices.

The component mounts in the request **Progress** tab. It reads existing `/api/v1/requests/{id}` and `/events?after=...` responses through the platform controller. It provides a collapsible activity group, expandable event details, recorded timestamps/evidence labels, independently reported execution/checkpoint/cleanup, and the final answer as escaped plain text. Result evidence remains in the existing Result tab. It has no composer or execution commands.

Event timestamps determine a **recorded activity span**, not the provider's full execution duration. Missing timestamps produce no estimated duration. Unavailable sync retains saved history and a visible warning. Request navigation unmounts the React root; polling preserves expanded rows. Existing request authorization and cancellation semantics are unchanged. There are no new backend endpoints.

## Build and test

From this directory:

```sh
npm ci --ignore-scripts
npm run build
npm test
npm run check:assets
```

The build produces `../web/poc/worklog.js` and `worklog.css`, committed so the existing Go-only Docker build can embed them without Node at runtime or build time. Rebuild and commit both when changing source or locked dependencies. CSS includes Tailwind utilities scoped to `.t3-worklog`, without global preflight, and respects reduced motion. The UI styling bridge is limited to the WorkLog host.

From repository root, run the existing web tests:

```sh
node --test components/raptor/web/poc/*.test.mjs tests/harness/go-platform-web.test.mjs
```

This is the bounded UI reuse slice, not the full T3 chat timeline. It does not add token streaming, conversation resume, or a new provider.
