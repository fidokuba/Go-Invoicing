import { useState } from "react";
import { Link } from "react-router-dom";
import { Plus, Search } from "lucide-react";
import { useCustomers } from "@/api/queries/customers";
import { useDebouncedValue } from "@/lib/useDebouncedValue";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/button";
import { Input, Select } from "@/components/ui/input";
import { QueryBoundary } from "@/components/ui/query-boundary";
import { EmptyState } from "@/components/ui/empty-state";
import { TableContainer, THead, TBody, Tr, Th, Td } from "@/components/ui/table";
import { Pagination } from "@/components/ui/pagination";
import { CustomerStatusBadge, type CustomerStatus } from "@/components/customer/CustomerStatusBadge";
import { formatTimestampDate } from "@/lib/date";

const PAGE_SIZE = 20;

export function CustomersListPage() {
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState<CustomerStatus | "">("");
  const [offset, setOffset] = useState(0);
  const debouncedSearch = useDebouncedValue(search);

  const query = useCustomers({
    limit: PAGE_SIZE,
    offset,
    search: debouncedSearch || undefined,
    status: status || undefined,
  });

  return (
    <div>
      <PageHeader
        title="Customers"
        actions={
          <Button asChild>
            <Link to="/customers/new" className="flex items-center gap-2">
              <Plus className="h-4 w-4" aria-hidden="true" />
              New customer
            </Link>
          </Button>
        }
      />

      <div className="mb-4 flex flex-wrap gap-3">
        <div className="relative w-64">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" aria-hidden="true" />
          <Input
            type="search"
            placeholder="Search customers…"
            aria-label="Search customers"
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
            setStatus(e.target.value as CustomerStatus | "");
          }}
        >
          <option value="">All statuses</option>
          <option value="active">Active</option>
          <option value="inactive">Inactive</option>
          <option value="archived">Archived</option>
        </Select>
      </div>

      <QueryBoundary query={query}>
        {(page) =>
          page.items.length === 0 ? (
            <EmptyState
              title={debouncedSearch || status ? "No matching customers" : "No customers yet"}
              description={
                debouncedSearch || status
                  ? "Try a different search or filter."
                  : "Add your first customer to start creating invoices for them."
              }
              action={
                !debouncedSearch && !status ? (
                  <Button asChild>
                    <Link to="/customers/new">New customer</Link>
                  </Button>
                ) : undefined
              }
            />
          ) : (
            <TableContainer>
              <THead>
                <Tr>
                  <Th>Name</Th>
                  <Th>Company</Th>
                  <Th>Email</Th>
                  <Th>Status</Th>
                  <Th>Created</Th>
                </Tr>
              </THead>
              <TBody>
                {page.items.map((customer) => (
                  <Tr key={customer.id} className="hover:bg-slate-50">
                    <Td>
                      <Link to={`/customers/${customer.id}`} className="font-medium text-brand-600 hover:underline">
                        {customer.name}
                      </Link>
                    </Td>
                    <Td>{customer.companyName ?? "—"}</Td>
                    <Td>{customer.email ?? "—"}</Td>
                    <Td>
                      <CustomerStatusBadge status={customer.status as CustomerStatus} />
                    </Td>
                    <Td>{formatTimestampDate(customer.createdAt)}</Td>
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
