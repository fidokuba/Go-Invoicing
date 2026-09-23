import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useRegister } from "@/api/queries/auth";
import { friendlyMessage } from "@/api/errors";
import { Button } from "@/components/ui/button";
import { Input, Label, FieldError } from "@/components/ui/input";
import { Alert } from "@/components/ui/alert";

const MIN_PASSWORD_LENGTH = 12;

export function RegisterPage() {
  const [organisationName, setOrganisationName] = useState("");
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [touched, setTouched] = useState(false);
  const register = useRegister();
  const navigate = useNavigate();

  const passwordTooShort = password.length > 0 && password.length < MIN_PASSWORD_LENGTH;

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setTouched(true);
    if (register.isPending || passwordTooShort) return;

    register.mutate(
      { organisationName, name, email, password },
      { onSuccess: () => navigate("/login?registered=1", { replace: true }) },
    );
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-50 px-4 py-12">
      <div className="w-full max-w-sm">
        <h1 className="mb-1 text-center text-lg font-semibold text-slate-900">Go Invoicing</h1>
        <p className="mb-6 text-center text-sm text-slate-500">Create your organisation's account</p>

        <div className="rounded-lg border border-slate-200 bg-white p-6 shadow-sm">
          {register.isError && (
            <div className="mb-4">
              <Alert>{friendlyMessage(register.error)}</Alert>
            </div>
          )}

          <form onSubmit={handleSubmit} noValidate>
            <div className="mb-4">
              <Label htmlFor="organisationName">Organisation name</Label>
              <Input
                id="organisationName"
                required
                value={organisationName}
                onChange={(e) => setOrganisationName(e.target.value)}
              />
            </div>
            <div className="mb-4">
              <Label htmlFor="name">Your name</Label>
              <Input id="name" required value={name} onChange={(e) => setName(e.target.value)} />
            </div>
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
              <Input
                id="password"
                type="password"
                autoComplete="new-password"
                required
                minLength={MIN_PASSWORD_LENGTH}
                aria-invalid={touched && passwordTooShort}
                aria-describedby="password-help"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
              {touched && passwordTooShort ? (
                <FieldError>{`Password must be at least ${MIN_PASSWORD_LENGTH} characters.`}</FieldError>
              ) : (
                <p id="password-help" className="mt-1 text-xs text-slate-500">
                  At least {MIN_PASSWORD_LENGTH} characters.
                </p>
              )}
            </div>
            <Button type="submit" className="w-full" disabled={register.isPending}>
              {register.isPending ? "Creating account…" : "Create account"}
            </Button>
          </form>
        </div>

        <p className="mt-4 text-center text-sm text-slate-500">
          Already have an account?{" "}
          <Link to="/login" className="font-medium text-brand-600 hover:underline">
            Sign in
          </Link>
        </p>
      </div>
    </div>
  );
}
