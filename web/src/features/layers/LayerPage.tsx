import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import {
  ArrowLeft,
  Code2,
  Info,
  Loader2,
  Map as MapIcon,
  Palette,
  Pencil,
  Plus,
  RefreshCw,
  Save,
  Settings,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/api/client";
import {
  useDeleteLayer,
  useLayer,
  useLayerStyles,
  useRefreshLayer,
  useRetryRasterCache,
  useSetLayerStyles,
  useStyles,
  useUpdateLayer,
} from "@/api/hooks";
import type { Layer, RasterSource, VectorSource } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm";
import { CopyButton } from "@/components/ui/copy-button";
import { ErrorBox, PageHeader } from "@/components/ui/empty-state";
import { Input, NativeSelect, Textarea } from "@/components/ui/input";
import { Field } from "@/components/ui/label";
import { Skeleton, Switch, Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/misc";
import { MapPreview } from "@/lib/ol/MapPreview";
import { publicWmsUrl, useCapabilities } from "@/lib/ol/wms";
import { formatBytes } from "@/lib/utils";
import { layerIcon, layerTypeLabel } from "./LayersPage";
import { SridPicker } from "./SridPicker";

export function LayerPage() {
  const { name = "" } = useParams();
  const layer = useLayer(name);
  const refresh = useRefreshLayer(name);
  const del = useDeleteLayer();
  const navigate = useNavigate();

  if (layer.isLoading) return <Skeleton className="h-96" />;
  if (layer.isError) return <ErrorBox error={layer.error} />;
  const l = layer.data!;
  const Icon = layerIcon(l);

  const doRefresh = async () => {
    try {
      const r = await refresh.mutateAsync();
      toast.success("Metadata re-read from the source");
      r.warnings?.forEach((w) => toast.warning(w));
    } catch (e) {
      toast.error(e instanceof ApiError ? e.full : String(e));
    }
  };

  const remove = async () => {
    try {
      await del.mutateAsync(name);
      toast.success(`Layer “${name}” deleted`);
      navigate("/layers");
    } catch (e) {
      toast.error(e instanceof ApiError ? e.full : String(e));
    }
  };

  return (
    <>
      <Button variant="ghost" size="sm" asChild className="mb-2 -ml-2">
        <Link to="/layers">
          <ArrowLeft /> Layers
        </Link>
      </Button>
      <PageHeader
        title={
          <span className="flex flex-wrap items-center gap-3">
            <Icon className="size-6" />
            {l.title || l.name}
            <Badge variant="secondary">{layerTypeLabel(l)}</Badge>
            {!l.enabled && <Badge variant="warning">Disabled</Badge>}
          </span>
        }
        description={
          <>
            <span className="font-mono">{l.name}</span> from store{" "}
            <Link className="text-primary hover:underline" to={`/stores/${encodeURIComponent(l.store)}`}>
              {l.store}
            </Link>
          </>
        }
        actions={
          <>
            <Button variant="outline" asChild>
              <Link to={`/map?layers=${encodeURIComponent(l.name)}`}>
                <MapIcon /> Open in viewer
              </Link>
            </Button>
            <Button variant="outline" onClick={doRefresh} disabled={refresh.isPending}>
              <RefreshCw className={refresh.isPending ? "animate-spin" : ""} /> Refresh
            </Button>
            <ConfirmDialog
              title={`Delete layer “${name}”?`}
              description="The layer disappears from WMS/WFS. The source table or file is not touched."
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

      <div className="grid gap-4 lg:grid-cols-5">
        <Card className="overflow-hidden lg:col-span-3">
          {l.enabled ? (
            <MapPreview
              layer={l.name}
              version={`${l.updated_at}|${l.styles.join(",")}`}
              className="h-[460px] w-full bg-muted"
            />
          ) : (
            <div className="flex h-[460px] items-center justify-center bg-muted text-sm text-muted-foreground">
              Enable the layer to preview it.
            </div>
          )}
        </Card>

        <div className="lg:col-span-2">
          <Tabs defaultValue="info">
            <TabsList>
              <TabsTrigger value="info">
                <Info /> Info
              </TabsTrigger>
              <TabsTrigger value="settings">
                <Settings /> Settings
              </TabsTrigger>
              <TabsTrigger value="styles">
                <Palette /> Styles
              </TabsTrigger>
            </TabsList>
            <TabsContent value="info">
              <InfoCard layer={l} />
            </TabsContent>
            <TabsContent value="settings">
              <SettingsCard layer={l} />
              <ProjectionCard layer={l} />
            </TabsContent>
            <TabsContent value="styles">
              <StylesCard layer={l} />
            </TabsContent>
          </Tabs>
        </div>
      </div>

      <Snippets layer={l} />
    </>
  );
}

function InfoCard({ layer: l }: { layer: Layer }) {
  const caps = useCapabilities();
  const bbox = caps.data?.get(l.name)?.bbox4326;
  const rows: [string, React.ReactNode][] = [];
  if (l.kind === "vector") {
    const s = l.source as VectorSource;
    rows.push(
      ["Table", `${s.schema}.${s.table}`],
      ["Geometry", `${s.geom_type} (${s.geom_column})`],
      ["Unique id", s.pk],
      ["SRID", s.declared_srid ? `EPSG:${s.srid} (set by you; the column's own: ${s.column_srid ? `EPSG:${s.column_srid}` : "none"})` : `EPSG:${s.srid}`],
    );
  } else {
    const s = l.source as RasterSource;
    rows.push(
      ["File", <span className="break-all font-mono text-xs">{s.path}</span>],
      ["Bands", `${s.bands} × ${s.dtype}`],
      ["CRS", s.declared_srid ? `EPSG:${s.srid} (set by you; the file's own: ${s.file_srid ? `EPSG:${s.file_srid}` : "custom"})` : s.srid ? `EPSG:${s.srid}` : "custom"],
      ["No-data", s.nodata ?? "none"],
    );
    if (s.not_cog) rows.push(["Fast copy", <FastCopyStatus layer={l} src={s} />]);
  }
  rows.push(["WFS", l.wfs_enabled ? "enabled" : "off"]);
  if (bbox) rows.push(["Lon/lat bounds", <span className="font-mono text-xs">{bbox.map((n) => n.toFixed(4)).join(", ")}</span>]);
  return (
    <Card>
      <CardContent className="p-4">
        <dl className="grid grid-cols-[7rem_1fr] gap-x-4 gap-y-2 text-sm">
          {rows.map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="text-muted-foreground">{k}</dt>
              <dd className="min-w-0">{v}</dd>
            </div>
          ))}
        </dl>
        {l.abstract && <p className="mt-4 text-sm text-muted-foreground">{l.abstract}</p>}
      </CardContent>
    </Card>
  );
}

function SettingsCard({ layer: l }: { layer: Layer }) {
  const update = useUpdateLayer(l.name);
  const [f, setF] = useState({
    title: l.title,
    abstract: l.abstract,
    enabled: l.enabled,
    wfs_enabled: l.wfs_enabled,
    min_scale: l.min_scale?.toString() ?? "",
    max_scale: l.max_scale?.toString() ?? "",
  });
  useEffect(() => {
    setF({
      title: l.title,
      abstract: l.abstract,
      enabled: l.enabled,
      wfs_enabled: l.wfs_enabled,
      min_scale: l.min_scale?.toString() ?? "",
      max_scale: l.max_scale?.toString() ?? "",
    });
  }, [l]);

  const save = async () => {
    try {
      await update.mutateAsync({
        title: f.title,
        abstract: f.abstract,
        enabled: f.enabled,
        wfs_enabled: f.wfs_enabled,
        min_scale: f.min_scale === "" ? null : Number(f.min_scale),
        max_scale: f.max_scale === "" ? null : Number(f.max_scale),
      });
      toast.success("Layer saved");
    } catch (e) {
      toast.error(e instanceof ApiError ? e.full : String(e));
    }
  };

  return (
    <Card>
      <CardContent className="grid gap-4 p-4">
        <Field label="Title">
          <Input value={f.title} onChange={(e) => setF({ ...f, title: e.target.value })} />
        </Field>
        <Field label="Description">
          <Textarea value={f.abstract} onChange={(e) => setF({ ...f, abstract: e.target.value })} />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Min scale 1:" hint="Hidden when zoomed in further">
            <Input type="number" min={0} value={f.min_scale} onChange={(e) => setF({ ...f, min_scale: e.target.value })} />
          </Field>
          <Field label="Max scale 1:" hint="Hidden when zoomed out further">
            <Input type="number" min={0} value={f.max_scale} onChange={(e) => setF({ ...f, max_scale: e.target.value })} />
          </Field>
        </div>
        <label className="flex items-center gap-3 text-sm">
          <Switch checked={f.enabled} onCheckedChange={(v) => setF({ ...f, enabled: v })} /> Enabled (served over WMS)
        </label>
        {l.kind === "vector" && (
          <label className="flex items-center gap-3 text-sm">
            <Switch checked={f.wfs_enabled} onCheckedChange={(v) => setF({ ...f, wfs_enabled: v })} /> WFS (feature
            download)
          </label>
        )}
        <div>
          <Button onClick={save} disabled={update.isPending}>
            <Save /> Save
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

/** Ready-to-copy snippets for common clients (PLAN.md §11.3). */
function Snippets({ layer: l }: { layer: Layer }) {
  const url = publicWmsUrl(l.wms_url);
  const getMap = `${url}?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=${l.name}&STYLES=&CRS=EPSG:3857&BBOX={bbox}&WIDTH=256&HEIGHT=256&FORMAT=image/png&TRANSPARENT=TRUE`;
  const snippets: { id: string; label: string; code: string }[] = [
    {
      id: "ol",
      label: "OpenLayers",
      code: `import ImageLayer from "ol/layer/Image";
import ImageWMS from "ol/source/ImageWMS";

const layer = new ImageLayer({
  source: new ImageWMS({
    url: "${url}",
    params: { LAYERS: "${l.name}", STYLES: "" },
    serverType: "mapserver",
  }),
});
map.addLayer(layer);`,
    },
    {
      id: "leaflet",
      label: "Leaflet",
      code: `L.tileLayer.wms("${url}", {
  layers: "${l.name}",
  styles: "",
  format: "image/png",
  transparent: true,
}).addTo(map);`,
    },
    {
      id: "qgis",
      label: "QGIS",
      code: `Layer → Add Layer → Add WMS/WMTS Layer… → New
  Name: MapServer
  URL:  ${url}
Connect, then pick “${l.title || l.name}”.`,
    },
    { id: "wms", label: "WMS URL", code: getMap },
  ];
  if (l.kind === "vector" && l.wfs_enabled) {
    snippets.push({
      id: "wfs",
      label: "WFS (GeoJSON)",
      code: `${url}?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetFeature&TYPENAMES=${l.name}&OUTPUTFORMAT=geojson&COUNT=100`,
    });
  }

  return (
    <Card className="mt-4">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Code2 className="size-4" /> Use this layer
        </CardTitle>
        <CardDescription>Copy a snippet for your web app or GIS client.</CardDescription>
      </CardHeader>
      <CardContent>
        <Tabs defaultValue="ol">
          <TabsList className="flex-wrap">
            {snippets.map((s) => (
              <TabsTrigger key={s.id} value={s.id}>
                {s.label}
              </TabsTrigger>
            ))}
          </TabsList>
          {snippets.map((s) => (
            <TabsContent key={s.id} value={s.id}>
              <div className="relative">
                <pre className="overflow-x-auto rounded-md bg-muted p-4 pr-24 font-mono text-xs leading-relaxed">
                  {s.code}
                </pre>
                <div className="absolute right-2 top-2">
                  <CopyButton text={s.code} />
                </div>
              </div>
            </TabsContent>
          ))}
        </Tabs>
      </CardContent>
    </Card>
  );
}

/** Default + alternate styles (WMS STYLES=), PLAN.md §6.2. */
function StylesCard({ layer: l }: { layer: Layer }) {
  const assigned = useLayerStyles(l.name);
  const styles = useStyles();
  const setStyles = useSetLayerStyles(l.name);
  const [def, setDef] = useState("");
  const [alts, setAlts] = useState<string[]>([]);

  useEffect(() => {
    if (assigned.data) {
      setDef(assigned.data.default);
      setAlts(assigned.data.alternates);
    }
  }, [assigned.data]);

  const compatible = (styles.data ?? []).filter((s) => s.target === l.kind);
  const unusedAlts = compatible.filter((s) => s.name !== def && !alts.includes(s.name));

  const save = async () => {
    try {
      await setStyles.mutateAsync({ default: def, alternates: def ? alts.filter((a) => a !== def) : [] });
      toast.success("Styles saved");
    } catch (e) {
      toast.error(e instanceof ApiError ? e.full : String(e));
    }
  };

  return (
    <Card>
      <CardContent className="grid gap-4 p-4">
        <Field label="Default style" hint="Used when a client sends an empty STYLES=">
          <NativeSelect value={def} onChange={(e) => setDef(e.target.value)}>
            <option value="">Built-in default ({layerTypeLabel(l).toLowerCase()})</option>
            {compatible.map((s) => (
              <option key={s.name} value={s.name}>
                {s.name}
              </option>
            ))}
          </NativeSelect>
        </Field>

        {def && (
          <div className="grid gap-2">
            <div className="text-sm font-medium">Alternate styles</div>
            <p className="text-xs text-muted-foreground">Selectable with WMS STYLES=&lt;name&gt; and in the map viewer.</p>
            {alts.map((a) => (
              <div key={a} className="flex items-center justify-between rounded-md border px-3 py-1.5 text-sm">
                {a}
                <Button size="sm" variant="ghost" onClick={() => setAlts(alts.filter((x) => x !== a))}>
                  <Trash2 />
                </Button>
              </div>
            ))}
            {unusedAlts.length > 0 && (
              <NativeSelect
                value=""
                onChange={(e) => e.target.value && setAlts([...alts, e.target.value])}
                className="h-8"
              >
                <option value="">＋ Add alternate…</option>
                {unusedAlts.map((s) => (
                  <option key={s.name} value={s.name}>
                    {s.name}
                  </option>
                ))}
              </NativeSelect>
            )}
          </div>
        )}

        <div className="flex flex-wrap gap-2">
          <Button onClick={save} disabled={setStyles.isPending}>
            <Save /> Save
          </Button>
          {def ? (
            <Button variant="outline" asChild>
              <Link to={`/styles/${encodeURIComponent(def)}?layer=${encodeURIComponent(l.name)}`}>
                <Pencil /> Edit {def}
              </Link>
            </Button>
          ) : (
            <Button variant="outline" asChild>
              <Link to={`/styles/new?layer=${encodeURIComponent(l.name)}`}>
                <Plus /> New style for this layer
              </Link>
            </Button>
          )}
        </div>
        {compatible.length === 0 && (
          <p className="text-xs text-muted-foreground">
            No {l.kind} styles yet.{" "}
            <Link to="/styles" className="text-primary hover:underline">
              Create or import one
            </Link>
            .
          </p>
        )}
      </CardContent>
    </Card>
  );
}

/**
 * Change the layer's coordinate system at any time, like GeoServer's
 * "Declared SRS" with "Force declared". The source is not changed.
 */
function ProjectionCard({ layer: l }: { layer: Layer }) {
  const update = useUpdateLayer(l.name);
  const vector = l.kind === "vector" ? (l.source as VectorSource) : null;
  const raster = l.kind === "raster" ? (l.source as RasterSource) : null;
  const current = (vector ?? raster)?.srid ?? 0;
  const declared = !!(vector ?? raster)?.declared_srid;
  // The source's own SRID: the column's or the file's (0 = none / custom).
  const own = declared ? (vector ? (vector.column_srid ?? 0) : (raster?.file_srid ?? 0)) : current;
  const [srid, setSrid] = useState(String(current || ""));
  const [suggest, setSuggest] = useState(false);
  useEffect(() => setSrid(String(current || "")), [current]);
  const valid = /^[1-9][0-9]{3,5}$/.test(srid);

  const apply = async (value: number) => {
    try {
      await update.mutateAsync({ srid: value });
      toast.success(value === 0 ? "Using the source's own coordinate system" : `Coordinate system set to EPSG:${value}`);
      setSuggest(false);
    } catch (e) {
      toast.error(e instanceof ApiError ? e.full : String(e));
    }
  };

  return (
    <Card className="mt-4">
      <CardContent className="grid gap-3 p-4">
        <div>
          <div className="text-sm font-medium">Coordinate system</div>
          <p className="text-xs text-muted-foreground">
            {declared
              ? `Set to EPSG:${current} here. The ${vector ? "column" : "file"}'s own: ${own ? `EPSG:${own}` : "none"}.`
              : `EPSG:${current || "?"}, from the ${vector ? "geometry column" : "file"}.`}{" "}
            Change it if the data shows in the wrong place. Your {vector ? "database" : "file"} is not changed.
          </p>
        </div>
        <div className="flex flex-wrap items-end gap-2">
          <Field label="EPSG code" error={srid && !valid ? "A number like 4326" : undefined} className="w-36">
            <Input value={srid} onChange={(e) => setSrid(e.target.value.replace(/\D/g, ""))} inputMode="numeric" />
          </Field>
          <Button onClick={() => apply(Number(srid))} disabled={!valid || Number(srid) === current || update.isPending}>
            <Save /> Apply
          </Button>
          {vector && (
            <Button variant="outline" onClick={() => setSuggest((v) => !v)}>
              {suggest ? "Hide suggestions" : "Suggest"}
            </Button>
          )}
          {declared && own > 0 && (
            <Button variant="ghost" onClick={() => apply(0)} disabled={update.isPending}>
              Use the {vector ? "column" : "file"}'s own (EPSG:{own})
            </Button>
          )}
        </div>
        {suggest && vector && (
          <SridPicker
            request={{ store: l.store, schema: vector.schema, table: vector.table, geom_column: vector.geom_column }}
            value={srid}
            onChange={setSrid}
          />
        )}
      </CardContent>
    </Card>
  );
}

/** Status of the Cloud Optimized copy MapNest makes of a non-COG raster. */
function FastCopyStatus({ layer: l, src }: { layer: Layer; src: RasterSource }) {
  const retry = useRetryRasterCache(l.name);
  const again = (
    <Button
      variant="link"
      size="sm"
      className="h-auto p-0"
      disabled={retry.isPending}
      onClick={() => retry.mutate(undefined, { onError: (e) => toast.error(e instanceof ApiError ? e.full : String(e)) })}
    >
      {src.cache_status === "failed" ? "Retry" : "Make again"}
    </Button>
  );
  switch (src.cache_status) {
    case "building":
      return (
        <span className="flex items-center gap-1.5">
          <Loader2 className="size-3.5 animate-spin" /> Making a Cloud Optimized copy…{" "}
          <span className="text-xs text-muted-foreground">
            {src.cache_file ? "the previous copy is used until then" : "zoomed-out maps are slow until it is ready"}
          </span>
        </span>
      );
    case "ready":
      return (
        <span>
          Ready{src.cache_bytes ? ` (${formatBytes(src.cache_bytes)})` : ""}, maps use it.{" "}
          <span className="text-xs text-muted-foreground">The original file is not changed.</span> {again}
        </span>
      );
    case "failed":
      return (
        <span className="text-destructive">
          Failed: <span className="break-words text-xs">{src.cache_error}</span> {again}
        </span>
      );
    default:
      return <span className="text-muted-foreground">Waiting… {again}</span>;
  }
}
