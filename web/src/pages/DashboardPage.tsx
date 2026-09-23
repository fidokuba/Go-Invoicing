import { Link } from "react-router-dom";
import { useQueries } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { client } from "@/api/client";
import { unwrap } from "@/api/unwrap";
import { useInvoices } from "@/api/queries/invoices";
import { PageHeader } from "@/components/layout/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { QueryBoundary } from "@/components/ui/query-boundary";
import { EmptyState } from "@/components/ui/empty-state";
import { TableContainer, THead, TBody, Tr, Th, Td } from "@/components/ui/table";
import { InvoiceStatusBadge, type InvoiceStatus } from "@/components/invoice/InvoiceStatusBadge";
import { formatMoney } from "@/lib/money";
import { formatDateOnly } from "@/lib/date";

// Milestone 12 section 7/45: every count below comes from a list
// request's own `pagination.total` with limit=1 — a cheap, O(1)-ish
// count query, never a full row fetch just to derive a number. There is
// no aggregate/analytics endpoint in this API (and this milestone
// deliberately doesn't add one — see section 7), so a metric this can't
// get this way (e.g. a sum of outstanding amounts across every unpaid
// invoice, which would require paging through the entire result set) is
// simply not shown here.
const STATUS_COUNT_CARDS: { status: InvoiceStatus; label: string }[] = [
  { status: "draft", label: "Draft" },
  { status: "sent", label: "Sent" },
  { status: "overdue", label: "Overdue" },
  { status: "paid", label: "Paid" },
];

function useStatusCounts() {
  return useQueries({
    queries: STATUS_COUNT_CARDS.map(({ status }) => ({
      queryKey: ["invoices", "count", status],
      queryFn: () =>
        unwrap(client.GET("/api/v1/invoices", { params: { query: { status, limit: 1 } } })),
    })),
  });
}

export function DashboardPage() {
  const counts = useStatusCounts();
  const recentInvoices = useInvoices({ limit: 5, sort: "issueDate", order: "desc" });

  return (
    <div>
      <PageHeader
        title="Dashboard"
        actions={
          <Button asChild>
            <Link to="/invoices/new" className="flex items-center gap-2">
              <Plus className="h-4 w-4" aria-hidden="true" />
              Create invoice
            </Link>
          </Button>
        }
      />

      <div className="mb-6 grid grid-cols-2 gap-4 sm:grid-cols-4">
        {STATUS_COUNT_CARDS.map(({ status, label }, index) => {
          const result = counts[index];
          return (
            <Card key={status}>
              <CardContent>
                <p className="text-sm text-slate-500">{label}</p>
                <p className="mt-1 text-2xl font-semibold text-slate-900">
                  {result.isLoading ? "—" : result.isError ? "?" : result.data?.pagination.total}
                </p>
              </CardContent>
            </Card>
          );
        })}
      </div>

      <Card>
        <div className="border-b border-slate-200 px-5 py-4">
          <h2 className="text-sm font-semibold text-slate-900">Recent invoices</h2>
        </div>
        <QueryBoundary query={recentInvoices}>
          {(page) =>
            page.items.length === 0 ? (
              <div className="p-5">
                <EmptyState
                  title="No invoices yet"
                  description="Create your first invoice to get started."
                  action={
                    <Button asChild>
                      <Link to="/invoices/new">Create invoice</Link>
                    </Button>
                  }
                />
              </div>
            ) : (
              <TableContainer>
                <THead>
                  <Tr>
                    <Th>Invoice</Th>
                    <Th>Issue date</Th>
                    <Th>Due date</Th>
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
                      <Td>{formatDateOnly(invoice.dueDate)}</Td>
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
    </div>
  );
}
