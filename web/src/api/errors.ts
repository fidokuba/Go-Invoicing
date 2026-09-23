/**
 * API error mapping (Milestone 12 section 22).
 *
 * Every application-generated error response uses one JSON envelope
 * (api/openapi.yaml's ErrorBody: `{"error": {"code", "message"}}`).
 * ApiError carries the HTTP status and that envelope's fields; every
 * mutation/query in src/api/queries throws one of these (via `unwrap`)
 * instead of a raw fetch/openapi-fetch error, so every consumer has one
 * consistent shape to branch on.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

/** A safe, user-facing message for any error a mutation/query can throw.
 * Never surfaces raw server detail (stack traces, SQL, etc.) — the
 * backend's own error envelope already guarantees its `message` is safe
 * to display (see api/openapi.yaml's ErrorDetail), but a generic,
 * consistent phrasing per status code reads better in the UI than the
 * server's terser wording, and covers network/unexpected failures the
 * server never produced at all. */
export function friendlyMessage(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.status) {
      case 400:
        return err.message || "Please check the form and try again.";
      case 401:
        return "Your session has expired. Please log in again.";
      case 403:
        return "You don't have permission to do that.";
      case 404:
        return "That couldn't be found.";
      case 409:
        return err.message || "That couldn't be completed because of its current status.";
      case 413:
        return "That request was too large.";
      case 415:
        return "Unsupported request format.";
      case 429:
        return "You're doing that too much right now — please wait a moment and try again.";
      default:
        return "Something went wrong on our end. Please try again.";
    }
  }

  if (err instanceof TypeError) {
    // fetch() rejects with TypeError on a genuine network failure (DNS,
    // connection refused, offline) — distinct from any HTTP response.
    return "Network error — check your connection and try again.";
  }

  return "Something went wrong. Please try again.";
}

/** Whether a failed request is worth offering a plain "Retry" action for
 * (a network failure or a transient server-side condition), as opposed
 * to one where retrying identically will just fail the same way (a
 * validation or permission error). */
export function isRetryable(err: unknown): boolean {
  if (err instanceof ApiError) {
    return err.status >= 500 || err.status === 429;
  }
  return err instanceof TypeError;
}
