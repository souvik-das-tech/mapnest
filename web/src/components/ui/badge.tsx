import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const badgeVariants = cva("inline-flex h-5 items-center gap-1 rounded-md px-1.5 text-[11px] font-medium whitespace-nowrap", {
  variants: {
    variant: {
      default: "bg-accent text-accent-foreground",
      secondary: "bg-muted text-muted-foreground",
      success: "bg-success/15 text-success",
      warning: "bg-warning/20 text-foreground",
      destructive: "bg-destructive/15 text-destructive",
      outline: "border text-muted-foreground",
    },
  },
  defaultVariants: { variant: "default" },
});

export function Badge({
  className,
  variant,
  ...p
}: React.HTMLAttributes<HTMLSpanElement> & VariantProps<typeof badgeVariants>) {
  return <span className={cn(badgeVariants({ variant }), className)} {...p} />;
}

export function StatusDot({ ok, className }: { ok: boolean | undefined; className?: string }) {
  return (
    <span
      className={cn(
        "inline-block size-2 shrink-0 rounded-full",
        ok === undefined ? "bg-muted-foreground/40" : ok ? "bg-success" : "bg-destructive",
        className,
      )}
    />
  );
}
