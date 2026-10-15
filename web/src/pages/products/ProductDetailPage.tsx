import { Link, useParams } from "react-router-dom";
import { Pencil } from "lucide-react";
import { useProduct } from "@/api/queries/products";
import { useInvoiceSettings, useOrganisation } from "@/api/queries/organisation";
import { PageHeader } from "@/components/layout/PageHeader";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { QueryBoundary } from "@/components/ui/query-boundary";
import { formatMoney } from "@/lib/money";

// Editing happens on its own page (ProductEditPage, /products/:id/edit).
export function ProductDetailPage() {
  const { id } = useParams<{ id: string }>();
  const query = useProduct(id);
  const settings = useInvoiceSettings();
  const currency = settings.data?.currency ?? "GBP";
  const organisation = useOrganisation();
  const vatRegistered = organisation.data?.vatRegistered ?? false;

  return (
    <QueryBoundary query={query}>
      {(product) => (
        <div className="max-w-xl">
          <PageHeader
            title={product.name}
            description={product.sku}
            actions={
              <div className="flex items-center gap-3">
                <Badge tone={product.isActive ? "green" : "slate"}>{product.isActive ? "Active" : "Inactive"}</Badge>
                <Button asChild variant="secondary">
                  <Link to={`/products/${product.id}/edit`} className="flex items-center gap-2">
                    <Pencil className="h-4 w-4" aria-hidden="true" />
                    Edit
                  </Link>
                </Button>
              </div>
            }
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
                {vatRegistered && (
                  <div className="flex justify-between gap-4">
                    <dt className="text-slate-500">VAT %</dt>
                    <dd className="text-slate-900">{product.vatRate != null ? `${product.vatRate}%` : "—"}</dd>
                  </div>
                )}
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
