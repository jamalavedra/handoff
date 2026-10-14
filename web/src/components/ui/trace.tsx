import * as React from "react"
import { ChevronRight } from "lucide-react"

import { cn } from "@/lib"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"

// Trace: shadcn/ui's trace component (ui.shadcn.com/docs/components/trace), adapted to this app's
// new-york setup: lucide icons, Radix data-state attributes, and TraceTitle's running prop for the
// app's agent-shimmer.

function Trace({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="trace"
      className={cn(
        "flex w-full min-w-0 flex-col gap-1.5 *:has-[>[data-slot=trace-line]]:flex *:has-[>[data-slot=trace-line]]:w-fit *:has-[>[data-slot=trace-line]]:max-w-full *:has-[>[data-slot=trace-line]]:min-w-0 *:has-[>[data-slot=trace-line]]:rounded-md *:has-[>[data-slot=trace-line]]:text-left *:has-[>[data-slot=trace-line]]:outline-none *:has-[>[data-slot=trace-line]]:focus-visible:ring-3 *:has-[>[data-slot=trace-line]]:focus-visible:ring-ring/50",
        className
      )}
      {...props}
    />
  )
}

function TraceLine({
  className,
  status,
  ...props
}: React.ComponentProps<"span"> & {
  status?: "running" | "error" | "pending"
}) {
  return (
    <span
      data-slot="trace-line"
      data-status={status}
      className={cn(
        "flex w-fit max-w-full min-w-0 items-center gap-2 text-xs text-muted-foreground transition-colors in-[a,button]:cursor-pointer in-[a:hover,button:hover]:text-foreground data-[status=error]:text-amber-700 dark:data-[status=error]:text-amber-500 data-[status=pending]:opacity-60",
        className
      )}
      {...props}
    />
  )
}

function TraceIcon({ className, ...props }: React.ComponentProps<"span">) {
  return (
    <span
      data-slot="trace-icon"
      aria-hidden="true"
      className={cn(
        "flex size-3.5 shrink-0 items-center justify-center [&_svg]:pointer-events-none [&_svg:not([class*='size-'])]:size-3.5 [[data-slot=trace-title]~&]:-ml-1",
        className
      )}
      {...props}
    />
  )
}

// A running line's title shimmers (the app's agent-shimmer, which stands still for reduced motion).
function TraceTitle({ className, running, ...props }: React.ComponentProps<"span"> & { running?: boolean }) {
  return <span data-slot="trace-title" className={cn("min-w-0 truncate", running && "agent-shimmer", className)} {...props} />
}

function TraceDescription({ className, ...props }: React.ComponentProps<"span">) {
  return <span data-slot="trace-description" className={cn("shrink-0 text-muted-foreground/70 tabular-nums", className)} {...props} />
}

function TraceCollapsible({ className, ...props }: Omit<React.ComponentProps<typeof Collapsible>, "asChild">) {
  return <Collapsible data-slot="trace-collapsible" className={cn("flex w-full min-w-0 flex-col", className)} {...props} />
}

function TraceCollapsibleTrigger({ className, children, ...props }: Omit<React.ComponentProps<typeof CollapsibleTrigger>, "asChild">) {
  return (
    <CollapsibleTrigger
      data-slot="trace-collapsible-trigger"
      className={cn(
        "group/trace-collapsible-trigger flex w-fit max-w-full min-w-0 cursor-pointer items-center gap-1 rounded-md text-left text-muted-foreground transition-colors outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50",
        className
      )}
      {...props}
    >
      {children}
      <TraceIcon className="transition-transform group-data-[state=open]/trace-collapsible-trigger:rotate-90">
        <ChevronRight />
      </TraceIcon>
    </CollapsibleTrigger>
  )
}

function TraceCollapsibleContent({ className, children, ...props }: Omit<React.ComponentProps<typeof CollapsibleContent>, "asChild">) {
  return (
    <CollapsibleContent data-slot="trace-collapsible-content" className="min-w-0 overflow-hidden" {...props}>
      <div
        data-slot="trace-collapsible-content-inner"
        className={cn("mt-1.5 mb-1 ml-[calc(--spacing(1.5)-1px)] min-w-0 border-l pr-1 pl-3 leading-relaxed wrap-break-word text-muted-foreground", className)}
      >
        {children}
      </div>
    </CollapsibleContent>
  )
}

export {
  Trace,
  TraceLine,
  TraceIcon,
  TraceTitle,
  TraceDescription,
  TraceCollapsible,
  TraceCollapsibleTrigger,
  TraceCollapsibleContent,
}
