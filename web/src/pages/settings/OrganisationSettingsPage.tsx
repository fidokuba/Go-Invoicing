import { useRef, useState, type ChangeEvent, type FormEvent } from "react";
import { ImagePlus, Sparkles, Trash2 } from "lucide-react";
import {
  MAX_LOGO_BYTES,
  useDeleteOrganisationLogo,
  useOrganisationLogoUrl,
  useUploadOrganisationLogo,
  useUpdateOrganisation,
  useVersionedOrganisation,
} from "@/api/queries/organisation";
import { useCreateTestData } from "@/api/queries/testData";
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

const LOGO_TYPES = ["image/png", "image/jpeg", "image/gif", "image/webp"];

function readAsDataUrl(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result as string);
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}

// LogoCard uploads/replaces/removes the organisation's logo, shown by the
// Logo block of custom invoice layouts. Everyone sees the current logo;
// only an admin (the server's own gate too) can change it. Type and size
// are checked here only to fail fast — the server re-checks both.
function LogoCard({ canEdit }: { canEdit: boolean }) {
  const logoUrl = useOrganisationLogoUrl();
  const upload = useUploadOrganisationLogo();
  const remove = useDeleteOrganisationLogo();
  const fileInput = useRef<HTMLInputElement>(null);
  const [fileError, setFileError] = useState<string | null>(null);

  async function handleFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    setFileError(null);
    remove.reset();
    if (!LOGO_TYPES.includes(file.type)) {
      setFileError("Choose a PNG, JPEG, GIF or WebP image.");
      return;
    }
    if (file.size > MAX_LOGO_BYTES) {
      setFileError("The logo must be 1 MB or smaller.");
      return;
    }
    upload.mutate(await readAsDataUrl(file));
  }

  const error = fileError ?? (upload.isError ? friendlyMessage(upload.error) : remove.isError ? friendlyMessage(remove.error) : null);

  return (
    <Card>
      <CardContent>
        <h2 className="text-sm font-medium text-slate-900">Logo</h2>
        <p className="mt-1 text-sm text-slate-500">
          Shown wherever a custom invoice layout includes a Logo block. Invoices you've already sent keep the logo
          they were sent with.
        </p>
        {error && (
          <div className="mt-4">
            <Alert>{error}</Alert>
          </div>
        )}
        <div className="mt-4 flex flex-wrap items-center gap-4">
          <div className="flex h-24 w-48 items-center justify-center rounded-md border border-slate-200 bg-slate-50 p-2">
            {logoUrl ? (
              <img src={logoUrl} alt="Organisation logo" className="max-h-full max-w-full object-contain" />
            ) : (
              <span className="text-sm text-slate-400">No logo</span>
            )}
          </div>
          {canEdit && (
            <div className="flex gap-2">
              <input
                ref={fileInput}
                type="file"
                accept={LOGO_TYPES.join(",")}
                className="hidden"
                aria-label="Logo file"
                onChange={handleFile}
              />
              <Button variant="secondary" disabled={upload.isPending} onClick={() => fileInput.current?.click()}>
                <ImagePlus className="h-4 w-4" aria-hidden="true" />
                {upload.isPending ? "Uploading…" : logoUrl ? "Replace logo" : "Upload logo"}
              </Button>
              {logoUrl && (
                <Button
                  variant="ghost"
                  disabled={remove.isPending}
                  onClick={() => {
                    setFileError(null);
                    upload.reset();
                    remove.mutate();
                  }}
                >
                  <Trash2 className="h-4 w-4" aria-hidden="true" />
                  Remove
                </Button>
              )}
            </div>
          )}
        </div>
        {canEdit && <p className="mt-3 text-xs text-slate-500">PNG, JPEG, GIF or WebP, up to 1 MB.</p>}
      </CardContent>
    </Card>
  );
}

// TestDataCard is the "Create Test Data" action: admin only (same gate
// the server itself enforces — see internal/sampledata.Handler.Create
// — so this is belt-and-braces, not the only thing standing between a
// non-admin and this button). Purely additive: every call creates a new
// batch of fake customers/products/invoices rather than touching
// anything that already exists, so there's nothing destructive here to
// confirm the way Delete elsewhere in Settings does.
function TestDataCard() {
  const createTestData = useCreateTestData();

  return (
    <Card>
      <CardContent>
        <h2 className="text-sm font-medium text-slate-900">Test data</h2>
        <p className="mt-1 text-sm text-slate-500">
          Create 5 fake customers, a handful of fake products, and a varying number of invoices for each
          customer (a mix of draft, sent, and paid) — useful for trying the app out without entering real data
          by hand. Safe to run more than once; each run adds another batch.
        </p>
        {createTestData.isError && (
          <div className="mt-4">
            <Alert>{friendlyMessage(createTestData.error)}</Alert>
          </div>
        )}
        {createTestData.isSuccess && (
          <div className="mt-4">
            <Alert tone="info" title="Test data created">
              {createTestData.data.customersCreated} customers, {createTestData.data.productsCreated} products,{" "}
              {createTestData.data.invoicesCreated} invoices ({createTestData.data.invoicesSent} sent,{" "}
              {createTestData.data.invoicesPaid} paid).
            </Alert>
          </div>
        )}
        <div className="mt-4">
          <Button variant="secondary" disabled={createTestData.isPending} onClick={() => createTestData.mutate()}>
            <Sparkles className="h-4 w-4" aria-hidden="true" />
            {createTestData.isPending ? "Creating…" : "Create Test Data"}
          </Button>
        </div>
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
    <div className="space-y-6">
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
      <LogoCard canEdit={canEdit} />
      {canEdit && <TestDataCard />}
    </div>
  );
}
