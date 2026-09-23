import { Badge, type BadgeTone } from "@/components/ui/badge";

export type InvoiceStatus = "draft" | "sent" | "overdue" | "paid";

// The invoice's effective status is computed and returned by the backend
// (InvoiceResponse.status) — "overdue" is a derived, non-persisted state
// (see api/openapi.yaml's Invoices tag), never recomputed here from
// dueDate. This component only ever renders whatever status field the
// API already returned.
const LABELS: Record<InvoiceStatus, string> = {
  draft: "Draft",
  sent: "Sent",
  overdue: "Overdue",
  paid: "Paid",
};

const TONES: Record<InvoiceStatus, BadgeTone> = {
  draft: "slate",
  sent: "blue",
  overdue: "amber",
  paid: "green",
};

export function InvoiceStatusBadge({ status }: { status: InvoiceStatus }) {
  return <Badge tone={TONES[status]}>{LABELS[status]}</Badge>;
}
