import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AuthUser } from "./authStore";

const testUser: AuthUser = {
  id: "11111111-1111-1111-1111-111111111111",
  organisationId: "22222222-2222-2222-2222-222222222222",
  name: "Ada Lovelace",
  email: "ada@example.com",
  role: "admin",
  isActive: true,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

// Each test re-imports the module fresh (vi.resetModules) so its
// module-level `readStorage()` call — which only ever runs once, at
// import time — is exercised under that test's own localStorage state,
// matching how a real page load reads it exactly once.
async function freshStore() {
  const mod = await import("./authStore");
  return mod;
}

beforeEach(() => {
  vi.resetModules();
  localStorage.clear();
});

afterEach(() => {
  localStorage.clear();
});

describe("authStore", () => {
  it("starts logged out when localStorage is empty", async () => {
    const { authStore } = await freshStore();
    const state = authStore.getState();
    expect(state.token).toBeNull();
    expect(state.user).toBeNull();
  });

  it("restores a session already in localStorage (page refresh)", async () => {
    const { authStore, STORAGE_KEY } = await freshStore();
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ token: "tok-123", user: testUser }));
    vi.resetModules();
    const { authStore: reloaded } = await freshStore();
    void authStore; // first import unused after reset
    expect(reloaded.getState()).toEqual({ token: "tok-123", user: testUser });
  });

  it("setSession updates state and persists to localStorage", async () => {
    const { authStore, STORAGE_KEY } = await freshStore();
    authStore.setSession("tok-abc", testUser);

    expect(authStore.getState()).toEqual({ token: "tok-abc", user: testUser });
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY)!)).toEqual({ token: "tok-abc", user: testUser });
  });

  it("clear resets state and removes it from localStorage", async () => {
    const { authStore, STORAGE_KEY } = await freshStore();
    authStore.setSession("tok-abc", testUser);
    authStore.clear();

    expect(authStore.getState()).toEqual({ token: null, user: null });
    expect(localStorage.getItem(STORAGE_KEY)).toBeNull();
  });

  it("notifies subscribers on setSession and clear", async () => {
    const { authStore } = await freshStore();
    const listener = vi.fn();
    const unsubscribe = authStore.subscribe(listener);

    authStore.setSession("tok-abc", testUser);
    authStore.clear();

    expect(listener).toHaveBeenCalledTimes(2);
    unsubscribe();
  });

  it("does not notify a listener after it unsubscribes", async () => {
    const { authStore } = await freshStore();
    const listener = vi.fn();
    const unsubscribe = authStore.subscribe(listener);
    unsubscribe();

    authStore.setSession("tok-abc", testUser);

    expect(listener).not.toHaveBeenCalled();
  });

  it("ignores corrupted localStorage content rather than throwing", async () => {
    const { STORAGE_KEY } = await freshStore();
    localStorage.setItem(STORAGE_KEY, "{not valid json");
    vi.resetModules();

    const { authStore } = await freshStore();
    expect(authStore.getState()).toEqual({ token: null, user: null });
  });
});
