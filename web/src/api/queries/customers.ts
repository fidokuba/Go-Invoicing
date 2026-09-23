import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { client } from "../client";
import { unwrap } from "../unwrap";
import { ApiError } from "../errors";
import type { components } from "../schema";

type CreateCustomerRequest = components["schemas"]["CreateCustomerRequest"];
type UpsertBillingAddressRequest = components["schemas"]["UpsertBillingAddressRequest"];

export interface CustomersListParams {
  limit?: number;
  offset?: number;
  search?: string;
  status?: "active" | "inactive" | "archived";
  sort?: "name" | "companyName" | "createdAt";
  order?: "asc" | "desc";
}

export function useCustomers(params: CustomersListParams) {
  return useQuery({
    queryKey: ["customers", params],
    queryFn: () => unwrap(client.GET("/api/v1/customers", { params: { query: params } })),
  });
}

export function useCustomer(id: string | undefined) {
  return useQuery({
    queryKey: ["customers", id],
    queryFn: () => unwrap(client.GET("/api/v1/customers/{id}", { params: { path: { id: id! } } })),
    enabled: Boolean(id),
  });
}

export function useCreateCustomer() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateCustomerRequest) => unwrap(client.POST("/api/v1/customers", { body })),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["customers"] });
    },
  });
}

/** A customer has at most one billing address; "not found" is a normal,
 * expected state (no address set yet), so it resolves to `null` rather
 * than surfacing as a query error banner — see CustomerDetailPage. */
export function useCustomerBillingAddress(id: string | undefined) {
  return useQuery({
    queryKey: ["customers", id, "billing-address"],
    queryFn: async () => {
      try {
        return await unwrap(
          client.GET("/api/v1/customers/{id}/billing-address", { params: { path: { id: id! } } }),
        );
      } catch (err) {
        if (err instanceof ApiError && err.status === 404) return null;
        throw err;
      }
    },
    enabled: Boolean(id),
  });
}

/**
 * A single, page-scoped lookup of customer id -> display name, used by
 * the invoices list/editor so a "Customer" column/select doesn't fetch
 * one customer per invoice row (Milestone 12 section 45's N+1 warning).
 * One bounded request (the API's own maximum page size) stands in for a
 * per-row join; an organisation with more than 200 customers will have
 * some invoice rows fall back to showing a raw id — a known, documented
 * scale limit rather than unbounded per-row fetching.
 */
const CUSTOMER_LOOKUP_LIMIT = 200;

export function useCustomerLookup() {
  const query = useCustomers({ limit: CUSTOMER_LOOKUP_LIMIT, sort: "name", order: "asc" });
  const lookup = new Map<string, string>();
  for (const customer of query.data?.items ?? []) {
    lookup.set(customer.id, customer.companyName || customer.name);
  }
  return { ...query, lookup };
}

export function useUpsertCustomerBillingAddress(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: UpsertBillingAddressRequest) =>
      unwrap(client.PUT("/api/v1/customers/{id}/billing-address", { params: { path: { id } }, body })),
    onSuccess: (data) => {
      queryClient.setQueryData(["customers", id, "billing-address"], data);
    },
  });
}
