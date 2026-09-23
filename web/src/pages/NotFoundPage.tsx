import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";

export function NotFoundPage() {
  return (
    <div className="flex flex-col items-center justify-center py-24 text-center">
      <p className="text-sm font-medium text-slate-400">404</p>
      <h1 className="mt-2 text-xl font-semibold text-slate-900">Page not found</h1>
      <p className="mt-2 text-sm text-slate-500">The page you're looking for doesn't exist.</p>
      <Button className="mt-6" onClick={() => history.back()}>
        Go back
      </Button>
      <Link to="/dashboard" className="mt-3 text-sm text-brand-600 hover:underline">
        Return to dashboard
      </Link>
    </div>
  );
}
