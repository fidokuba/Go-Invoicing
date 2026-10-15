// Mirror admin.MaxQuantityUnitLength / admin.MaxQuantityUnits.
export const MAX_QUANTITY_UNIT_LENGTH = 30;
export const MAX_QUANTITY_UNITS = 50;

/** The choices for a quantity unit: the organisation's settings units,
 * plus `current` when it isn't (or is no longer) one of them, so an
 * existing product's or line's unit is never silently dropped. */
export function quantityUnitOptions(units: readonly string[], current: string): string[] {
  return current && !units.includes(current) ? [...units, current] : [...units];
}

/** Why `unit` can't be added to `units`, or null if it can — the same
 * rules the API applies to the settings list. */
export function quantityUnitError(units: readonly string[], unit: string): string | null {
  const trimmed = unit.trim();
  if (!trimmed) return "Enter a unit.";
  if ([...trimmed].length > MAX_QUANTITY_UNIT_LENGTH) {
    return `Units can be at most ${MAX_QUANTITY_UNIT_LENGTH} characters.`;
  }
  if (units.some((u) => u.toLowerCase() === trimmed.toLowerCase())) return "That unit is already in the list.";
  if (units.length >= MAX_QUANTITY_UNITS) return `At most ${MAX_QUANTITY_UNITS} units are allowed.`;
  return null;
}
