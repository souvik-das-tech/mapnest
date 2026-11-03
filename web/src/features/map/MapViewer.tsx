import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router";
import OlMap from "ol/Map";
import View from "ol/View";
import Overlay from "ol/Overlay";
import { defaults as defaultControls } from "ol/control/defaults";
import ScaleLine from "ol/control/ScaleLine";
import type ImageLayer from "ol/layer/Image";
import type ImageWMS from "ol/source/ImageWMS";
import WMSGetFeatureInfo from "ol/format/WMSGetFeatureInfo";
import { fromLonLat, toLonLat } from "ol/proj";
import {
  ArrowLeft,
  Eye,
  EyeOff,
  GripVertical,
  Layers as LayersIcon,
  ListTree,
  Link2,
  Map as MapIcon,
  PanelLeftClose,
  PanelLeftOpen,
  Plus,
  X,
  ZoomIn,
} from "lucide-react";
import { toast } from "sonner";
import { useLayers } from "@/api/hooks";
import { Button } from "@/components/ui/button";
import { NativeSelect } from "@/components/ui/input";
import { Tooltip } from "@/components/ui/misc";
import { basemapLayer, extent3857, OWS_URL, useCapabilities, wmsLayer } from "@/lib/ol/wms";
import { cn } from "@/lib/utils";
import { WorkspaceFilter } from "@/app/workspace";

interface Entry {
  name: string;
  visible: boolean;
  opacity: number;
  style: string;
}

interface Popup {
  coordinate: number[];
  results: { layer: string; props: Record<string, unknown>[] }[];
}

/** Parses ?layers=a,b&op=1,0.5&st=,night&x=&y=&z= into state. */
function readUrl(p: URLSearchParams) {
  const names = (p.get("layers") ?? "").split(",").filter(Boolean);
  const op = (p.get("op") ?? "").split(",");
  const st = (p.get("st") ?? "").split(",");
  const hidden = new Set((p.get("off") ?? "").split(",").filter(Boolean));
  const entries: Entry[] = names.map((name, i) => ({
    name,
    visible: !hidden.has(name),
    opacity: op[i] !== undefined && op[i] !== "" ? Math.min(1, Math.max(0, Number(op[i]))) : 1,
    style: st[i] ?? "",
  }));
  const x = Number(p.get("x"));
  const y = Number(p.get("y"));
  const z = Number(p.get("z"));
  const view = p.has("x") && p.has("y") && p.has("z") && [x, y, z].every(Number.isFinite) ? { x, y, z } : null;
  return { entries, view };
}

/**
 * Full-screen viewer: layer panel (toggle, opacity, style, reorder),
 * OpenStreetMap basemap, GetFeatureInfo popup, legend, zoom to layer, shareable URL.
 * PLAN.md §11.3 Map viewer.
 */
export function MapViewer() {
  const [params, setParams] = useSearchParams();
  const initial = useMemo(() => readUrl(params), []); // eslint-disable-line react-hooks/exhaustive-deps
  const all = useLayers();
  const caps = useCapabilities();

  const [entries, setEntries] = useState<Entry[]>(initial.entries);
  // Narrows the "Add a layer" list; kept in the link like everything else.
  const [workspace, setWorkspace] = useState(() => params.get("workspace") ?? "");
  const workspaceRef = useRef(workspace);
  workspaceRef.current = workspace;
  const [panel, setPanel] = useState<"layers" | "legend" | null>("layers");
  const [popup, setPopup] = useState<Popup | null>(null);
  const [dragIndex, setDragIndex] = useState<number | null>(null);

  const mapEl = useRef<HTMLDivElement>(null);
  const popupEl = useRef<HTMLDivElement>(null);
  const map = useRef<OlMap | null>(null);
  const overlay = useRef<Overlay | null>(null);
  const wms = useRef(new Map<string, ImageLayer<ImageWMS>>());
  const entriesRef = useRef(entries);
  entriesRef.current = entries;

  // Create the map once.
  useEffect(() => {
    if (!mapEl.current) return;
    overlay.current = new Overlay({
      element: popupEl.current!,
      positioning: "bottom-center",
      offset: [0, -10],
      autoPan: { animation: { duration: 200 } },
    });
    const v = initial.view;
    map.current = new OlMap({
      target: mapEl.current,
      view: new View({
        center: v ? fromLonLat([v.x, v.y]) : fromLonLat([80, 22]),
        zoom: v ? v.z : 4,
      }),
      layers: [basemapLayer()],
      overlays: [overlay.current],
      controls: defaultControls({ rotate: false }).extend([new ScaleLine()]),
    });
    map.current.on("moveend", () => syncUrl());
    map.current.on("singleclick", (e) => featureInfo(e.coordinate));
    return () => {
      map.current?.setTarget(undefined);
      map.current = null;
      wms.current.clear();
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // WMS layers follow `entries` (first entry = top).
  useEffect(() => {
    const m = map.current;
    if (!m) return;
    const want = new Set(entries.map((e) => e.name));
    for (const [name, layer] of wms.current) {
      if (!want.has(name)) {
        m.removeLayer(layer);
        wms.current.delete(name);
      }
    }
    entries.forEach((e, i) => {
      let layer = wms.current.get(e.name);
      if (!layer) {
        layer = wmsLayer(e.name, e.style, e.opacity);
        wms.current.set(e.name, layer);
        m.addLayer(layer);
      }
      layer.setVisible(e.visible);
      layer.setOpacity(e.opacity);
      layer.setZIndex(entries.length - i);
      const src = layer.getSource();
      if (src && src.getParams().STYLES !== e.style) src.updateParams({ STYLES: e.style });
    });
    syncUrl();
  }, [entries]); // eslint-disable-line react-hooks/exhaustive-deps

  // The map area changes size when the panel opens or closes.
  useEffect(() => {
    map.current?.updateSize();
  }, [panel]);

  // If the URL had layers but no view, zoom to the first one.
  useEffect(() => {
    if (initial.view || !caps.data || !initial.entries.length) return;
    zoomTo(initial.entries[0].name);
  }, [caps.data]); // eslint-disable-line react-hooks/exhaustive-deps

  function syncUrl() {
    const m = map.current;
    if (!m) return;
    const es = entriesRef.current;
    const center = toLonLat(m.getView().getCenter() ?? [0, 0]);
    const p = new URLSearchParams();
    if (workspaceRef.current) p.set("workspace", workspaceRef.current);
    if (es.length) {
      p.set("layers", es.map((e) => e.name).join(","));
      if (es.some((e) => e.opacity !== 1)) p.set("op", es.map((e) => +e.opacity.toFixed(2)).join(","));
      if (es.some((e) => e.style)) p.set("st", es.map((e) => e.style).join(","));
      const off = es.filter((e) => !e.visible).map((e) => e.name);
      if (off.length) p.set("off", off.join(","));
    }
    p.set("x", center[0].toFixed(5));
    p.set("y", center[1].toFixed(5));
    p.set("z", (m.getView().getZoom() ?? 4).toFixed(2));
    setParams(p, { replace: true });
  }

  function zoomTo(name: string) {
    const ext = extent3857(caps.data?.get(name));
    if (ext && map.current) map.current.getView().fit(ext, { padding: [40, 40, 40, 40], maxZoom: 18, duration: 300 });
    else toast.info("No extent available for this layer");
  }

  async function featureInfo(coordinate: number[]) {
    const m = map.current;
    if (!m) return;
    const res = m.getView().getResolution();
    const visible = entriesRef.current.filter((e) => e.visible);
    if (!res || !visible.length) return;
    const results: Popup["results"] = [];
    await Promise.all(
      visible.map(async (e) => {
        const url = wms.current.get(e.name)?.getSource()?.getFeatureInfoUrl(coordinate, res, "EPSG:3857", {
          INFO_FORMAT: "application/vnd.ogc.gml",
          FEATURE_COUNT: 5,
        });
        if (!url) return;
        try {
          const text = await (await fetch(url)).text();
          const feats = new WMSGetFeatureInfo().readFeatures(text);
          if (feats.length)
            results.push({
              layer: e.name,
              props: feats.map((f) => {
                const p = { ...f.getProperties() };
                delete p[f.getGeometryName()];
                delete p.boundedBy;
                return p;
              }),
            });
        } catch {
          /* raster layers or errors: ignore */
        }
      }),
    );
    if (results.length) {
      setPopup({ coordinate, results });
      overlay.current?.setPosition(coordinate);
    } else {
      setPopup(null);
      overlay.current?.setPosition(undefined);
    }
  }

  const update = (name: string, patch: Partial<Entry>) =>
    setEntries((es) => es.map((e) => (e.name === name ? { ...e, ...patch } : e)));

  const available = (all.data ?? []).filter(
    (l) => l.enabled && (!workspace || l.workspace === workspace) && !entries.some((e) => e.name === l.name),
  );

  const share = async () => {
    try {
      await navigator.clipboard.writeText(window.location.href);
      toast.success("Link copied");
    } catch {
      toast.error("Could not copy the link");
    }
  };

  const visibleCount = entries.filter((e) => e.visible).length;

  return (
    <div className="flex h-full flex-col bg-background">
      {/* Header */}
      <header className="flex h-12 shrink-0 items-center gap-2 border-b bg-card px-3">
        <Tooltip content="Back to admin">
          <Button variant="ghost" size="icon" asChild>
            <Link to="/" aria-label="Back to admin">
              <ArrowLeft />
            </Link>
          </Button>
        </Tooltip>
        <Tooltip content={panel ? "Hide panel" : "Show panel"}>
          <Button variant="ghost" size="icon" onClick={() => setPanel(panel ? null : "layers")} aria-label="Toggle panel">
            {panel ? <PanelLeftClose /> : <PanelLeftOpen />}
          </Button>
        </Tooltip>
        <div className="mx-1 h-5 w-px bg-border" />
        <MapIcon className="size-4 text-muted-foreground" />
        <span className="text-[13px] font-semibold">Map viewer</span>
        {entries.length > 0 && (
          <span className="hidden text-xs text-muted-foreground sm:inline">
            · {visibleCount} of {entries.length} layer{entries.length === 1 ? "" : "s"} visible
          </span>
        )}

        <div className="ml-auto flex items-center gap-2">
          <WorkspaceFilter
            value={workspace}
            onChange={(ws) => {
              workspaceRef.current = ws;
              setWorkspace(ws);
              syncUrl();
            }}
            className="h-8"
          />
          <Button variant="outline" onClick={share}>
            <Link2 /> <span className="hidden sm:inline">Copy link</span>
          </Button>
        </div>
      </header>

      <div className="relative flex min-h-0 flex-1">
        {/* Side panel: docked on desktop, over the map on phones */}
        {panel && (
          <aside className="absolute inset-y-0 left-0 z-20 flex w-72 shrink-0 flex-col border-r bg-card shadow-xl md:static md:shadow-none">
            <div className="border-b p-3">
              <div className="flex h-8 items-center rounded-md bg-muted p-0.5">
                {(["layers", "legend"] as const).map((p) => (
                  <button
                    key={p}
                    onClick={() => setPanel(p)}
                    className={cn(
                      "flex h-7 flex-1 cursor-pointer items-center justify-center gap-1.5 rounded-[5px] text-[13px] font-medium transition-colors [&_svg]:size-3.5",
                      panel === p ? "bg-card text-foreground shadow-xs" : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    {p === "layers" ? <LayersIcon /> : <ListTree />}
                    {p === "layers" ? `Layers${entries.length ? ` (${entries.length})` : ""}` : "Legend"}
                  </button>
                ))}
              </div>
            </div>

            {panel === "layers" ? (
              <>
                <div className="border-b p-3">
                  <NativeSelect
                    value=""
                    disabled={!available.length}
                    onChange={(e) => {
                      const name = e.target.value;
                      if (!name) return;
                      setEntries((es) => [{ name, visible: true, opacity: 1, style: "" }, ...es]);
                      if (!entries.length) setTimeout(() => zoomTo(name), 300);
                    }}
                  >
                    <option value="">
                      {available.length ? "＋ Add a layer…" : workspace ? `No more layers in ${workspace}` : "All layers added"}
                    </option>
                    {available.map((l) => (
                      <option key={l.name} value={l.name}>
                        {l.title || l.name}
                      </option>
                    ))}
                  </NativeSelect>
                </div>
                <ul className="flex-1 space-y-1.5 overflow-y-auto p-3">
                  {entries.length === 0 && (
                    <li className="flex flex-col items-center gap-2 rounded-lg border border-dashed px-4 py-8 text-center text-[13px] text-muted-foreground">
                      <Plus className="size-4" /> Add a layer to start.
                    </li>
                  )}
                  {entries.map((e, i) => {
                    const cap = caps.data?.get(e.name);
                    const title = all.data?.find((l) => l.name === e.name)?.title || cap?.title || e.name;
                    return (
                      <li
                        key={e.name}
                        draggable
                        onDragStart={() => setDragIndex(i)}
                        onDragOver={(ev) => ev.preventDefault()}
                        onDrop={() => {
                          if (dragIndex === null || dragIndex === i) return;
                          setEntries((es) => {
                            const copy = [...es];
                            const [moved] = copy.splice(dragIndex, 1);
                            copy.splice(i, 0, moved);
                            return copy;
                          });
                          setDragIndex(null);
                        }}
                        onDragEnd={() => setDragIndex(null)}
                        className={cn("rounded-md border bg-background/50 px-2 py-1.5", dragIndex === i && "opacity-50")}
                      >
                        <div className="flex items-center gap-0.5">
                          <GripVertical className="size-3.5 shrink-0 cursor-grab text-muted-foreground/60" />
                          <IconButton label={e.visible ? "Hide" : "Show"} onClick={() => update(e.name, { visible: !e.visible })}>
                            {e.visible ? <Eye /> : <EyeOff className="text-muted-foreground" />}
                          </IconButton>
                          <span
                            className={cn("min-w-0 flex-1 truncate px-1 text-[13px]", !e.visible && "text-muted-foreground")}
                            title={title}
                          >
                            {title}
                          </span>
                          <IconButton label="Zoom to layer" onClick={() => zoomTo(e.name)}>
                            <ZoomIn />
                          </IconButton>
                          <IconButton label="Remove" onClick={() => setEntries((es) => es.filter((x) => x.name !== e.name))}>
                            <X />
                          </IconButton>
                        </div>
                        <div className="mt-1 flex items-center gap-2 pl-[22px]">
                          <input
                            type="range"
                            min={0}
                            max={1}
                            step={0.05}
                            value={e.opacity}
                            onChange={(ev) => update(e.name, { opacity: Number(ev.target.value) })}
                            className="h-1 flex-1 cursor-pointer accent-[var(--primary)]"
                            aria-label="Opacity"
                          />
                          <span className="w-8 text-right text-[11px] tabular-nums text-muted-foreground">
                            {Math.round(e.opacity * 100)}%
                          </span>
                        </div>
                        {cap && cap.styles.length > 1 && (
                          <NativeSelect
                            className="mt-1.5 h-7 text-xs"
                            value={e.style}
                            onChange={(ev) => update(e.name, { style: ev.target.value })}
                          >
                            <option value="">Default style</option>
                            {cap.styles.map((s) => (
                              <option key={s.name} value={s.name}>
                                {s.title || s.name}
                              </option>
                            ))}
                          </NativeSelect>
                        )}
                      </li>
                    );
                  })}
                </ul>
                {entries.length > 1 && (
                  <p className="border-t px-3 py-2 text-[11px] text-muted-foreground">
                    Drag to reorder. The top layer draws on top.
                  </p>
                )}
              </>
            ) : (
              <div className="flex-1 space-y-4 overflow-y-auto p-3">
                {visibleCount === 0 && <p className="text-[13px] text-muted-foreground">No visible layers.</p>}
                {entries
                  .filter((e) => e.visible)
                  .map((e) => (
                    <div key={e.name}>
                      <div className="mb-1.5 truncate text-xs font-medium">
                        {all.data?.find((l) => l.name === e.name)?.title || e.name}
                      </div>
                      <img
                        alt=""
                        className="max-w-full rounded-md border bg-white p-1.5"
                        src={`${OWS_URL}?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetLegendGraphic&FORMAT=image/png&SLD_VERSION=1.1.0&LAYER=${encodeURIComponent(e.name)}&STYLE=${encodeURIComponent(e.style)}`}
                      />
                    </div>
                  ))}
              </div>
            )}
          </aside>
        )}

        {/* Map */}
        <div className="relative min-w-0 flex-1">
          <div ref={mapEl} className="map-viewer absolute inset-0 bg-muted" />

          {/* GetFeatureInfo popup */}
          <div ref={popupEl} className={cn(!popup && "hidden")}>
            {popup && (
              <div className="relative max-h-72 w-72 overflow-y-auto rounded-lg border bg-card p-3 text-xs shadow-xl">
                <button
                  onClick={() => {
                    setPopup(null);
                    overlay.current?.setPosition(undefined);
                  }}
                  className="absolute right-2 top-2 cursor-pointer rounded p-0.5 hover:bg-muted"
                  aria-label="Close"
                >
                  <X className="size-3.5" />
                </button>
                {popup.results.map((r) => (
                  <div key={r.layer} className="mb-2 last:mb-0">
                    <div className="mb-1 pr-5 font-semibold">{r.layer}</div>
                    {r.props.map((p, i) => (
                      <dl key={i} className="mb-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 border-t pt-1 first:border-0">
                        {Object.entries(p).map(([k, v]) => (
                          <div key={k} className="contents">
                            <dt className="text-muted-foreground">{k}</dt>
                            <dd className="break-words">{String(v)}</dd>
                          </div>
                        ))}
                      </dl>
                    ))}
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function IconButton({ label, onClick, children }: { label: string; onClick: () => void; children: React.ReactNode }) {
  return (
    <Tooltip content={label}>
      <button
        onClick={onClick}
        aria-label={label}
        className="flex size-6 shrink-0 cursor-pointer items-center justify-center rounded text-foreground/80 hover:bg-muted hover:text-foreground [&_svg]:size-3.5"
      >
        {children}
      </button>
    </Tooltip>
  );
}
