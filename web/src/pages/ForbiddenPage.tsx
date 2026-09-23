import { Link } from "react-router-dom";
import { ShieldAlert } from "lucide-react";

/** Shown when the backend rejects a request with 403 in a context where
 * the whole page depends on it (Milestone 12 section 21/22) — proactive
 * hiding of controls (see useAuth's hasRole) is the primary defence, but
 * the backend's own authorization remains authoritative, so this state
 * must exist for the case a role changes elsewhere, a stale link is
 * followed, or hiding didn't cover every path. */
export function ForbiddenPage() {
  return (
    <div className="flex flex-col items-center justify-center py-24 text-center">
      <ShieldAlert className="h-8 w-8 text-slate-300" aria-hidden="true" />
      <h1 className="mt-3 text-xl font-semibold text-slate-900">You don't have permission to view this</h1>
      <p className="mt-2 text-sm text-slate-500">
        Your account role doesn't allow this action. Contact an administrator if you believe this is wrong.
      </p>
      <Link to="/dashboard" className="mt-6 text-sm text-brand-600 hover:underline">
        Return to dashboard
      </Link>
    </div>
  );
}
