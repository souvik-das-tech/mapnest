import { cn } from "@/lib/utils";

export const APP_NAME = "MapNest";
export const APP_TAGLINE = "Lightweight map publishing, powered by MapServer.";

/** Credit for MapServer, which does all the map rendering. Keep it visible. */
export function MapServerCredit({ className }: { className?: string }) {
  return (
    <p className={cn("text-[11px] leading-snug text-muted-foreground", className)}>
      Maps rendered by{" "}
      <a
        href="https://mapserver.org"
        target="_blank"
        rel="noreferrer"
        className="font-medium text-foreground underline-offset-2 hover:underline"
      >
        MapServer
      </a>
      , an{" "}
      <a href="https://www.osgeo.org" target="_blank" rel="noreferrer" className="underline-offset-2 hover:underline">
        OSGeo
      </a>{" "}
      project.
    </p>
  );
}

/** The MapNest mark (public/logo.svg): nested map layers with a location. */
export function AppLogo({ className }: { className?: string }) {
  return <img src="/logo.svg" alt="" aria-hidden="true" draggable={false} className={className} />;
}
