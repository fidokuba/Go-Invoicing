import createClient, { type Middleware } from "openapi-fetch";
import type { paths } from "./schema";
import { authStore } from "@/lib/authStore";

// baseUrl is "" (relative), not an absolute host: in development, Vite's
// own dev-server proxy (see vite.config.ts's server.proxy) forwards
// /api/* and /health* to the Go API on a different port, so the browser
// only ever talks to one origin and no CORS configuration is needed
// anywhere. In production the frontend is served by the same Go binary
// as the API (internal/webui), so it's the same origin there too — see
// the README's frontend/production sections.
export const client = createClient<paths>({ baseUrl: "" });

// authMiddleware attaches the current bearer token (if any) to every
// request, and reacts to a 401 by clearing the local session — see
// authStore's own comment for why this lives in a plain store rather
// than a React context. It never redirects itself: ProtectedRoute reacts
// to the resulting "no token" state on its own next render, which is
// what actually sends an authenticated-route visitor back to /login (see
// that component for why this can't loop).
//
// /api/v1/auth/login is deliberately excluded from the clear-on-401
// step: a wrong-password login attempt already returns 401 by design
// (see api/openapi.yaml) and must NOT be treated as "an existing session
// just expired" — there is no session to clear, and doing so would be
// meaningless busywork on every failed login attempt, not a bug, but
// worth being explicit about.
const authMiddleware: Middleware = {
  onRequest({ request }) {
    const { token } = authStore.getState();
    if (token) {
      request.headers.set("Authorization", `Bearer ${token}`);
    }
    return request;
  },
  onResponse({ request, response }) {
    if (response.status === 401 && !request.url.endsWith("/api/v1/auth/login")) {
      authStore.clear();
    }
    return response;
  },
};

client.use(authMiddleware);
