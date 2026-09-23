import { useState, type FormEvent } from "react";
import { useOrganisation, useUpdateOrganisation } from "@/api/queries/organisation";
import { useAuth, hasRole } from "@/lib/useAuth";
import { friendlyMessage } from "@/api/errors";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input, Label } from "@/components/ui/input";
import { Alert } from "@/components/ui/alert";
import { QueryBoundary } from "@/components/ui/query-boundary";
import type { components } from "@/api/schema";

type Organisation = components["schemas"]["OrganisationResponse"];

// The organisation's own billing/registered address lives on the same
// resource as OrganisationSettingsPage (GET/PATCH /organisation) — the
// API has no separate endpoint for it. This page presents just the
// address fields as their own form for clarity, submitting through the
// identical PATCH /organisation.
function BillingAddressForm({ organisation, canEdit }: { organisation: Organisation; canEdit: boolean }) {
  const [address, setAddress] = useState(organisation.address ?? "");
  const [city, setCity] = useState(organisation.city ?? "");
  const [state, setState] = useState(organisation.state ?? "");
  const [postalCode, setPostalCode] = useState(organisation.postalCode ?? "");
  const [country, setCountry] = useState(organisation.country ?? "");
  const [saved, setSaved] = useState(false);
  const update = useUpdateOrganisation();

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (update.isPending) return;
    setSaved(false);
    update.mutate(
      { address, city, state, postalCode, country },
      { onSuccess: () => setSaved(true) },
    );
  }

  return (
    <Card>
      <CardContent>
        {!canEdit && (
          <div className="mb-4">
            <Alert tone="info">Only an administrator can change these details.</Alert>
          </div>
        )}
        {update.isError && (
          <div className="mb-4">
            <Alert>{friendlyMessage(update.error)}</Alert>
          </div>
        )}
        {saved && !update.isError && (
          <div className="mb-4">
            <Alert tone="info" title="Saved">
              Billing address updated.
            </Alert>
          </div>
        )}
        <form onSubmit={handleSubmit} noValidate className="max-w-lg space-y-4">
          <div>
            <Label htmlFor="addr-street">Address</Label>
            <Input id="addr-street" disabled={!canEdit} value={address} onChange={(e) => setAddress(e.target.value)} />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <Label htmlFor="addr-city">City</Label>
              <Input id="addr-city" disabled={!canEdit} value={city} onChange={(e) => setCity(e.target.value)} />
            </div>
            <div>
              <Label htmlFor="addr-state">State/region</Label>
              <Input id="addr-state" disabled={!canEdit} value={state} onChange={(e) => setState(e.target.value)} />
            </div>
            <div>
              <Label htmlFor="addr-postalCode">Postal code</Label>
              <Input
                id="addr-postalCode"
                disabled={!canEdit}
                value={postalCode}
                onChange={(e) => setPostalCode(e.target.value)}
              />
            </div>
            <div>
              <Label htmlFor="addr-country">Country</Label>
              <Input id="addr-country" disabled={!canEdit} value={country} onChange={(e) => setCountry(e.target.value)} />
            </div>
          </div>
          {canEdit && (
            <div className="flex justify-end">
              <Button type="submit" disabled={update.isPending}>
                {update.isPending ? "Saving…" : "Save changes"}
              </Button>
            </div>
          )}
        </form>
      </CardContent>
    </Card>
  );
}

export function BillingAddressSettingsPage() {
  const query = useOrganisation();
  const { user } = useAuth();
  const canEdit = hasRole(user, "admin");

  return (
    <QueryBoundary query={query}>
      {(organisation) => <BillingAddressForm organisation={organisation} canEdit={canEdit} />}
    </QueryBoundary>
  );
}
