import { describe, expect, it } from "vitest";
import { ApiError, friendlyMessage, isRetryable } from "./errors";

describe("friendlyMessage", () => {
  it("maps 401 to a session-expired message", () => {
    expect(friendlyMessage(new ApiError(401, "unauthorized", "unauthorized"))).toMatch(/session has expired/i);
  });

  it("maps 403 to a permission message", () => {
    expect(friendlyMessage(new ApiError(403, "forbidden", "forbidden"))).toMatch(/permission/i);
  });

  it("maps 404 to a not-found message", () => {
    expect(friendlyMessage(new ApiError(404, "invoice_not_found", "invoice not found"))).toMatch(/couldn't be found/i);
  });

  it("passes through a server-provided 409 message (already safe to display)", () => {
    expect(friendlyMessage(new ApiError(409, "invoice_already_sent", "invoice has already been sent"))).toBe(
      "invoice has already been sent",
    );
  });

  it("explains an idempotency_key_reused conflict and points at the recorded payments", () => {
    const message = friendlyMessage(
      new ApiError(409, "idempotency_key_reused", "Idempotency-Key has already been used for a different payment request."),
    );
    expect(message).toMatch(/already recorded/i);
    expect(message).toMatch(/recorded payments/i);
    expect(message).not.toMatch(/idempotency/i);
  });

  it("gives a generic message for 500, never exposing raw detail", () => {
    expect(friendlyMessage(new ApiError(500, "internal_error", "internal server error"))).toMatch(
      /went wrong on our end/i,
    );
  });

  it("maps a network TypeError to a network-error message", () => {
    expect(friendlyMessage(new TypeError("Failed to fetch"))).toMatch(/network error/i);
  });

  it("gives a generic fallback for an unrecognised error", () => {
    expect(friendlyMessage("something weird")).toMatch(/went wrong/i);
  });
});

describe("isRetryable", () => {
  it("treats 5xx as retryable", () => {
    expect(isRetryable(new ApiError(500, "internal_error", "x"))).toBe(true);
  });

  it("treats 429 as retryable", () => {
    expect(isRetryable(new ApiError(429, "rate_limited", "x"))).toBe(true);
  });

  it("treats 400/404/409 as not retryable", () => {
    expect(isRetryable(new ApiError(400, "validation_failed", "x"))).toBe(false);
    expect(isRetryable(new ApiError(404, "not_found", "x"))).toBe(false);
    expect(isRetryable(new ApiError(409, "conflict", "x"))).toBe(false);
  });

  it("treats a network TypeError as retryable", () => {
    expect(isRetryable(new TypeError("Failed to fetch"))).toBe(true);
  });
});
