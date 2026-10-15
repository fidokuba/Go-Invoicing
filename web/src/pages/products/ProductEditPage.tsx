import { useNavigate, useParams } from "react-router-dom";
import { useProduct, useUpdateProduct } from "@/api/queries/products";
import { useInvoiceSettings, useOrganisation } from "@/api/queries/organisation";
import { friendlyMessage } from "@/api/errors";
import { PageHeader } from "@/components/layout/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Alert } from "@/components/ui/alert";
import { QueryBoundary } from "@/components/ui/query-boundary";
import { ProductForm } from "@/components/product/ProductForm";

/** Edits every product field except its ID. Invoice lines already
 * created from the product keep their own copies of its values. */
export function ProductEditPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const query = useProduct(id);
  const settings = useInvoiceSettings();
  const organisation = useOrganisation();
  const currency = settings.data?.currency ?? "GBP";
  const vatRegistered = organisation.data?.vatRegistered ?? false;
  const updateProduct = useUpdateProduct(id!);

  return (
    <QueryBoundary query={query}>
      {(product) => (
        <div className="max-w-xl">
          <PageHeader title={`Edit ${product.name}`} />
          <Card>
            <CardContent>
              {updateProduct.isError && (
                <div className="mb-4">
                  <Alert>{friendlyMessage(updateProduct.error)}</Alert>
                </div>
              )}
              <ProductForm
                product={product}
                currency={currency}
                vatRegistered={vatRegistered}
                quantityUnits={settings.data?.quantityUnits ?? []}
                submitLabel="Save changes"
                pendingLabel="Saving…"
                isPending={updateProduct.isPending}
                onSubmit={(values) =>
                  updateProduct.mutate(values, { onSuccess: () => navigate(`/products/${product.id}`) })
                }
              />
            </CardContent>
          </Card>
        </div>
      )}
    </QueryBoundary>
  );
}
