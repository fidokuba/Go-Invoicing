import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { client } from "../client";
import { unwrap } from "../unwrap";
import type { components } from "../schema";

type CreateUserRequest = components["schemas"]["CreateUserRequest"];

export interface UsersListParams {
  limit?: number;
  offset?: number;
  role?: "admin" | "manager" | "user";
  active?: boolean;
}

export function useUsers(params: UsersListParams) {
  return useQuery({
    queryKey: ["users", params],
    queryFn: () => unwrap(client.GET("/api/v1/users", { params: { query: params } })),
  });
}

export function useUser(id: string | undefined) {
  return useQuery({
    queryKey: ["users", id],
    queryFn: () => unwrap(client.GET("/api/v1/users/{id}", { params: { path: { id: id! } } })),
    enabled: Boolean(id),
  });
}

export function useCreateUser() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateUserRequest) => unwrap(client.POST("/api/v1/users", { body })),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["users"] });
    },
  });
}
