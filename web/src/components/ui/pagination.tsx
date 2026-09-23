import { Button } from "./button";

export interface PaginationInfo {
  limit: number;
  offset: number;
  total: number;
}

/** A simple, keyboard-accessible previous/next pager driven directly by
 * the API's own {limit, offset, total} envelope (Milestone 12 section
 * 27) — every list endpoint returns this exact shape, so one component
 * covers customers, products, invoices, and users. */
export function Pagination({
  pagination,
  onOffsetChange,
}: {
  pagination: PaginationInfo;
  onOffsetChange: (offset: number) => void;
}) {
  const { limit, offset, total } = pagination;
  if (total === 0) return null;

  const from = offset + 1;
  const to = Math.min(offset + limit, total);
  const hasPrevious = offset > 0;
  const hasNext = to < total;

  return (
    <div className="flex items-center justify-between border-t border-slate-200 px-4 py-3 text-sm text-slate-600">
      <p>
        Showing <span className="font-medium text-slate-900">{from}</span>–
        <span className="font-medium text-slate-900">{to}</span> of{" "}
        <span className="font-medium text-slate-900">{total}</span>
      </p>
      <div className="flex gap-2">
        <Button
          variant="secondary"
          size="sm"
          disabled={!hasPrevious}
          onClick={() => onOffsetChange(Math.max(0, offset - limit))}
        >
          Previous
        </Button>
        <Button
          variant="secondary"
          size="sm"
          disabled={!hasNext}
          onClick={() => onOffsetChange(offset + limit)}
        >
          Next
        </Button>
      </div>
    </div>
  );
}
