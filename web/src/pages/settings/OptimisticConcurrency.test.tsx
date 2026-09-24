import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { authStore, type AuthUser } from "@/lib/authStore";
import { OrganisationSettingsPage } from "./OrganisationSettingsPage";
import { InvoiceSettingsPage } from "./InvoiceSettingsPage";

// Milestone 13 Part 2: the settings forms' optimistic-concurrency
// behaviour, against a mocked API client.
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

function organisation(phone: string) {
  return {
    id: admin.organisationId,
    name: "Acme Ltd",
    phone,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  };
}

function ok(data: unknown, etag: string) {
  return Promise.resolve({ data, response: new Response(null, { status: 200, headers: { ETag: etag } }) });
}

function stale() {
  return Promise.resolve({
    error: { error: { code: "precondition_failed", message: "this resource has been modified since it was read" } },
    response: new Response(null, { status: 412 }),
  });
}

/** The If-Match sent on the nth PATCH (0-based). */
function ifMatchOf(call: number): string {
  return patch.mock.calls[call][1].params.header["If-Match"];
}

function renderPage(page: React.ReactNode) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(<QueryClientProvider client={queryClient}>{page}</QueryClientProvider>);
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

describe("Organisation settings optimistic concurrency", () => {
  it("sends the loaded ETag as If-Match, then the ETag its own save returned", async () => {
    get.mockImplementation(() => ok(organisation("0100"), '"1"'));
    patch
      .mockImplementationOnce(() => ok(organisation("0200"), '"2"'))
      .mockImplementationOnce(() => ok(organisation("0300"), '"3"'));
    const user = userEvent.setup();
    renderPage(<OrganisationSettingsPage />);

    const phone = await screen.findByLabelText("Phone");
    await user.clear(phone);
    await user.type(phone, "0200");
    await user.click(screen.getByRole("button", { name: "Save changes" }));
    expect(await screen.findByText("Organisation details updated.")).toBeInTheDocument();

    await user.clear(phone);
    await user.type(phone, "0300");
    await user.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(patch).toHaveBeenCalledTimes(2));

    expect(ifMatchOf(0)).toBe('"1"');
    expect(ifMatchOf(1)).toBe('"2"');
    expect(patch.mock.calls[0][1].body).toMatchObject({ phone: "0200" });
  });

  it("on 412 explains, and Reload refetches and resets the form to the latest version", async () => {
    get
      .mockImplementationOnce(() => ok(organisation("0100"), '"1"'))
      .mockImplementationOnce(() => ok(organisation("0999-SOMEONE-ELSE"), '"2"'));
    patch.mockImplementationOnce(stale).mockImplementationOnce(() => ok(organisation("0555"), '"3"'));
    const user = userEvent.setup();
    renderPage(<OrganisationSettingsPage />);

    const phone = await screen.findByLabelText("Phone");
    await user.clear(phone);
    await user.type(phone, "0200");
    await user.click(screen.getByRole("button", { name: "Save changes" }));

    expect(
      await screen.findByText(
        "This record has changed since you opened it. Reload the latest version before saving your changes.",
      ),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Reload" }));

    // The form now shows the other person's saved value, and the message is gone.
    await waitFor(() => expect(screen.getByLabelText("Phone")).toHaveValue("0999-SOMEONE-ELSE"));
    expect(screen.queryByText(/has changed since you opened it/)).not.toBeInTheDocument();
    expect(get).toHaveBeenCalledTimes(2);

    // Saving now is based on the reloaded version.
    await user.clear(screen.getByLabelText("Phone"));
    await user.type(screen.getByLabelText("Phone"), "0555");
    await user.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(patch).toHaveBeenCalledTimes(2));
    expect(ifMatchOf(0)).toBe('"1"');
    expect(ifMatchOf(1)).toBe('"2"');
  });
});

describe("Invoice settings optimistic concurrency", () => {
  it("sends the loaded ETag as If-Match and shows the reload message on 412", async () => {
    get.mockImplementation(() => ok({ currency: "GBP", paymentTerms: 30, invoicePrefix: "INV-" }, '"7"'));
    patch.mockImplementationOnce(stale);
    const user = userEvent.setup();
    renderPage(<InvoiceSettingsPage />);

    await user.click(await screen.findByRole("button", { name: "Save changes" }));

    await waitFor(() => expect(patch).toHaveBeenCalledTimes(1));
    expect(ifMatchOf(0)).toBe('"7"');
    expect(await screen.findByRole("button", { name: "Reload" })).toBeInTheDocument();
  });
});
