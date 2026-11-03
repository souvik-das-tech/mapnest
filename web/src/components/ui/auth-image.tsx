import { useEffect, useState } from "react";
import { ImageOff } from "lucide-react";
import { fetchImageURL } from "@/api/client";
import { cn } from "@/lib/utils";

/**
 * <img> for API endpoints that need the session header (icons, legends).
 * Shows `fallback` (or a muted icon) when the image cannot be loaded.
 */
export function AuthImage({
  src,
  alt = "",
  className,
  style,
  fallback,
}: {
  src: string;
  alt?: string;
  className?: string;
  style?: React.CSSProperties;
  fallback?: React.ReactNode;
}) {
  const [url, setUrl] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    const ctrl = new AbortController();
    let objectUrl: string | null = null;
    setFailed(false);
    setUrl(null);
    fetchImageURL(src, ctrl.signal)
      .then((u) => {
        objectUrl = u;
        setUrl(u);
      })
      .catch(() => {
        if (!ctrl.signal.aborted) setFailed(true);
      });
    return () => {
      ctrl.abort();
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [src]);

  if (failed)
    return (
      <>
        {fallback ?? (
          <span className={cn("flex items-center justify-center text-muted-foreground", className)}>
            <ImageOff className="size-5" />
          </span>
        )}
      </>
    );
  if (!url) return <span className={cn("block animate-pulse rounded bg-muted", className)} />;
  return <img src={url} alt={alt} className={className} style={style} />;
}
