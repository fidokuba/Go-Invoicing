import { useState } from "react";
import { Link } from "react-router-dom";
import { Plus, Search } from "lucide-react";
import { useCustomers, type CustomerStatusValue } from "@/api/queries/customers";
import { useDebouncedValue } from "@/lib/useDebouncedValue";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input, Label } from "@/components/ui/input";
import { QueryBoundary } from "@/components/ui/query-boundary";
import { EmptyState } from "@/components/ui/empty-state";
import { TableContainer, THead, TBody, Tr, Th, Td } from "@/components/ui/table";
import { Pagination } from "@/components/ui/pagination";
import { CustomerStatusBadge, type CustomerStatus } from "@/components/customer/CustomerStatusBadge";
import { formatTimestampDate } from "@/lib/date";

const PAGE_SIZE = 20;

const STATUS_OPTIONS: { value: CustomerStatusValue; label: string }[] = [
  { value: "active", label: "Active" },
  { value: "inactive", label: "Inactive" },
  { value: "archived", label: "Archived" },
];

// Archived customers are retired, so the list starts without them; tick
// Archived in the filter pane to see them.
const DEFAULT_STATUSES: CustomerStatusValue[] = ["active", "inactive"];

function sameStatuses(a: CustomerStatusValue[], b: CustomerStatusValue[]) {
  return a.length === b.length && a.every((status) => b.includes(status));
}

function FilterPane({
  name,
  onNameChange,
  statuses,
  onStatusesChange,
  onReset,
  isDefault,
}: {
  name: string;
  onNameChange: (name: string) => void;
  statuses: CustomerStatusValue[];
  onStatusesChange: (statuses: CustomerStatusValue[]) => void;
  onReset: () => void;
  isDefault: boolean;
}) {
  return (
    <Card>
      <CardContent>
        <div className="flex flex-wrap items-end gap-x-8 gap-y-4">
          <div className="w-full sm:w-72">
            <Label htmlFor="customer-name-filter">Name</Label>
            <div className="relative">
              <Search
                className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400"
                aria-hidden="true"
              />
              <Input
                id="customer-name-filter"
                type="search"
                placeholder="Name, company or email"
                className="pl-9"
                value={name}
                onChange={(e) => onNameChange(e.target.value)}
              />
            </div>
          </div>

          <fieldset>
            <legend className="mb-2 text-sm font-medium text-slate-700">Status</legend>
            <div className="flex h-9 items-center gap-5">
              {STATUS_OPTIONS.map((option) => (
                <label key={option.value} className="flex items-center gap-2 text-sm text-slate-700">
                  <input
                    type="checkbox"
                    className="h-4 w-4 rounded border-slate-300"
                    checked={statuses.includes(option.value)}
                    onChange={(e) =>
                      onStatusesChange(
                        e.target.checked
                          ? STATUS_OPTIONS.map((o) => o.value).filter((v) => v === option.value || statuses.includes(v))
                          : statuses.filter((v) => v !== option.value),
                      )
                    }
                  />
                  {option.label}
                </label>
              ))}
            </div>
          </fieldset>

          {!isDefault && (
            <Button variant="ghost" size="sm" className="mb-0.5" onClick={onReset}>
              Reset filters
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

export function CustomersListPage() {
  const [name, setName] = useState("");
  const [statuses, setStatuses] = useState<CustomerStatusValue[]>(DEFAULT_STATUSES);
  const [offset, setOffset] = useState(0);
  const debouncedName = useDebouncedValue(name);
  const isDefault = name === "" && sameStatuses(statuses, DEFAULT_STATUSES);

  const query = useCustomers(
    {
      limit: PAGE_SIZE,
      offset,
      search: debouncedName || undefined,
      status: statuses,
    },
    { enabled: statuses.length > 0 },
  );

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

      <div className="space-y-4">
        <section aria-label="Customer filters">
          <FilterPane
            name={name}
            onNameChange={(value) => {
              setOffset(0);
              setName(value);
            }}
            statuses={statuses}
            onStatusesChange={(value) => {
              setOffset(0);
              setStatuses(value);
            }}
            onReset={() => {
              setOffset(0);
              setName("");
              setStatuses(DEFAULT_STATUSES);
            }}
            isDefault={isDefault}
          />
        </section>

        <div>
          {statuses.length === 0 ? (
            <EmptyState title="No statuses selected" description="Tick at least one status in the filter pane." />
          ) : (
            <QueryBoundary query={query}>
              {(page) =>
                page.items.length === 0 ? (
                  <EmptyState
                    title={isDefault ? "No customers yet" : "No matching customers"}
                    description={
                      isDefault
                        ? "Add your first customer to start creating invoices for them."
                        : "Try a different name or status, or reset the filters."
                    }
                    action={
                      isDefault ? (
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
                            <Link
                              to={`/customers/${customer.id}`}
                              className="font-medium text-brand-600 hover:underline"
                            >
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
          )}
        </div>
      </div>
    </div>
  );
}
