import type { SelectHTMLAttributes } from "react";
import { Select } from "@/components/ui/input";
import { quantityUnitOptions } from "@/lib/quantityUnits";

interface QuantityUnitSelectProps extends Omit<SelectHTMLAttributes<HTMLSelectElement>, "value" | "onChange"> {
  /** The organisation's settings units. */
  units: readonly string[];
  /** "" for no unit. */
  value: string;
  onChange: (value: string) => void;
}

/** A dropdown of quantity units, with "No unit" as the empty choice. */
export function QuantityUnitSelect({ units, value, onChange, ...props }: QuantityUnitSelectProps) {
  return (
    <Select {...props} value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="">No unit</option>
      {quantityUnitOptions(units, value).map((unit) => (
        <option key={unit} value={unit}>
          {unit}
        </option>
      ))}
    </Select>
  );
}
