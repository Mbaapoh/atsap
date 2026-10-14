import type { ReactNode } from "react";

export interface Column<Row> {
  key: string;
  header: string;
  render: (row: Row) => ReactNode;
  // numeric right-aligns the column and uses tabular figures, for
  // extension numbers, channel counts, dates and durations.
  numeric?: boolean;
}

interface DataTableProps<Row> {
  // caption names the table for assistive technology; it may be visually
  // hidden when the page heading already says what the table is.
  caption: string;
  captionHidden?: boolean;
  columns: Column<Row>[];
  rows: Row[];
  rowKey: (row: Row) => string;
  // rowActions renders the per-row menu; destructive actions belong here,
  // not in the page header (CONSOLE-DESIGN "App shell").
  rowActions?: (row: Row) => ReactNode;
  empty?: ReactNode;
}

// DataTable keeps every column and scrolls horizontally inside its own
// region rather than hiding data on a narrow screen (HLD-19 §7). The
// scroll region is keyboard-focusable and named, so a keyboard user can
// scroll it too.
export function DataTable<Row>({ caption, captionHidden = false, columns, rows, rowKey, rowActions, empty }: DataTableProps<Row>) {
  if (rows.length === 0 && empty) {
    return <>{empty}</>;
  }
  // The wrapper scrolls a wide table sideways; a named, focusable region
  // is how keyboard users scroll it (WCAG 2.1.1, axe scrollable-region-
  // focusable), so tabIndex on a non-interactive element is deliberate.
  return (
    // oxlint-disable-next-line jsx-a11y/no-noninteractive-tabindex
    <div className="table-scroll" role="region" aria-label={caption} tabIndex={0}>
      <table className="data-table">
        <caption className={captionHidden ? "visually-hidden" : undefined}>{caption}</caption>
        <thead>
          <tr>
            {columns.map((c) => (
              <th key={c.key} scope="col" className={c.numeric ? "numeric-cell" : undefined}>
                {c.header}
              </th>
            ))}
            {rowActions ? (
              <th scope="col">
                <span className="visually-hidden">Actions</span>
              </th>
            ) : null}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={rowKey(row)}>
              {columns.map((c) => (
                <td key={c.key} className={c.numeric ? "numeric-cell" : undefined}>
                  {c.render(row)}
                </td>
              ))}
              {rowActions ? <td className="row-actions">{rowActions(row)}</td> : null}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
