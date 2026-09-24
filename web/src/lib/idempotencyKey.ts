import { ApiError } from "@/api/errors";

/**
 * Idempotency keys for payment creation (Milestone 13 Part 1).
 *
 * POST /api/v1/invoices/{id}/payments requires an `Idempotency-Key`
 * (16–128 characters from A–Z a–z 0–9 . _ ~ : -; see api/openapi.yaml).
 * One key represents one logical payment attempt, not one fetch() call:
 * it is reused for every retry of that attempt, so a retry after an
 * uncertain failure can never record the payment twice.
 */

/** A fresh, random 128-bit key as 32 lowercase hex characters.
 *
 * Uses crypto.getRandomValues rather than crypto.randomUUID: randomUUID
 * only exists in secure contexts (HTTPS or localhost), and the supported
 * self-hosted deployment serves this app over plain HTTP on a LAN
 * address (see README), where it is undefined. getRandomValues is
 * available in every context. */
export function newIdempotencyKey(): string {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

/** Whether a failed payment request's outcome is unknown — the server may
 * or may not have recorded it — so a retry must reuse the same key: a
 * network failure (fetch rejects with TypeError), a 5xx, or anything
 * that isn't a recognised API response at all. An ordinary 4xx is
 * definitive: the server rejected the request and recorded nothing. */
export function isUncertainFailure(err: unknown): boolean {
  if (err instanceof ApiError) {
    return err.status >= 500;
  }
  return true;
}

/** The server's stable error code for a key already used, on this
 * invoice, by a different payment request. */
export const IDEMPOTENCY_KEY_REUSED = "idempotency_key_reused";

export function isIdempotencyKeyReused(err: unknown): boolean {
  return err instanceof ApiError && err.code === IDEMPOTENCY_KEY_REUSED;
}
