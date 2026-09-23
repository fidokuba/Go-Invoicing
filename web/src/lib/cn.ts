import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

/** Merge Tailwind class lists, letting a later class override an earlier
 * conflicting one (e.g. `cn("p-2", condition && "p-4")`) — the standard
 * shadcn/ui-style helper. */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
