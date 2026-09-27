import { createContext, useContext, type CSSProperties, type ReactNode } from "react";
import type { Config, Data } from "@puckeditor/core";

// The invoice-template builder's Puck component set (custom invoice
// layouts, Phase 3, extended in Phase 4 to bind real invoice data).
// Promoted from an evaluation spike — see git history for the "SPIKE"
// version this replaces — into the real, shared config used by:
//
//   - the editable builder page (InvoiceTemplateBuilderPage, using
//     Puck's own editor component) — never wrapped in an
//     InvoiceDataProvider, so every block falls back to
//     sampleInvoiceData below, which is what makes the builder's canvas
//     look like a real invoice while you design it;
//   - the renderer service (renderer/src/renderTemplate.js), which
//     imports this exact file, wraps it in InvoiceDataProvider with one
//     real invoice's actual figures, and renders it with Puck's
//     Render component (read-only) via React's server renderer — the
//     one place this file's components ever see real data instead of
//     the sample.
//
// The split below matters: fields Puck lets a user configure are always
// styling/arrangement (font, size, color, width, which columns, in what
// order) — never an invoice's actual content (seller name, line items,
// totals). A template describes *layout*, not one hardcoded business's
// details; the same saved template has to render correctly for every
// invoice it's ever used on, not just whichever one happened to be open
// when it was designed.

export interface InvoiceRenderSeller {
  name: string;
  addressLines: string[];
  email: string;
  phone: string;
  website: string;
  taxId: string;
}

export interface InvoiceRenderCustomer {
  displayName: string;
  addressLines: string[];
  email: string;
  taxId: string;
}

export interface InvoiceRenderLine {
  description: string;
  quantity: string;
  unitPrice: string;
  vatRate: string;
  vatAmount: string;
  total: string;
}

// InvoiceRenderData's shape mirrors internal/renderer.RenderRequest
// field-for-field (see that Go type, and renderer/src/server.js's own
// mapping into this shape) — this is the wire contract between the Go
// API and the renderer service, expressed on both ends.
export interface InvoiceRenderData {
  invoiceNumber: string;
  issueDate: string;
  dueDate: string;
  statusLabel: string;
  seller: InvoiceRenderSeller;
  customer: InvoiceRenderCustomer;
  lines: InvoiceRenderLine[];
  vatRegistered: boolean;
  subtotal: string;
  vatTotal: string;
  total: string;
  showPaymentSummary: boolean;
  amountPaid: string;
  amountOutstanding: string;
  notes: string;
}

/** Representative sample data for the builder's own live preview — never
 * one real invoice's actual figures (see this file's own doc comment). */
export const sampleInvoiceData: InvoiceRenderData = {
  invoiceNumber: "INV-0001",
  issueDate: "15 Jan 2026",
  dueDate: "14 Feb 2026",
  statusLabel: "",
  seller: {
    name: "Acme Consulting Ltd",
    addressLines: ["123 High Street", "London, EC1A 1AA"],
    email: "hello@acme.example",
    phone: "",
    website: "",
    taxId: "GB123456789",
  },
  customer: {
    displayName: "Widget Co",
    addressLines: ["456 Market Road", "Manchester, M1 1AA"],
    email: "",
    taxId: "",
  },
  lines: [
    {
      description: "Consulting services",
      quantity: "1",
      unitPrice: "£500.00",
      vatRate: "20%",
      vatAmount: "£100.00",
      total: "£600.00",
    },
    {
      description: "Software licence",
      quantity: "1",
      unitPrice: "£500.00",
      vatRate: "20%",
      vatAmount: "£100.00",
      total: "£600.00",
    },
  ],
  vatRegistered: true,
  subtotal: "£1,000.00",
  vatTotal: "£200.00",
  total: "£1,200.00",
  showPaymentSummary: false,
  amountPaid: "",
  amountOutstanding: "",
  notes: "",
};

const InvoiceDataContext = createContext<InvoiceRenderData>(sampleInvoiceData);

/** Wraps a subtree so every block in it reads realData instead of
 * sampleInvoiceData — used only by the renderer service's server-side
 * render, never by the builder page itself. */
export function InvoiceDataProvider({ value, children }: { value: InvoiceRenderData; children: ReactNode }) {
  return <InvoiceDataContext.Provider value={value}>{children}</InvoiceDataContext.Provider>;
}

function useInvoiceData(): InvoiceRenderData {
  return useContext(InvoiceDataContext);
}

interface SizeAndTextProps {
  width: string;
  height: number;
  fontFamily: string;
  fontSize: number;
  fontWeight: string;
  textAlign: string;
}

// A native <input type="color"> swatch/picker instead of a free-typed
// hex string — shared by every color field below (TotalsBlock's
// accentColor, Divider's color) so they all get the same picker rather
// than each hand-rolling one.
const colorField = {
  type: "custom",
  render: ({ value, onChange }: { value: string; onChange: (value: string) => void }) => (
    <input
      type="color"
      value={value || "#000000"}
      onChange={(e) => onChange(e.target.value)}
      style={{ width: "100%", height: 32, padding: 0, border: "1px solid #cbd5e1", borderRadius: 4 }}
    />
  ),
} as const;

const widthOptions = [
  { label: "25%", value: "25%" },
  { label: "50%", value: "50%" },
  { label: "75%", value: "75%" },
  { label: "100% (full width)", value: "100%" },
];

const fontFamilyOptions = [
  { label: "Sans-serif (default)", value: "system-ui, sans-serif" },
  { label: "Serif", value: "Georgia, serif" },
  { label: "Monospace", value: "'Courier New', monospace" },
];

const fontWeightOptions = [
  { label: "Normal", value: "400" },
  { label: "Bold", value: "700" },
];

const textAlignOptions = [
  { label: "Left", value: "left" },
  { label: "Center", value: "center" },
  { label: "Right", value: "right" },
];

// Every block gets these — width plus basic typography — so "resize"
// and "change fonts/formatting" are available uniformly, the same way a
// real template builder would offer them on every block, not just one.
const sizeAndTextFields = {
  width: { type: "select", options: widthOptions },
  height: { type: "number", label: "Height (px, 0 = auto)", min: 0, max: 800, step: 10 },
  fontFamily: { type: "select", options: fontFamilyOptions },
  fontSize: { type: "number", min: 8, max: 48 },
  fontWeight: { type: "select", options: fontWeightOptions },
  textAlign: { type: "select", options: textAlignOptions },
} as const;

const sizeAndTextDefaults = {
  width: "100%",
  height: 0,
  fontFamily: "system-ui, sans-serif",
  fontSize: 13,
  fontWeight: "400",
  textAlign: "left",
};

function blockStyle(props: SizeAndTextProps): CSSProperties {
  return {
    width: props.width,
    height: props.height > 0 ? props.height : undefined,
    overflow: props.height > 0 ? "auto" : undefined,
    fontFamily: props.fontFamily,
    fontSize: props.fontSize,
    fontWeight: props.fontWeight as CSSProperties["fontWeight"],
    textAlign: props.textAlign as CSSProperties["textAlign"],
  };
}

function AddressLines({ lines }: { lines: string[] }) {
  // lines is typed as always an array, but the real wire data isn't: a Go
  // nil []string (renderer.Seller/Customer.AddressLines, when an
  // organisation or customer has no address at all) marshals to JSON
  // `null`, not `[]` — the same gap classicTemplate.js's own renderLines
  // already guards against with `?? []`, just not yet caught here since
  // this path only runs for a real custom template.
  return (
    <>
      {(lines ?? []).filter(Boolean).map((line, i) => (
        <div key={i}>{line}</div>
      ))}
    </>
  );
}

// LineItemsTable's column choices — mirrors the fixed set the current
// gopdf/Classic renderer draws (see invoice.InvoicePDFLine — "quantity"
// matches renderer.Line's own JSON tag exactly, not a frontend-only
// name), but here the user picks which of them appear, in what order,
// and under what label.
const columnKeyOptions = [
  { label: "Description", value: "description" },
  { label: "Quantity", value: "quantity" },
  { label: "Unit Price", value: "unitPrice" },
  { label: "VAT Rate", value: "vatRate" },
  { label: "VAT Amount", value: "vatAmount" },
  { label: "Total", value: "total" },
];

function SellerBlockRender(props: unknown) {
  const p = props as SizeAndTextProps;
  const { seller, vatRegistered } = useInvoiceData();
  return (
    <div style={blockStyle(p)}>
      <div style={{ fontWeight: 600 }}>{seller.name}</div>
      <div style={{ color: "#475569" }}>
        <AddressLines lines={seller.addressLines} />
        <AddressLines lines={[seller.email, seller.phone, seller.website]} />
        {vatRegistered && seller.taxId && <div>VAT Registration Number: {seller.taxId}</div>}
      </div>
    </div>
  );
}

function CustomerBlockRender(props: unknown) {
  const p = props as SizeAndTextProps;
  const { customer } = useInvoiceData();
  return (
    <div style={blockStyle(p)}>
      <div style={{ fontWeight: 600, marginBottom: 4 }}>BILL TO</div>
      <div>{customer.displayName}</div>
      <div style={{ color: "#475569" }}>
        <AddressLines lines={customer.addressLines} />
        {customer.email && <div>{customer.email}</div>}
        {customer.taxId && <div>Tax ID: {customer.taxId}</div>}
      </div>
    </div>
  );
}

function InvoiceTitleRender(props: unknown) {
  const p = props as SizeAndTextProps & { text: string };
  const { invoiceNumber, issueDate, dueDate, statusLabel } = useInvoiceData();
  return (
    <div style={blockStyle(p)}>
      <div>{p.text}</div>
      <div style={{ fontSize: 13, fontWeight: 400 }}>
        <div>Invoice #: {invoiceNumber}</div>
        <div>Issue Date: {issueDate}</div>
        <div>Due Date: {dueDate}</div>
      </div>
      {statusLabel && (
        <div
          style={{
            display: "inline-block",
            marginTop: 8,
            padding: "4px 12px",
            borderRadius: 2,
            color: "#fff",
            fontSize: 13,
            fontWeight: 700,
            background: statusLabel === "PAID" ? "#28783c" : statusLabel === "OVERDUE" ? "#b03030" : "#5a5a5a",
          }}
        >
          {statusLabel}
        </div>
      )}
    </div>
  );
}

function LineItemsTableRender(props: unknown) {
  const p = props as SizeAndTextProps & { columns: { key: string; label: string }[] };
  const columns = p.columns ?? [];
  const { lines } = useInvoiceData();
  return (
    <table style={{ ...blockStyle(p), borderCollapse: "collapse" }}>
      <thead>
        <tr style={{ background: "#e2e8f0" }}>
          {columns.map((col) => (
            <th key={col.key} style={{ textAlign: col.key === "description" ? "left" : "right", padding: 6 }}>
              {col.label}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {lines.map((row, i) => (
          <tr key={i} style={{ borderBottom: "1px solid #e2e8f0" }}>
            {columns.map((col) => (
              <td key={col.key} style={{ padding: 6, textAlign: col.key === "description" ? "left" : "right" }}>
                {(row as unknown as Record<string, string>)[col.key] ?? ""}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function TotalsBlockRender(props: unknown) {
  const p = props as SizeAndTextProps & { accentColor: string };
  const data = useInvoiceData();

  const rows: [string, string][] = data.vatRegistered
    ? [
        ["Subtotal", data.subtotal],
        ["VAT", data.vatTotal],
        ["Total", data.total],
      ]
    : [["Total", data.total]];
  if (data.showPaymentSummary) {
    rows.push(["Amount Paid", data.amountPaid], ["Balance Due", data.amountOutstanding]);
  }

  return (
    <div style={{ ...blockStyle(p), marginLeft: "auto" }}>
      {rows.map(([label, value]) => (
        <div
          key={label}
          style={
            label === "Total"
              ? {
                  display: "flex",
                  justifyContent: "space-between",
                  fontWeight: 700,
                  color: p.accentColor,
                  borderTop: "1px solid #cbd5e1",
                  marginTop: 4,
                  paddingTop: 4,
                }
              : { display: "flex", justifyContent: "space-between" }
          }
        >
          <span>{label}</span>
          <span>{value}</span>
        </div>
      ))}
    </div>
  );
}

function NotesRender(props: unknown) {
  const p = props as SizeAndTextProps;
  const { notes } = useInvoiceData();
  if (!notes) return <></>;
  return (
    <div style={blockStyle(p)}>
      <div style={{ fontWeight: 600, marginBottom: 4 }}>Notes</div>
      <div style={{ whiteSpace: "pre-line" }}>{notes}</div>
    </div>
  );
}

export const templateBuilderConfig: Config = {
  components: {
    Logo: {
      fields: {
        text: { type: "text" },
        ...sizeAndTextFields,
      },
      defaultProps: { text: "[Your Logo]", ...sizeAndTextDefaults, height: 60 },
      render: (props) => {
        const p = props as unknown as SizeAndTextProps & { text: string };
        return (
          <div
            style={{
              ...blockStyle(p),
              border: "2px dashed #94a3b8",
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              color: "#64748b",
            }}
          >
            {p.text}
          </div>
        );
      },
    },
    // SellerBlock/CustomerBlock/InvoiceTitle/LineItemsTable/TotalsBlock/
    // Notes below take no content fields of their own (beyond style) —
    // their actual content always comes from useInvoiceData(), never
    // from something typed into the template (see this file's own doc
    // comment for why). Each render function is a properly-named,
    // PascalCase component (SellerBlockRender etc., defined below,
    // before this config object) rather than an inline arrow function:
    // that's what a hook call inside it requires — ESLint's
    // react-hooks/rules-of-hooks recognises a component by that naming
    // convention, the same as React itself does at runtime.
    SellerBlock: {
      fields: { ...sizeAndTextFields },
      defaultProps: { ...sizeAndTextDefaults },
      render: SellerBlockRender,
    },
    CustomerBlock: {
      fields: { ...sizeAndTextFields },
      defaultProps: { ...sizeAndTextDefaults },
      render: CustomerBlockRender,
    },
    InvoiceTitle: {
      fields: {
        text: { type: "text", label: "Title text" },
        ...sizeAndTextFields,
      },
      defaultProps: {
        text: "INVOICE",
        ...sizeAndTextDefaults,
        fontSize: 22,
        fontWeight: "700",
        textAlign: "right",
      },
      render: InvoiceTitleRender,
    },
    LineItemsTable: {
      fields: {
        columns: {
          type: "array",
          arrayFields: {
            key: { type: "select", options: columnKeyOptions },
            label: { type: "text" },
          },
          defaultItemProps: { key: "description", label: "Description" },
          getItemSummary: (item: { label?: string }) => item.label || "Column",
        },
        ...sizeAndTextFields,
      },
      defaultProps: {
        columns: [
          { key: "description", label: "Description" },
          { key: "quantity", label: "Qty" },
          { key: "unitPrice", label: "Unit Price" },
          { key: "vatRate", label: "VAT" },
          { key: "vatAmount", label: "VAT Amt" },
          { key: "total", label: "Total" },
        ],
        ...sizeAndTextDefaults,
      },
      render: LineItemsTableRender,
    },
    TotalsBlock: {
      fields: {
        accentColor: colorField,
        ...sizeAndTextFields,
      },
      defaultProps: { accentColor: "#0f172a", ...sizeAndTextDefaults, width: "50%" },
      render: TotalsBlockRender,
    },
    Notes: {
      fields: { ...sizeAndTextFields },
      defaultProps: { ...sizeAndTextDefaults },
      render: NotesRender,
    },
    // Divider/Spacer are layout-only blocks — no text/font fields, since
    // "look like a heading" doesn't apply to a blank line or blank space.
    Divider: {
      fields: {
        thickness: { type: "number", min: 1, max: 10 },
        color: colorField,
        width: { type: "select", options: widthOptions },
      },
      defaultProps: { thickness: 1, color: "#cbd5e1", width: "100%" },
      render: (props) => {
        const p = props as unknown as { thickness: number; color: string; width: string };
        return (
          <hr
            style={{
              border: "none",
              borderTop: `${p.thickness}px solid ${p.color}`,
              width: p.width,
              margin: "0 auto",
            }}
          />
        );
      },
    },
    Spacer: {
      fields: {
        height: { type: "number", min: 4, max: 200 },
      },
      defaultProps: { height: 24 },
      render: (props) => {
        const p = props as unknown as { height: number };
        return <div style={{ height: p.height }} />;
      },
    },
  },
};

/** A brand-new template's starting point — an empty canvas. */
export const emptyTemplateDefinition: Data = { content: [], root: {} };
