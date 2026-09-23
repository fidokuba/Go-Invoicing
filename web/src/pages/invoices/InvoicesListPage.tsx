import { useState } from "react";
import { Link } from "react-router-dom";
import { Plus, Search } from "lucide-react";
import { useInvoices } from "@/api/queries/invoices";
import { useCustomerLookup } from "@/api/queries/customers";
import { useDebouncedValue } from "@/lib/useDebouncedValue";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/button";
import { Input, Select } from "@/components/ui/input";
import { QueryBoundary } from "@/components/ui/query-boundary";
import { EmptyState } from "@/components/ui/empty-state";
import { TableContainer, THead, TBody, Tr, Th, Td } from "@/components/ui/table";
import { Pagination } from "@/components/ui/pagination";
import { InvoiceStatusBadge, type InvoiceStatus } from "@/components/invoice/InvoiceStatusBadge";
import { formatMoney } from "@/lib/money";
import { formatDateOnly } from "@/lib/date";

const PAGE_SIZE = 20;

export function InvoicesListPage() {
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState<InvoiceStatus | "">("");
  const [offset, setOffset] = useState(0);
  const debouncedSearch = useDebouncedValue(search);
  const customerLookup = useCustomerLookup();

  const query = useInvoices({
    limit: PAGE_SIZE,
    offset,
    search: debouncedSearch || undefined,
    status: status || undefined,
  });

  return (
    <div>
      <PageHeader
        title="Invoices"
        actions={
          <Button asChild>
            <Link to="/invoices/new" className="flex items-center gap-2">
              <Plus className="h-4 w-4" aria-hidden="true" />
              Create invoice
            </Link>
          </Button>
        }
      />

      <div className="mb-4 flex flex-wrap gap-3">
        <div className="relative w-64">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" aria-hidden="true" />
          <Input
            type="search"
            placeholder="Search by invoice number…"
            aria-label="Search invoices"
            className="pl-9"
            value={search}
            onChange={(e) => {
              setOffset(0);
              setSearch(e.target.value);
            }}
          />
        </div>
        <Select
          aria-label="Filter by status"
          className="w-40"
          value={status}
          onChange={(e) => {
            setOffset(0);
            setStatus(e.target.value as InvoiceStatus | "");
          }}
        >
          <option value="">All statuses</option>
          <option value="draft">Draft</option>
          <option value="sent">Sent</option>
          <option value="overdue">Overdue</option>
          <option value="paid">Paid</option>
        </Select>
      </div>

      <QueryBoundary query={query}>
        {(page) =>
          page.items.length === 0 ? (
            <EmptyState
              title={debouncedSearch || status ? "No matching invoices" : "No invoices yet"}
              description={
                debouncedSearch || status ? "Try a different search or filter." : "Create your first invoice."
              }
              action={
                !debouncedSearch && !status ? (
                  <Button asChild>
                    <Link to="/invoices/new">Create invoice</Link>
                  </Button>
                ) : undefined
              }
            />
          ) : (
            <TableContainer>
              <THead>
                <Tr>
                  <Th>Invoice</Th>
                  <Th>Customer</Th>
                  <Th>Issue date</Th>
                  <Th>Due date</Th>
                  <Th>Status</Th>
                  <Th className="text-right">Total</Th>
                  <Th className="text-right">Outstanding</Th>
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
                    <Td>{customerLookup.lookup.get(invoice.customerId) ?? "—"}</Td>
                    <Td>{formatDateOnly(invoice.issueDate)}</Td>
                    <Td>{formatDateOnly(invoice.dueDate)}</Td>
                    <Td>
                      <InvoiceStatusBadge status={invoice.status as InvoiceStatus} />
                    </Td>
                    <Td className="text-right">{formatMoney(invoice.total, invoice.currency)}</Td>
                    <Td className="text-right">{formatMoney(invoice.amountOutstanding, invoice.currency)}</Td>
                  </Tr>
                ))}
              </TBody>
              <Pagination pagination={page.pagination} onOffsetChange={setOffset} />
            </TableContainer>
          )
        }
      </QueryBoundary>
    </div>
  );
}
