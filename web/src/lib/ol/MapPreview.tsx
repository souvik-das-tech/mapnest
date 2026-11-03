import { useEffect, useRef } from "react";
import OlMap from "ol/Map";
import View from "ol/View";
import { defaults as defaultControls } from "ol/control/defaults";
import type ImageLayer from "ol/layer/Image";
import type ImageWMS from "ol/source/ImageWMS";
import { basemapLayer, extent3857, useCapabilities, wmsLayer } from "./wms";

/** Small live WMS map of one layer, zoomed to its extent. */
export function MapPreview({
  layer,
  style = "",
  className,
  version,
}: {
  layer: string;
  style?: string;
  className?: string;
  /** Change to force a reload (e.g. after editing). */
  version?: string | number;
}) {
  const el = useRef<HTMLDivElement>(null);
  const map = useRef<OlMap | null>(null);
  const wms = useRef<ImageLayer<ImageWMS> | null>(null);
  const caps = useCapabilities();

  useEffect(() => {
    if (!el.current) return;
    const base = basemapLayer();
    wms.current = wmsLayer(layer, style);
    map.current = new OlMap({
      target: el.current,
      layers: [base, wms.current],
      view: new View({ center: [0, 0], zoom: 2 }),
      controls: defaultControls({ attribution: false, rotate: false }),
    });
    return () => {
      map.current?.setTarget(undefined);
      map.current = null;
    };
    // Rebuilt when the layer changes; style updates go through updateParams below.
  }, [layer]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    wms.current?.getSource()?.updateParams({ STYLES: style, _v: version ?? "" });
  }, [style, version]);

  useEffect(() => {
    const ext = extent3857(caps.data?.get(layer));
    if (ext && map.current) map.current.getView().fit(ext, { padding: [24, 24, 24, 24], maxZoom: 18 });
  }, [caps.data, layer]);

  return <div ref={el} className={className} />;
}
