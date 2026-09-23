import type { components } from "@/api/schema";

export type AuthUser = components["schemas"]["UserResponse"];

export interface AuthState {
  token: string | null;
  user: AuthUser | null;
}

// Exported so tests can pre-seed/inspect localStorage directly under the
// exact same key this module reads/writes.
export const STORAGE_KEY = "go-invoicing.session";

/**
 * Token storage (Milestone 12 section 5).
 *
 * The opaque bearer token and last-known user record are kept in
 * `localStorage` under one JSON key — the simplest browser-storage
 * approach available, chosen deliberately for this milestone over a
 * more involved alternative (an httpOnly cookie session, which would
 * require redesigning the backend's Bearer-token authentication itself —
 * explicitly out of scope for M12).
 *
 * Trade-off: unlike an httpOnly cookie, this token is readable by any
 * JavaScript running on the page, so a successful XSS against this
 * frontend could exfiltrate it. This is mitigated, not eliminated, by
 * this codebase avoiding `dangerouslySetInnerHTML` and any other
 * raw-HTML injection point (see the security review in the milestone
 * report) — but it remains a real trade-off worth stating plainly rather
 * than glossing over. `sessionStorage` was considered instead (scoped to
 * one tab, cleared on close) but rejected: it would silently log a user
 * out every time they close and reopen the browser, which is worse
 * day-to-day UX for what is, in the end, the same class of storage from
 * an XSS-exposure standpoint. Revisiting this — e.g. a cookie-based
 * session — is a reasonable candidate for a future milestone, not this
 * one.
 */
function readStorage(): AuthState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return { token: null, user: null };
    const parsed = JSON.parse(raw) as Partial<AuthState>;
    if (typeof parsed.token === "string" && parsed.user) {
      return { token: parsed.token, user: parsed.user as AuthUser };
    }
  } catch {
    // Corrupt/inaccessible storage (private browsing, quota, manual
    // tampering) — fall through to a clean, logged-out state rather than
    // throwing during module load.
  }
  return { token: null, user: null };
}

function writeStorage(state: AuthState) {
  try {
    if (state.token && state.user) {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
    } else {
      localStorage.removeItem(STORAGE_KEY);
    }
  } catch {
    // Storage unavailable — the in-memory state (below) still works for
    // the rest of this tab's session; it just won't survive a refresh.
  }
}

let state: AuthState = readStorage();
const listeners = new Set<() => void>();

function notify() {
  for (const listener of listeners) listener();
}

/**
 * A small, plain (non-React) store so both React components (via
 * useSyncExternalStore in useAuth) and the plain API client module (see
 * api/client.ts's 401-handling middleware) can share one source of
 * truth for the current session, without the API client needing to
 * import React or hold a Router reference. Clearing the session here is
 * the *only* thing the 401 middleware does — see ProtectedRoute for how
 * that turns into an actual redirect, without any imperative navigation
 * call from inside this module.
 */
export const authStore = {
  getState(): AuthState {
    return state;
  },
  subscribe(listener: () => void): () => void {
    listeners.add(listener);
    return () => listeners.delete(listener);
  },
  setSession(token: string, user: AuthUser) {
    state = { token, user };
    writeStorage(state);
    notify();
  },
  clear() {
    if (state.token === null && state.user === null) return;
    state = { token: null, user: null };
    writeStorage(state);
    notify();
  },
};
