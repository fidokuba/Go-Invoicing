import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/api/errors";
import { isIdempotencyKeyReused, isUncertainFailure, newIdempotencyKey } from "./idempotencyKey";

// The backend's accepted syntax (api/openapi.yaml IdempotencyKeyHeader).
const BACKEND_KEY_PATTERN = /^[A-Za-z0-9._~:-]{16,128}$/;

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("newIdempotencyKey", () => {
  it("produces a 128-bit hex key the backend accepts", () => {
    const key = newIdempotencyKey();
    expect(key).toMatch(/^[0-9a-f]{32}$/);
    expect(key).toMatch(BACKEND_KEY_PATTERN);
  });

  it("produces a different key every time", () => {
    const keys = new Set(Array.from({ length: 1000 }, () => newIdempotencyKey()));
    expect(keys.size).toBe(1000);
  });

  it("works without crypto.randomUUID (plain-HTTP, non-secure contexts)", () => {
    const getRandomValues = crypto.getRandomValues.bind(crypto);
    vi.stubGlobal("crypto", { getRandomValues });

    expect(newIdempotencyKey()).toMatch(BACKEND_KEY_PATTERN);
  });
});

describe("isUncertainFailure", () => {
  it("treats a network failure as uncertain", () => {
    expect(isUncertainFailure(new TypeError("Failed to fetch"))).toBe(true);
  });

  it("treats any 5xx as uncertain", () => {
    expect(isUncertainFailure(new ApiError(500, "internal_error", "internal server error"))).toBe(true);
    expect(isUncertainFailure(new ApiError(503, "unknown", "Service Unavailable"))).toBe(true);
  });

  it("treats an unrecognised error as uncertain", () => {
    expect(isUncertainFailure(new Error("something odd"))).toBe(true);
  });

  it("treats an ordinary 4xx as definitive", () => {
    expect(isUncertainFailure(new ApiError(400, "validation_failed", "bad"))).toBe(false);
    expect(isUncertainFailure(new ApiError(404, "invoice_not_found", "invoice not found"))).toBe(false);
    expect(isUncertainFailure(new ApiError(409, "conflict", "exceeds outstanding"))).toBe(false);
  });
});

describe("isIdempotencyKeyReused", () => {
  it("recognises only the idempotency_key_reused code", () => {
    expect(isIdempotencyKeyReused(new ApiError(409, "idempotency_key_reused", "reused"))).toBe(true);
    expect(isIdempotencyKeyReused(new ApiError(409, "conflict", "exceeds outstanding"))).toBe(false);
    expect(isIdempotencyKeyReused(new TypeError("Failed to fetch"))).toBe(false);
  });
});
