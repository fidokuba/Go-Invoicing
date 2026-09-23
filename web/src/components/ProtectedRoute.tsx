import { Navigate, Outlet, useLocation } from "react-router-dom";
import { useAuth } from "@/lib/useAuth";

/**
 * Guards every authenticated route (Milestone 12 section 5).
 *
 * If there is no local session, this redirects to /login, remembering
 * the attempted location in navigation state so LoginPage can send the
 * user back where they were headed. This also handles the "API returned
 * 401" case with no special-case code at all: the API client's
 * middleware (see api/client.ts) clears the shared auth store on any
 * 401, useAuth's useSyncExternalStore subscription re-renders this
 * component on that change, and it then redirects — exactly the same
 * path as an ordinary unauthenticated visit.
 *
 * This can never loop: /login itself is not rendered through this
 * guard, so clearing the session while already on /login (there is
 * nothing to clear there — see api/client.ts's own note on excluding the
 * login endpoint) never triggers another redirect back into itself.
 */
export function ProtectedRoute() {
  const { isAuthenticated } = useAuth();
  const location = useLocation();

  if (!isAuthenticated) {
    return <Navigate to="/login" state={{ from: location }} replace />;
  }

  return <Outlet />;
}
