import { forwardRef, type InputHTMLAttributes, type ReactNode } from "react";
import { cn } from "@/lib/cn";

type CheckboxProps = Omit<InputHTMLAttributes<HTMLInputElement>, "type"> & {
  label: ReactNode;
};

/** A native checkbox with its label beside it — clicking the label toggles it. */
export const Checkbox = forwardRef<HTMLInputElement, CheckboxProps>(({ className, label, id, ...props }, ref) => (
  <div className="flex items-center gap-2">
    <input
      ref={ref}
      id={id}
      type="checkbox"
      className={cn("h-4 w-4 rounded border-slate-300 text-slate-900 disabled:opacity-50", className)}
      {...props}
    />
    <label htmlFor={id} className="text-sm font-medium text-slate-700">
      {label}
    </label>
  </div>
));
Checkbox.displayName = "Checkbox";
