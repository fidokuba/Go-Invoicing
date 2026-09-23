import { Badge, type BadgeTone } from "@/components/ui/badge";

export type CustomerStatus = "active" | "inactive" | "archived";

const LABELS: Record<CustomerStatus, string> = {
  active: "Active",
  inactive: "Inactive",
  archived: "Archived",
};

const TONES: Record<CustomerStatus, BadgeTone> = {
  active: "green",
  inactive: "slate",
  archived: "red",
};

export function CustomerStatusBadge({ status }: { status: CustomerStatus }) {
  return <Badge tone={TONES[status]}>{LABELS[status]}</Badge>;
}
