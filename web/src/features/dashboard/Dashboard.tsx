import { Link } from "react-router";
import { Boxes, Database, Feather, FileCode2, FolderLock, Gauge, Globe, Image, Layers, Palette, Plus } from "lucide-react";
import { useIcons, useLayers, useStores, useStyles, useWorkspaces } from "@/api/hooks";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { APP_NAME, MapServerCredit } from "@/components/ui/credit";
import { PageHeader } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/misc";
import { cn } from "@/lib/utils";

export function Dashboard() {
  const stores = useStores();
  const layers = useLayers();
  const styles = useStyles();
  const icons = useIcons();
  const workspaces = useWorkspaces();

  const counts = [
    { label: "Workspaces", value: workspaces.data?.length, icon: Boxes, to: "/workspaces" },
    { label: "Stores", value: stores.data?.length, icon: Database, to: "/stores" },
    { label: "Layers", value: layers.data?.length, icon: Layers, to: "/layers" },
    { label: "Styles", value: styles.data?.length, icon: Palette, to: "/styles" },
    { label: "Icons", value: icons.data?.length, icon: Image, to: "/icons" },
  ];

  return (
    <>
      <PageHeader
        title="Dashboard"
        description={`Overview of your ${APP_NAME} installation.`}
        actions={
          <Button asChild>
            <Link to="/layers/new">
              <Plus /> Publish layer
            </Link>
          </Button>
        }
      />

      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
        {counts.map(({ label, value, icon: Icon, to }) => {
          const body = (
            <Card className="transition-shadow hover:shadow-md">
              <CardContent className="flex items-center gap-4 p-4">
                <div className="flex size-8 items-center justify-center rounded-md bg-accent text-accent-foreground">
                  <Icon className="size-5" />
                </div>
                <div>
                  <div className="text-xl font-semibold leading-none tabular-nums">
                    {value === undefined ? (to ? <Skeleton className="h-7 w-8" /> : "–") : value}
                  </div>
                  <div className="mt-1 text-xs text-muted-foreground">{label}</div>
                </div>
              </CardContent>
            </Card>
          );
          return to ? (
            <Link key={label} to={to} className="block">
              {body}
            </Link>
          ) : (
            <div key={label}>{body}</div>
          );
        })}
      </div>

      <WhyMapNest className="mt-4" />
    </>
  );
}

// Measured 2026-10-15 on a Windows 11 laptop (Docker Desktop); see README.md.
const FOOTPRINT = [
  { name: "MapNest API", mb: 11 },
  { name: "MapServer", mb: 88 },
  { name: "Web UI (nginx)", mb: 15 },
];

const REASONS = [
  {
    icon: Feather,
    title: "Light on memory",
    text: "The whole stack idles at about 115 MB of RAM. GeoServer runs on Java and production setups usually give it 1–2 GB of heap.",
  },
  {
    icon: Gauge,
    title: "Fast out of the box",
    text: "182 WMS requests/s with p95 35 ms in our laptop load test. It starts in seconds, with no JVM warm-up.",
  },
  {
    icon: FileCode2,
    title: "The GeoServer workflow you know",
    text: "Stores, layers and styles work the same way. Import your existing SLD files and icons.",
  },
  {
    icon: Globe,
    title: "Standard OGC services",
    text: "WMS, and WFS for vector layers, work with OpenLayers, Leaflet, QGIS and any other GIS client.",
  },
  {
    icon: FolderLock,
    title: "Your data stays where it is",
    text: "Read-only access to your PostGIS, S3 buckets and raster folders. Settings live in one SQLite file; a backup is one .tar.gz.",
  },
];

/** Why pick MapNest over GeoServer, for people building basic GIS apps. */
function WhyMapNest({ className }: { className?: string }) {
  const total = FOOTPRINT.reduce((n, f) => n + f.mb, 0);
  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle>Why {APP_NAME} instead of GeoServer?</CardTitle>
        <CardDescription>
          For basic GIS apps: publish your maps as WMS/WFS without running a heavy Java server.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        <div className="rounded-md border p-3">
          <div className="flex items-baseline justify-between gap-2">
            <span className="text-sm font-medium">Idle memory, whole stack</span>
            <span className="text-lg font-semibold tabular-nums">~{total} MB</span>
          </div>
          <div className="mt-2 flex h-2 overflow-hidden rounded-full bg-muted" role="img" aria-label="Memory per service">
            {FOOTPRINT.map((f, i) => (
              <div
                key={f.name}
                className={cn("h-full", ["bg-primary", "bg-primary/60", "bg-primary/30"][i])}
                style={{ width: `${(f.mb / total) * 100}%` }}
              />
            ))}
          </div>
          <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
            {FOOTPRINT.map((f, i) => (
              <span key={f.name} className="flex items-center gap-1.5">
                <span className={cn("size-2 rounded-full", ["bg-primary", "bg-primary/60", "bg-primary/30"][i])} />
                {f.name} {f.mb} MB
              </span>
            ))}
          </div>
        </div>

        <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {REASONS.map(({ icon: Icon, title, text }) => (
            <li key={title} className="flex gap-3">
              <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-accent text-accent-foreground">
                <Icon className="size-4" />
              </span>
              <div>
                <div className="text-sm font-medium">{title}</div>
                <p className="text-xs leading-relaxed text-muted-foreground">{text}</p>
              </div>
            </li>
          ))}
        </ul>

        <div className="rounded-md bg-muted/60 p-3 text-xs leading-relaxed text-muted-foreground">
          <span className="font-medium text-foreground">When GeoServer is the better fit:</span> you need built-in tile
          caching (WMTS/GeoWebCache), WPS processing, or per-user security across many workspaces.
        </div>

        <div className="border-t pt-3">
          <MapServerCredit className="text-xs" />
          <p className="mt-1 text-xs text-muted-foreground">
            {APP_NAME} is an admin API and UI on top of a stock MapServer. All rendering, WMS and WFS come from
            MapServer, in production use since the 1990s.
          </p>
        </div>
      </CardContent>
    </Card>
  );
}
