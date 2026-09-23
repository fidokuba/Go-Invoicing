import { useParams } from "react-router-dom";
import { useProduct } from "@/api/queries/products";
import { useInvoiceSettings } from "@/api/queries/organisation";
import { PageHeader } from "@/components/layout/PageHeader";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { QueryBoundary } from "@/components/ui/query-boundary";
import { formatMoney } from "@/lib/money";

// Products have no update endpoint in this API (only create + read — see
// api/openapi.yaml's Products tag) — this page is deliberately view-only,
// matching the audited backend contract rather than inventing an edit
// flow the API doesn't support.
export function ProductDetailPage() {
  const { id } = useParams<{ id: string }>();
  const query = useProduct(id);
  const settings = useInvoiceSettings();
  const currency = settings.data?.currency ?? "GBP";

  return (
    <QueryBoundary query={query}>
      {(product) => (
        <div className="max-w-xl">
          <PageHeader
            title={product.name}
            description={product.sku}
            actions={<Badge tone={product.isActive ? "green" : "slate"}>{product.isActive ? "Active" : "Inactive"}</Badge>}
          />
          <Card>
            <CardHeader>
              <CardTitle>Details</CardTitle>
            </CardHeader>
            <CardContent>
              <dl className="space-y-2 text-sm">
                <div className="flex justify-between gap-4">
                  <dt className="text-slate-500">Price</dt>
                  <dd className="text-slate-900">{formatMoney(product.price, currency)}</dd>
                </div>
                <div className="flex justify-between gap-4">
                  <dt className="text-slate-500">Category</dt>
                  <dd className="text-slate-900">{product.category ?? "—"}</dd>
                </div>
                <div className="flex justify-between gap-4">
                  <dt className="text-slate-500">Description</dt>
                  <dd className="max-w-xs text-right text-slate-900">{product.description ?? "—"}</dd>
                </div>
              </dl>
            </CardContent>
          </Card>
        </div>
      )}
    </QueryBoundary>
  );
}
