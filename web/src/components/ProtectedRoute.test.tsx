import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { ProtectedRoute } from "./ProtectedRoute";
import { authStore, type AuthUser } from "@/lib/authStore";

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

function renderProtected(initialEntry: string) {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/login" element={<div>Login page</div>} />
        <Route element={<ProtectedRoute />}>
          <Route path="/dashboard" element={<div>Dashboard content</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  authStore.clear();
});

afterEach(() => {
  cleanup();
  authStore.clear();
});

describe("ProtectedRoute", () => {
  it("redirects to /login when there is no session", () => {
    renderProtected("/dashboard");
    expect(screen.getByText("Login page")).toBeInTheDocument();
  });

  it("renders the protected content when a session exists", () => {
    authStore.setSession("tok-123", testUser);
    renderProtected("/dashboard");
    expect(screen.getByText("Dashboard content")).toBeInTheDocument();
  });

  it("redirects away as soon as the session is cleared (e.g. by a 401)", () => {
    authStore.setSession("tok-123", testUser);
    renderProtected("/dashboard");
    expect(screen.getByText("Dashboard content")).toBeInTheDocument();

    act(() => {
      authStore.clear();
    });

    expect(screen.getByText("Login page")).toBeInTheDocument();
  });
});
