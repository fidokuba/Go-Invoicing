import { useSyncExternalStore } from "react";
import { authStore, type AuthUser } from "./authStore";

export interface AuthContextValue {
  token: string | null;
  user: AuthUser | null;
  isAuthenticated: boolean;
}

/** Reactive view of the shared auth store (see authStore.ts) — re-renders
 * any component that calls this whenever the session changes, including
 * when the API client's 401 middleware clears it out from under an
 * in-progress page. */
export function useAuth(): AuthContextValue {
  const state = useSyncExternalStore(authStore.subscribe, authStore.getState);
  return {
    token: state.token,
    user: state.user,
    isAuthenticated: state.token !== null && state.user !== null,
  };
}

export type UserRole = "admin" | "manager" | "user";

/** Frontend role checks are UX only (Milestone 12 section 21/23) — they
 * decide which actions are worth *showing*, never whether an action is
 * actually allowed; the backend enforces that independently on every
 * request regardless of what this returns. */
export function hasRole(user: AuthUser | null, ...roles: UserRole[]): boolean {
  return user !== null && roles.includes(user.role as UserRole);
}
