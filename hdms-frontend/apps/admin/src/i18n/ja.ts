/**
 * The admin console's Japanese catalogue, and the type reference for every
 * other locale. Values start as English and are translated in one pass, so the
 * extraction commits stay mechanical and reviewable.
 */
export const ja = {
  nav: {
    dashboard: "Dashboard",
    devices: "Devices",
    users: "Users",
    loans: "Loans",
    credentials: "Credentials",
    reports: "Reports",
    audit: "Audit",
    settings: "Settings",
  },
  common: {
    save: "Save",
    cancel: "Cancel",
    delete: "Delete",
    edit: "Edit",
    search: "Search",
    loading: "Loading…",
    noResults: "No results",
  },
} as const;

export type AdminMessages = typeof ja;
