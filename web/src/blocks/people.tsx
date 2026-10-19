import { useState } from "react";
import { ExternalLink, UserPlus } from "lucide-react";
import { initials, network } from "./artifacts";

type Person = { name: string; url: string; network: string; handle?: string; headline?: string; note?: string; avatar?: string };

// PeopleBlock: people worth connecting with, a card each in a row that scrolls sideways, with a
// button to connect or follow on their network. Pictures come through the server's image proxy.
export function PeopleBlock({ data }: { data: { people?: Person[] } }) {
  const people = data.people ?? [];
  if (!people.length) return null;
  return (
    <div className="-mx-1 flex snap-x gap-2 overflow-x-auto px-1 pb-1">
      {people.map((p) => <Card key={p.url} p={p} single={people.length === 1} />)}
    </div>
  );
}

function Card({ p, single }: { p: Person; single: boolean }) {
  const [broken, setBroken] = useState(false);
  const n = network(p.network);
  return (
    <div className={single ? "flex w-full items-center gap-3 rounded-xl border border-border bg-card p-2.5" : "flex w-56 shrink-0 snap-start flex-col gap-2 rounded-xl border border-border bg-card p-2.5"}>
      <div className="flex min-w-0 flex-1 items-center gap-2.5">
        {p.avatar && !broken ? (
          <img src={p.avatar} alt="" loading="lazy" referrerPolicy="no-referrer" onError={() => setBroken(true)} className="size-11 shrink-0 rounded-full bg-muted object-cover" />
        ) : (
          <span className="flex size-11 shrink-0 items-center justify-center rounded-full bg-violet-100 text-sm font-semibold text-violet-700" aria-hidden>{initials(p.name)}</span>
        )}
        <span className="min-w-0">
          <span className="block truncate text-sm font-medium">{p.name}</span>
          {p.headline && <span className="block truncate text-xs text-muted-foreground">{p.headline}</span>}
          <span className="block truncate text-[11px] text-muted-foreground/80">{n.label}{p.handle && ` · ${p.handle}`}</span>
        </span>
      </div>
      {p.note && !single && <p className="line-clamp-2 text-xs text-muted-foreground">{p.note}</p>}
      <a
        href={p.url}
        target="_blank"
        rel="noreferrer noopener"
        onClick={(e) => e.stopPropagation()}
        aria-label={`${n.action} ${p.name} on ${n.label}`}
        className={`flex h-8 shrink-0 items-center justify-center gap-1.5 rounded-full px-3 text-xs font-medium transition-transform active:scale-[0.97] ${p.network === "web" ? "border border-border hover:bg-muted" : "bg-primary text-primary-foreground"}`}
      >
        {p.network === "web" ? <ExternalLink className="size-3.5" /> : <UserPlus className="size-3.5" />}{n.action}
      </a>
    </div>
  );
}
