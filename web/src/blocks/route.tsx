import { useEffect, useRef, useState } from "react";
import L from "leaflet";
import "leaflet/dist/leaflet.css";
import { Bike, Car, Footprints, Maximize2, Navigation } from "lucide-react";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { cn } from "../lib";
import { directionsURL, distance, duration, type Mode } from "./artifacts";

type End = { name: string; lat: number; lng: number; address?: string };
type Route = { from: End; to: End; mode: Mode; distance_m: number; duration_s: number; path: [number, number][]; note?: string };

const modes = { walk: { icon: Footprints, label: "walk" }, bike: { icon: Bike, label: "by bike" }, drive: { icon: Car, label: "drive" } };

// RouteBlock: the way from one place to another on a map, with how long it takes; Directions opens
// it in a maps app, Expand on a bigger map.
export default function RouteBlock({ data }: { data: Route }) {
  const [big, setBig] = useState(false);
  if (!data?.path?.length) return null;
  const m = modes[data.mode] ?? modes.walk;
  return (
    <div className="overflow-hidden rounded-xl border border-border bg-card">
      <div className="relative">
        <RouteMap route={data} className="h-44" />
        <button type="button" onClick={() => setBig(true)} className="absolute top-2 right-2 z-[400] flex items-center gap-1 rounded-full border border-border bg-card/95 px-2.5 py-1 text-xs font-medium shadow-sm hover:bg-card">
          Expand <Maximize2 className="size-3" />
        </button>
      </div>
      <div className="flex items-center gap-3 p-2.5">
        <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-accent"><m.icon className="size-4 text-foreground/70" /></span>
        <span className="min-w-0 flex-1">
          <span className="block text-sm font-medium tabular-nums">{duration(data.duration_s)} {m.label} · {distance(data.distance_m)}</span>
          <span className="block truncate text-xs text-muted-foreground" title={`${data.from.name} → ${data.to.name}`}>{data.from.name} → {data.to.name}</span>
          {data.note && <span className="mt-0.5 line-clamp-2 block text-xs text-muted-foreground">{data.note}</span>}
        </span>
        <a
          href={directionsURL(data.from, data.to, data.mode)}
          target="_blank"
          rel="noreferrer noopener"
          onClick={(e) => e.stopPropagation()}
          className="flex h-8 shrink-0 items-center gap-1.5 rounded-full bg-primary px-3 text-xs font-medium text-primary-foreground transition-transform active:scale-[0.97]"
        >
          <Navigation className="size-3.5" />Directions
        </a>
      </div>
      <Dialog open={big} onOpenChange={setBig}>
        <DialogContent className="max-w-4xl gap-2 p-3 sm:max-w-4xl">
          <DialogTitle className="px-1 text-base">{data.from.name} → {data.to.name}</DialogTitle>
          <DialogDescription className="px-1">{duration(data.duration_s)} {m.label} · {distance(data.distance_m)}</DialogDescription>
          <RouteMap route={data} className="h-[70dvh] rounded-lg" />
        </DialogContent>
      </Dialog>
    </div>
  );
}

const dot = (label: string, dark: boolean) =>
  L.divIcon({
    className: "",
    iconSize: [22, 22],
    iconAnchor: [11, 11],
    html: `<span style="display:flex;align-items:center;justify-content:center;width:22px;height:22px;border-radius:11px;border:2px solid #fff;background:${dark ? "#111" : "#7c3aed"};box-shadow:0 1px 4px rgba(0,0,0,.35);color:#fff;font:600 10px system-ui">${label}</span>`,
  });

function RouteMap({ route, className }: { route: Route; className?: string }) {
  const el = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!el.current) return;
    const m = L.map(el.current, { scrollWheelZoom: false, attributionControl: true });
    L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", { maxZoom: 19, attribution: "© OpenStreetMap" }).addTo(m);
    const line = L.polyline(route.path, { color: "#7c3aed", weight: 5, opacity: 0.85 }).addTo(m);
    L.marker([route.from.lat, route.from.lng], { icon: dot("A", false), title: route.from.name }).addTo(m);
    L.marker([route.to.lat, route.to.lng], { icon: dot("B", true), title: route.to.name }).addTo(m);
    m.fitBounds(line.getBounds(), { padding: [24, 24], maxZoom: 16 });
    // A map that mounts hidden (a dialog opening) needs to measure itself again.
    const t = setTimeout(() => m.invalidateSize(), 200);
    return () => { clearTimeout(t); m.remove(); };
  }, [route]);
  return <div ref={el} className={cn("w-full bg-muted", className)} role="region" aria-label={`Route from ${route.from.name} to ${route.to.name}`} />;
}
