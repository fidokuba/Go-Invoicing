import type { ReactNode } from "react";
import { AlertTriangle, Info, RefreshCw } from "lucide-react";
import { Button } from "./button";
import { cn } from "@/lib/cn";

export type AlertTone = "error" | "info";

const styles: Record<AlertTone, string> = {
  error: "border-red-200 bg-red-50 text-red-800",
  info: "border-blue-200 bg-blue-50 text-blue-800",
};

export function Alert({
  tone = "error",
  title,
  children,
  onRetry,
}: {
  tone?: AlertTone;
  title?: string;
  children?: ReactNode;
  onRetry?: () => void;
}) {
  const Icon = tone === "error" ? AlertTriangle : Info;
  return (
    <div role="alert" className={cn("flex items-start gap-3 rounded-md border px-4 py-3 text-sm", styles[tone])}>
      <Icon className="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
      <div className="flex-1">
        {title && <p className="font-medium">{title}</p>}
        {children && <p className="mt-0.5">{children}</p>}
      </div>
      {onRetry && (
        <Button variant="secondary" size="sm" onClick={onRetry}>
          <RefreshCw className="h-3.5 w-3.5" aria-hidden="true" />
          Retry
        </Button>
      )}
    </div>
  );
}
