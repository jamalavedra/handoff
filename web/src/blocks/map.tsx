import { useEffect, useRef, useState } from "react";
import L from "leaflet";
import "leaflet/dist/leaflet.css";
import { ExternalLink, Maximize2, MapPin } from "lucide-react";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { cn } from "../lib";

type Place = { name: string; lat: number; lng: number; address?: string; url?: string; note?: string };

// MapBlock: the places the agent found on an OpenStreetMap map, with a card per place below it.
// Picking a card flies to its pin; Expand opens a bigger map.
export default function MapBlock({ data }: { data: { places?: Place[] } }) {
  const places = data.places ?? [];
  const [picked, setPicked] = useState<number>();
  const [big, setBig] = useState(false);
  if (!places.length) return null;
  return (
    <div className="overflow-hidden rounded-xl border border-border bg-card">
      <div className="relative">
        <PlacesMap places={places} picked={picked} onPick={setPicked} className="h-48" />
        <button type="button" onClick={() => setBig(true)} className="absolute top-2 right-2 z-[400] flex items-center gap-1 rounded-full border border-border bg-card/95 px-2.5 py-1 text-xs font-medium shadow-sm hover:bg-card">
          Expand <Maximize2 className="size-3" />
        </button>
      </div>
      <div className="flex snap-x gap-2 overflow-x-auto p-2">
        {places.map((p, i) => (
          <button key={i} type="button" onClick={() => setPicked(i)} className={cn("w-48 shrink-0 snap-start rounded-lg border px-2.5 py-2 text-left", picked === i ? "border-ring bg-accent" : "border-border hover:bg-muted")}>
            <span className="flex items-center gap-1 text-sm font-medium"><MapPin className="size-3.5 shrink-0 text-violet-600" /><span className="truncate">{p.name}</span></span>
            {p.note && <span className="mt-0.5 line-clamp-2 block text-xs text-muted-foreground">{p.note}</span>}
            {p.url && (
              <a href={p.url} target="_blank" rel="noreferrer noopener" onClick={(e) => e.stopPropagation()} className="mt-1 flex items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground">
                <ExternalLink className="size-3" /><span className="truncate">{hostOf(p.url)}</span>
              </a>
            )}
          </button>
        ))}
      </div>
      <Dialog open={big} onOpenChange={setBig}>
        <DialogContent className="max-w-4xl gap-2 p-3 sm:max-w-4xl">
          <DialogTitle className="px-1 text-base">Map</DialogTitle>
          <DialogDescription className="sr-only">The places on a bigger map</DialogDescription>
          <PlacesMap places={places} picked={picked} onPick={setPicked} className="h-[70dvh] rounded-lg" />
        </DialogContent>
      </Dialog>
    </div>
  );
}

const hostOf = (u: string) => {
  try {
    return new URL(u).hostname.replace(/^www\./, "");
  } catch {
    return u;
  }
};

const pin = (n: number, on: boolean) =>
  L.divIcon({
    className: "",
    iconSize: [26, 26],
    iconAnchor: [13, 26],
    html: `<span style="display:flex;align-items:center;justify-content:center;width:26px;height:26px;border-radius:13px 13px 13px 2px;transform:rotate(-45deg);background:${on ? "#111" : "#7c3aed"};box-shadow:0 1px 4px rgba(0,0,0,.35)"><span style="transform:rotate(45deg);color:#fff;font:600 11px system-ui">${n}</span></span>`,
  });

function PlacesMap({ places, picked, onPick, className }: { places: Place[]; picked?: number; onPick: (i: number) => void; className?: string }) {
  const el = useRef<HTMLDivElement>(null);
  const map = useRef<L.Map | null>(null);
  const markers = useRef<L.Marker[]>([]);
  useEffect(() => {
    if (!el.current) return;
    const m = L.map(el.current, { scrollWheelZoom: false, attributionControl: true });
    L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", { maxZoom: 19, attribution: "© OpenStreetMap" }).addTo(m);
    markers.current = places.map((p, i) => L.marker([p.lat, p.lng], { icon: pin(i + 1, false), title: p.name }).addTo(m).on("click", () => onPick(i)));
    const bounds = L.latLngBounds(places.map((p) => [p.lat, p.lng] as [number, number]));
    if (places.length === 1) m.setView(bounds.getCenter(), 15);
    else m.fitBounds(bounds, { padding: [24, 24], maxZoom: 15 });
    map.current = m;
    // A map that mounts hidden (a dialog opening) needs to measure itself again.
    const t = setTimeout(() => m.invalidateSize(), 200);
    return () => { clearTimeout(t); m.remove(); map.current = null; };
  }, [places]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    markers.current.forEach((mk, i) => mk.setIcon(pin(i + 1, i === picked)));
    if (picked !== undefined && map.current) map.current.flyTo([places[picked].lat, places[picked].lng], Math.max(map.current.getZoom(), 15), { duration: 0.5 });
  }, [picked, places]);
  return <div ref={el} className={cn("w-full bg-muted", className)} role="region" aria-label="Map of places" />;
}
