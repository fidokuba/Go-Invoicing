import { useState } from "react";
import { Link } from "react-router-dom";
import { Plus, Search } from "lucide-react";
import { useProducts } from "@/api/queries/products";
import { useInvoiceSettings } from "@/api/queries/organisation";
import { useDebouncedValue } from "@/lib/useDebouncedValue";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/button";
import { Input, Select } from "@/components/ui/input";
import { QueryBoundary } from "@/components/ui/query-boundary";
import { EmptyState } from "@/components/ui/empty-state";
import { TableContainer, THead, TBody, Tr, Th, Td } from "@/components/ui/table";
import { Pagination } from "@/components/ui/pagination";
import { Badge } from "@/components/ui/badge";
import { formatMoney } from "@/lib/money";

const PAGE_SIZE = 20;

export function ProductsListPage() {
  const [search, setSearch] = useState("");
  const [activeFilter, setActiveFilter] = useState<"" | "true" | "false">("");
  const [offset, setOffset] = useState(0);
  const debouncedSearch = useDebouncedValue(search);
  // Products carry a catalogue price in minor units but no currency
  // field of their own (see ProductResponse) — the organisation's
  // current invoice currency is the only sensible one to display it in.
  const settings = useInvoiceSettings();
  const currency = settings.data?.currency ?? "GBP";

  const query = useProducts({
    limit: PAGE_SIZE,
    offset,
    search: debouncedSearch || undefined,
    isActive: activeFilter === "" ? undefined : activeFilter === "true",
  });

  return (
    <div>
      <PageHeader
        title="Products"
        description="A reusable catalogue of items to add to invoice lines."
        actions={
          <Button asChild>
            <Link to="/products/new" className="flex items-center gap-2">
              <Plus className="h-4 w-4" aria-hidden="true" />
              New product
            </Link>
          </Button>
        }
      />

      <div className="mb-4 flex flex-wrap gap-3">
        <div className="relative w-64">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" aria-hidden="true" />
          <Input
            type="search"
            placeholder="Search products…"
            aria-label="Search products"
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
          value={activeFilter}
          onChange={(e) => {
            setOffset(0);
            setActiveFilter(e.target.value as "" | "true" | "false");
          }}
        >
          <option value="">All statuses</option>
          <option value="true">Active</option>
          <option value="false">Inactive</option>
        </Select>
      </div>

      <QueryBoundary query={query}>
        {(page) =>
          page.items.length === 0 ? (
            <EmptyState
              title={debouncedSearch || activeFilter ? "No matching products" : "No products yet"}
              description={
                debouncedSearch || activeFilter
                  ? "Try a different search or filter."
                  : "Add a product to quickly populate invoice lines later."
              }
              action={
                !debouncedSearch && !activeFilter ? (
                  <Button asChild>
                    <Link to="/products/new">New product</Link>
                  </Button>
                ) : undefined
              }
            />
          ) : (
            <TableContainer>
              <THead>
                <Tr>
                  <Th>Name</Th>
                  <Th>SKU</Th>
                  <Th>Category</Th>
                  <Th className="text-right">Price</Th>
                  <Th>Status</Th>
                </Tr>
              </THead>
              <TBody>
                {page.items.map((product) => (
                  <Tr key={product.id} className="hover:bg-slate-50">
                    <Td>
                      <Link to={`/products/${product.id}`} className="font-medium text-brand-600 hover:underline">
                        {product.name}
                      </Link>
                    </Td>
                    <Td className="font-mono text-xs">{product.sku}</Td>
                    <Td>{product.category ?? "—"}</Td>
                    <Td className="text-right">{formatMoney(product.price, currency)}</Td>
                    <Td>
                      <Badge tone={product.isActive ? "green" : "slate"}>
                        {product.isActive ? "Active" : "Inactive"}
                      </Badge>
                    </Td>
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
