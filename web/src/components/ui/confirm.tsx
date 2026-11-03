import * as React from "react";
import * as AlertDialog from "@radix-ui/react-alert-dialog";
import { Button } from "./button";

/**
 * Confirmation for destructive actions. `dependents` is shown before
 * confirming, e.g. "used by 3 layers" (PLAN.md §11.2). It blocks the action
 * unless `inUseWarning` says what happens to them instead.
 */
export function ConfirmDialog({
  trigger,
  title,
  description,
  dependents,
  inUseWarning,
  confirmLabel = "Delete",
  onConfirm,
  pending,
}: {
  trigger: React.ReactNode;
  title: string;
  description?: React.ReactNode;
  dependents?: string[];
  inUseWarning?: string;
  confirmLabel?: string;
  onConfirm: () => void;
  pending?: boolean;
}) {
  const inUse = (dependents?.length ?? 0) > 0;
  const blocked = inUse && !inUseWarning;
  return (
    <AlertDialog.Root>
      <AlertDialog.Trigger asChild>{trigger}</AlertDialog.Trigger>
      <AlertDialog.Portal>
        <AlertDialog.Overlay className="fixed inset-0 z-50 bg-black/40 backdrop-blur-[2px]" />
        <AlertDialog.Content className="fixed left-1/2 top-1/2 z-50 grid w-[calc(100%-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 gap-4 rounded-xl border bg-card p-6 shadow-xl">
          <AlertDialog.Title className="text-lg font-semibold">{title}</AlertDialog.Title>
          {description && (
            <AlertDialog.Description className="text-sm text-muted-foreground">{description}</AlertDialog.Description>
          )}
          {inUse && (
            <div className="rounded-md border border-warning/40 bg-warning/10 p-3 text-sm">
              <p className="font-medium">
                Used by {dependents!.length} item{dependents!.length > 1 ? "s" : ""}:
              </p>
              <p className="mt-1 text-muted-foreground">{dependents!.join(", ")}</p>
              <p className="mt-2">{inUseWarning ?? "Remove those first."}</p>
            </div>
          )}
          <div className="flex justify-end gap-2">
            <AlertDialog.Cancel asChild>
              <Button variant="outline">Cancel</Button>
            </AlertDialog.Cancel>
            <AlertDialog.Action asChild>
              <Button variant="destructive" disabled={blocked || pending} onClick={onConfirm}>
                {confirmLabel}
              </Button>
            </AlertDialog.Action>
          </div>
        </AlertDialog.Content>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
