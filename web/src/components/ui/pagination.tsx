import * as React from "react";
import { useSearchParams } from "react-router";
import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "./button";
import { NativeSelect } from "./input";

/**
 * Client-side paging over an already loaded list. Page and page size live in
 * the URL (?page=&size=), so reloads and back navigation keep the place.
 * Changing `resetKey` (e.g. the search text) goes back to page 1.
 */
export function usePagination<T>(items: T[], { sizes, resetKey = "" }: { sizes: number[]; resetKey?: string }) {
  const [params, setParams] = useSearchParams();
  const rawSize = Number(params.get("size"));
  const size = sizes.includes(rawSize) ? rawSize : sizes[0];
  const pages = Math.max(1, Math.ceil(items.length / size));
  const page = Math.min(Math.max(1, Number(params.get("page")) || 1), pages);

  const update = React.useCallback(
    (next: { page?: number; size?: number }) =>
      setParams(
        (p) => {
          if (next.size !== undefined) {
            if (next.size === sizes[0]) p.delete("size");
            else p.set("size", String(next.size));
          }
          if (next.page !== undefined) {
            if (next.page <= 1) p.delete("page");
            else p.set("page", String(next.page));
          }
          return p;
        },
        { replace: true },
      ),
    [setParams, sizes],
  );

  const first = React.useRef(true);
  React.useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    update({ page: 1 });
  }, [resetKey]); // eslint-disable-line react-hooks/exhaustive-deps

  const start = (page - 1) * size;
  return {
    items: items.slice(start, start + size),
    page,
    pages,
    size,
    sizes,
    total: items.length,
    start,
    setPage: (n: number) => update({ page: Math.min(Math.max(1, n), pages) }),
    // Keep the first visible item on screen when the size changes.
    setSize: (n: number) => update({ size: n, page: Math.floor(start / n) + 1 }),
  };
}

export type Paging = ReturnType<typeof usePagination>;

/** Page numbers around the current one, with gaps: 1 … 4 5 [6] 7 8 … 20 */
function pageList(page: number, pages: number): (number | "gap")[] {
  const out: (number | "gap")[] = [];
  const lo = Math.max(2, page - 1);
  const hi = Math.min(pages - 1, page + 1);
  out.push(1);
  if (lo > 2) out.push(lo === 3 ? 2 : "gap");
  for (let i = lo; i <= hi; i++) out.push(i);
  if (hi < pages - 1) out.push(hi === pages - 2 ? pages - 1 : "gap");
  if (pages > 1) out.push(pages);
  return out;
}

export function Pagination({ paging, noun, className }: { paging: Paging; noun: string; className?: string }) {
  const { page, pages, size, sizes, total, start, setPage, setSize } = paging;
  if (total === 0) return null;
  const end = Math.min(start + size, total);
  return (
    <nav aria-label="Pagination" className={cn("flex flex-wrap items-center gap-x-4 gap-y-2 text-sm", className)}>
      <span className="text-muted-foreground">
        {start + 1}–{end} of {total} {noun}
        {total === 1 ? "" : "s"}
      </span>
      <label className="flex items-center gap-2 text-muted-foreground">
        Per page
        <NativeSelect
          value={size}
          onChange={(e) => setSize(Number(e.target.value))}
          className="h-7 w-auto py-0 text-xs"
          aria-label="Items per page"
        >
          {sizes.map((n) => (
            <option key={n} value={n}>
              {n}
            </option>
          ))}
        </NativeSelect>
      </label>
      {pages > 1 && (
        <div className="ml-auto flex items-center gap-1">
          <Button variant="ghost" size="icon" className="size-7" onClick={() => setPage(1)} disabled={page === 1} aria-label="First page">
            <ChevronsLeft />
          </Button>
          <Button variant="ghost" size="icon" className="size-7" onClick={() => setPage(page - 1)} disabled={page === 1} aria-label="Previous page">
            <ChevronLeft />
          </Button>
          {pageList(page, pages).map((p, i) =>
            p === "gap" ? (
              <span key={`gap${i}`} className="px-1 text-muted-foreground">
                …
              </span>
            ) : (
              <Button
                key={p}
                variant={p === page ? "default" : "ghost"}
                size="icon"
                className="size-7 text-xs tabular-nums"
                onClick={() => setPage(p)}
                aria-label={`Page ${p}`}
                aria-current={p === page ? "page" : undefined}
              >
                {p}
              </Button>
            ),
          )}
          <Button variant="ghost" size="icon" className="size-7" onClick={() => setPage(page + 1)} disabled={page === pages} aria-label="Next page">
            <ChevronRight />
          </Button>
          <Button variant="ghost" size="icon" className="size-7" onClick={() => setPage(pages)} disabled={page === pages} aria-label="Last page">
            <ChevronsRight />
          </Button>
        </div>
      )}
    </nav>
  );
}
