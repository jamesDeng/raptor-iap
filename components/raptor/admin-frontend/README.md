# Raptor Admin frontend

Vue Vben Admin layout with Ant Design Vue, Vue Router and TypeScript. This application replaces the Go-rendered Admin pages. The regular platform UI lives in `../platform-frontend`.

The existing Go Admin service (`../cmd/admin`) embeds `dist/` and proxies `/api/` to the existing backend. The hostname remains `admin.rdev.raptor-iap.top`; no routing, port or backend migration is required. Administrator authorization and CSRF remain enforced by the backend. The browser keeps the CSRF token only in memory and uses the existing HttpOnly cookie session.

## Development

Use Node 22.23.3 and pnpm 10.28.2. Run `pnpm install --frozen-lockfile --ignore-scripts`, then `pnpm dev`. The development server proxies API requests to the existing Admin service on localhost port 8873.

Run `pnpm test`, `pnpm typecheck`, `pnpm build`, and `pnpm test:e2e` (after `pnpm exec playwright install chromium`). Browser acceptance uses synthetic API fixtures and never writes live settings.

Commit `dist/` after every source change: plain Go builds embed these files without Node. The Docker build compiles them again from the frozen lockfile. CI rebuilds and checks for asset drift before publishing.

## Upstream

`UPSTREAM.json` pins the exact upstream Vben commit and lists the included production dependency closure. `packages/` and `internal/tailwind-config/` retain upstream source and attribution; package manifests omit unused development scripts and tooling. `LICENSE.vben` contains the upstream MIT license. Raptor owns `src/`, tests, build configuration and the Go embed wrapper. Upstream demos, mock authentication and mock backend are excluded.
