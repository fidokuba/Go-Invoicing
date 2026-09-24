import { useState, type FormEvent } from "react";
import { useVersionedOrganisation, useUpdateOrganisation } from "@/api/queries/organisation";
import { useAuth, hasRole } from "@/lib/useAuth";
import { friendlyMessage, isStaleWriteError } from "@/api/errors";
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
function BillingAddressForm({
  organisation,
  etag: loadedETag,
  canEdit,
  onReload,
}: {
  organisation: Organisation;
  etag: string;
  canEdit: boolean;
  onReload: () => void;
}) {
  // The version these fields were loaded from (Milestone 13 Part 2) —
  // deliberately pinned here rather than read from the query cache on
  // submit, so a background refetch can never pair a newer ETag with
  // these older field values. Advanced only by this form's own
  // successful save; a 412 is resolved by Reload (a fresh remount).
  const [etag, setEtag] = useState(loadedETag);
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
      { body: { address, city, state, postalCode, country }, etag },
      {
        onSuccess: (versioned) => {
          setEtag(versioned.etag);
          setSaved(true);
        },
      },
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
            {isStaleWriteError(update.error) ? (
              <Alert onRetry={onReload} retryLabel="Reload">
                {friendlyMessage(update.error)}
              </Alert>
            ) : (
              <Alert>{friendlyMessage(update.error)}</Alert>
            )}
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
  const query = useVersionedOrganisation();
  // Bumped by Reload after a 412, remounting the form from the freshly
  // fetched version so its fields and pinned ETag are reset together.
  const [formKey, setFormKey] = useState(0);
  const reload = async () => {
    await query.refetch();
    setFormKey((key) => key + 1);
  };
  const { user } = useAuth();
  const canEdit = hasRole(user, "admin");

  return (
    <QueryBoundary query={query}>
      {(versioned) => (
        <BillingAddressForm
          key={formKey}
          organisation={versioned.data}
          etag={versioned.etag}
          canEdit={canEdit}
          onReload={reload}
        />
      )}
    </QueryBoundary>
  );
}
