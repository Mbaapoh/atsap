// PAGE_SIZE is how many rows a list page holds.
export const PAGE_SIZE = 50;

interface PagerProps {
  // page is zero-based.
  page: number;
  // rowCount is how many rows the current page returned.
  rowCount: number;
  pageSize?: number;
  onPage: (page: number) => void;
  label: string;
}

// Pager moves through an offset-paged list. The API returns no totals, so
// it shows the page number only and enables Next only when a full page
// came back — never a fabricated "of N" (console-admin-screens D5).
export function Pager({ page, rowCount, pageSize = PAGE_SIZE, onPage, label }: PagerProps) {
  const hasPrevious = page > 0;
  const hasNext = rowCount >= pageSize;
  if (!hasPrevious && !hasNext) return null;
  return (
    <nav className="pager" aria-label={label}>
      <button type="button" className="button" disabled={!hasPrevious} onClick={() => onPage(page - 1)}>
        Previous
      </button>
      <span className="pager-page" aria-live="polite">
        Page {page + 1}
      </span>
      <button type="button" className="button" disabled={!hasNext} onClick={() => onPage(page + 1)}>
        Next
      </button>
    </nav>
  );
}
