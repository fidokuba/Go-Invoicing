import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { client } from "../client";
import { unwrapVersioned, type Versioned } from "../unwrap";
import type { components } from "../schema";

type Organisation = components["schemas"]["OrganisationResponse"];
type Settings = components["schemas"]["SettingsResponse"];
type UpdateOrganisationRequest = components["schemas"]["UpdateOrganisationRequest"];
type UpdateSettingsRequest = components["schemas"]["UpdateSettingsRequest"];

/**
 * Organisation and invoice settings are protected by optimistic
 * concurrency (Milestone 13 Part 2): GET returns an ETag, and PATCH must
 * send it back as If-Match (412 if someone else saved in between). The
 * query cache therefore holds `{ data, etag }`; the plain hooks select
 * just `data` for read-only consumers, while the edit forms use the
 * versioned hooks and pin the ETag they loaded with.
 */
const organisationKey = ["organisation"] as const;
const settingsKey = ["organisation", "settings"] as const;

const fetchOrganisation = () => unwrapVersioned(client.GET("/api/v1/organisation", {}));
const fetchSettings = () => unwrapVersioned(client.GET("/api/v1/organisation/settings", {}));
const selectData = <T,>(versioned: Versioned<T>) => versioned.data;

export function useOrganisation() {
  return useQuery({ queryKey: organisationKey, queryFn: fetchOrganisation, select: selectData<Organisation> });
}

export function useVersionedOrganisation() {
  return useQuery({ queryKey: organisationKey, queryFn: fetchOrganisation });
}

export interface VersionedUpdate<B> {
  body: B;
  /** The ETag of the version this edit was based on — sent as If-Match. */
  etag: string;
}

export function useUpdateOrganisation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ body, etag }: VersionedUpdate<UpdateOrganisationRequest>) =>
      unwrapVersioned(client.PATCH("/api/v1/organisation", { params: { header: { "If-Match": etag } }, body })),
    onSuccess: (versioned) => {
      queryClient.setQueryData(organisationKey, versioned);
    },
  });
}

export function useInvoiceSettings() {
  return useQuery({ queryKey: settingsKey, queryFn: fetchSettings, select: selectData<Settings> });
}

export function useVersionedInvoiceSettings() {
  return useQuery({ queryKey: settingsKey, queryFn: fetchSettings });
}

export function useUpdateInvoiceSettings() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ body, etag }: VersionedUpdate<UpdateSettingsRequest>) =>
      unwrapVersioned(
        client.PATCH("/api/v1/organisation/settings", { params: { header: { "If-Match": etag } }, body }),
      ),
    onSuccess: (versioned) => {
      queryClient.setQueryData(settingsKey, versioned);
    },
  });
}
