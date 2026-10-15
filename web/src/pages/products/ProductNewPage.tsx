import { useNavigate } from "react-router-dom";
import { useCreateProduct } from "@/api/queries/products";
import { useInvoiceSettings, useOrganisation } from "@/api/queries/organisation";
import { friendlyMessage } from "@/api/errors";
import { PageHeader } from "@/components/layout/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Alert } from "@/components/ui/alert";
import { ProductForm } from "@/components/product/ProductForm";

export function ProductNewPage() {
  const navigate = useNavigate();
  const settings = useInvoiceSettings();
  const organisation = useOrganisation();
  const currency = settings.data?.currency ?? "GBP";
  const vatRegistered = organisation.data?.vatRegistered ?? false;
  const createProduct = useCreateProduct();

  return (
    <div className="max-w-xl">
      <PageHeader title="New product" />
      <Card>
        <CardContent>
          {createProduct.isError && (
            <div className="mb-4">
              <Alert>{friendlyMessage(createProduct.error)}</Alert>
            </div>
          )}
          <ProductForm
            currency={currency}
            vatRegistered={vatRegistered}
            submitLabel="Create product"
            pendingLabel="Creating…"
            isPending={createProduct.isPending}
            onSubmit={({ isActive: _isActive, ...values }) =>
              createProduct.mutate(values, { onSuccess: (product) => navigate(`/products/${product.id}`) })
            }
          />
        </CardContent>
      </Card>
    </div>
  );
}
