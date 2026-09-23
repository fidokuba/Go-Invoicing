import { QueryClient } from "@tanstack/react-query";
import { ApiError } from "@/api/errors";

/** One shared TanStack Query client (Milestone 12 section 28: keep
 * client-global state minimal — this is the one piece of "global state
 * management" the app uses, for server-state caching/loading/mutation
 * status, not a general application state store). Retries are disabled
 * for 4xx errors (a validation/permission/not-found error will not
 * succeed by retrying identically) but left on TanStack Query's own
 * sensible default backoff for everything else (network failures,
 * 5xx). */
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: (failureCount, error) => {
        if (error instanceof ApiError && error.status >= 400 && error.status < 500) {
          return false;
        }
        return failureCount < 2;
      },
      staleTime: 10_000,
      refetchOnWindowFocus: false,
    },
    mutations: {
      retry: false,
    },
  },
});
