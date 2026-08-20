# `@hdms/ui`

Shared design tokens and styling utilities for HDMS frontend applications.

## Package Boundaries

- `@hdms/ui` exports design tokens (`tokens.css`) and the `cn` helper (`src/index.ts`) only.
- **Component implementations remain strictly app-local** (`apps/kiosk/src/components/ui/` and `apps/admin/src/components/ui/`).

### Rationale

The kiosk and admin applications have fundamentally different physical and ergonomic constraints:
- **Kiosk UI:** Built for touch interactions on a shared counter-mounted iPad. Primary buttons and touch targets must be >= 64 px tall (with secondary targets >= 48 px), with large tactile press animations, high visual feedback, and simplified layouts.
- **Admin UI:** Built for desktop mouse/keyboard use with standard 36–40 px controls, dense data tables, modal dialogs, and admin forms.

**Do not lift app-local UI components (such as buttons or form fields) into `@hdms/ui` for "consistency".** Shared design tokens (color scales, radii, focus rings) in `tokens.css` enforce brand and theme alignment without compromising platform ergonomics.
