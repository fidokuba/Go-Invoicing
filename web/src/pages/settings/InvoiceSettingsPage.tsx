import { useState, type FormEvent } from "react";
import { X } from "lucide-react";
import { useVersionedInvoiceSettings, useUpdateInvoiceSettings } from "@/api/queries/organisation";
import { useAuth, hasRole } from "@/lib/useAuth";
import { friendlyMessage, isStaleWriteError } from "@/api/errors";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input, Label, FieldError } from "@/components/ui/input";
import { Alert } from "@/components/ui/alert";
import { QueryBoundary } from "@/components/ui/query-boundary";
import type { components } from "@/api/schema";
import { quantityUnitError } from "@/lib/quantityUnits";

type Settings = components["schemas"]["SettingsResponse"];

/** Edits the quantity units list; changes are saved with the rest of the
 * form. */
function QuantityUnitsEditor({
  units,
  onChange,
  disabled,
}: {
  units: string[];
  onChange: (units: string[]) => void;
  disabled: boolean;
}) {
  const [newUnit, setNewUnit] = useState("");
  const [error, setError] = useState<string | null>(null);

  function add() {
    const problem = quantityUnitError(units, newUnit);
    setError(problem);
    if (problem) return;
    onChange([...units, newUnit.trim()]);
    setNewUnit("");
  }

  return (
    <div>
      <Label htmlFor="newQuantityUnit">Quantity units</Label>
      <p className="mb-2 text-xs text-slate-500">
        The choices for a product's or invoice line's Qty unit. Removing a unit doesn't change products or invoices
        that already use it.
      </p>
      {units.length === 0 ? (
        <p className="mb-2 text-sm text-slate-500">No units yet.</p>
      ) : (
        <ul className="mb-2 flex flex-wrap gap-2" aria-label="Current quantity units">
          {units.map((unit) => (
            <li
              key={unit}
              className="flex items-center gap-1 rounded-full border border-slate-200 bg-slate-50 py-0.5 pl-3 pr-1 text-sm text-slate-800"
            >
              {unit}
              {!disabled && (
                <button
                  type="button"
                  className="rounded-full p-0.5 text-slate-500 hover:bg-slate-200 hover:text-slate-800"
                  aria-label={`Remove ${unit}`}
                  onClick={() => onChange(units.filter((u) => u !== unit))}
                >
                  <X className="h-3.5 w-3.5" aria-hidden="true" />
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {!disabled && (
        <>
          <div className="flex gap-2">
            <Input
              id="newQuantityUnit"
              placeholder="e.g. Pallets"
              value={newUnit}
              aria-invalid={error !== null}
              onChange={(e) => setNewUnit(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  add();
                }
              }}
            />
            <Button type="button" variant="secondary" onClick={add}>
              Add
            </Button>
          </div>
          <FieldError>{error}</FieldError>
        </>
      )}
    </div>
  );
}

function InvoiceSettingsForm({
  settings,
  etag: loadedETag,
  canEdit,
  onReload,
}: {
  settings: Settings;
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
  const [currency, setCurrency] = useState(settings.currency);
  const [paymentTerms, setPaymentTerms] = useState(String(settings.paymentTerms));
  const [invoicePrefix, setInvoicePrefix] = useState(settings.invoicePrefix);
  const [quantityUnits, setQuantityUnits] = useState(settings.quantityUnits);
  const [currencyError, setCurrencyError] = useState<string | null>(null);
  const [termsError, setTermsError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const update = useUpdateInvoiceSettings();

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (update.isPending) return;
    setSaved(false);

    let ok = true;
    setCurrencyError(null);
    setTermsError(null);
    if (!/^[A-Z]{3}$/i.test(currency.trim())) {
      setCurrencyError("Enter a 3-letter currency code, e.g. GBP.");
      ok = false;
    }
    const terms = Number(paymentTerms);
    if (!Number.isInteger(terms) || terms < 0) {
      setTermsError("Enter a whole number of days, 0 or more.");
      ok = false;
    }
    if (!ok) return;

    update.mutate(
      { body: { currency: currency.trim().toUpperCase(), paymentTerms: terms, invoicePrefix, quantityUnits }, etag },
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
            <Alert tone="info">Only an administrator can change invoice settings.</Alert>
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
              Invoice settings updated. This does not change any invoice already sent.
            </Alert>
          </div>
        )}
        <form onSubmit={handleSubmit} noValidate className="max-w-md space-y-4">
          <div>
            <Label htmlFor="currency">Currency</Label>
            <Input
              id="currency"
              maxLength={3}
              disabled={!canEdit}
              value={currency}
              aria-invalid={currencyError !== null}
              onChange={(e) => setCurrency(e.target.value.toUpperCase())}
            />
            <FieldError>{currencyError}</FieldError>
          </div>
          <div>
            <Label htmlFor="paymentTerms">Payment terms (days)</Label>
            <Input
              id="paymentTerms"
              inputMode="numeric"
              disabled={!canEdit}
              value={paymentTerms}
              aria-invalid={termsError !== null}
              onChange={(e) => setPaymentTerms(e.target.value)}
            />
            <FieldError>{termsError}</FieldError>
            <p className="mt-1 text-xs text-slate-500">0 means invoices are due immediately.</p>
          </div>
          <div>
            <Label htmlFor="invoicePrefix">Invoice number prefix</Label>
            <Input
              id="invoicePrefix"
              disabled={!canEdit}
              value={invoicePrefix}
              onChange={(e) => setInvoicePrefix(e.target.value)}
            />
            <p className="mt-1 text-xs text-slate-500">
              e.g. "{invoicePrefix || "INV-"}" + "1" = "{(invoicePrefix || "INV-") + "1"}"
            </p>
          </div>
          <QuantityUnitsEditor units={quantityUnits} onChange={setQuantityUnits} disabled={!canEdit} />
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

export function InvoiceSettingsPage() {
  const query = useVersionedInvoiceSettings();
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
        <InvoiceSettingsForm
          key={formKey}
          settings={versioned.data}
          etag={versioned.etag}
          canEdit={canEdit}
          onReload={reload}
        />
      )}
    </QueryBoundary>
  );
}
