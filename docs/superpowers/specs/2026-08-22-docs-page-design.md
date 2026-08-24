# In-App Documentation Page — Design Spec

Date: 2026-08-22
Version: Ku-CRUD v1.5
Scope: Frontend only (SPA), no backend changes.

## Goal

An in-app documentation page (`/docs`) covering the features implemented in
v1.5, split into two audiences:

1. **For Business Users** — how to use the app at runtime (grids, filters,
   views, import/export, etc.). No IT background assumed.
2. **For Developers** — runtime configuration owned by the
   programmer/admin persona (datasources, table definitions, relations,
   computed columns, validations, formatting, views, roles, transfer,
   security behavior).

The docs describe **runtime usage and configuration only** — no development,
build, deployment, or API reference content.

## Decisions

- **Location**: `web/src/pages/Docs.tsx`, route `/docs` inside the `App`
  layout (requires login). No backend endpoint — content is static and
  versioned with the app itself.
- **Audience switch**: two buttons toggling a `useState` value — no new UI
  component library dependency (no shadcn Tabs in the project).
- **Language**: English only, hardcoded strings (not routed through the i18n
  dictionaries). Sidebar/header labels go through `t()` with
  `nav.docs`/`app.header.docs` keys ("Documentation" in both languages).
- **Visibility**: sidebar link shown to all logged-in users (both audiences
  need it).
- **Styling**: existing Tailwind + shadcn primitives (`Card`, `Badge`,
  `Button`, `Separator`) consistent with the rest of the app.

## Page structure

```
/docs
├── Tab: For Business Users
│   ├── Getting started (login, language switch, navigation)
│   ├── Working with the grid (search, sort, pagination, group-by)
│   ├── Filtering data (advanced filters, chips, saved filters)
│   ├── Views (grid / kanban / calendar, drag-drop cards, event clicks)
│   ├── Editing rows (create/edit/delete, fk picker, m2m picker, JSON fields)
│   ├── CSV import & export, bulk delete
│   ├── Understanding the audit trail
│   └── Common messages (drift banner, delete conflicts, validation errors)
└── Tab: For Developers
    ├── Datasources (register, test, encrypted passwords)
    ├── Table definition wizard (columns, keys, labels, flags, default sort)
    ├── Column types & validation rules
    ├── Relations (fk, m2m junction requirements, display fields)
    ├── Computed columns (allowlisted formulas, limits: no server filter/sort)
    ├── Formatting (enum colors, number format, datetime)
    ├── Views config (default view, kanban board column, calendar dates)
    ├── Schema drift & re-sync
    ├── Metadata export/import (no passwords, duplicate handling)
    ├── Users & roles (RBAC model, grants, immutable first user)
    └── Runtime security behavior (rate limit, sessions, masked IDs)
```

Content is derived from README "How it works" 1–13 and project-brief v1–v1.5,
rewritten per audience, focused on what the user does in the UI at runtime.

## Files touched

| File | Change |
|------|--------|
| `web/src/pages/Docs.tsx` | new — the whole page |
| `web/src/main.tsx` | add route `docs` under App |
| `web/src/components/Sidebar.tsx` | nav item (BookOpen icon, all users) |
| `web/src/App.tsx` | header title/subtitle for `/docs` |
| `web/src/lib/i18n.tsx` | `nav.docs`, `app.header.docs(+/Sub)` keys (en + id) |

## Non-goals

- No markdown renderer / MDX — content is JSX sections.
- No search within docs.
- No API reference, no dev/build/deploy docs.
- No per-language docs content (English only).

## Testing

- `npm run build` (includes strict type-check) must pass.
- Manual: `/docs` renders for a logged-in non-admin user; both tabs switch;
  sidebar link present; header title correct.
