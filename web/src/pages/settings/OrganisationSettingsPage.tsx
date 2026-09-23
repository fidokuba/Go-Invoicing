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

function OrganisationForm({ organisation, canEdit }: { organisation: Organisation; canEdit: boolean }) {
  const [name, setName] = useState(organisation.name);
  const [email, setEmail] = useState(organisation.email ?? "");
  const [phone, setPhone] = useState(organisation.phone ?? "");
  const [website, setWebsite] = useState(organisation.website ?? "");
  const [taxId, setTaxId] = useState(organisation.taxId ?? "");
  const [saved, setSaved] = useState(false);
  const update = useUpdateOrganisation();

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (update.isPending) return;
    setSaved(false);
    update.mutate(
      { name, email, phone, website, taxId },
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
              Organisation details updated.
            </Alert>
          </div>
        )}
        <form onSubmit={handleSubmit} noValidate className="max-w-lg space-y-4">
          <div>
            <Label htmlFor="org-name">Name</Label>
            <Input id="org-name" required disabled={!canEdit} value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div>
            <Label htmlFor="org-email">Email</Label>
            <Input id="org-email" type="email" disabled={!canEdit} value={email} onChange={(e) => setEmail(e.target.value)} />
          </div>
          <div>
            <Label htmlFor="org-phone">Phone</Label>
            <Input id="org-phone" type="tel" disabled={!canEdit} value={phone} onChange={(e) => setPhone(e.target.value)} />
          </div>
          <div>
            <Label htmlFor="org-website">Website</Label>
            <Input id="org-website" disabled={!canEdit} value={website} onChange={(e) => setWebsite(e.target.value)} />
          </div>
          <div>
            <Label htmlFor="org-taxId">Tax ID</Label>
            <Input id="org-taxId" disabled={!canEdit} value={taxId} onChange={(e) => setTaxId(e.target.value)} />
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

export function OrganisationSettingsPage() {
  const query = useOrganisation();
  const { user } = useAuth();
  const canEdit = hasRole(user, "admin");

  return (
    <QueryBoundary query={query}>
      {(organisation) => <OrganisationForm organisation={organisation} canEdit={canEdit} />}
    </QueryBoundary>
  );
}
