import * as React from "react";
import * as AlertDialog from "@radix-ui/react-alert-dialog";
import { Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/api/client";
import { cn } from "@/lib/utils";
import { Button } from "./button";

/** Native checkbox styled for the app, with indeterminate support. */
export function Checkbox({
  checked,
  indeterminate,
  onCheckedChange,
  className,
  ...p
}: Omit<React.InputHTMLAttributes<HTMLInputElement>, "type" | "onChange"> & {
  checked: boolean;
  indeterminate?: boolean;
  onCheckedChange: (checked: boolean) => void;
}) {
  const ref = React.useRef<HTMLInputElement>(null);
  React.useEffect(() => {
    if (ref.current) ref.current.indeterminate = !!indeterminate;
  }, [indeterminate]);
  return (
    <input
      ref={ref}
      type="checkbox"
      checked={checked}
      onChange={(e) => onCheckedChange(e.target.checked)}
      onClick={(e) => e.stopPropagation()}
      className={cn("size-4 shrink-0 cursor-pointer rounded accent-primary", className)}
      {...p}
    />
  );
}

/**
 * Selection over the currently visible items. Items hidden by a filter or
 * removed from the server drop out of `selected`, so bulk actions only touch
 * what the user can see.
 */
export function useSelection(keys: string[]) {
  const [picked, setPicked] = React.useState<Set<string>>(() => new Set());
  const selected = keys.filter((k) => picked.has(k));
  const all = keys.length > 0 && selected.length === keys.length;
  return {
    selected,
    all,
    some: selected.length > 0 && !all,
    has: (k: string) => picked.has(k),
    toggle: (k: string, on: boolean) =>
      setPicked((s) => {
        const n = new Set(s);
        if (on) n.add(k);
        else n.delete(k);
        return n;
      }),
    setAll: (on: boolean) =>
      setPicked((s) => {
        const n = new Set(s);
        keys.forEach((k) => (on ? n.add(k) : n.delete(k)));
        return n;
      }),
    remove: (ks: string[]) =>
      setPicked((s) => {
        const n = new Set(s);
        ks.forEach((k) => n.delete(k));
        return n;
      }),
    clear: () => setPicked(new Set()),
  };
}

export type Selection = ReturnType<typeof useSelection>;

/**
 * Toolbar above a list: select all, count, and "Delete selected". Items with
 * dependents are listed and skipped, since the API refuses to delete them.
 */
export function BulkBar({
  selection,
  total,
  noun,
  dependents,
  inUseWarning,
  onDelete,
  showSelectAll = true,
  className,
  children,
}: {
  showSelectAll?: boolean;
  selection: Selection;
  total: number;
  noun: string;
  dependents?: (name: string) => string[];
  /** Set to allow deleting items in use; shown as the warning. */
  inUseWarning?: string;
  onDelete: (name: string) => Promise<unknown>;
  className?: string;
  children?: React.ReactNode;
}) {
  const n = selection.selected.length;
  const plural = (c: number) => `${c} ${noun}${c === 1 ? "" : "s"}`;
  return (
    <div className={cn("flex min-h-8 flex-wrap items-center gap-2", className)}>
      {showSelectAll ? (
        <label className="flex cursor-pointer items-center gap-2 text-sm text-muted-foreground select-none">
          <Checkbox
            checked={selection.all}
            indeterminate={selection.some}
            onCheckedChange={selection.setAll}
            disabled={total === 0}
            aria-label="Select all"
          />
          {n > 0 ? <span className="font-medium text-foreground">{n} selected</span> : "Select all"}
        </label>
      ) : (
        n > 0 && <span className="text-sm font-medium">{n} selected</span>
      )}
      {n > 0 && (
        <Button variant="ghost" size="sm" onClick={selection.clear}>
          <X /> Clear
        </Button>
      )}
      {/* Always mounted, so the dialog survives its items vanishing mid-delete. */}
      <BulkDeleteDialog
        names={selection.selected}
        plural={plural}
        dependents={dependents}
        inUseWarning={inUseWarning}
        onDelete={onDelete}
        onDone={selection.remove}
      />
      {children}
    </div>
  );
}

function BulkDeleteDialog({
  names,
  plural,
  dependents,
  inUseWarning,
  onDelete,
  onDone,
}: {
  names: string[];
  plural: (n: number) => string;
  dependents?: (name: string) => string[];
  inUseWarning?: string;
  onDelete: (name: string) => Promise<unknown>;
  onDone: (deleted: string[]) => void;
}) {
  const [open, setOpen] = React.useState(false);
  const [progress, setProgress] = React.useState<number | null>(null);
  const [count, setCount] = React.useState(0);
  const inUse = names.map((name) => ({ name, deps: dependents?.(name) ?? [] })).filter((b) => b.deps.length);
  const deletable = inUseWarning ? names : names.filter((name) => !inUse.some((b) => b.name === name));
  const running = progress !== null;

  const run = async () => {
    const done: string[] = [];
    const failed: string[] = [];
    setCount(deletable.length);
    setProgress(0);
    // One at a time: the API rebuilds the mapfile after each delete.
    for (const name of deletable) {
      try {
        await onDelete(name);
        done.push(name);
      } catch (e) {
        failed.push(name);
        toast.error(`${name}: ${e instanceof ApiError ? e.full : String(e)}`);
      }
      setProgress((p) => (p ?? 0) + 1);
    }
    setProgress(null);
    setOpen(false);
    onDone(done);
    if (done.length) toast.success(`${plural(done.length)} deleted`);
    if (failed.length) toast.warning(`${plural(failed.length)} not deleted`);
  };

  const preview = deletable.slice(0, 8);
  return (
    <AlertDialog.Root open={open} onOpenChange={(o) => !running && setOpen(o)}>
      <AlertDialog.Trigger asChild>
        <Button variant="destructive" size="sm" className={cn(!names.length && !open && "hidden")}>
          <Trash2 /> Delete selected
        </Button>
      </AlertDialog.Trigger>
      <AlertDialog.Portal>
        <AlertDialog.Overlay className="fixed inset-0 z-50 bg-black/40 backdrop-blur-[2px]" />
        <AlertDialog.Content className="fixed left-1/2 top-1/2 z-50 grid w-[calc(100%-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 gap-4 rounded-xl border bg-card p-6 shadow-xl">
          <AlertDialog.Title className="text-lg font-semibold">Delete {plural(running ? count : deletable.length)}?</AlertDialog.Title>
          <AlertDialog.Description className="text-sm text-muted-foreground">
            {deletable.length > 0 ? (
              <>
                {preview.join(", ")}
                {deletable.length > preview.length && ` and ${deletable.length - preview.length} more`}. This cannot be
                undone.
              </>
            ) : (
              "Nothing to delete: every selected item is in use."
            )}
          </AlertDialog.Description>
          {inUse.length > 0 && (
            <div className="max-h-48 overflow-y-auto rounded-md border border-warning/40 bg-warning/10 p-3 text-sm">
              <p className="font-medium">
                {inUseWarning ? "Still in use" : "Skipped, still in use"} ({inUse.length}):
              </p>
              <ul className="mt-1 space-y-0.5 text-muted-foreground">
                {inUse.map((b) => (
                  <li key={b.name}>
                    <span className="text-foreground">{b.name}</span> · used by {b.deps.join(", ")}
                  </li>
                ))}
              </ul>
              {inUseWarning && <p className="mt-2">{inUseWarning}</p>}
            </div>
          )}
          <div className="flex justify-end gap-2">
            <AlertDialog.Cancel asChild>
              <Button variant="outline" disabled={running}>
                Cancel
              </Button>
            </AlertDialog.Cancel>
            <Button
              variant="destructive"
              disabled={deletable.length === 0 || running}
              onClick={(e) => {
                e.preventDefault();
                run();
              }}
            >
              {running ? `Deleting ${progress}/${count}…` : `Delete ${plural(deletable.length)}`}
            </Button>
          </div>
        </AlertDialog.Content>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
