import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { client } from "../client";
import { unwrap, unwrapVersioned, type Versioned } from "../unwrap";
import type { VersionedUpdate } from "./organisation";
import type { components } from "../schema";

type Template = components["schemas"]["TemplateResponse"];
type CreateTemplateRequest = components["schemas"]["CreateTemplateRequest"];
type UpdateTemplateRequest = components["schemas"]["UpdateTemplateRequest"];

/**
 * Invoice templates follow the same optimistic-concurrency shape
 * organisation/settings do (see src/api/queries/organisation.ts's own
 * comment): GET returns an ETag, PATCH must send it back as If-Match.
 * The list itself carries no per-item ETag — a client fetches the
 * single-resource GET (useVersionedTemplate) before editing one, the
 * same GET-then-PATCH flow the settings form already uses.
 */
const templatesKey = ["templates"] as const;
const templateKey = (id: string) => ["templates", id] as const;

export function useTemplates() {
  return useQuery({
    queryKey: templatesKey,
    queryFn: () => unwrap(client.GET("/api/v1/templates", {})),
    select: (list) => list.items,
  });
}

export function useVersionedTemplate(id: string) {
  return useQuery({
    queryKey: templateKey(id),
    queryFn: () => unwrapVersioned(client.GET("/api/v1/templates/{id}", { params: { path: { id } } })),
    enabled: id !== "",
  });
}

export function useCreateTemplate() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateTemplateRequest) => unwrap(client.POST("/api/v1/templates", { body })),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templatesKey });
    },
  });
}

export function useUpdateTemplate(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ body, etag }: VersionedUpdate<UpdateTemplateRequest>) =>
      unwrapVersioned(
        client.PATCH("/api/v1/templates/{id}", { params: { path: { id }, header: { "If-Match": etag } }, body }),
      ),
    onSuccess: (versioned: Versioned<Template>) => {
      queryClient.setQueryData(templateKey(id), versioned);
      queryClient.invalidateQueries({ queryKey: templatesKey });
    },
  });
}

export function useDeleteTemplate() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => unwrap(client.DELETE("/api/v1/templates/{id}", { params: { path: { id } } })),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templatesKey });
    },
  });
}

/** "Use This Layout" — makes id the organisation's default template. */
export function useSetDefaultTemplate() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      unwrap(client.POST("/api/v1/templates/{id}/default", { params: { path: { id } } })),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templatesKey });
    },
  });
}
