import { useEffect, useState } from "react";

/** Debounces a fast-changing value (e.g. a search input) so dependent
 * queries (customers/products/invoices search) don't fire on every
 * keystroke. */
export function useDebouncedValue<T>(value: T, delayMs = 300): T {
  const [debounced, setDebounced] = useState(value);

  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(timer);
  }, [value, delayMs]);

  return debounced;
}
