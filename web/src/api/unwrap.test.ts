import { describe, expect, it } from "vitest";
import { unwrap } from "./unwrap";
import { ApiError } from "./errors";

function failure(status: number, headers: Record<string, string> = {}) {
  return {
    error: { error: { code: "rate_limited", message: "too many requests" } },
    response: new Response(null, { status, headers }),
  };
}

async function errorFrom(result: ReturnType<typeof failure>): Promise<ApiError> {
  try {
    await unwrap(result);
  } catch (err) {
    return err as ApiError;
  }
  throw new Error("expected unwrap to throw");
}

describe("unwrap Retry-After", () => {
  it("carries a 429's Retry-After seconds on the ApiError", async () => {
    const err = await errorFrom(failure(429, { "Retry-After": "6" }));
    expect(err).toBeInstanceOf(ApiError);
    expect(err.retryAfterSeconds).toBe(6);
  });

  it("ignores a missing or non-numeric Retry-After, and any non-429", async () => {
    for (const result of [failure(429), failure(429, { "Retry-After": "soon" }), failure(503, { "Retry-After": "6" })]) {
      expect((await errorFrom(result)).retryAfterSeconds).toBeUndefined();
    }
  });
});
