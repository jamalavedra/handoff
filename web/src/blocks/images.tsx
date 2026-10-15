import { useState } from "react";

type Image = { src: string; page: string; host: string; caption?: string };

// ImagesBlock: the pictures of the pages the agent found, in a row that scrolls sideways; each opens
// its page. Images load straight from their sites, without a referrer.
export function ImagesBlock({ data }: { data: { images?: Image[] } }) {
  const [broken, setBroken] = useState<string[]>([]);
  const images = (data.images ?? []).filter((i) => !broken.includes(i.src));
  if (!images.length) return null;
  return (
    <div className="-mx-1 flex snap-x gap-2 overflow-x-auto px-1 pb-1">
      {images.map((i) => (
        <a key={i.src} href={i.page} target="_blank" rel="noreferrer noopener" className="group w-44 shrink-0 snap-start overflow-hidden rounded-xl border border-border bg-card" onClick={(e) => e.stopPropagation()}>
          <img src={i.src} alt={i.caption ?? i.host} loading="lazy" referrerPolicy="no-referrer" onError={() => setBroken((b) => [...b, i.src])} className="h-32 w-full bg-muted object-cover transition-transform group-hover:scale-[1.02]" />
          <span className="block px-2 py-1.5">
            {i.caption && <span className="block truncate text-xs font-medium text-foreground">{i.caption}</span>}
            <span className="block truncate text-[11px] text-muted-foreground">{i.host}</span>
          </span>
        </a>
      ))}
    </div>
  );
}
