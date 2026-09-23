import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { client } from "../client";
import { unwrap } from "../unwrap";
import type { components } from "../schema";

type UpdateOrganisationRequest = components["schemas"]["UpdateOrganisationRequest"];
type UpdateSettingsRequest = components["schemas"]["UpdateSettingsRequest"];

export function useOrganisation() {
  return useQuery({
    queryKey: ["organisation"],
    queryFn: () => unwrap(client.GET("/api/v1/organisation", {})),
  });
}

export function useUpdateOrganisation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: UpdateOrganisationRequest) => unwrap(client.PATCH("/api/v1/organisation", { body })),
    onSuccess: (data) => {
      queryClient.setQueryData(["organisation"], data);
    },
  });
}

export function useInvoiceSettings() {
  return useQuery({
    queryKey: ["organisation", "settings"],
    queryFn: () => unwrap(client.GET("/api/v1/organisation/settings", {})),
  });
}

export function useUpdateInvoiceSettings() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: UpdateSettingsRequest) =>
      unwrap(client.PATCH("/api/v1/organisation/settings", { body })),
    onSuccess: (data) => {
      queryClient.setQueryData(["organisation", "settings"], data);
    },
  });
}
