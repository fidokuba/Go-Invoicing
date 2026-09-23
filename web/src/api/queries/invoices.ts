import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { client } from "../client";
import { unwrap } from "../unwrap";
import type { components } from "../schema";

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

export function useCreatePayment(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: CreatePaymentRequest) =>
      unwrap(client.POST("/api/v1/invoices/{id}/payments", { params: { path: { id } }, body })),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["invoices", id] });
      queryClient.invalidateQueries({ queryKey: ["invoices", id, "payments"] });
      queryClient.invalidateQueries({ queryKey: ["invoices"], exact: false });
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
