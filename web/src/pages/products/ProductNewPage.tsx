import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { useCreateProduct } from "@/api/queries/products";
import { useInvoiceSettings } from "@/api/queries/organisation";
import { friendlyMessage } from "@/api/errors";
import { parseMoneyInput } from "@/lib/money";
import { PageHeader } from "@/components/layout/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input, Label, Textarea, FieldError } from "@/components/ui/input";
import { Alert } from "@/components/ui/alert";

export function ProductNewPage() {
  const [name, setName] = useState("");
  const [sku, setSku] = useState("");
  const [description, setDescription] = useState("");
  const [category, setCategory] = useState("");
  const [priceInput, setPriceInput] = useState("0.00");
  const [priceError, setPriceError] = useState<string | null>(null);
  const navigate = useNavigate();
  const settings = useInvoiceSettings();
  const currency = settings.data?.currency ?? "GBP";
  const createProduct = useCreateProduct();

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (createProduct.isPending) return;

    const priceMinor = parseMoneyInput(priceInput, currency);
    if (priceMinor === null) {
      setPriceError("Enter a valid, non-negative amount.");
      return;
    }
    setPriceError(null);

    createProduct.mutate(
      {
        name,
        sku,
        description: description || undefined,
        category: category || undefined,
        price: priceMinor,
      },
      { onSuccess: (product) => navigate(`/products/${product.id}`) },
    );
  }

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
          <form onSubmit={handleSubmit} noValidate className="space-y-4">
            <div>
              <Label htmlFor="name">Name</Label>
              <Input id="name" required value={name} onChange={(e) => setName(e.target.value)} />
            </div>
            <div>
              <Label htmlFor="sku">SKU</Label>
              <Input id="sku" required value={sku} onChange={(e) => setSku(e.target.value)} />
            </div>
            <div>
              <Label htmlFor="description">Description</Label>
              <Textarea id="description" rows={3} value={description} onChange={(e) => setDescription(e.target.value)} />
            </div>
            <div>
              <Label htmlFor="category">Category</Label>
              <Input id="category" value={category} onChange={(e) => setCategory(e.target.value)} />
            </div>
            <div>
              <Label htmlFor="price">Price ({currency})</Label>
              <Input
                id="price"
                inputMode="decimal"
                value={priceInput}
                onChange={(e) => setPriceInput(e.target.value)}
                aria-invalid={priceError !== null}
              />
              <FieldError>{priceError}</FieldError>
              <p className="mt-1 text-xs text-slate-500">
                A catalogue default only — never enforced when this product is added to an invoice line.
              </p>
            </div>
            <div className="flex justify-end gap-2 pt-2">
              <Button type="button" variant="secondary" onClick={() => navigate(-1)}>
                Cancel
              </Button>
              <Button type="submit" disabled={createProduct.isPending}>
                {createProduct.isPending ? "Creating…" : "Create product"}
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
