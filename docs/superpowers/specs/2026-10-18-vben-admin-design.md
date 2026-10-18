# Vben Admin migration

The owner authorized implementation, PR merge and independent testing on October 18, without intermediate approval pauses.

Keep `components/raptor/platform-frontend` for the existing platform UI. Scaffold `components/raptor/admin-frontend` using the upstream Vben layout and its production workspace dependencies, Ant Design Vue, Vue Router and TypeScript. Preserve upstream MIT attribution and record the exact source commit. Include only Raptor navigation and pages.

The existing Go Admin binary continues listening on its current port and serving the current Admin hostname. Embed the compiled SPA and proxy `/api/` to the existing backend. Keep cookie sessions and in-memory CSRF tokens, administrator enforcement, existing API envelopes and backend errors. No mock backend or browser token persistence. Existing page URLs and legacy section hashes remain usable. Missing assets and unknown routes return 404.

Migrate Users creation/listing; Environment Groups creation/listing; Environments search/detail/create/edit with structured repository rows; GitHub token status/save/delete; Codex browser/device connection, cancellation, polling, discovery, disconnect and versioned model-policy editing. Keep secret inputs cleared after submission and never redisplay saved secrets. Stop model polling when leaving the page.

Build the SPA in the existing Raptor Docker build, with a frozen pnpm lockfile and pinned package-manager version. Commit generated static files so normal Go builds continue working without Node. CI rebuilds and verifies them. Deploy through an image publication and reviewed GitOps digest PR; preserve routes and Terraform ownership.

Verify API/CSRF and session failure handling, all migrated page flows with synthetic browser fixtures, Go static routing/proxy behavior, complete existing offline suites, production build and live Admin sign-in/session denial and read-only page checks. Never change live account, token, model or environment data merely for acceptance.
