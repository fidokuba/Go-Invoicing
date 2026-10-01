import { useMutation, useQueryClient } from "@tanstack/react-query";
import { client } from "../client";
import { unwrap } from "../unwrap";

/**
 * "Create Test Data" (Settings page, admin only) — seeds 5 fake
 * customers, a varying pool of fake products, and a varying number of
 * invoices per customer (some single-line, some multi-line; a mix of
 * Draft/Sent/Paid). The server does all of the actual generation; this
 * hook just calls it and invalidates every list the result could affect,
 * so the Customers/Products/Invoices pages immediately show the new
 * data without a manual refresh.
 */
export function useCreateTestData() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => unwrap(client.POST("/api/v1/test-data", {})),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["customers"] });
      queryClient.invalidateQueries({ queryKey: ["products"] });
      queryClient.invalidateQueries({ queryKey: ["invoices"] });
    },
  });
}
