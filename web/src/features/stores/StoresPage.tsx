import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { Cloud, Database, FolderOpen, Plus } from "lucide-react";
import { useDeleteStore, useHealth, useLayers, useStores } from "@/api/hooks";
import type { LocalConnection, PostGISConnection, S3Connection, Store, StoreType } from "@/api/types";
import { Badge, StatusDot } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { BulkBar, Checkbox, useSelection } from "@/components/ui/bulk-delete";
import { Card, CardContent } from "@/components/ui/card";
import { EmptyState, ErrorBox, PageHeader } from "@/components/ui/empty-state";
import { Pagination, usePagination } from "@/components/ui/pagination";
import { Skeleton } from "@/components/ui/misc";
import { cn } from "@/lib/utils";
import { inWorkspace, useWorkspaceParam, WorkspaceFilter } from "@/app/workspace";
import { StoreWizard } from "./StoreWizard";

const PAGE_SIZES = [12, 24, 48, 96];

export function StoresPage() {
  const allStores = useStores();
  const [current, setWorkspace] = useWorkspaceParam();
  // The list shows the filtered workspace only; queries keep their state.
  const stores = { ...allStores, data: allStores.data && inWorkspace(allStores.data, current) };
  const layers = useLayers();
  const health = useHealth(true);
  const del = useDeleteStore();
  const selection = useSelection((stores.data ?? []).map((s) => s.name));
  const paging = usePagination(stores.data ?? [], { sizes: PAGE_SIZES });
  const [params, setParams] = useSearchParams();
  const [open, setOpen] = useState(params.get("new") === "1");
  const navigate = useNavigate();

  const storeLayers = (name: string) => (layers.data ?? []).filter((l) => l.store === name).map((l) => l.name);
  const layerCount = (name: string) => storeLayers(name).length;

  const openWizard = (o: boolean) => {
    setOpen(o);
    if (!o && params.has("new"))
      setParams(
        (p) => {
          p.delete("new");
          return p;
        },
        { replace: true },
      );
  };

  return (
    <>
      <PageHeader
        title="Stores"
        description={
          current
            ? `Stores in workspace “${current}”.`
            : "Connections to your PostGIS databases, S3 buckets and local raster folders."
        }
        actions={
          <>
            <WorkspaceFilter value={current} onChange={setWorkspace} />
            <Button onClick={() => setOpen(true)}>
              <Plus /> Add store
            </Button>
          </>
        }
      />

      {stores.isLoading && (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-32" />
          ))}
        </div>
      )}
      {stores.isError && <ErrorBox error={stores.error} />}
      {stores.data?.length === 0 && (
        <EmptyState
          icon={<Database />}
          title={current ? `No stores in “${current}” yet` : "No stores yet"}
          hint="A store is a connection to your own PostGIS database, S3 bucket or local raster folder. Add one to start publishing layers."
          action={
            <Button onClick={() => setOpen(true)}>
              <Plus /> Add your first store
            </Button>
          }
        />
      )}

      {!!stores.data?.length && (
        <BulkBar
          className="mb-3"
          selection={selection}
          total={stores.data.length}
          noun="store"
          dependents={storeLayers}
          onDelete={(name) => del.mutateAsync(name)}
        />
      )}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {paging.items.map((s) => (
          <div key={s.name} className="group relative">
            <Checkbox
              checked={selection.has(s.name)}
              onCheckedChange={(on) => selection.toggle(s.name, on)}
              aria-label={`Select ${s.name}`}
              className={cn(
                "absolute left-3 top-3 z-10 opacity-0 transition-opacity group-hover:opacity-100 focus:opacity-100",
                (selection.selected.length > 0 || selection.has(s.name)) && "opacity-100",
              )}
            />
            <StoreCard
              store={s}
              layers={layerCount(s.name)}
              ok={health.data?.stores?.[s.name]?.ok}
              selected={selection.has(s.name)}
            />
          </div>
        ))}
      </div>

      <Pagination paging={paging} noun="store" className="mt-4" />

      <StoreWizard open={open} onOpenChange={openWizard} defaultWorkspace={current} onSaved={(s) => navigate(`/stores/${encodeURIComponent(s.name)}`)} />
    </>
  );
}

export function storeLabel(t: StoreType): string {
  return t === "postgis" ? "PostGIS" : t === "s3" ? "S3" : "Local folder";
}

export function StoreIcon({ type, className }: { type: StoreType; className?: string }) {
  const Icon = type === "postgis" ? Database : type === "s3" ? Cloud : FolderOpen;
  return <Icon className={className} />;
}

export function storeSummary(s: Store): string {
  if (s.type === "postgis") {
    const c = s.connection as PostGISConnection;
    return `${c.user}@${c.host}:${c.port}/${c.database} · ${c.schema}`;
  }
  if (s.type === "local") {
    const c = s.connection as LocalConnection;
    return `Local folder · ${c.path || "(whole raster folder)"}`;
  }
  const c = s.connection as S3Connection;
  return `${c.endpoint || "AWS"} · ${c.bucket}${c.prefix ? "/" + c.prefix : ""}`;
}

function StoreCard({ store, layers, ok, selected }: { store: Store; layers: number; ok?: boolean; selected?: boolean }) {
  return (
    <Link to={`/stores/${encodeURIComponent(store.name)}`} className="block h-full">
      <Card
        className={cn(
          "h-full transition-all hover:border-primary/50 hover:shadow-md",
          selected && "border-primary ring-1 ring-primary",
        )}
      >
        <CardContent className="flex flex-col gap-3 p-4">
          <div className="flex items-center gap-3 pl-6">
            <div className="flex size-8 items-center justify-center rounded-md bg-accent text-accent-foreground">
              <StoreIcon type={store.type} className="size-5" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className="min-w-0 truncate font-semibold">{store.name}</span>
                <StatusDot ok={ok} />
              </div>
              <div className="flex flex-wrap gap-1">
                <Badge variant="secondary">{storeLabel(store.type)}</Badge>
                <Badge variant="outline" title="Workspace">
                  {store.workspace}
                </Badge>
              </div>
            </div>
          </div>
          <p className="truncate text-xs text-muted-foreground" title={storeSummary(store)}>
            {storeSummary(store)}
          </p>
          <p className="text-sm text-muted-foreground">
            {layers} layer{layers === 1 ? "" : "s"}
          </p>
        </CardContent>
      </Card>
    </Link>
  );
}
