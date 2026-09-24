import { useState, type FormEvent } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { useLogin } from "@/api/queries/auth";
import { loginErrorMessage } from "@/api/errors";
import { Button } from "@/components/ui/button";
import { Input, Label } from "@/components/ui/input";
import { PasswordInput } from "@/components/ui/password-input";
import { Alert } from "@/components/ui/alert";

interface LocationState {
  from?: { pathname: string };
}

export function LoginPage() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const login = useLogin();
  const navigate = useNavigate();
  const location = useLocation();

  const registered = new URLSearchParams(location.search).get("registered") === "1";

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (login.isPending) return; // duplicate-submit protection

    login.mutate(
      { email, password },
      {
        onSuccess: () => {
          const state = location.state as LocationState | null;
          navigate(state?.from?.pathname ?? "/dashboard", { replace: true });
        },
      },
    );
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-50 px-4">
      <div className="w-full max-w-sm">
        <h1 className="mb-1 text-center text-lg font-semibold text-slate-900">Go Invoicing</h1>
        <p className="mb-6 text-center text-sm text-slate-500">Sign in to your account</p>

        <div className="rounded-lg border border-slate-200 bg-white p-6 shadow-sm">
          {registered && (
            <div className="mb-4">
              <Alert tone="info" title="Account created">
                You can now sign in with your new account.
              </Alert>
            </div>
          )}

          {login.isError && (
            <div className="mb-4">
              <Alert>{loginErrorMessage(login.error)}</Alert>
            </div>
          )}

          <form onSubmit={handleSubmit} noValidate>
            <div className="mb-4">
              <Label htmlFor="email">Email</Label>
              <Input
                id="email"
                type="email"
                autoComplete="email"
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            </div>
            <div className="mb-6">
              <Label htmlFor="password">Password</Label>
              <PasswordInput
                id="password"
                autoComplete="current-password"
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </div>
            <Button type="submit" className="w-full" disabled={login.isPending}>
              {login.isPending ? "Signing in…" : "Sign in"}
            </Button>
          </form>
        </div>

        <p className="mt-4 text-center text-sm text-slate-500">
          New to Go Invoicing?{" "}
          <Link to="/register" className="font-medium text-brand-600 hover:underline">
            Create an account
          </Link>
        </p>
      </div>
    </div>
  );
}
