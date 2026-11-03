import { useRef, useState } from "react";
import { Check, Code2, Copy, Image, Trash2, Upload } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/api/client";
import { useDeleteIcon, useIcons, useUploadIcon } from "@/api/hooks";
import { AuthImage } from "@/components/ui/auth-image";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { BulkBar, Checkbox, useSelection } from "@/components/ui/bulk-delete";
import { Card, CardContent } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm";
import { EmptyState, ErrorBox, PageHeader } from "@/components/ui/empty-state";
import { Pagination, usePagination } from "@/components/ui/pagination";
import { Skeleton } from "@/components/ui/misc";
import { cn, copyText } from "@/lib/utils";

const PAGE_SIZES = [24, 48, 96, 192];
const ICON_IN_USE = "Those styles will be taken off their layers, which switch to the default style. Re-upload the icon to use them again.";

/** Icon gallery with multi-file drag-and-drop upload (PLAN.md §11.3 Icons). */
export function IconsPage() {
  const icons = useIcons();
  const upload = useUploadIcon();
  const del = useDeleteIcon();
  const input = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);
  const [busy, setBusy] = useState(0);
  const selection = useSelection((icons.data ?? []).map((i) => i.name));
  const paging = usePagination(icons.data ?? [], { sizes: PAGE_SIZES });

  const uploadFiles = async (files: FileList | File[]) => {
    const list = Array.from(files).filter((f) => f.type === "image/png" || f.name.toLowerCase().endsWith(".png"));
    if (!list.length) {
      toast.error("Only PNG files are supported");
      return;
    }
    setBusy(list.length);
    let ok = 0;
    for (const file of list) {
      try {
        await upload.mutateAsync({ file });
        ok++;
      } catch (e) {
        toast.error(`${file.name}: ${e instanceof ApiError ? e.full : String(e)}`);
      }
      setBusy((b) => b - 1);
    }
    if (ok) toast.success(`${ok} icon${ok > 1 ? "s" : ""} uploaded`);
  };

  const remove = async (name: string) => {
    try {
      await del.mutateAsync(name);
      toast.success(`Icon “${name}” deleted`);
    } catch (e) {
      toast.error(e instanceof ApiError ? e.full : String(e));
    }
  };

  return (
    <div
      onDragOver={(e) => {
        e.preventDefault();
        setDragging(true);
      }}
      onDragLeave={(e) => {
        if (e.currentTarget === e.target) setDragging(false);
      }}
      onDrop={(e) => {
        e.preventDefault();
        setDragging(false);
        uploadFiles(e.dataTransfer.files);
      }}
      className="relative"
    >
      <PageHeader
        title="Icons"
        description="PNG icons for point styles. Copy an icon's path into the xlink:href of your SLD (or copy the whole ExternalGraphic)."
        actions={
          <Button onClick={() => input.current?.click()} disabled={busy > 0}>
            <Upload /> {busy > 0 ? `Uploading ${busy}…` : "Upload icons"}
          </Button>
        }
      />
      <input
        ref={input}
        type="file"
        accept="image/png"
        multiple
        hidden
        onChange={(e) => {
          if (e.target.files) uploadFiles(e.target.files);
          e.target.value = "";
        }}
      />

      {dragging && (
        <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center rounded-xl border-2 border-dashed border-primary bg-primary/5 text-sm font-medium text-primary">
          Drop PNG files to upload
        </div>
      )}

      {icons.isLoading && <Skeleton className="h-48" />}
      {icons.isError && <ErrorBox error={icons.error} />}
      {icons.data?.length === 0 && (
        <EmptyState
          icon={<Image />}
          title="No icons yet"
          hint="Drag PNG files here, or use Upload. Icons up to 512×512 px."
          action={
            <Button onClick={() => input.current?.click()}>
              <Upload /> Upload icons
            </Button>
          }
        />
      )}

      {!!icons.data?.length && (
        <BulkBar
          className="mb-3"
          selection={selection}
          total={icons.data.length}
          noun="icon"
          dependents={(name) => icons.data?.find((i) => i.name === name)?.used_by ?? []}
          inUseWarning={ICON_IN_USE}
          onDelete={(name) => del.mutateAsync(name)}
        />
      )}
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
        {paging.items.map((ic) => (
          <Card
            key={ic.name}
            className={cn("group relative", selection.has(ic.name) && "border-primary ring-1 ring-primary")}
          >
            <Checkbox
              checked={selection.has(ic.name)}
              onCheckedChange={(on) => selection.toggle(ic.name, on)}
              aria-label={`Select ${ic.name}`}
              className={cn(
                "absolute left-3 top-3 z-10 opacity-0 transition-opacity group-hover:opacity-100 focus:opacity-100",
                (selection.selected.length > 0 || selection.has(ic.name)) && "opacity-100",
              )}
            />
            <CardContent className="flex flex-col items-center gap-2 p-4">
              <div className="flex size-16 items-center justify-center rounded-lg bg-[repeating-conic-gradient(var(--muted)_0_25%,transparent_0_50%)] bg-[length:12px_12px]">
                <AuthImage
                  src={`/icons/${encodeURIComponent(ic.name)}`}
                  alt={ic.name}
                  className="max-h-12 max-w-12 object-contain"
                />
              </div>
              <div className="w-full truncate text-center text-sm font-medium" title={ic.name}>
                {ic.name}
              </div>
              <div className="text-xs text-muted-foreground">
                {ic.width}×{ic.height}
              </div>
              {ic.used_by.length > 0 ? (
                <Badge variant="secondary" title={ic.used_by.join(", ")}>
                  {ic.used_by.length} style{ic.used_by.length > 1 ? "s" : ""}
                </Badge>
              ) : (
                <Badge variant="outline">unused</Badge>
              )}
              <div className="mt-1 grid w-full grid-cols-2 gap-1">
                <CopyIconButton label="Path" title="Copy the path for xlink:href" text={ic.name} />
                <CopyIconButton label="SLD" title="Copy an SLD ExternalGraphic for this icon" text={sldSnippet(ic.name, ic.height)} code />
              </div>
            </CardContent>
            <ConfirmDialog
              title={`Delete icon “${ic.name}”?`}
              dependents={ic.used_by}
              inUseWarning={ICON_IN_USE}
              onConfirm={() => remove(ic.name)}
              pending={del.isPending}
              trigger={
                <button
                  className={cn(
                    "absolute right-2 top-2 cursor-pointer rounded-md p-1 text-muted-foreground opacity-0 transition-opacity hover:bg-muted hover:text-destructive group-hover:opacity-100 focus:opacity-100",
                  )}
                  aria-label="Delete"
                >
                  <Trash2 className="size-4" />
                </button>
              }
            />
          </Card>
        ))}
      </div>
      <Pagination paging={paging} noun="icon" className="mt-4" />
    </div>
  );
}

/** The <Graphic> block that draws this icon, ready to paste into a PointSymbolizer. */
export function sldSnippet(name: string, height?: number) {
  return `<Graphic>
  <ExternalGraphic>
    <OnlineResource xlink:type="simple" xlink:href="${name}"/>
    <Format>image/png</Format>
  </ExternalGraphic>
  <Size>${height ?? 24}</Size>
</Graphic>`;
}

export function CopyIconButton({ label, title, text, code }: { label: string; title: string; text: string; code?: boolean }) {
  const [done, setDone] = useState(false);
  return (
    <Button
      variant="outline"
      size="sm"
      title={title}
      className="h-7 px-1.5"
      onClick={async () => {
        if (await copyText(text)) {
          setDone(true);
          setTimeout(() => setDone(false), 1500);
          toast.success(code ? "SLD snippet copied" : `Copied “${text}”`);
        } else {
          toast.error("Could not copy. Your browser blocked the clipboard.");
        }
      }}
    >
      {done ? <Check /> : code ? <Code2 /> : <Copy />}
      {label}
    </Button>
  );
}
