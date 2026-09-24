import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { authStore, type AuthUser } from "@/lib/authStore";
import { OrganisationSettingsPage } from "./OrganisationSettingsPage";

// The "VAT Registered Company?" tick box: the VAT Registration Number field only exists
// while it's ticked, a VAT number is required to save it ticked, and
// unticking keeps the stored number rather than clearing it.
const { get, patch } = vi.hoisted(() => ({ get: vi.fn(), patch: vi.fn() }));
vi.mock("@/api/client", () => ({ client: { GET: get, PATCH: patch, POST: vi.fn() } }));

const admin: AuthUser = {
  id: "11111111-1111-1111-1111-111111111111",
  organisationId: "22222222-2222-2222-2222-222222222222",
  name: "Ada Admin",
  email: "ada@example.com",
  role: "admin",
  isActive: true,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

function organisation(vatRegistered: boolean, taxId?: string) {
  return {
    id: admin.organisationId,
    name: "Acme Ltd",
    vatRegistered,
    taxId,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  };
}

function ok(data: unknown, etag: string) {
  return Promise.resolve({ data, response: new Response(null, { status: 200, headers: { ETag: etag } }) });
}

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <OrganisationSettingsPage />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  get.mockReset();
  patch.mockReset();
  authStore.setSession("tok", admin);
});

afterEach(() => {
  cleanup();
  authStore.clear();
});

describe("Organisation VAT registration", () => {
  it("hides the VAT Registration Number field until the box is ticked", async () => {
    get.mockImplementation(() => ok(organisation(false), '"1"'));
    const user = userEvent.setup();
    renderPage();

    const checkbox = await screen.findByLabelText("VAT Registered Company?");
    expect(checkbox).not.toBeChecked();
    expect(screen.queryByLabelText("VAT Registration Number")).not.toBeInTheDocument();

    await user.click(checkbox);
    expect(screen.getByLabelText("VAT Registration Number")).toBeInTheDocument();

    await user.click(checkbox);
    expect(screen.queryByLabelText("VAT Registration Number")).not.toBeInTheDocument();
  });

  it("shows the stored VAT Registration Number for a registered organisation", async () => {
    get.mockImplementation(() => ok(organisation(true, "GB123456789"), '"1"'));
    renderPage();

    expect(await screen.findByLabelText("VAT Registered Company?")).toBeChecked();
    expect(screen.getByLabelText("VAT Registration Number")).toHaveValue("GB123456789");
  });

  it("requires a VAT number before saving as registered", async () => {
    get.mockImplementation(() => ok(organisation(false), '"1"'));
    patch.mockImplementation(() => ok(organisation(true, "GB123456789"), '"2"'));
    const user = userEvent.setup();
    renderPage();

    await user.click(await screen.findByLabelText("VAT Registered Company?"));
    await user.click(screen.getByRole("button", { name: "Save changes" }));

    expect(await screen.findByText("Enter your VAT registration number.")).toBeInTheDocument();
    expect(patch).not.toHaveBeenCalled();

    await user.type(screen.getByLabelText("VAT Registration Number"), "GB123456789");
    await user.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() => expect(patch).toHaveBeenCalledTimes(1));
    expect(patch.mock.calls[0][1].body).toMatchObject({ vatRegistered: true, taxId: "GB123456789" });
  });

  it("unticking saves as not registered without clearing the stored VAT Registration Number", async () => {
    get.mockImplementation(() => ok(organisation(true, "GB123456789"), '"1"'));
    patch.mockImplementation(() => ok(organisation(false, "GB123456789"), '"2"'));
    const user = userEvent.setup();
    renderPage();

    await user.click(await screen.findByLabelText("VAT Registered Company?"));
    await user.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() => expect(patch).toHaveBeenCalledTimes(1));
    const body = patch.mock.calls[0][1].body;
    expect(body.vatRegistered).toBe(false);
    expect(body).not.toHaveProperty("taxId");
  });
});
