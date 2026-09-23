import type { HTMLAttributes } from "react";
import { cn } from "@/lib/cn";

export type BadgeTone = "slate" | "blue" | "amber" | "red" | "green";

const tones: Record<BadgeTone, string> = {
  slate: "bg-slate-100 text-slate-700",
  blue: "bg-blue-100 text-blue-700",
  amber: "bg-amber-100 text-amber-800",
  red: "bg-red-100 text-red-700",
  green: "bg-green-100 text-green-700",
};

export interface BadgeProps extends HTMLAttributes<HTMLSpanElement> {
  tone?: BadgeTone;
}

/** A small status pill. Status is always conveyed by its text label, not
 * colour alone (Milestone 12 section 25) — the leading dot is a purely
 * decorative scan aid on top of that, marked aria-hidden. */
export function Badge({ tone = "slate", className, children, ...props }: BadgeProps) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium",
        tones[tone],
        className,
      )}
      {...props}
    >
      <span aria-hidden="true" className="h-1.5 w-1.5 rounded-full bg-current opacity-70" />
      {children}
    </span>
  );
}
