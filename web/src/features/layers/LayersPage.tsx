import { useState } from "react";
import { Link, useSearchParams } from "react-router";
import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import { Grid3x3, Layers, Plus, Search, Spline, Square, Dot, X, Eye, ExternalLink, Check, Link2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/api/client";
import { useDeleteLayer, useLayers, useUpdateLayer } from "@/api/hooks";
import type { Layer, VectorSource } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { BulkBar, Checkbox, useSelection, type Selection } from "@/components/ui/bulk-delete";
import { Card } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { EmptyState, ErrorBox, PageHeader } from "@/components/ui/empty-state";
import { Input, NativeSelect } from "@/components/ui/input";
import { Skeleton, Switch, Tooltip } from "@/components/ui/misc";
import { Pagination, usePagination } from "@/components/ui/pagination";
import { MapPreview } from "@/lib/ol/MapPreview";
import { layerUrls } from "@/lib/ol/wms";
import { copyText } from "@/lib/utils";
import { inWorkspace, useWorkspaceParam, WorkspaceFilter } from "@/app/workspace";

export function layerIcon(l: Pick<Layer, "kind" | "source">) {
  if (l.kind === "raster") return Grid3x3;
  switch ((l.source as VectorSource).layer_type) {
    case "POINT":
      return Dot;
    case "LINE":
      return Spline;
    default:
      return Square;
  }
}

export function layerTypeLabel(l: Pick<Layer, "kind" | "source">) {
  if (l.kind === "raster") return "Raster";
  const t = (l.source as VectorSource).layer_type;
  return t.charAt(0) + t.slice(1).toLowerCase();
}

const PAGE_SIZES = [25, 50, 100];

export function LayersPage() {
  const layers = useLayers();
  const del = useDeleteLayer();
  const [q, setQ] = useState("");
  const [preview, setPreview] = useState<string | null>(null);
  // ?store= keeps the filter in the URL, so store pages can link here.
  const [params, setParams] = useSearchParams();
  const store = params.get("store") ?? "";
  const setStore = (s: string) =>
    setParams((p) => {
      if (s) p.set("store", s);
      else p.delete("store");
      return p;
    });
  const [current, setWorkspace] = useWorkspaceParam();
  const inWs = inWorkspace(layers.data, current);
  const storeNames = [...new Set(inWs.map((l) => l.store))].sort();

  const filtered = inWs.filter(
    (l) =>
      (!store || l.store === store) &&
      `${l.name} ${l.title} ${l.store}`.toLowerCase().includes(q.toLowerCase()),
  );

  const selection = useSelection(filtered.map((l) => l.name));
  const paging = usePagination(filtered, { sizes: PAGE_SIZES, resetKey: `${q}|${store}|${current}` });

  return (
    <>
      <PageHeader
        title="Layers"
        description="Published map layers, served over WMS (and WFS where enabled)."
        actions={
          <Button asChild>
            <Link to="/layers/new">
              <Plus /> Publish layer
            </Link>
          </Button>
        }
      />

      {layers.isLoading && <Skeleton className="h-64" />}
      {layers.isError && <ErrorBox error={layers.error} />}
      {layers.data?.length === 0 && (
        <EmptyState
          icon={<Layers />}
          title="No layers yet"
          hint="Publish a PostGIS table or a COG from one of your stores to make it a map layer."
          action={
            <Button asChild>
              <Link to="/layers/new">
                <Plus /> Publish your first layer
              </Link>
            </Button>
          }
        />
      )}

      {!!layers.data?.length && (
        <Card className="overflow-hidden">
          <div className="flex flex-wrap items-center gap-2 border-b p-3">
            <div className="relative w-full max-w-xs">
              <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input placeholder="Search layers…" value={q} onChange={(e) => setQ(e.target.value)} className="pl-8" />
            </div>
            <WorkspaceFilter
              value={current}
              onChange={(ws) => {
                // A store from another workspace would hide everything; drop it in the same URL update.
                if (store && ws && !layers.data?.some((l) => l.store === store && l.workspace === ws))
                  setParams(
                    (p) => {
                      p.set("workspace", ws);
                      p.delete("store");
                      p.delete("page");
                      return p;
                    },
                    { replace: true },
                  );
                else setWorkspace(ws);
              }}
            />
            <NativeSelect value={store} onChange={(e) => setStore(e.target.value)} className="w-auto min-w-40" aria-label="Filter by store">
              <option value="">All stores</option>
              {storeNames.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </NativeSelect>
            {(q || store || current) && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  setQ("");
                  setParams(
                    (p) => {
                      p.delete("store");
                      p.delete("workspace");
                      p.delete("page");
                      return p;
                    },
                    { replace: true },
                  );
                }}
              >
                <X /> Clear
              </Button>
            )}
            <BulkBar
              selection={selection}
              total={filtered.length}
              noun="layer"
              onDelete={(name) => del.mutateAsync(name)}
              showSelectAll={false}
            />
            <span className="ml-auto text-xs text-muted-foreground">
              {filtered.length} of {inWs.length} layers{current ? ` in ${current}` : ""}
            </span>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="bg-muted/50 text-left text-xs uppercase tracking-wide text-muted-foreground">
                <tr>
                  <th className="w-10 py-2 pl-4">
                    <Checkbox
                      checked={selection.all}
                      indeterminate={selection.some}
                      onCheckedChange={selection.setAll}
                      disabled={filtered.length === 0}
                      aria-label="Select all layers"
                    />
                  </th>
                  <th className="px-4 py-2 font-medium">Layer</th>
                  <th className="px-4 py-2 font-medium">Type</th>
                  <th className="px-4 py-2 font-medium">Store</th>
                  {!current && <th className="px-4 py-2 font-medium">Workspace</th>}
                  <th className="px-4 py-2 font-medium">Style</th>
                  <th className="px-4 py-2 text-right font-medium">Enabled</th>
                  <th className="w-24 py-2 pr-4">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {paging.items.map((l) => (
                  <LayerRow key={l.name} layer={l} selection={selection} onPreview={() => setPreview(l.name)} showWorkspace={!current} />
                ))}
              </tbody>
            </table>
            {filtered.length === 0 && (
              <p className="py-10 text-center text-sm text-muted-foreground">No layers match these filters.</p>
            )}
          </div>
          <Pagination paging={paging} noun="layer" className="border-t px-4 py-2.5" />
        </Card>
      )}

      <LayerPreviewDialog layer={layers.data?.find((l) => l.name === preview)} onClose={() => setPreview(null)} />
    </>
  );
}

/** Large live WMS preview of one layer, with a picker for its styles. */
function LayerPreviewDialog({ layer: l, onClose }: { layer?: Layer; onClose: () => void }) {
  const [style, setStyle] = useState("");
  return (
    <Dialog
      open={!!l}
      onOpenChange={(o) => {
        if (!o) {
          onClose();
          setStyle("");
        }
      }}
    >
      <DialogContent className="max-w-4xl">
        {l && (
          <>
            <DialogHeader className="pr-6">
              <DialogTitle>{l.title || l.name}</DialogTitle>
              <DialogDescription>
                {l.name} · {layerTypeLabel(l)} · {l.store}
              </DialogDescription>
            </DialogHeader>
            {l.enabled ? (
              <MapPreview
                layer={l.name}
                style={style}
                version={`${l.updated_at}|${l.styles.join(",")}`}
                className="h-[60vh] min-h-72 w-full overflow-hidden rounded-md border bg-muted"
              />
            ) : (
              <div className="flex h-[60vh] min-h-72 items-center justify-center rounded-md border bg-muted text-sm text-muted-foreground">
                This layer is disabled. Enable it to preview it.
              </div>
            )}
            <div className="flex flex-wrap items-center gap-2">
              {l.enabled && l.styles.length > 1 && (
                <NativeSelect value={style} onChange={(e) => setStyle(e.target.value)} className="w-auto min-w-40" aria-label="Style">
                  {l.styles.map((st, i) => (
                    <option key={st} value={i === 0 ? "" : st}>
                      {st}
                      {i === 0 ? " (default)" : ""}
                    </option>
                  ))}
                </NativeSelect>
              )}
              <Button asChild variant="outline" size="sm" className="ml-auto">
                <Link to={`/layers/${encodeURIComponent(l.name)}`}>
                  <ExternalLink /> Open layer
                </Link>
              </Button>
            </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

function LayerRow({
  layer: l,
  selection,
  onPreview,
  showWorkspace,
}: {
  layer: Layer;
  selection: Selection;
  onPreview: () => void;
  showWorkspace: boolean;
}) {
  const update = useUpdateLayer(l.name);
  const Icon = layerIcon(l);
  const checked = selection.has(l.name);
  return (
    <tr className={checked ? "bg-primary/5" : "hover:bg-muted/40"}>
      <td className="py-3 pl-4">
        <Checkbox checked={checked} onCheckedChange={(on) => selection.toggle(l.name, on)} aria-label={`Select ${l.name}`} />
      </td>
      <td className="px-4 py-3">
        <Link to={`/layers/${encodeURIComponent(l.name)}`} className="flex items-center gap-3">
          <span className="flex size-8 items-center justify-center rounded-md bg-accent text-accent-foreground">
            <Icon className="size-4" />
          </span>
          <span>
            <span className="block font-medium hover:underline">{l.title || l.name}</span>
            <span className="block text-xs text-muted-foreground">{l.name}</span>
          </span>
        </Link>
      </td>
      <td className="px-4 py-3">
        <Badge variant="secondary">{layerTypeLabel(l)}</Badge>
      </td>
      <td className="px-4 py-3">
        <Link to={`/stores/${encodeURIComponent(l.store)}`} className="text-muted-foreground hover:underline">
          {l.store}
        </Link>
      </td>
      {showWorkspace && (
        <td className="px-4 py-3">
          <Badge variant="outline">{l.workspace}</Badge>
        </td>
      )}
      <td className="px-4 py-3 text-muted-foreground">
        {l.styles[0] ?? "default"}
        {l.styles.length > 1 && <span className="ml-1 text-xs">+{l.styles.length - 1}</span>}
      </td>
      <td className="px-4 py-3 text-right">
        <Switch
          checked={l.enabled}
          disabled={update.isPending}
          onCheckedChange={async (enabled) => {
            try {
              await update.mutateAsync({ enabled });
              toast.success(`${l.name} ${enabled ? "enabled" : "disabled"}`);
            } catch (e) {
              toast.error(e instanceof ApiError ? e.full : String(e));
            }
          }}
        />
      </td>
      <td className="whitespace-nowrap py-3 pr-4 text-right">
        <CopyUrlMenu layer={l} />
        <Tooltip content="Preview">
          <Button variant="ghost" size="icon" onClick={onPreview} aria-label={`Preview ${l.name}`}>
            <Eye />
          </Button>
        </Tooltip>
      </td>
    </tr>
  );
}

/** Row menu that copies the layer's WMS/WFS URLs. */
function CopyUrlMenu({ layer: l }: { layer: Layer }) {
  const [copied, setCopied] = useState(false);
  const copy = async (label: string, url: string) => {
    if (await copyText(url)) {
      toast.success(`${label} copied`, { description: url.length > 90 ? url.slice(0, 90) + "…" : url });
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } else {
      toast.error("Could not copy. Your browser blocked the clipboard.");
    }
  };
  const urls = layerUrls(l);
  return (
    <DropdownMenu.Root>
      <Tooltip content="Copy WMS/WFS URL">
        <DropdownMenu.Trigger asChild>
          <Button variant="ghost" size="icon" aria-label={`Copy URLs for ${l.name}`}>
            {copied ? <Check className="text-success" /> : <Link2 />}
          </Button>
        </DropdownMenu.Trigger>
      </Tooltip>
      <DropdownMenu.Portal>
        <DropdownMenu.Content
          align="end"
          sideOffset={4}
          className="z-50 min-w-64 rounded-md border bg-card p-1 text-sm shadow-lg"
        >
          <DropdownMenu.Label className="px-2 py-1.5 text-xs font-medium text-muted-foreground">
            Copy URL for {l.name}
          </DropdownMenu.Label>
          {urls.map((u) => (
            <div key={u.id}>
              {(u.id === "wfs" || (u.id === "wms" && urls[0].id === "ws")) && <DropdownMenu.Separator className="my-1 h-px bg-border" />}
              <DropdownMenu.Item
                onSelect={() => copy(u.label, u.url)}
                className="flex cursor-pointer flex-col items-start rounded-sm px-2 py-1.5 outline-none data-[highlighted]:bg-muted"
              >
                <span className="font-medium">{u.label}</span>
                <span className="text-xs text-muted-foreground">{u.hint}</span>
              </DropdownMenu.Item>
            </div>
          ))}
          {!(l.kind === "vector" && l.wfs_enabled) && (
            <>
              <DropdownMenu.Separator className="my-1 h-px bg-border" />
              <p className="px-2 py-1.5 text-xs text-muted-foreground">
                {l.kind === "raster" ? "WFS is for vector layers only." : "WFS is off for this layer. Turn it on in the layer settings."}
              </p>
            </>
          )}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}
