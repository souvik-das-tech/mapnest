import { Info } from "lucide-react";
import { cn } from "@/lib/utils";

const code = "rounded bg-muted px-1 font-mono text-[11px]";

/**
 * What changes for SLDs brought over from GeoServer: rendering is done by
 * MapServer, which reads the SLD through MapNest's converter.
 */
export function GeoServerNotes({ defaultOpen = false, className }: { defaultOpen?: boolean; className?: string }) {
  return (
    <details open={defaultOpen} className={cn("group rounded-lg border bg-card text-sm", className)}>
      <summary className="flex cursor-pointer items-center gap-2 px-4 py-3 font-medium">
        <Info className="size-4 text-primary" /> Coming from GeoServer? What is different here
      </summary>
      <div className="grid gap-4 border-t px-4 py-3 text-[13px] leading-relaxed">
        <section>
          <h3 className="font-medium">Only the first matching rule is drawn</h3>
          <p className="text-muted-foreground">
            GeoServer draws <em>every</em> rule whose filter matches a feature. MapServer draws only the{" "}
            <em>first</em> one. Put specific rules first and general ones last. Rules with{" "}
            <code className={code}>&lt;ElseFilter/&gt;</code> are moved to the end for you. Several{" "}
            <code className={code}>FeatureTypeStyle</code>s are merged into one list, so layered effects (e.g. a road
            casing in one FeatureTypeStyle and the road in another) need both strokes in the same rule.
          </p>
        </section>

        <section>
          <h3 className="font-medium">Icons are matched by file name</h3>
          <p className="text-muted-foreground">
            Upload the PNGs on the Icons page. In <code className={code}>xlink:href</code> only the file name counts:{" "}
            <code className={code}>school.png</code>, <code className={code}>icons/school.png</code> and{" "}
            <code className={code}>http://geoserver/styles/school.png</code> all find <code className={code}>school.png</code>.
            Use <b>Icon paths</b> above to copy them. Saving fails if an icon is not uploaded yet.
          </p>
        </section>

        <section>
          <h3 className="font-medium">Supported filters</h3>
          <p className="text-muted-foreground">
            <code className={code}>PropertyIsEqualTo</code>, <code className={code}>NotEqualTo</code>,{" "}
            <code className={code}>LessThan</code>, <code className={code}>LessThanOrEqualTo</code>,{" "}
            <code className={code}>GreaterThan</code>, <code className={code}>GreaterThanOrEqualTo</code>,{" "}
            <code className={code}>Between</code>, <code className={code}>Like</code>, <code className={code}>IsNull</code>,
            combined with <code className={code}>And</code> / <code className={code}>Or</code> /{" "}
            <code className={code}>Not</code>, plus <code className={code}>ElseFilter</code> and{" "}
            <code className={code}>Min/MaxScaleDenominator</code>.
          </p>
        </section>

        <section>
          <h3 className="font-medium">Not supported (shown as warnings, the rest still works)</h3>
          <ul className="list-disc pl-5 text-muted-foreground">
            <li>Spatial filters (BBOX, Intersects, DWithin…) and functions inside filters (e.g. strToLowerCase)</li>
            <li>Colours or sizes calculated from attributes (a plain attribute value for size or rotation works)</li>
            <li>GraphicFill / GraphicStroke patterns: drawn as a solid fill or plain line</li>
            <li>Rendering transformations, inline images, Geometry overrides</li>
            <li>stroke-dashoffset and LineSymbolizer PerpendicularOffset</li>
          </ul>
        </section>

        <section>
          <h3 className="font-medium">Good to know</h3>
          <ul className="list-disc pl-5 text-muted-foreground">
            <li>
              Well-known marks: circle, square, triangle, star, cross, x (plus diamond, hexagon, pentagon, arrow).
            </li>
            <li>Fonts are mapped to Sans, Serif or Mono (DejaVu), with bold and italic.</li>
            <li>
              A style file can contain one UserStyle. Assign the style to layers on the layer page; extra styles are
              picked with <code className={code}>STYLES=</code> in WMS requests, as in GeoServer.
            </li>
            <li>
              Check <b>Warnings</b> and <b>Generated MapServer classes</b> on the right to see exactly what was
              converted.
            </li>
          </ul>
        </section>
      </div>
    </details>
  );
}
