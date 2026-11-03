import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { ArrowLeft, FileImage, Layers, Pencil, PlugZap, RefreshCw, Table2, Trash2, Upload } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/api/client";
import { useDeleteStore, useLayers, useMoveStore, useStore, useStoreResources, useTestStore, useWorkspaces } from "@/api/hooks";
import type { Store } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm";
import { EmptyState, ErrorBox, PageHeader } from "@/components/ui/empty-state";
import { Input, NativeSelect } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/misc";
import { formatBytes, formatNumber } from "@/lib/utils";
import { StoreWizard } from "./StoreWizard";
import { StoreIcon, storeLabel, storeSummary } from "./StoresPage";

export function StorePage() {
  const { name = "" } = useParams();
  const store = useStore(name);
  const layers = useLayers();
  const resources = useStoreResources(name, store.isSuccess);
  const test = useTestStore();
  const del = useDeleteStore();
  const navigate = useNavigate();
  const [editOpen, setEditOpen] = useState(false);
  const [filter, setFilter] = useState("");

  const dependents = layers.data?.filter((l) => l.store === name).map((l) => l.name) ?? [];
  // Which resources are already published, to label them.
  const publishedTables = new Set(
    layers.data
      ?.filter((l) => l.store === name && l.kind === "vector")
      .map((l) => {
        const s = l.source as { schema: string; table: string };
        return `${s.schema}.${s.table}`;
      }),
  );
  const publishedPaths = new Set(
    layers.data?.filter((l) => l.store === name && l.kind === "raster").map((l) => (l.source as { path: string }).path),
  );

  if (store.isLoading) return <Skeleton className="h-64" />;
  if (store.isError) return <ErrorBox error={store.error} />;
  const s = store.data!;

  const runTest = async () => {
    try {
      const r = await test.mutateAsync(name);
      if (r.ok) toast.success(`Connected in ${r.latency_ms} ms`);
      else toast.error(r.error ?? "Connection failed");
    } catch (e) {
      toast.error(e instanceof ApiError ? e.full : String(e));
    }
  };

  const remove = async () => {
    try {
      await del.mutateAsync(name);
      toast.success(`Store “${name}” deleted`);
      navigate("/stores");
    } catch (e) {
      toast.error(e instanceof ApiError ? e.full : String(e));
    }
  };

  const f = filter.toLowerCase();
  const res = resources.data;

  return (
    <>
      <Button variant="ghost" size="sm" asChild className="mb-2 -ml-2">
        <Link to="/stores">
          <ArrowLeft /> Stores
        </Link>
      </Button>
      <PageHeader
        title={
          <span className="flex items-center gap-3">
            <StoreIcon type={s.type} className="size-6" />
            {s.name}
            <Badge variant="secondary">{storeLabel(s.type)}</Badge>
          </span>
        }
        description={storeSummary(s)}
        actions={
          <>
            <MoveStore store={s} layers={dependents.length} />
            {dependents.length > 0 && (
              <Button variant="outline" asChild>
                <Link to={`/layers?store=${encodeURIComponent(name)}`}>
                  <Layers /> {dependents.length} layer{dependents.length === 1 ? "" : "s"}
                </Link>
              </Button>
            )}
            <Button variant="outline" onClick={runTest} disabled={test.isPending}>
              <PlugZap /> Test
            </Button>
            <Button variant="outline" onClick={() => setEditOpen(true)}>
              <Pencil /> Edit
            </Button>
            <ConfirmDialog
              title={`Delete store “${name}”?`}
              description="The connection settings are removed. Your database or bucket is not touched."
              dependents={dependents}
              onConfirm={remove}
              pending={del.isPending}
              trigger={
                <Button variant="outline" className="text-destructive">
                  <Trash2 /> Delete
                </Button>
              }
            />
          </>
        }
      />

      <Card>
        <CardHeader className="flex-row flex-wrap items-center justify-between gap-3">
          <div>
            <CardTitle>{s.type === "postgis" ? "Tables" : "GeoTIFF files"}</CardTitle>
            <CardDescription>
              {s.type === "postgis"
                ? "Geometry tables in this schema. Publish one to make it a map layer."
                : s.type === "local"
                  ? "GeoTIFFs in this folder and its subfolders. Publish one to make it a map layer."
                  : "GeoTIFFs under this prefix. Publish one to make it a map layer."}
            </CardDescription>
          </div>
          <div className="flex gap-2">
            <Input placeholder="Filter…" value={filter} onChange={(e) => setFilter(e.target.value)} className="w-48" />
            <Button variant="outline" size="icon" onClick={() => resources.refetch()} aria-label="Reload">
              <RefreshCw className={resources.isFetching ? "animate-spin" : ""} />
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {resources.isLoading && <Skeleton className="h-40" />}
          {resources.isError && <ErrorBox error={resources.error} />}

          {res?.type === "postgis" &&
            (res.tables.length === 0 ? (
              <EmptyState
                icon={<Table2 />}
                title="No geometry tables"
                hint="This schema has no tables with a geometry column, or the user cannot see them."
              />
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead className="text-left text-xs uppercase tracking-wide text-muted-foreground">
                    <tr className="border-b">
                      <th className="py-2 pr-4 font-medium">Table</th>
                      <th className="py-2 pr-4 font-medium">Geometry</th>
                      <th className="py-2 pr-4 font-medium">SRID</th>
                      <th className="py-2 pr-4 text-right font-medium">Rows (est.)</th>
                      <th />
                    </tr>
                  </thead>
                  <tbody className="divide-y">
                    {res.tables
                      .filter((t) => t.table.toLowerCase().includes(f))
                      .map((t) => {
                        const key = `${t.schema}.${t.table}`;
                        return (
                          <tr key={`${key}.${t.geom_column}`}>
                            <td className="py-2 pr-4">
                              <div className="font-medium">{t.table}</div>
                              <div className="text-xs text-muted-foreground">{t.geom_column}</div>
                            </td>
                            <td className="py-2 pr-4">
                              <Badge variant="outline">{t.geom_type}</Badge>
                            </td>
                            <td className="py-2 pr-4 tabular-nums">{t.srid}</td>
                            <td className="py-2 pr-4 text-right tabular-nums">{formatNumber(t.estimated_rows)}</td>
                            <td className="py-2 text-right">
                              {!t.can_select ? (
                                <Badge variant="warning">No SELECT permission</Badge>
                              ) : publishedTables.has(key) ? (
                                <Badge variant="success">Published</Badge>
                              ) : (
                                <Button size="sm" asChild>
                                  <Link
                                    to={`/layers/new?store=${encodeURIComponent(name)}&schema=${encodeURIComponent(t.schema)}&table=${encodeURIComponent(t.table)}&geom_column=${encodeURIComponent(t.geom_column)}`}
                                  >
                                    <Upload /> Publish
                                  </Link>
                                </Button>
                              )}
                            </td>
                          </tr>
                        );
                      })}
                  </tbody>
                </table>
              </div>
            ))}

          {(res?.type === "s3" || res?.type === "local") &&
            (res.objects.length === 0 ? (
              <EmptyState
                icon={<FileImage />}
                title="No GeoTIFFs found"
                hint={
                  res.type === "local"
                    ? "Copy .tif files (ideally Cloud Optimized GeoTIFF) into this folder on the server, then reload."
                    : "Upload .tif files (Cloud Optimized GeoTIFF) under this bucket and prefix, then reload."
                }
              />
            ) : (
              <>
                <ul className="divide-y">
                  {res.objects
                    .filter((o) => o.path.toLowerCase().includes(f))
                    .map((o) => (
                      <li key={o.path} className="flex items-center gap-3 py-2">
                        <FileImage className="size-4 text-muted-foreground" />
                        <span className="min-w-0 flex-1 truncate font-mono text-xs">{o.path}</span>
                        <span className="text-xs tabular-nums text-muted-foreground">{formatBytes(o.size)}</span>
                        {publishedPaths.has(o.path) ? (
                          <Badge variant="success">Published</Badge>
                        ) : (
                          <Button size="sm" asChild>
                            <Link to={`/layers/new?store=${encodeURIComponent(name)}&path=${encodeURIComponent(o.path)}`}>
                              <Upload /> Publish
                            </Link>
                          </Button>
                        )}
                      </li>
                    ))}
                </ul>
                {res.truncated && (
                  <p className="mt-3 text-xs text-muted-foreground">
                    Showing the first 1000 files. Use a prefix or subfolder to narrow the store.
                  </p>
                )}
              </>
            ))}
        </CardContent>
      </Card>

      <StoreWizard open={editOpen} onOpenChange={setEditOpen} editing={s} />
    </>
  );
}

/** Workspace picker: moving the store moves its layers to that workspace's address. */
function MoveStore({ store, layers }: { store: Store; layers: number }) {
  const workspaces = useWorkspaces();
  const move = useMoveStore();
  if ((workspaces.data?.length ?? 0) < 2) return null;
  return (
    <label className="flex items-center gap-2 text-sm text-muted-foreground">
      Workspace
      <NativeSelect
        value={store.workspace}
        disabled={move.isPending}
        className="w-auto min-w-36"
        onChange={async (e) => {
          const to = e.target.value;
          if (layers > 0 && !confirm(`Move ${store.name} and its ${layers} layer${layers === 1 ? "" : "s"} to workspace “${to}”? They will be served at that workspace's address instead.`)) return;
          try {
            await move.mutateAsync({ store: store.name, workspace: to });
            toast.success(`Moved to workspace “${to}”`);
          } catch (err) {
            toast.error(err instanceof ApiError ? err.full : String(err));
          }
        }}
      >
        {workspaces.data!.map((w) => (
          <option key={w.name} value={w.name}>
            {w.title && w.title !== w.name ? `${w.title} (${w.name})` : w.name}
          </option>
        ))}
      </NativeSelect>
    </label>
  );
}
