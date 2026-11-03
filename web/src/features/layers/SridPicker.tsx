import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import OlMap from "ol/Map";
import View from "ol/View";
import Feature from "ol/Feature";
import Point from "ol/geom/Point";
import VectorLayer from "ol/layer/Vector";
import VectorSource from "ol/source/Vector";
import { Circle, Fill, Stroke, Style } from "ol/style";
import { fromLonLat } from "ol/proj";
import { defaults as defaultControls } from "ol/control/defaults";
import { Loader2, MapPin } from "lucide-react";
import { api } from "@/api/client";
import type { LayerInput, SRIDCandidate } from "@/api/types";
import { ErrorBox } from "@/components/ui/empty-state";
import { basemapLayer } from "@/lib/ol/wms";
import { cn } from "@/lib/utils";

const fmtLonLat = (lon: number, lat: number) =>
  `${Math.abs(lon).toFixed(2)}°${lon >= 0 ? "E" : "W"}, ${Math.abs(lat).toFixed(2)}°${lat >= 0 ? "N" : "S"}`;

/**
 * Suggested coordinate systems for a column without SRID. Each one puts the
 * data somewhere on Earth; the user knows where their data is, so the map
 * makes the right one easy to spot (and to click).
 */
export function SridPicker({
  request,
  value,
  onChange,
}: {
  request: Pick<LayerInput, "store" | "schema" | "table" | "geom_column">;
  value: string;
  onChange: (srid: string) => void;
}) {
  const tz = -new Date().getTimezoneOffset(); // minutes east of UTC: a longitude hint
  const q = useQuery({
    queryKey: ["srid-candidates", request.store, request.schema, request.table, request.geom_column],
    queryFn: () =>
      api.post<{ candidates: SRIDCandidate[] }>(`/layers/srid-candidates?tz_offset=${tz}`, { name: "x", ...request }),
    staleTime: Infinity,
    retry: false,
  });
  const [showAll, setShowAll] = useState(false);
  const cands = q.data?.candidates ?? [];

  // Preselect the best match once.
  const picked = useRef(false);
  useEffect(() => {
    if (!picked.current && cands.length) {
      picked.current = true;
      onChange(String(cands[0].srid));
    }
  }, [cands, onChange]);

  if (q.isLoading)
    return (
      <div className="flex items-center gap-2 rounded-md bg-muted px-3 py-3 text-sm">
        <Loader2 className="size-4 animate-spin" /> Finding coordinate systems that fit your data… (a few seconds)
      </div>
    );
  if (q.isError) return <ErrorBox error={q.error} />;
  if (!cands.length)
    return (
      <p className="rounded-md bg-muted px-3 py-2 text-sm text-muted-foreground">
        No EPSG coordinate system fits these coordinates. Type the code below if you know it.
      </p>
    );

  const list = showAll ? cands : cands.slice(0, 8);
  return (
    <div className="grid gap-3 lg:grid-cols-[1fr_1.1fr]">
      <CandidateMap cands={cands} value={value} onPick={onChange} />
      <div className="grid content-start gap-1">
        <p className="text-xs text-muted-foreground">
          {cands.length === 1
            ? "This system fits your data:"
            : `${cands.length} systems fit these numbers. Pick the one that puts your data in the right place (dots on the map):`}
        </p>
        <ul className="max-h-72 overflow-y-auto rounded-md border" role="radiogroup">
          {list.map((c, i) => (
            <li key={c.srid}>
              <button
                type="button"
                role="radio"
                aria-checked={value === String(c.srid)}
                onClick={() => onChange(String(c.srid))}
                className={cn(
                  "flex w-full cursor-pointer items-start gap-2 border-b px-3 py-2 text-left text-sm last:border-b-0",
                  value === String(c.srid) ? "bg-primary/10" : "hover:bg-muted",
                )}
              >
                <span
                  className={cn(
                    "mt-1 size-3 shrink-0 rounded-full border-2",
                    value === String(c.srid) ? "border-primary bg-primary" : "border-muted-foreground/50",
                  )}
                />
                <span className="min-w-0 flex-1">
                  <span className="block font-medium">
                    EPSG:{c.srid} · {c.name}
                    {i === 0 && <span className="ml-2 rounded bg-success/15 px-1.5 text-[11px] text-success">best match</span>}
                    {c.used_in_database && (
                      <span className="ml-2 rounded bg-accent px-1.5 text-[11px]">used by other tables</span>
                    )}
                  </span>
                  <span className="block truncate text-xs text-muted-foreground">
                    <MapPin className="mr-0.5 inline size-3" />
                    Data would be near {fmtLonLat(c.lon, c.lat)} · valid for {c.area}
                  </span>
                </span>
              </button>
            </li>
          ))}
        </ul>
        {cands.length > list.length && (
          <button type="button" onClick={() => setShowAll(true)} className="cursor-pointer text-left text-xs text-primary hover:underline">
            Show all {cands.length}
          </button>
        )}
      </div>
    </div>
  );
}

function CandidateMap({ cands, value, onPick }: { cands: SRIDCandidate[]; value: string; onPick: (srid: string) => void }) {
  const el = useRef<HTMLDivElement>(null);
  const map = useRef<OlMap | null>(null);
  const source = useMemo(() => new VectorSource(), []);
  const onPickRef = useRef(onPick);
  onPickRef.current = onPick;

  useEffect(() => {
    if (!el.current) return;
    const layer = new VectorLayer({ source });
    map.current = new OlMap({
      target: el.current,
      layers: [basemapLayer(), layer],
      view: new View({ center: [0, 0], zoom: 1 }),
      controls: defaultControls({ attribution: false, rotate: false }),
    });
    map.current.on("click", (e) => {
      const f = map.current?.forEachFeatureAtPixel(e.pixel, (x) => x, { hitTolerance: 6 });
      if (f) onPickRef.current(String(f.get("srid")));
    });
    map.current.on("pointermove", (e) => {
      const hit = map.current?.hasFeatureAtPixel(e.pixel, { hitTolerance: 6 });
      if (el.current) el.current.style.cursor = hit ? "pointer" : "";
    });
    return () => {
      map.current?.setTarget(undefined);
      map.current = null;
    };
  }, [source]);

  // One dot per distinct place (datum variants land on the same spot).
  useEffect(() => {
    source.clear();
    for (const c of cands) {
      const f = new Feature({ geometry: new Point(fromLonLat([c.lon, c.lat])), srid: c.srid });
      source.addFeature(f);
    }
    const ext = source.getExtent();
    if (map.current && ext && cands.length) {
      map.current.getView().fit(ext, { padding: [30, 30, 30, 30], maxZoom: 6 });
    }
  }, [cands, source]);

  useEffect(() => {
    source.getFeatures().forEach((f) => {
      const on = String(f.get("srid")) === value;
      f.setStyle(
        new Style({
          zIndex: on ? 2 : 1,
          image: new Circle({
            radius: on ? 8 : 5,
            fill: new Fill({ color: on ? "#2563eb" : "rgba(220, 38, 38, 0.75)" }),
            stroke: new Stroke({ color: "#ffffff", width: 2 }),
          }),
        }),
      );
    });
  }, [value, source, cands]);

  return <div ref={el} className="h-72 w-full overflow-hidden rounded-md border bg-muted" aria-label="Where the data would be" />;
}
