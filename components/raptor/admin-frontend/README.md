# Raptor Admin frontend

Reserved for the Vue Vben Admin frontend. No frontend application has been scaffolded yet.

- Platform frontend: `../platform-frontend/`.
- Existing Admin server: `../cmd/admin/` and `../internal/admin/`.
- Keep the existing Admin hostname: `admin.rdev.raptor-iap.top`.
- Planned routing: `/` serves the Admin frontend; `/api/` continues through the Admin service to the Go backend.
- Backend authorization and CSRF protection remain required.
