import type { ReactNode } from "react";
import { friendlyMessage } from "@/api/errors";
import { Alert } from "./alert";
import { PageLoading } from "./spinner";

interface QueryLike<T> {
  isLoading: boolean;
  isError: boolean;
  error: unknown;
  data: T | undefined;
  refetch: () => void;
}

/**
 * The one shared loading/error boundary every data-driven page in this
 * app uses (Milestone 12 sections 24/27) — a page never renders a blank
 * screen while its primary query is pending or has failed. Empty-result
 * handling stays page-specific (each page knows what "no invoices yet"
 * vs. "no customers yet" should say — see EmptyState) rather than being
 * generalised here.
 */
export function QueryBoundary<T>({
  query,
  loading,
  children,
}: {
  query: QueryLike<T>;
  loading?: ReactNode;
  children: (data: T) => ReactNode;
}) {
  if (query.isLoading) {
    return <>{loading ?? <PageLoading />}</>;
  }

  if (query.isError) {
    return (
      <Alert title="Couldn't load this page" onRetry={query.refetch}>
        {friendlyMessage(query.error)}
      </Alert>
    );
  }

  if (query.data === undefined) return null;

  return <>{children(query.data)}</>;
}
