import { useRef } from "react";
import { Link, useNavigate } from "react-router";
import { AlertTriangle, FileUp, Palette, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/api/client";
import { useDeleteStyle, useSaveStyle, useStyles } from "@/api/hooks";
import { AuthImage } from "@/components/ui/auth-image";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { BulkBar, Checkbox, useSelection } from "@/components/ui/bulk-delete";
import { Card, CardContent } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm";
import { EmptyState, ErrorBox, PageHeader } from "@/components/ui/empty-state";
import { Pagination, usePagination } from "@/components/ui/pagination";
import { Skeleton } from "@/components/ui/misc";
import { cn } from "@/lib/utils";
import { GeoServerNotes } from "./GeoServerNotes";

const PAGE_SIZES = [12, 24, 48, 96];
const STYLE_IN_USE = "Those layers will switch to their next assigned style, or to the default style.";

/** Turns "Roads (main).sld" into "roads_main". */
export function styleNameFromFile(file: string): string {
  let n = file.replace(/\.(sld|xml|json)$/i, "").replace(/[^A-Za-z0-9_]+/g, "_").replace(/^_+|_+$/g, "");
  if (!/^[A-Za-z]/.test(n)) n = "s_" + n;
  return n.toLowerCase().slice(0, 63);
}

/** Style grid with legend thumbnails (PLAN.md §11.3 Styles). */
export function StylesPage() {
  const styles = useStyles();
  const del = useDeleteStyle();
  const save = useSaveStyle();
  const navigate = useNavigate();
  const input = useRef<HTMLInputElement>(null);
  const selection = useSelection((styles.data ?? []).map((s) => s.name));
  const paging = usePagination(styles.data ?? [], { sizes: PAGE_SIZES });

  const importFiles = async (files: FileList) => {
    for (const file of Array.from(files)) {
      const name = styleNameFromFile(file.name);
      const body = await file.text();
      const format = file.name.toLowerCase().endsWith(".json") ? "json" : "sld";
      try {
        const st = await save.mutateAsync({ name, format, body, isNew: true });
        toast.success(`Style “${name}” imported`);
        st.warnings.forEach((w) => toast.warning(`${name}: ${w}`));
        if (files.length === 1) navigate(`/styles/${encodeURIComponent(name)}`);
      } catch (e) {
        toast.error(`${file.name}: ${e instanceof ApiError ? e.full : String(e)}`);
      }
    }
  };

  const remove = async (name: string) => {
    try {
      await del.mutateAsync(name);
      toast.success(`Style “${name}” deleted`);
    } catch (e) {
      toast.error(e instanceof ApiError ? e.full : String(e));
    }
  };

  return (
    <div
      onDragOver={(e) => e.preventDefault()}
      onDrop={(e) => {
        e.preventDefault();
        if (e.dataTransfer.files.length) importFiles(e.dataTransfer.files);
      }}
    >
      <PageHeader
        title="Styles"
        description="SLD styles, converted once to MapServer classes. Drop .sld files here to import."
        actions={
          <>
            <Button variant="outline" onClick={() => input.current?.click()}>
              <FileUp /> Import SLD
            </Button>
            <Button asChild>
              <Link to="/styles/new">
                <Plus /> New style
              </Link>
            </Button>
          </>
        }
      />
      <GeoServerNotes className="mb-4" />
      <input
        ref={input}
        type="file"
        accept=".sld,.xml,.json"
        multiple
        hidden
        onChange={(e) => {
          if (e.target.files) importFiles(e.target.files);
          e.target.value = "";
        }}
      />

      {styles.isLoading && <Skeleton className="h-48" />}
      {styles.isError && <ErrorBox error={styles.error} />}
      {styles.data?.length === 0 && (
        <EmptyState
          icon={<Palette />}
          title="No styles yet"
          hint="Create one in the visual editor, or import your existing GeoServer SLD files."
          action={
            <div className="flex gap-2">
              <Button variant="outline" onClick={() => input.current?.click()}>
                <FileUp /> Import SLD
              </Button>
              <Button asChild>
                <Link to="/styles/new">
                  <Plus /> New style
                </Link>
              </Button>
            </div>
          }
        />
      )}

      {!!styles.data?.length && (
        <BulkBar
          className="mb-3"
          selection={selection}
          total={styles.data.length}
          noun="style"
          dependents={(name) => styles.data?.find((s) => s.name === name)?.used_by ?? []}
          inUseWarning={STYLE_IN_USE}
          onDelete={(name) => del.mutateAsync(name)}
        />
      )}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {paging.items.map((st) => (
          <Card
            key={st.name}
            className={cn(
              "group relative transition-shadow hover:shadow-md",
              selection.has(st.name) && "border-primary ring-1 ring-primary",
            )}
          >
            <Checkbox
              checked={selection.has(st.name)}
              onCheckedChange={(on) => selection.toggle(st.name, on)}
              aria-label={`Select ${st.name}`}
              className={cn(
                "absolute left-3 top-3 z-10 opacity-0 transition-opacity group-hover:opacity-100 focus:opacity-100",
                (selection.selected.length > 0 || selection.has(st.name)) && "opacity-100",
              )}
            />
            <Link to={`/styles/${encodeURIComponent(st.name)}`}>
              <div className="flex h-28 items-center justify-center rounded-t-lg border-b bg-muted/40 p-3">
                {st.used_by.length > 0 ? (
                  <AuthImage
                    src={`/styles/${encodeURIComponent(st.name)}/legend`}
                    className="max-h-24 max-w-full object-contain"
                    fallback={<Palette className="size-8 text-muted-foreground/50" />}
                  />
                ) : (
                  <span className="text-xs text-muted-foreground">Assign to a layer to see the legend</span>
                )}
              </div>
              <CardContent className="p-4">
                <div className="flex items-center gap-2">
                  <span className="min-w-0 truncate font-medium">{st.name}</span>
                  <Badge variant="secondary">{st.format.toUpperCase()}</Badge>
                  <Badge variant="outline">{st.target}</Badge>
                </div>
                <div className="mt-2 flex items-center gap-3 text-xs text-muted-foreground">
                  <span>
                    {st.used_by.length ? `Used by ${st.used_by.join(", ")}` : "Not assigned"}
                  </span>
                  {st.warnings.length > 0 && (
                    <span className="flex items-center gap-1 text-warning" title={st.warnings.join("\n")}>
                      <AlertTriangle className="size-3.5" /> {st.warnings.length}
                    </span>
                  )}
                </div>
              </CardContent>
            </Link>
            <ConfirmDialog
              title={`Delete style “${st.name}”?`}
              dependents={st.used_by}
              inUseWarning={STYLE_IN_USE}
              onConfirm={() => remove(st.name)}
              pending={del.isPending}
              trigger={
                <button
                  className="absolute right-2 top-2 cursor-pointer rounded-md bg-card/80 p-1 text-muted-foreground opacity-0 transition-opacity hover:text-destructive group-hover:opacity-100 focus:opacity-100"
                  aria-label="Delete"
                >
                  <Trash2 className="size-4" />
                </button>
              }
            />
          </Card>
        ))}
      </div>
      <Pagination paging={paging} noun="style" className="mt-4" />
    </div>
  );
}
