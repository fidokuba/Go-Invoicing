import { useState, type FormEvent } from "react";
import { Link, useParams } from "react-router-dom";
import { useCustomer, useCustomerBillingAddress, useUpsertCustomerBillingAddress } from "@/api/queries/customers";
import { useInvoices } from "@/api/queries/invoices";
import { friendlyMessage } from "@/api/errors";
import { PageHeader } from "@/components/layout/PageHeader";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input, Label } from "@/components/ui/input";
import { Alert } from "@/components/ui/alert";
import { QueryBoundary } from "@/components/ui/query-boundary";
import { EmptyState } from "@/components/ui/empty-state";
import { TableContainer, THead, TBody, Tr, Th, Td } from "@/components/ui/table";
import { CustomerStatusBadge, type CustomerStatus } from "@/components/customer/CustomerStatusBadge";
import { InvoiceStatusBadge, type InvoiceStatus } from "@/components/invoice/InvoiceStatusBadge";
import { formatMoney } from "@/lib/money";
import { formatDateOnly } from "@/lib/date";

function BillingAddressCard({ customerId }: { customerId: string }) {
  const addressQuery = useCustomerBillingAddress(customerId);
  const upsert = useUpsertCustomerBillingAddress(customerId);
  const [editing, setEditing] = useState(false);
  const [street, setStreet] = useState("");
  const [city, setCity] = useState("");
  const [state, setState] = useState("");
  const [postalCode, setPostalCode] = useState("");
  const [country, setCountry] = useState("");

  function startEditing(existing: { street: string; city: string; state?: string; postalCode: string; country: string } | null) {
    setStreet(existing?.street ?? "");
    setCity(existing?.city ?? "");
    setState(existing?.state ?? "");
    setPostalCode(existing?.postalCode ?? "");
    setCountry(existing?.country ?? "");
    setEditing(true);
  }

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (upsert.isPending) return;
    upsert.mutate(
      { street, city, state: state || undefined, postalCode, country },
      { onSuccess: () => setEditing(false) },
    );
  }

  return (
    <Card>
      <CardHeader className="flex items-center justify-between">
        <CardTitle>Billing address</CardTitle>
      </CardHeader>
      <CardContent>
        <QueryBoundary query={addressQuery}>
          {(address) => {
            if (editing) {
              return (
                <form onSubmit={handleSubmit} noValidate className="space-y-3">
                  {upsert.isError && <Alert>{friendlyMessage(upsert.error)}</Alert>}
                  <div>
                    <Label htmlFor="street">Street</Label>
                    <Input id="street" required value={street} onChange={(e) => setStreet(e.target.value)} />
                  </div>
                  <div className="grid grid-cols-2 gap-3">
                    <div>
                      <Label htmlFor="city">City</Label>
                      <Input id="city" required value={city} onChange={(e) => setCity(e.target.value)} />
                    </div>
                    <div>
                      <Label htmlFor="state">State/region</Label>
                      <Input id="state" value={state} onChange={(e) => setState(e.target.value)} />
                    </div>
                    <div>
                      <Label htmlFor="postalCode">Postal code</Label>
                      <Input
                        id="postalCode"
                        required
                        value={postalCode}
                        onChange={(e) => setPostalCode(e.target.value)}
                      />
                    </div>
                    <div>
                      <Label htmlFor="country">Country</Label>
                      <Input id="country" required value={country} onChange={(e) => setCountry(e.target.value)} />
                    </div>
                  </div>
                  <div className="flex justify-end gap-2 pt-1">
                    <Button type="button" variant="secondary" onClick={() => setEditing(false)}>
                      Cancel
                    </Button>
                    <Button type="submit" disabled={upsert.isPending}>
                      {upsert.isPending ? "Saving…" : "Save address"}
                    </Button>
                  </div>
                </form>
              );
            }

            if (!address) {
              return (
                <EmptyState
                  title="No billing address set"
                  description="Add a billing address to use on this customer's invoices."
                  action={<Button onClick={() => startEditing(null)}>Add address</Button>}
                />
              );
            }

            return (
              <div className="flex items-start justify-between">
                <address className="not-italic text-sm text-slate-700">
                  <p>{address.street}</p>
                  <p>
                    {address.city}
                    {address.state ? `, ${address.state}` : ""} {address.postalCode}
                  </p>
                  <p>{address.country}</p>
                </address>
                <Button variant="secondary" size="sm" onClick={() => startEditing(address)}>
                  Edit
                </Button>
              </div>
            );
          }}
        </QueryBoundary>
      </CardContent>
    </Card>
  );
}

function CustomerInvoicesCard({ customerId }: { customerId: string }) {
  const query = useInvoices({ customerId, limit: 10, sort: "issueDate", order: "desc" });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Invoices</CardTitle>
      </CardHeader>
      <QueryBoundary query={query}>
        {(page) =>
          page.items.length === 0 ? (
            <div className="p-5">
              <EmptyState title="No invoices for this customer yet" />
            </div>
          ) : (
            <TableContainer>
              <THead>
                <Tr>
                  <Th>Invoice</Th>
                  <Th>Issue date</Th>
                  <Th>Status</Th>
                  <Th className="text-right">Total</Th>
                </Tr>
              </THead>
              <TBody>
                {page.items.map((invoice) => (
                  <Tr key={invoice.id} className="hover:bg-slate-50">
                    <Td>
                      <Link to={`/invoices/${invoice.id}`} className="font-medium text-brand-600 hover:underline">
                        {invoice.invoiceNumber}
                      </Link>
                    </Td>
                    <Td>{formatDateOnly(invoice.issueDate)}</Td>
                    <Td>
                      <InvoiceStatusBadge status={invoice.status as InvoiceStatus} />
                    </Td>
                    <Td className="text-right">{formatMoney(invoice.total, invoice.currency)}</Td>
                  </Tr>
                ))}
              </TBody>
            </TableContainer>
          )
        }
      </QueryBoundary>
    </Card>
  );
}

export function CustomerDetailPage() {
  const { id } = useParams<{ id: string }>();
  const query = useCustomer(id);

  return (
    <QueryBoundary query={query}>
      {(customer) => (
        <div>
          <PageHeader
            title={customer.name}
            description={customer.companyName}
            actions={<CustomerStatusBadge status={customer.status as CustomerStatus} />}
          />

          <div className="grid gap-6 lg:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle>Details</CardTitle>
              </CardHeader>
              <CardContent>
                <dl className="space-y-2 text-sm">
                  <div className="flex justify-between gap-4">
                    <dt className="text-slate-500">Email</dt>
                    <dd className="text-slate-900">{customer.email ?? "—"}</dd>
                  </div>
                  <div className="flex justify-between gap-4">
                    <dt className="text-slate-500">Phone</dt>
                    <dd className="text-slate-900">{customer.phone ?? "—"}</dd>
                  </div>
                  <div className="flex justify-between gap-4">
                    <dt className="text-slate-500">Tax ID</dt>
                    <dd className="text-slate-900">{customer.taxId ?? "—"}</dd>
                  </div>
                </dl>
              </CardContent>
            </Card>

            <BillingAddressCard customerId={customer.id} />
          </div>

          <div className="mt-6">
            <CustomerInvoicesCard customerId={customer.id} />
          </div>
        </div>
      )}
    </QueryBoundary>
  );
}
