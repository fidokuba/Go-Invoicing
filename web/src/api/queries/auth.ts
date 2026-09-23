import { useMutation } from "@tanstack/react-query";
import { client } from "../client";
import { unwrap } from "../unwrap";
import { authStore } from "@/lib/authStore";

export function useLogin() {
  return useMutation({
    mutationFn: async (input: { email: string; password: string }) => {
      const result = await unwrap(client.POST("/api/v1/auth/login", { body: input }));
      authStore.setSession(result.token, result.user);
      return result;
    },
  });
}

export function useRegister() {
  return useMutation({
    mutationFn: (input: {
      organisationName: string;
      name: string;
      email: string;
      password: string;
    }) =>
      unwrap(
        client.POST("/api/v1/register", {
          body: {
            organisation: { name: input.organisationName },
            user: { name: input.name, email: input.email, password: input.password },
          },
        }),
      ),
  });
}

export function useLogout() {
  return useMutation({
    mutationFn: async () => {
      // Best-effort: this call revokes the server-side session, but the
      // local session is always cleared regardless of whether it
      // succeeds (e.g. it may already be expired/revoked) — logging out
      // locally must never depend on the server call succeeding.
      try {
        await unwrap(client.POST("/api/v1/auth/logout", {}));
      } finally {
        authStore.clear();
      }
    },
  });
}
