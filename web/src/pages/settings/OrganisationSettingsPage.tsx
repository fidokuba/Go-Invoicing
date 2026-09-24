import { useState, type FormEvent } from "react";
import { useVersionedOrganisation, useUpdateOrganisation } from "@/api/queries/organisation";
import { useAuth, hasRole } from "@/lib/useAuth";
import { friendlyMessage, isStaleWriteError } from "@/api/errors";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input, Label, FieldError } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { Alert } from "@/components/ui/alert";
import { QueryBoundary } from "@/components/ui/query-boundary";
import type { components } from "@/api/schema";

type Organisation = components["schemas"]["OrganisationResponse"];

function OrganisationForm({
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
  const [name, setName] = useState(organisation.name);
  const [email, setEmail] = useState(organisation.email ?? "");
  const [phone, setPhone] = useState(organisation.phone ?? "");
  const [website, setWebsite] = useState(organisation.website ?? "");
  const [vatRegistered, setVatRegistered] = useState(organisation.vatRegistered);
  const [taxId, setTaxId] = useState(organisation.taxId ?? "");
  const [taxIdError, setTaxIdError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const update = useUpdateOrganisation();

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (update.isPending) return;
    setSaved(false);
    // A VAT registered business must show its VAT number on invoices; the
    // server enforces this too.
    if (vatRegistered && !taxId.trim()) {
      setTaxIdError("Enter your VAT registration number.");
      return;
    }
    setTaxIdError(null);
    update.mutate(
      // taxId is only sent while registered, so unticking keeps the stored
      // number for when the box is ticked again.
      { body: { name, email, phone, website, vatRegistered, ...(vatRegistered ? { taxId } : {}) }, etag },
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
          <Checkbox
            id="org-vatRegistered"
            label="VAT Registered Company?"
            disabled={!canEdit}
            checked={vatRegistered}
            onChange={(e) => setVatRegistered(e.target.checked)}
          />
          {vatRegistered && (
            <div>
              <Label htmlFor="org-taxId">VAT Registration Number</Label>
              <Input
                id="org-taxId"
                disabled={!canEdit}
                value={taxId}
                aria-invalid={taxIdError !== null}
                onChange={(e) => setTaxId(e.target.value)}
              />
              <FieldError>{taxIdError}</FieldError>
            </div>
          )}
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
        <OrganisationForm
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
