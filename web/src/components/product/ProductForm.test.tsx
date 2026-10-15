import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { ProductForm, type ProductFormValues } from "./ProductForm";

const existingProduct = {
  id: "66666666-6666-6666-6666-666666666666",
  organisationId: "7c9e6679-7425-40de-944b-e07fc1f90ae7",
  name: "Consulting (half day)",
  sku: "CONS-HD",
  description: "On-site",
  category: "Services",
  price: 50000,
  vatRate: 20,
  quantityUnit: "Day(s)",
  isActive: true,
  createdAt: "2026-01-15T10:00:00Z",
  updatedAt: "2026-01-15T10:00:00Z",
};

function renderForm(props: Partial<Parameters<typeof ProductForm>[0]> = {}) {
  const onSubmit = vi.fn<(values: ProductFormValues) => void>();
  render(
    <MemoryRouter>
      <ProductForm
        currency="GBP"
        vatRegistered
        quantityUnits={["Day(s)", "Hour(s)", "KG", "g", "Pcs"]}
        submitLabel="Save"
        pendingLabel="Saving…"
        isPending={false}
        onSubmit={onSubmit}
        {...props}
      />
    </MemoryRouter>,
  );
  return onSubmit;
}

afterEach(cleanup);

describe("ProductForm", () => {
  it("shows the VAT % field only to a VAT-registered organisation", () => {
    renderForm({ vatRegistered: false });
    expect(screen.queryByLabelText("VAT %")).toBeNull();
    cleanup();

    renderForm({ vatRegistered: true });
    expect(screen.getByLabelText("VAT %")).toBeTruthy();
  });

  it("creates with a VAT rate, and with none when VAT % is left blank", async () => {
    const user = userEvent.setup();
    const onSubmit = renderForm();

    await user.type(screen.getByLabelText("Name"), "Widget");
    await user.type(screen.getByLabelText("SKU"), "W-1");
    await user.clear(screen.getByLabelText("Price (GBP)"));
    await user.type(screen.getByLabelText("Price (GBP)"), "12.50");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(onSubmit).toHaveBeenLastCalledWith(expect.objectContaining({ name: "Widget", price: 1250, vatRate: undefined }));

    await user.type(screen.getByLabelText("VAT %"), "5");
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(onSubmit).toHaveBeenLastCalledWith(expect.objectContaining({ vatRate: 5 }));
  });

  it("rejects a VAT rate outside 0-100", async () => {
    const user = userEvent.setup();
    const onSubmit = renderForm();

    await user.type(screen.getByLabelText("Name"), "Widget");
    await user.type(screen.getByLabelText("SKU"), "W-1");
    await user.type(screen.getByLabelText("VAT %"), "120");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.getByText("Enter a percentage from 0 to 100.")).toBeTruthy();
  });

  it("starts from the product's values when editing, including Active", async () => {
    const user = userEvent.setup();
    const onSubmit = renderForm({ product: existingProduct });

    expect((screen.getByLabelText("Price (GBP)") as HTMLInputElement).value).toBe("500.00");
    expect((screen.getByLabelText("VAT %") as HTMLInputElement).value).toBe("20");

    await user.clear(screen.getByLabelText("SKU"));
    await user.type(screen.getByLabelText("SKU"), "CONS-HD-2");
    await user.click(screen.getByLabelText(/Active/));
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(onSubmit).toHaveBeenCalledWith({
      name: "Consulting (half day)",
      sku: "CONS-HD-2",
      description: "On-site",
      category: "Services",
      price: 50000,
      vatRate: 20,
      quantityUnit: "Day(s)",
      isActive: false,
    });
  });

  it("keeps an existing VAT rate when the VAT % field is hidden", async () => {
    const user = userEvent.setup();
    const onSubmit = renderForm({ product: existingProduct, vatRegistered: false });

    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ vatRate: 20 }));
  });

  it("offers no Active checkbox when creating", () => {
    renderForm();
    expect(screen.queryByLabelText(/Active/)).toBeNull();
  });
});

describe("ProductForm quantity unit", () => {
  it("offers the settings units and submits the chosen one", async () => {
    const user = userEvent.setup();
    const onSubmit = renderForm();

    const select = screen.getByLabelText("Qty unit") as HTMLSelectElement;
    expect([...select.options].map((o) => o.text)).toEqual(["No unit", "Day(s)", "Hour(s)", "KG", "g", "Pcs"]);

    await user.type(screen.getByLabelText("Name"), "Widget");
    await user.type(screen.getByLabelText("SKU"), "W-1");
    await user.selectOptions(select, "KG");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(onSubmit).toHaveBeenLastCalledWith(expect.objectContaining({ quantityUnit: "KG" }));
  });

  it("keeps a product's unit selectable after it's removed from the settings list", () => {
    renderForm({ product: { ...existingProduct, quantityUnit: "Pallets" } });

    const select = screen.getByLabelText("Qty unit") as HTMLSelectElement;
    expect(select.value).toBe("Pallets");
    expect([...select.options].map((o) => o.text)).toContain("Pallets");
  });
});
