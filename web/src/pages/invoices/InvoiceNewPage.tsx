import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { Plus, Trash2 } from "lucide-react";
import { useCreateInvoice } from "@/api/queries/invoices";
import { useCustomers } from "@/api/queries/customers";
import { useProducts } from "@/api/queries/products";
import { useInvoiceSettings, useOrganisation } from "@/api/queries/organisation";
import { friendlyMessage } from "@/api/errors";
import { formatMoney, minorToInputString } from "@/lib/money";
import { todayDateOnly, addDaysDateOnly } from "@/lib/date";
import {
  type LineDraft,
  type LineDraftErrors,
  newLineDraft,
  validateLineDraft,
  hasLineDraftErrors,
  previewLineTotal,
  previewInvoiceTotals,
  toCreateInvoiceLine,
  withoutVat,
} from "@/lib/invoiceLineDraft";
import { PageHeader } from "@/components/layout/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input, Label, Select, Textarea, FieldError } from "@/components/ui/input";
import { Alert } from "@/components/ui/alert";
import { PageLoading } from "@/components/ui/spinner";

export function InvoiceNewPage() {
  const navigate = useNavigate();
  const settings = useInvoiceSettings();
  const organisation = useOrganisation();
  const customers = useCustomers({ limit: 200, sort: "name", order: "asc", status: "active" });
  const products = useProducts({ limit: 200, sort: "name", order: "asc", isActive: true });
  const createInvoice = useCreateInvoice();

  const currency = settings.data?.currency ?? "GBP";
  // A business that isn't VAT registered must not charge VAT: the VAT
  // field and totals are hidden and every line is sent with a 0% rate.
  const vatRegistered = organisation.data?.vatRegistered ?? false;

  const [customerId, setCustomerId] = useState("");
  const [issueDate, setIssueDate] = useState(todayDateOnly());
  const [dueDate, setDueDate] = useState(() =>
    addDaysDateOnly(todayDateOnly(), settings.data?.paymentTerms ?? 30),
  );
  const [notes, setNotes] = useState("");
  const [lines, setLines] = useState<LineDraft[]>([newLineDraft()]);
  const [customerError, setCustomerError] = useState<string | null>(null);
  const [dateError, setDateError] = useState<string | null>(null);
  const [lineErrors, setLineErrors] = useState<Record<string, LineDraftErrors>>({});

  if (settings.isLoading || organisation.isLoading || customers.isLoading || products.isLoading) {
    return <PageLoading label="Loading invoice editor…" />;
  }

  function updateLine(key: string, patch: Partial<LineDraft>) {
    setLines((prev) => prev.map((line) => (line.key === key ? { ...line, ...patch } : line)));
  }

  function applyProduct(key: string, productId: string) {
    const product = products.data?.items.find((p) => p.id === productId);
    updateLine(key, {
      productId,
      // Product pricing is a convenience default only — never enforced;
      // the fields stay freely editable afterward (Milestone 12 section
      // 11's explicit requirement).
      ...(product
        ? { description: product.name, unitPrice: minorToInputString(product.price, currency) }
        : {}),
    });
  }

  function removeLine(key: string) {
    setLines((prev) => (prev.length > 1 ? prev.filter((line) => line.key !== key) : prev));
  }

  const effectiveLines = vatRegistered ? lines : withoutVat(lines);

  function validate(): boolean {
    let ok = true;
    setCustomerError(null);
    setDateError(null);

    if (!customerId) {
      setCustomerError("Select a customer.");
      ok = false;
    }
    if (issueDate && dueDate && dueDate < issueDate) {
      setDateError("Due date must not be before the issue date.");
      ok = false;
    }

    const nextLineErrors: Record<string, LineDraftErrors> = {};
    for (const line of effectiveLines) {
      const errors = validateLineDraft(line, currency);
      if (hasLineDraftErrors(errors)) {
        nextLineErrors[line.key] = errors;
        ok = false;
      }
    }
    setLineErrors(nextLineErrors);

    return ok;
  }

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (createInvoice.isPending) return;
    if (!validate()) return;

    createInvoice.mutate(
      {
        customerId,
        issueDate,
        dueDate,
        notes: notes || undefined,
        lines: effectiveLines.map((line) => toCreateInvoiceLine(line, currency)),
      },
      { onSuccess: (invoice) => navigate(`/invoices/${invoice.id}`) },
    );
  }

  const totals = previewInvoiceTotals(effectiveLines, currency);

  return (
    <div className="max-w-4xl">
      <PageHeader title="Create invoice" description="Saved as a draft until you send it." />

      {createInvoice.isError && (
        <div className="mb-4">
          <Alert>{friendlyMessage(createInvoice.error)}</Alert>
        </div>
      )}

      <form onSubmit={handleSubmit} noValidate className="space-y-6">
        <Card>
          <CardContent>
            <div className="grid gap-4 sm:grid-cols-3">
              <div>
                <Label htmlFor="customer">Customer</Label>
                <Select
                  id="customer"
                  value={customerId}
                  aria-invalid={customerError !== null}
                  onChange={(e) => setCustomerId(e.target.value)}
                >
                  <option value="">Select a customer…</option>
                  {customers.data?.items.map((customer) => (
                    <option key={customer.id} value={customer.id}>
                      {customer.companyName || customer.name}
                    </option>
                  ))}
                </Select>
                <FieldError>{customerError}</FieldError>
              </div>
              <div>
                <Label htmlFor="issueDate">Issue date</Label>
                <Input
                  id="issueDate"
                  type="date"
                  value={issueDate}
                  onChange={(e) => setIssueDate(e.target.value)}
                />
              </div>
              <div>
                <Label htmlFor="dueDate">Due date</Label>
                <Input id="dueDate" type="date" value={dueDate} onChange={(e) => setDueDate(e.target.value)} />
                <FieldError>{dateError}</FieldError>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardContent>
            <div className="space-y-4">
              {effectiveLines.map((line) => {
                const errors = lineErrors[line.key];
                const preview = previewLineTotal(line, currency);
                return (
                  <div key={line.key} className="rounded-md border border-slate-200 p-4">
                    <div className="grid gap-3 sm:grid-cols-12">
                      <div className="sm:col-span-4">
                        <Label htmlFor={`${line.key}-product`}>Product (optional)</Label>
                        <Select
                          id={`${line.key}-product`}
                          value={line.productId}
                          onChange={(e) => applyProduct(line.key, e.target.value)}
                        >
                          <option value="">None — enter manually</option>
                          {products.data?.items.map((product) => (
                            <option key={product.id} value={product.id}>
                              {product.name}
                            </option>
                          ))}
                        </Select>
                      </div>
                      <div className="sm:col-span-8">
                        <Label htmlFor={`${line.key}-description`}>Description</Label>
                        <Input
                          id={`${line.key}-description`}
                          value={line.description}
                          aria-invalid={Boolean(errors?.description)}
                          onChange={(e) => updateLine(line.key, { description: e.target.value })}
                        />
                        <FieldError>{errors?.description}</FieldError>
                      </div>
                      <div className="sm:col-span-2">
                        <Label htmlFor={`${line.key}-quantity`}>Quantity</Label>
                        <Input
                          id={`${line.key}-quantity`}
                          inputMode="decimal"
                          value={line.quantity}
                          aria-invalid={Boolean(errors?.quantity)}
                          onChange={(e) => updateLine(line.key, { quantity: e.target.value })}
                        />
                        <FieldError>{errors?.quantity}</FieldError>
                      </div>
                      <div className="sm:col-span-3">
                        <Label htmlFor={`${line.key}-unitPrice`}>Unit price ({currency})</Label>
                        <Input
                          id={`${line.key}-unitPrice`}
                          inputMode="decimal"
                          value={line.unitPrice}
                          aria-invalid={Boolean(errors?.unitPrice)}
                          onChange={(e) => updateLine(line.key, { unitPrice: e.target.value })}
                        />
                        <FieldError>{errors?.unitPrice}</FieldError>
                      </div>
                      {vatRegistered && (
                        <div className="sm:col-span-2">
                          <Label htmlFor={`${line.key}-vatRate`}>VAT %</Label>
                          <Input
                            id={`${line.key}-vatRate`}
                            inputMode="decimal"
                            value={line.vatRate}
                            aria-invalid={Boolean(errors?.vatRate)}
                            onChange={(e) => updateLine(line.key, { vatRate: e.target.value })}
                          />
                          <FieldError>{errors?.vatRate}</FieldError>
                        </div>
                      )}
                      <div className={`flex items-end justify-between ${vatRegistered ? "sm:col-span-3" : "sm:col-span-5"}`}>
                        <div>
                          <p className="text-xs text-slate-500">Line total</p>
                          <p className="text-sm font-medium text-slate-900">
                            {formatMoney(preview.total, currency)}
                          </p>
                        </div>
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          aria-label="Remove line"
                          disabled={lines.length === 1}
                          onClick={() => removeLine(line.key)}
                        >
                          <Trash2 className="h-4 w-4" aria-hidden="true" />
                        </Button>
                      </div>
                    </div>
                  </div>
                );
              })}

              <Button type="button" variant="secondary" size="sm" onClick={() => setLines((prev) => [...prev, newLineDraft()])}>
                <Plus className="h-4 w-4" aria-hidden="true" />
                Add line
              </Button>
            </div>
          </CardContent>
        </Card>

        <div className="grid gap-6 sm:grid-cols-2">
          <Card>
            <CardContent>
              <Label htmlFor="notes">Notes</Label>
              <Textarea id="notes" rows={4} value={notes} onChange={(e) => setNotes(e.target.value)} />
            </CardContent>
          </Card>

          <Card>
            <CardContent>
              <dl className="space-y-2 text-sm">
                {vatRegistered && (
                  <>
                    <div className="flex justify-between">
                      <dt className="text-slate-500">Subtotal</dt>
                      <dd className="text-slate-900">{formatMoney(totals.subtotal, currency)}</dd>
                    </div>
                    <div className="flex justify-between">
                      <dt className="text-slate-500">VAT</dt>
                      <dd className="text-slate-900">{formatMoney(totals.vatTotal, currency)}</dd>
                    </div>
                  </>
                )}
                <div
                  className={`flex justify-between text-base font-semibold ${vatRegistered ? "border-t border-slate-200 pt-2" : ""}`}
                >
                  <dt>Total</dt>
                  <dd>{formatMoney(totals.total, currency)}</dd>
                </div>
              </dl>
              <p className="mt-3 text-xs text-slate-500">
                Preview only — the server computes and stores the authoritative totals.
              </p>
            </CardContent>
          </Card>
        </div>

        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={() => navigate(-1)}>
            Cancel
          </Button>
          <Button type="submit" disabled={createInvoice.isPending}>
            {createInvoice.isPending ? "Creating…" : "Create draft invoice"}
          </Button>
        </div>
      </form>
    </div>
  );
}
