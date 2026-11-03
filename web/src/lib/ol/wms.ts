import { useQuery } from "@tanstack/react-query";
import { qk } from "@/api/hooks";
import type { Layer } from "@/api/types";
import WMSCapabilities from "ol/format/WMSCapabilities";
import TileLayer from "ol/layer/Tile";
import ImageLayer from "ol/layer/Image";
import OSM from "ol/source/OSM";
import ImageWMS from "ol/source/ImageWMS";
import { transformExtent } from "ol/proj";
import type { Extent as OlExtent } from "ol/extent";

/** OWS endpoint as seen by the browser: nginx (and the Vite dev proxy) forward /ows to MapServer. */
export const OWS_URL = "/ows";

export interface CapLayer {
  name: string;
  title: string;
  /** EX_GeographicBoundingBox: [minLon, minLat, maxLon, maxLat] */
  bbox4326?: [number, number, number, number];
  styles: { name: string; title: string; legendUrl?: string }[];
  queryable: boolean;
}

type RawLayer = {
  Name?: string;
  Title?: string;
  EX_GeographicBoundingBox?: number[];
  Style?: { Name: string; Title: string; LegendURL?: { OnlineResource: string }[] }[];
  queryable?: boolean;
  Layer?: RawLayer[];
};

/** Fetches and flattens WMS 1.3.0 capabilities. */
export async function fetchCapabilities(): Promise<Map<string, CapLayer>> {
  const res = await fetch(`${OWS_URL}?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetCapabilities`);
  if (!res.ok) throw new Error(`GetCapabilities failed: ${res.status}`);
  const caps = new WMSCapabilities().read(await res.text()) as { Capability?: { Layer?: RawLayer } };
  const out = new Map<string, CapLayer>();
  type Box = [number, number, number, number];
  // Returns the layer's bbox. A group without one of its own (a layer with
  // WFS on is a WMS group around its drawing layer) gets its children's union.
  const walk = (l: RawLayer | undefined): Box | undefined => {
    if (!l) return undefined;
    let box: Box | undefined;
    for (const child of l.Layer ?? []) {
      const c = walk(child);
      if (c) box = box ? [Math.min(box[0], c[0]), Math.min(box[1], c[1]), Math.max(box[2], c[2]), Math.max(box[3], c[3])] : c;
    }
    const b = l.EX_GeographicBoundingBox;
    if (b && b.length === 4) box = [b[0], b[1], b[2], b[3]];
    if (l.Name) {
      out.set(l.Name, {
        name: l.Name,
        title: l.Title ?? l.Name,
        bbox4326: box,
        styles: (l.Style ?? []).map((s) => ({
          name: s.Name,
          title: s.Title,
          legendUrl: s.LegendURL?.[0]?.OnlineResource,
        })),
        queryable: !!l.queryable || !!l.Layer?.some((c) => c.queryable),
      });
    }
    return box;
  };
  walk(caps.Capability?.Layer);
  return out;
}

export const useCapabilities = () =>
  useQuery({ queryKey: qk.capabilities, queryFn: fetchCapabilities, staleTime: 10_000 });

/** Layer extent in EPSG:3857 for view.fit(), from capabilities. */
export function extent3857(cap?: CapLayer): OlExtent | undefined {
  if (!cap?.bbox4326) return undefined;
  const [minx, miny, maxx, maxy] = cap.bbox4326;
  // Clamp to Web Mercator's valid latitude range.
  return transformExtent([minx, Math.max(miny, -85), maxx, Math.min(maxy, 85)], "EPSG:4326", "EPSG:3857");
}

export function wmsLayer(name: string, style = "", opacity = 1) {
  return new ImageLayer({
    opacity,
    source: new ImageWMS({
      url: OWS_URL,
      params: { LAYERS: name, STYLES: style, FORMAT: "image/png", TRANSPARENT: true, VERSION: "1.3.0" },
      ratio: 1,
      serverType: "mapserver",
    }),
    properties: { wmsName: name },
  });
}

/** The only basemap: OpenStreetMap tiles, drawn under every WMS layer. */
export function basemapLayer() {
  const layer = new TileLayer({ source: new OSM(), properties: { basemap: true } });
  layer.setZIndex(-1);
  return layer;
}

/** Absolute public WMS URL for snippets (wms_url from the API, else this origin). */
export function publicWmsUrl(wmsUrl?: string): string {
  if (wmsUrl) return wmsUrl.endsWith("?") ? wmsUrl.slice(0, -1) : wmsUrl;
  return `${window.location.origin}${OWS_URL}`;
}

export interface LayerUrl {
  id: string;
  label: string;
  hint: string;
  url: string;
}

/**
 * Copyable service URLs for one layer. GetMap uses WMS 1.1.1 (x,y axis order)
 * in the layer's native SRS over its extent, so it opens as a PNG as-is.
 */
export function layerUrls(
  l: Pick<Layer, "name" | "kind" | "wfs_enabled" | "wms_url" | "extent" | "source" | "workspace" | "ows_url">,
): LayerUrl[] {
  const base = publicWmsUrl(l.wms_url);
  // Without a known SRID the extent's units are unknown, so use the world in 4326.
  const native = (l.source as { srid?: number }).srid;
  const srid = native || 4326;
  let bbox = srid === 3857 ? "-20037508,-20037508,20037508,20037508" : "-180,-90,180,90";
  let width = 768;
  let height = 384;
  if (l.extent && native) {
    const { minx, miny, maxx, maxy } = l.extent;
    const pad = Math.max(maxx - minx, maxy - miny, 1e-6) * 0.05;
    const b = [minx - pad, miny - pad, maxx + pad, maxy + pad];
    bbox = b.map((v) => +v.toPrecision(10)).join(",");
    const ratio = (b[3] - b[1]) / (b[2] - b[0]);
    height = Math.max(64, Math.min(2048, Math.round(width * ratio)));
  }
  const name = encodeURIComponent(l.name);
  const urls: LayerUrl[] = [
    ...(l.ows_url
      ? [{ id: "ws", label: `Workspace “${l.workspace}” URL`, hint: "WMS/WFS with only this workspace's layers", url: l.ows_url }]
      : []),
    { id: "wms", label: "WMS service URL", hint: "All layers, for QGIS, OpenLayers, Leaflet", url: base },
    {
      id: "getmap",
      label: "WMS GetMap (PNG)",
      hint: "Opens the whole layer as an image",
      url: `${base}?SERVICE=WMS&VERSION=1.1.1&REQUEST=GetMap&LAYERS=${name}&STYLES=&SRS=EPSG:${srid}&BBOX=${bbox}&WIDTH=${width}&HEIGHT=${height}&FORMAT=image/png&TRANSPARENT=TRUE`,
    },
    {
      id: "wms-caps",
      label: "WMS GetCapabilities",
      hint: "Service description (XML)",
      url: `${base}?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetCapabilities`,
    },
  ];
  if (l.kind === "vector" && l.wfs_enabled) {
    urls.push(
      { id: "wfs", label: "WFS service URL", hint: "For QGIS and other WFS clients", url: base },
      {
        id: "wfs-geojson",
        label: "WFS GeoJSON",
        hint: "First 100 features",
        url: `${base}?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetFeature&TYPENAMES=${name}&OUTPUTFORMAT=geojson&COUNT=100`,
      },
    );
  }
  return urls;
}
