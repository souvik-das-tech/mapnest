import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { ArrowLeft, ArrowRight, Check, Database, FileImage, Loader2, Table2, Upload } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/api/client";
import { useCreateLayer, useInspectLayer, useLayers, useStoreResources, useStores } from "@/api/hooks";
import type { InspectResult, LayerInput, RasterSource, VectorSource } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { EmptyState, ErrorBox, PageHeader } from "@/components/ui/empty-state";
import { Input, NativeSelect, Textarea } from "@/components/ui/input";
import { SridPicker } from "./SridPicker";
import { Field } from "@/components/ui/label";
import { Skeleton, Switch } from "@/components/ui/misc";
import { cn, formatBytes, formatNumber } from "@/lib/utils";
import { StoreIcon, storeLabel } from "@/features/stores/StoresPage";

const steps = ["Store", "Resource", "Check", "Details"] as const;

/** Turns a table or file name into a valid layer name. */
function suggestName(s: string): string {
  const base = s.split("/").pop()!.replace(/\.(tif|tiff)$/i, "");
  let n = base.replace(/[^A-Za-z0-9_]/g, "_").replace(/_+/g, "_");
  if (!/^[A-Za-z]/.test(n)) n = "l_" + n;
  return n.slice(0, 63);
}

/**
 * store → resource → auto-detected metadata → name/title → publish.
 * PLAN.md §11.3 Layers.
 */
export function PublishWizard() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const stores = useStores();
  const layers = useLayers();
  const inspect = useInspectLayer();
  const create = useCreateLayer();

  const [store, setStore] = useState(params.get("store") ?? "");
  const [resource, setResource] = useState<Pick<LayerInput, "schema" | "table" | "geom_column" | "path">>({
    schema: params.get("schema") ?? undefined,
    table: params.get("table") ?? undefined,
    geom_column: params.get("geom_column") ?? undefined,
    path: params.get("path") ?? undefined,
  });
  const [overrides, setOverrides] = useState<Overrides>({});
  const [detected, setDetected] = useState<InspectResult | null>(null);
  const [details, setDetails] = useState({ name: "", title: "", abstract: "", wfs_enabled: false });

  const hasResource = !!(resource.table || resource.path);
  const [step, setStep] = useState(store && hasResource ? 2 : store ? 1 : 0);
  const storeObj = stores.data?.find((s) => s.name === store);
  const resources = useStoreResources(store, !!store && step === 1);
  const [hidePublished, setHidePublished] = useState(false);
  // Layers already published from each table / file of this store.
  const publishedAs = new Map<string, string[]>();
  for (const l of layers.data ?? []) {
    if (l.store !== store) continue;
    const s = l.source as { schema?: string; table?: string; path?: string };
    const key = l.kind === "vector" ? `${s.schema}.${s.table}` : (s.path ?? "");
    publishedAs.set(key, [...(publishedAs.get(key) ?? []), l.name]);
  }

  const request: LayerInput = useMemo(
    () => ({ name: details.name || "x", store, ...resource, ...overrides }),
    [details.name, store, resource, overrides],
  );

  // Step 2: inspect whenever we arrive (or overrides change).
  useEffect(() => {
    if (step !== 2) return;
    setDetected(null);
    inspect.mutate(request, {
      onSuccess: (r) => {
        setDetected(r);
        setDetails((d) => {
          if (d.name) return d;
          const base = resource.table ?? resource.path ?? "";
          return { ...d, name: suggestName(base), title: d.title || suggestName(base).replace(/_/g, " ") };
        });
      },
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step, store, resource, overrides]);

  const nameTaken = !!layers.data?.some((l) => l.name === details.name);
  const nameValid = /^[A-Za-z][A-Za-z0-9_]{0,62}$/.test(details.name);

  const publish = async () => {
    try {
      const l = await create.mutateAsync({
        ...request,
        name: details.name,
        title: details.title,
        abstract: details.abstract,
        wfs_enabled: details.wfs_enabled,
      });
      toast.success(`Layer “${l.name}” published`);
      l.warnings?.forEach((w) => toast.warning(w));
      navigate(`/layers/${encodeURIComponent(l.name)}`);
    } catch (e) {
      toast.error(e instanceof ApiError ? e.full : String(e));
    }
  };

  const inspectError = inspect.error instanceof ApiError ? inspect.error : null;
  const needsPk = inspectError?.details.some((d) => /primary key|set pk/i.test(d));
  const needsType = inspectError?.details.some((d) => /layer_type/i.test(d));
  const sridDetail = inspectError?.details.find((d) => /set srid/i.test(d));
  const suggestedSrid = Number(sridDetail?.match(/suggested srid (\d+)/i)?.[1]) || undefined;

  return (
    <>
      <Button variant="ghost" size="sm" asChild className="mb-2 -ml-2">
        <Link to="/layers">
          <ArrowLeft /> Layers
        </Link>
      </Button>
      <PageHeader title="Publish a layer" description="Pick a table or COG; we detect the rest." />

      {/* Stepper */}
      <ol className="mb-6 flex flex-wrap items-center gap-2 text-sm">
        {steps.map((s, i) => (
          <li key={s} className="flex items-center gap-2">
            <span
              className={cn(
                "flex size-6 items-center justify-center rounded-full border text-xs font-medium",
                i < step && "border-primary bg-primary text-primary-foreground",
                i === step && "border-primary text-primary",
                i > step && "text-muted-foreground",
              )}
            >
              {i < step ? <Check className="size-3.5" /> : i + 1}
            </span>
            <span className={i === step ? "font-medium" : "text-muted-foreground"}>{s}</span>
            {i < steps.length - 1 && <span className="mx-1 h-px w-6 bg-border" />}
          </li>
        ))}
      </ol>

      <Card>
        <CardContent className="p-4">
          {/* 0: store */}
          {step === 0 &&
            (stores.isLoading ? (
              <Skeleton className="h-32" />
            ) : stores.data?.length === 0 ? (
              <EmptyState
                icon={<Database />}
                title="No stores yet"
                hint="Add a PostGIS, S3 or local folder store first."
                action={
                  <Button asChild>
                    <Link to="/stores?new=1">Add store</Link>
                  </Button>
                }
              />
            ) : (
              <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {stores.data?.map((s) => (
                  <button
                    key={s.name}
                    onClick={() => {
                      setStore(s.name);
                      setResource({});
                      setStep(1);
                    }}
                    className="flex cursor-pointer items-center gap-3 rounded-lg border p-4 text-left transition-all hover:border-primary hover:shadow-md"
                  >
                    <StoreIcon type={s.type} className="size-5" />
                    <div>
                      <div className="font-medium">{s.name}</div>
                      <div className="text-xs text-muted-foreground">{storeLabel(s.type)}</div>
                    </div>
                  </button>
                ))}
              </div>
            ))}

          {/* 1: resource */}
          {step === 1 && (
            <>
              {resources.isLoading && <Skeleton className="h-40" />}
              {resources.isError && <ErrorBox error={resources.error} />}
              {resources.data && (
                <PublishedSummary
                  total={resources.data.type === "postgis" ? resources.data.tables.length : resources.data.objects.length}
                  published={
                    resources.data.type === "postgis"
                      ? resources.data.tables.filter((t) => publishedAs.has(`${t.schema}.${t.table}`)).length
                      : resources.data.objects.filter((o) => publishedAs.has(o.path)).length
                  }
                  hidePublished={hidePublished}
                  onHidePublished={setHidePublished}
                />
              )}
              {resources.data?.type === "postgis" && (
                <ul className="divide-y">
                  {resources.data.tables
                    .filter((t) => !hidePublished || !publishedAs.has(`${t.schema}.${t.table}`))
                    .map((t) => (
                    <li key={`${t.schema}.${t.table}.${t.geom_column}`}>
                      <button
                        disabled={!t.can_select}
                        onClick={() => {
                          setResource({ schema: t.schema, table: t.table, geom_column: t.geom_column });
                          setOverrides({});
                          setStep(2);
                        }}
                        className="flex w-full cursor-pointer items-center gap-3 rounded-md px-2 py-2 text-left hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        <Table2 className="size-4 text-muted-foreground" />
                        <span className="min-w-0 flex-1 truncate font-medium">{t.table}</span>
                        <PublishedBadge names={publishedAs.get(`${t.schema}.${t.table}`)} />
                        <Badge variant="outline">{t.geom_type}</Badge>
                        <span className="w-24 text-right text-xs tabular-nums text-muted-foreground">
                          {formatNumber(t.estimated_rows)} rows
                        </span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
              {(resources.data?.type === "s3" || resources.data?.type === "local") && (
                <ul className="divide-y">
                  {resources.data.objects
                    .filter((o) => !hidePublished || !publishedAs.has(o.path))
                    .map((o) => (
                    <li key={o.path}>
                      <button
                        onClick={() => {
                          setResource({ path: o.path });
                          setOverrides({});
                          setStep(2);
                        }}
                        className="flex w-full cursor-pointer items-center gap-3 rounded-md px-2 py-2 text-left hover:bg-muted"
                      >
                        <FileImage className="size-4 text-muted-foreground" />
                        <span className="min-w-0 flex-1 truncate font-mono text-xs">{o.path}</span>
                        <PublishedBadge names={publishedAs.get(o.path)} />
                        <span className="text-xs text-muted-foreground">{formatBytes(o.size)}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
              <div className="mt-4">
                <Button variant="ghost" onClick={() => setStep(0)}>
                  <ArrowLeft /> Back
                </Button>
              </div>
            </>
          )}

          {/* 2: detected metadata */}
          {step === 2 && (
            <div className="grid gap-4">
              <div className="text-sm text-muted-foreground">
                {storeObj?.type === "postgis" ? "Table" : "File"}:{" "}
                <span className="font-mono text-foreground">
                  {store} / {resource.path ?? `${resource.schema}.${resource.table}`}
                </span>
              </div>
              {inspect.isPending && (
                <div className="flex items-center gap-2 text-sm">
                  <Loader2 className="size-4 animate-spin" /> Reading metadata… (large COGs can take a moment)
                </div>
              )}
              {inspect.isError && <ErrorBox error={inspect.error} />}
              {(needsPk || needsType || sridDetail) && (
                <OverrideForm
                  needsPk={!!needsPk}
                  needsType={!!needsType}
                  needsSrid={!!sridDetail}
                  suggestedSrid={suggestedSrid}
                  request={request}
                  onApply={(o) => setOverrides((cur) => ({ ...cur, ...o }))}
                />
              )}
              {detected && <DetectedTable result={detected} />}
              {detected?.warnings?.map((w) => (
                <div key={w} className="rounded-md bg-warning/15 px-3 py-2 text-sm">
                  {w}
                </div>
              ))}
              <div className="flex justify-between">
                <Button variant="ghost" onClick={() => setStep(1)}>
                  <ArrowLeft /> Back
                </Button>
                <Button disabled={!detected} onClick={() => setStep(3)}>
                  Looks right <ArrowRight />
                </Button>
              </div>
            </div>
          )}

          {/* 3: details */}
          {step === 3 && (
            <div className="grid max-w-xl gap-4">
              <Field
                label="Layer name"
                hint="Used in WMS requests (LAYERS=…). Letters, digits and _."
                error={!nameValid ? "Start with a letter; letters, digits and _ only" : nameTaken ? "A layer with this name exists" : undefined}
              >
                <Input
                  value={details.name}
                  onChange={(e) => setDetails({ ...details, name: e.target.value })}
                  aria-invalid={!nameValid || nameTaken}
                />
              </Field>
              <Field label="Title" hint="Shown in WMS capabilities and the map viewer">
                <Input value={details.title} onChange={(e) => setDetails({ ...details, title: e.target.value })} />
              </Field>
              <Field label="Description">
                <Textarea value={details.abstract} onChange={(e) => setDetails({ ...details, abstract: e.target.value })} />
              </Field>
              {detected?.kind === "vector" && (
                <label className="flex items-center gap-3 text-sm">
                  <Switch
                    checked={details.wfs_enabled}
                    onCheckedChange={(v) => setDetails({ ...details, wfs_enabled: v })}
                  />
                  Also publish over WFS (feature download)
                </label>
              )}
              <p className="text-xs text-muted-foreground">
                A default style for the geometry type is applied. You can assign styles afterwards.
              </p>
              <div className="flex justify-between">
                <Button variant="ghost" onClick={() => setStep(2)}>
                  <ArrowLeft /> Back
                </Button>
                <Button disabled={!nameValid || nameTaken || create.isPending} onClick={publish}>
                  {create.isPending ? <Loader2 className="animate-spin" /> : <Upload />} Publish
                </Button>
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </>
  );
}

type Overrides = { pk?: string; layer_type?: string; srid?: number };


function OverrideForm({
  needsPk,
  needsType,
  needsSrid,
  suggestedSrid,
  request,
  onApply,
}: {
  needsPk: boolean;
  needsType: boolean;
  needsSrid: boolean;
  suggestedSrid?: number;
  request: LayerInput;
  onApply: (o: Overrides) => void;
}) {
  const [pk, setPk] = useState("");
  const [type, setType] = useState("POLYGON");
  const [srid, setSrid] = useState(String(suggestedSrid ?? 4326));
  const sridOK = /^[1-9][0-9]{3,5}$/.test(srid);
  return (
    <div className="grid gap-3 rounded-md border p-4 sm:grid-cols-[1fr_1fr_auto] sm:items-end">
      {needsSrid && (
        <div className="grid gap-3 sm:col-span-3">
          <p className="text-sm">
            <b>Declare the coordinate system.</b> The column has no SRID (typical after a QGIS import), so MapNest
            needs to know which EPSG code the coordinates are in, like GeoServer's “Declared SRS”. Your database is not
            changed.
          </p>
          {/* Suggestions need a PostGIS table; for a raster, type the code. */}
          {request.table && <SridPicker request={request} value={srid} onChange={setSrid} />}
          <div className="flex flex-wrap items-end gap-3">
            <Field label="EPSG code" error={!sridOK ? "A number like 4326" : undefined} className="w-40">
              <Input value={srid} onChange={(e) => setSrid(e.target.value.replace(/\D/g, ""))} inputMode="numeric" />
            </Field>
            <p className="pb-2 text-xs text-muted-foreground">
              Picked from the list, or type it if you know it (e.g. from the .prj file or QGIS layer properties).
            </p>
          </div>
        </div>
      )}
      {needsPk && (
        <Field label="Unique id column" hint="Needed for views and tables without a primary key">
          <Input value={pk} onChange={(e) => setPk(e.target.value)} placeholder="gid" />
        </Field>
      )}
      {needsType && (
        <Field label="Draw as">
          <NativeSelect value={type} onChange={(e) => setType(e.target.value)}>
            <option value="POINT">Points</option>
            <option value="LINE">Lines</option>
            <option value="POLYGON">Polygons</option>
          </NativeSelect>
        </Field>
      )}
      <Button
        variant="outline"
        disabled={needsSrid && !sridOK}
        className={needsSrid && !needsPk && !needsType ? "sm:col-start-3" : undefined}
        onClick={() =>
          onApply({
            ...(needsPk && pk ? { pk } : {}),
            ...(needsType ? { layer_type: type } : {}),
            ...(needsSrid ? { srid: Number(srid) } : {}),
          })
        }
      >
        {needsSrid ? "Use this and retry" : "Retry"}
      </Button>
    </div>
  );
}

function DetectedTable({ result }: { result: InspectResult }) {
  const e = result.extent;
  const fmt = (n: number) => (Math.abs(n) > 1000 ? n.toFixed(0) : n.toFixed(5));
  const rows: [string, React.ReactNode][] =
    result.kind === "vector"
      ? (() => {
          const s = result.source as VectorSource;
          return [
            ["Geometry", `${s.geom_type} → drawn as ${s.layer_type.toLowerCase()}s`],
            ["Geometry column", s.geom_column],
            ["Unique id", s.pk],
            ["SRID", s.declared_srid ? `EPSG:${s.srid} (declared; the column has none)` : `EPSG:${s.srid}`],
          ];
        })()
      : (() => {
          const s = result.source as RasterSource;
          return [
            ["Bands", `${s.bands} (${s.dtype})`],
            ["CRS", s.srid ? `EPSG:${s.srid}` : "custom (read from file)"],
            ["No-data", s.nodata ?? "none"],
            ...(s.min !== undefined && s.max !== undefined
              ? ([["Value range", `${fmt(s.min)} … ${fmt(s.max)}`]] as [string, React.ReactNode][])
              : []),
          ];
        })();
  return (
    <dl className="grid gap-x-6 gap-y-2 rounded-md bg-muted/50 p-4 text-sm sm:grid-cols-[10rem_1fr]">
      {rows.map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="text-muted-foreground">{k}</dt>
          <dd className="font-medium">{v}</dd>
        </div>
      ))}
      <dt className="text-muted-foreground">Extent</dt>
      <dd className="font-mono text-xs">
        {e ? `${fmt(e.minx)}, ${fmt(e.miny)} — ${fmt(e.maxx)}, ${fmt(e.maxy)}` : "unknown (empty table)"}
      </dd>
    </dl>
  );
}

/** "Published as …" for resources that already have layers, "New" otherwise. */
function PublishedBadge({ names }: { names?: string[] }) {
  if (!names?.length) return <Badge variant="secondary">New</Badge>;
  return (
    <Badge variant="success" title={`Already published as ${names.join(", ")}. Publishing again makes another layer.`}>
      Published{names.length > 1 ? ` ×${names.length}` : ""}
    </Badge>
  );
}

function PublishedSummary({
  total,
  published,
  hidePublished,
  onHidePublished,
}: {
  total: number;
  published: number;
  hidePublished: boolean;
  onHidePublished: (v: boolean) => void;
}) {
  return (
    <div className="mb-2 flex flex-wrap items-center justify-between gap-2 text-sm">
      <span className="text-muted-foreground">
        {total} available · <span className="text-success">{published} published</span> · {total - published} new
      </span>
      {published > 0 && (
        <label className="flex items-center gap-2 text-muted-foreground">
          <Switch checked={hidePublished} onCheckedChange={onHidePublished} /> Hide published
        </label>
      )}
    </div>
  );
}
