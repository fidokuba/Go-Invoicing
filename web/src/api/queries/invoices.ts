import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { client } from "../client";
import { unwrap } from "../unwrap";
import type { components } from "../schema";
import { isIdempotencyKeyReused } from "@/lib/idempotencyKey";

type CreateInvoiceRequest = components["schemas"]["CreateInvoiceRequest"];
type CreatePaymentRequest = components["schemas"]["CreatePaymentHTTPRequest"];

export interface InvoicesListParams {
  limit?: number;
  offset?: number;
  status?: "draft" | "sent" | "overdue" | "paid";
  customerId?: string;
  search?: string;
  issueDateFrom?: string;
  issueDateTo?: string;
  dueDateFrom?: string;
  dueDateTo?: string;
  sort?: "invoiceNumber" | "issueDate" | "dueDate" | "total" | "createdAt";
  order?: "asc" | "desc";
}

export function useInvoices(params: InvoicesListParams) {
  return useQuery({
    queryKey: ["invoices", params],
    queryFn: () => unwrap(client.GET("/api/v1/invoices", { params: { query: params } })),
  });
}

export function useInvoice(id: string | undefined) {
  return useQuery({
    queryKey: ["invoices", id],
    queryFn: () => unwrap(client.GET("/api/v1/invoices/{id}", { params: { path: { id: id! } } })),
    enabled: Boolean(id),
  });
}

export function useCreateInvoice() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateInvoiceRequest) => unwrap(client.POST("/api/v1/invoices", { body })),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["invoices"] });
    },
  });
}

/** Finalises (Send) a draft invoice — a lifecycle/snapshotting action
 * only; it does not email or otherwise deliver anything (see
 * api/openapi.yaml's own description of this operation). */
export function useSendInvoice(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => unwrap(client.POST("/api/v1/invoices/{id}/send", { params: { path: { id } } })),
    onSuccess: (data) => {
      queryClient.setQueryData(["invoices", id], data);
      queryClient.invalidateQueries({ queryKey: ["invoices"], exact: false });
    },
  });
}

export function useInvoicePayments(id: string | undefined) {
  return useQuery({
    queryKey: ["invoices", id, "payments"],
    queryFn: () => unwrap(client.GET("/api/v1/invoices/{id}/payments", { params: { path: { id: id! } } })),
    enabled: Boolean(id),
  });
}

export interface CreatePaymentVariables {
  body: CreatePaymentRequest;
  /** One logical payment attempt's key — reused for every retry of that
   * attempt (see src/lib/idempotencyKey.ts). */
  idempotencyKey: string;
}

export function useCreatePayment(id: string) {
  const queryClient = useQueryClient();
  // One prefix invalidation covers this invoice, its payments and every
  // invoice list. Deliberately a single call: overlapping
  // invalidateQueries calls each cancel the previous call's in-flight
  // refetch (cancelRefetch), which intermittently left the invoice or its
  // payment history showing pre-payment data.
  const invalidateInvoice = () => queryClient.invalidateQueries({ queryKey: ["invoices"] });
  return useMutation({
    mutationFn: ({ body, idempotencyKey }: CreatePaymentVariables) =>
      unwrap(
        client.POST("/api/v1/invoices/{id}/payments", {
          params: { path: { id }, header: { "Idempotency-Key": idempotencyKey } },
          body,
        }),
      ),
    // A replayed payment (201 + Idempotent-Replayed) is handled exactly
    // like a new one: either way it is now recorded.
    onSuccess: invalidateInvoice,
    onError: (err) => {
      // The key was already used for a different payment — something was
      // recorded that this screen may not be showing yet, so refresh it.
      if (isIdempotencyKeyReused(err)) invalidateInvoice();
    },
  });
}

/** Fetches the invoice's PDF as a Blob for the caller to turn into an
 * object URL (see components/invoice/InvoicePdfActions.tsx) — never
 * cached by React Query, since the server always renders it fresh (see
 * api/openapi.yaml's own note that nothing about it is cached). */
export async function fetchInvoicePdf(id: string): Promise<Blob> {
  return unwrap(
    client.GET("/api/v1/invoices/{id}/pdf", {
      params: { path: { id } },
      parseAs: "blob",
    }),
  );
}
