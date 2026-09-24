import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RecordPaymentDialog } from "./RecordPaymentDialog";

// Milestone 13 Part 1: the dialog's Idempotency-Key lifecycle. The API
// client is mocked so each test controls exactly what POST returns and can
// inspect the key every request carried. (That openapi-fetch puts
// params.header on the wire, and that the server enforces it, is proven
// end-to-end by the Playwright workflow.)
const { post } = vi.hoisted(() => ({ post: vi.fn() }));
vi.mock("@/api/client", () => ({ client: { POST: post, GET: vi.fn() } }));

const INVOICE_ID = "22222222-2222-2222-2222-222222222222";

const recordedPayment = {
  id: "44444444-4444-4444-4444-444444444444",
  invoiceId: INVOICE_ID,
  amount: 10000,
  paymentMethod: "",
  paymentDate: "2026-09-19",
  createdAt: "2026-09-19T09:05:00Z",
  updatedAt: "2026-09-19T09:05:00Z",
};

function success() {
  return Promise.resolve({ data: recordedPayment, response: new Response(null, { status: 201 }) });
}

function apiError(status: number, code: string, message: string) {
  return Promise.resolve({ error: { error: { code, message } }, response: new Response(null, { status }) });
}

function networkFailure() {
  return Promise.reject(new TypeError("Failed to fetch"));
}

/** The Idempotency-Key sent on the nth POST (0-based). */
function keyOf(call: number): string {
  return post.mock.calls[call][1].params.header["Idempotency-Key"];
}

let queryClient: QueryClient;
let onOpenChange: ReturnType<typeof vi.fn>;

function renderDialog() {
  return render(
    <QueryClientProvider client={queryClient}>
      <RecordPaymentDialog
        open
        onOpenChange={onOpenChange}
        invoiceId={INVOICE_ID}
        currency="GBP"
        amountOutstanding={10000}
      />
    </QueryClientProvider>,
  );
}

async function submit(user: ReturnType<typeof userEvent.setup>) {
  const callsBefore = post.mock.calls.length;
  await user.click(screen.getByRole("button", { name: "Record payment" }));
  await waitFor(() => expect(post.mock.calls.length).toBe(callsBefore + 1));
  await waitFor(() => expect(screen.getByRole("button", { name: "Record payment" })).toBeEnabled());
}

beforeEach(() => {
  post.mockReset();
  queryClient = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  onOpenChange = vi.fn();
});

afterEach(() => {
  cleanup();
});

describe("RecordPaymentDialog idempotency", () => {
  it("sends a valid Idempotency-Key with the payment", async () => {
    post.mockImplementation(success);
    const user = userEvent.setup();
    renderDialog();

    await submit(user);

    const [path, options] = post.mock.calls[0];
    expect(path).toBe("/api/v1/invoices/{id}/payments");
    expect(options.params.path).toEqual({ id: INVOICE_ID });
    expect(options.body).toMatchObject({ amount: 10000 });
    expect(keyOf(0)).toMatch(/^[A-Za-z0-9._~:-]{16,128}$/);
  });

  it("reuses the same key when retrying after a network failure", async () => {
    post.mockImplementationOnce(networkFailure).mockImplementationOnce(success);
    const user = userEvent.setup();
    renderDialog();

    await submit(user);
    expect(await screen.findByText(/network error/i)).toBeInTheDocument();
    await submit(user);

    expect(keyOf(1)).toBe(keyOf(0));
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
  });

  it("reuses the same key when retrying after a 5xx", async () => {
    post
      .mockImplementationOnce(() => apiError(500, "internal_error", "internal server error"))
      .mockImplementationOnce(() => apiError(503, "unknown", "Service Unavailable"))
      .mockImplementationOnce(success);
    const user = userEvent.setup();
    renderDialog();

    await submit(user);
    await submit(user);
    await submit(user);

    expect(keyOf(1)).toBe(keyOf(0));
    expect(keyOf(2)).toBe(keyOf(0));
  });

  it("keeps the key across an edit made while the outcome is uncertain", async () => {
    post.mockImplementationOnce(networkFailure).mockImplementationOnce(success);
    const user = userEvent.setup();
    renderDialog();

    await submit(user);
    await user.type(screen.getByLabelText("Reference (optional)"), "TX-1");
    await submit(user);

    // Same key, different payload: if the first attempt was in fact
    // recorded, the server answers 409 idempotency_key_reused rather than
    // recording a second payment.
    expect(keyOf(1)).toBe(keyOf(0));
  });

  it("clears the key after success, so the next payment gets a new one", async () => {
    post.mockImplementation(success);
    const user = userEvent.setup();
    renderDialog();

    await submit(user);
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    await submit(user);

    expect(keyOf(1)).not.toBe(keyOf(0));
  });

  it("uses a new key for a corrected submission after a definitive 4xx", async () => {
    post
      .mockImplementationOnce(() => apiError(409, "conflict", "payment amount exceeds the invoice's outstanding balance"))
      .mockImplementationOnce(() => apiError(400, "validation_failed", "payment amount must be greater than zero"))
      .mockImplementationOnce(success);
    const user = userEvent.setup();
    renderDialog();

    await submit(user);
    expect(await screen.findByText(/exceeds the invoice's outstanding balance/i)).toBeInTheDocument();
    await submit(user);
    await submit(user);

    expect(new Set([keyOf(0), keyOf(1), keyOf(2)]).size).toBe(3);
  });

  it("on idempotency_key_reused: explains, refreshes the invoice, and starts a new key", async () => {
    post
      .mockImplementationOnce(networkFailure)
      .mockImplementationOnce(() =>
        apiError(409, "idempotency_key_reused", "Idempotency-Key has already been used for a different payment request."),
      )
      .mockImplementationOnce(success);
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const user = userEvent.setup();
    renderDialog();

    await submit(user);
    await submit(user);

    expect(await screen.findByText(/check the invoice's recorded payments/i)).toBeInTheDocument();
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["invoices"] });
    expect(onOpenChange).not.toHaveBeenCalledWith(false);

    await submit(user);
    expect(keyOf(1)).toBe(keyOf(0));
    expect(keyOf(2)).not.toBe(keyOf(1));
  });

  it("still ignores a second click while a payment is in flight", async () => {
    let resolve!: (value: unknown) => void;
    post.mockImplementation(() => new Promise((r) => (resolve = r)));
    const user = userEvent.setup();
    renderDialog();

    const button = screen.getByRole("button", { name: "Record payment" });
    await user.click(button);
    await waitFor(() => expect(screen.getByRole("button", { name: "Recording…" })).toBeDisabled());
    await user.click(screen.getByRole("button", { name: "Recording…" }));

    expect(post).toHaveBeenCalledTimes(1);

    resolve({ data: recordedPayment, response: new Response(null, { status: 201 }) });
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
  });

  it("still validates the amount client-side without sending anything", async () => {
    const user = userEvent.setup();
    renderDialog();

    const amount = screen.getByLabelText("Amount (GBP)");
    await user.clear(amount);
    await user.type(amount, "0");
    await user.click(screen.getByRole("button", { name: "Record payment" }));

    expect(await screen.findByText(/valid amount greater than zero/i)).toBeInTheDocument();
    expect(post).not.toHaveBeenCalled();
  });
});
