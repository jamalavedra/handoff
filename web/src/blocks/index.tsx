import { lazy, Suspense, type ComponentType } from "react";
import { ActionBlock } from "./action";
import { ImagesBlock } from "./images";
import { PeopleBlock } from "./people";
import { ReminderBlock } from "./reminder";

// Blocks are the small artifacts the agent attaches to a message instead of a wall of text: images,
// a map, a route, a reminder, people to connect with, an action waiting for a tap. The server resolves each kind
// (internal/core/blocks.go, blocks_artifacts.go); here each kind has a renderer, and kinds this
// version doesn't know are skipped. Adding a kind is one renderer here and one plugin there.

export type Block = { type: string; data: unknown };

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const renderers: Record<string, ComponentType<{ data: any }>> = {
  images: ImagesBlock,
  map: lazy(() => import("./map")), // Leaflet only loads when a map is shown
  route: lazy(() => import("./route")),
  reminder: ReminderBlock,
  people: PeopleBlock,
  action: ActionBlock,
};

export function Blocks({ blocks, className }: { blocks?: Block[]; className?: string }) {
  const known = (blocks ?? []).filter((b) => renderers[b.type]);
  if (!known.length) return null;
  return (
    <div className={className}>
      {known.map((b, i) => {
        const R = renderers[b.type];
        return (
          <Suspense key={i} fallback={<div className="my-2 h-48 animate-pulse rounded-xl bg-muted" />}>
            <div className="my-2"><R data={b.data} /></div>
          </Suspense>
        );
      })}
    </div>
  );
}
