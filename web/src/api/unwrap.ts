import { ApiError } from "./errors";

interface FetchResult<T> {
  data?: T;
  error?: unknown;
  response: Response;
}

interface ErrorEnvelope {
  error?: { code?: string; message?: string };
}

/** Turn an openapi-fetch result into a plain value or a thrown ApiError,
 * so every query/mutation hook (see src/api/queries) gets one consistent
 * shape: either the typed response body, or an ApiError a component can
 * branch on (see src/api/errors.ts). */
export async function unwrap<T>(result: FetchResult<T> | Promise<FetchResult<T>>): Promise<T> {
  const { data, error, response } = await result;

  if (error !== undefined) {
    const body = error as ErrorEnvelope;
    throw new ApiError(
      response.status,
      body.error?.code ?? "unknown",
      body.error?.message ?? (response.statusText || "request failed"),
    );
  }

  return data as T;
}
