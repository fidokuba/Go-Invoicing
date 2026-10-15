import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { parseMoneyInput, minorToInputString } from "@/lib/money";
import { Button } from "@/components/ui/button";
import { Input, Label, Textarea, FieldError } from "@/components/ui/input";
import type { components } from "@/api/schema";

type Product = components["schemas"]["ProductResponse"];

export interface ProductFormValues {
  name: string;
  sku: string;
  description?: string;
  category?: string;
  price: number;
  vatRate?: number;
  isActive: boolean;
}

interface ProductFormProps {
  /** The product being edited; omitted when creating one. */
  product?: Product;
  currency: string;
  /** The VAT % field is only shown to a VAT-registered organisation. */
  vatRegistered: boolean;
  submitLabel: string;
  pendingLabel: string;
  isPending: boolean;
  onSubmit: (values: ProductFormValues) => void;
}

/** The fields shared by the New product and Edit product pages. Editing
 * also offers the Active checkbox; a new product always starts active. */
export function ProductForm({
  product,
  currency,
  vatRegistered,
  submitLabel,
  pendingLabel,
  isPending,
  onSubmit,
}: ProductFormProps) {
  const navigate = useNavigate();
  const [name, setName] = useState(product?.name ?? "");
  const [sku, setSku] = useState(product?.sku ?? "");
  const [description, setDescription] = useState(product?.description ?? "");
  const [category, setCategory] = useState(product?.category ?? "");
  const [priceInput, setPriceInput] = useState(product ? minorToInputString(product.price, currency) : "0.00");
  const [vatRateInput, setVatRateInput] = useState(product?.vatRate != null ? String(product.vatRate) : "");
  const [isActive, setIsActive] = useState(product?.isActive ?? true);
  const [priceError, setPriceError] = useState<string | null>(null);
  const [vatRateError, setVatRateError] = useState<string | null>(null);

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (isPending) return;

    const priceMinor = parseMoneyInput(priceInput, currency);
    setPriceError(priceMinor === null ? "Enter a valid, non-negative amount." : null);

    // Blank means "no default VAT rate". When the organisation isn't VAT
    // registered the field is hidden, so whatever it holds is kept as-is.
    let vatRate: number | undefined;
    let vatRateValid = true;
    if (vatRegistered) {
      if (vatRateInput.trim() !== "") {
        vatRate = Number(vatRateInput);
        vatRateValid = Number.isFinite(vatRate) && vatRate >= 0 && vatRate <= 100;
      }
    } else {
      vatRate = product?.vatRate;
    }
    setVatRateError(vatRateValid ? null : "Enter a percentage from 0 to 100.");

    if (priceMinor === null || !vatRateValid) return;

    onSubmit({
      name,
      sku,
      description: description || undefined,
      category: category || undefined,
      price: priceMinor,
      vatRate,
      isActive,
    });
  }

  return (
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
      {vatRegistered && (
        <div>
          <Label htmlFor="vatRate">VAT %</Label>
          <Input
            id="vatRate"
            inputMode="decimal"
            placeholder="No default"
            value={vatRateInput}
            onChange={(e) => setVatRateInput(e.target.value)}
            aria-invalid={vatRateError !== null}
          />
          <FieldError>{vatRateError}</FieldError>
          <p className="mt-1 text-xs text-slate-500">
            Filled in on an invoice line when this product is picked, and still editable there.
          </p>
        </div>
      )}
      {product && (
        <label className="flex items-center gap-2 text-sm text-slate-700">
          <input
            type="checkbox"
            className="h-4 w-4 rounded border-slate-300"
            checked={isActive}
            onChange={(e) => setIsActive(e.target.checked)}
          />
          Active (inactive products can't be picked on new invoices)
        </label>
      )}
      <div className="flex justify-end gap-2 pt-2">
        <Button type="button" variant="secondary" onClick={() => navigate(-1)}>
          Cancel
        </Button>
        <Button type="submit" disabled={isPending}>
          {isPending ? pendingLabel : submitLabel}
        </Button>
      </div>
    </form>
  );
}
