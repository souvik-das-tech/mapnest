import * as React from "react";
import * as LabelPrimitive from "@radix-ui/react-label";
import { cn } from "@/lib/utils";

export function Label({ className, ...p }: React.ComponentProps<typeof LabelPrimitive.Root>) {
  return <LabelPrimitive.Root className={cn("text-[13px] font-medium leading-none", className)} {...p} />;
}

/** Label + control + hint or error: the standard form row. */
export function Field({
  label,
  hint,
  error,
  children,
  htmlFor,
  className,
}: {
  label: string;
  hint?: string;
  error?: string;
  children: React.ReactNode;
  htmlFor?: string;
  className?: string;
}) {
  return (
    // content-start: in a row with a taller neighbour (e.g. one with a hint), keep
    // label and control at the top instead of spreading them apart.
    <div className={cn("grid content-start gap-1.5", className)}>
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
      {error ? (
        <p className="text-[11.5px] text-destructive">{error}</p>
      ) : hint ? (
        <p className="text-[11.5px] text-muted-foreground">{hint}</p>
      ) : null}
    </div>
  );
}
