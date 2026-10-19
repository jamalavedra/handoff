import * as React from "react"
import { cn } from "@/lib"
import { DropdownMenu as M } from "radix-ui"
import { Check, ChevronRight } from "lucide-react"

// Menus in the style of the app's popovers: rounded, roomy rows with an icon, a value on the right
// and submenus that open to the side.

const DropdownMenu = M.Root
const DropdownMenuTrigger = M.Trigger
const DropdownMenuSub = M.Sub

const surface = "z-50 min-w-52 overflow-hidden rounded-2xl border border-border bg-popover p-1.5 text-popover-foreground shadow-lg outline-none data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95"
const row = "relative flex w-full cursor-default select-none items-center gap-3 rounded-lg px-2.5 py-2 text-sm outline-none data-[highlighted]:bg-accent data-[disabled]:opacity-50 [&_svg]:size-4 [&_svg]:shrink-0 [&>svg:first-child]:text-muted-foreground"

function DropdownMenuContent({ className, sideOffset = 6, align = "end", ...props }: React.ComponentProps<typeof M.Content>) {
  return <M.Portal><M.Content sideOffset={sideOffset} align={align} className={cn(surface, "max-h-[var(--radix-dropdown-menu-content-available-height)] overflow-y-auto", className)} {...props} /></M.Portal>
}

function DropdownMenuSubContent({ className, ...props }: React.ComponentProps<typeof M.SubContent>) {
  return <M.Portal><M.SubContent sideOffset={6} className={cn(surface, className)} {...props} /></M.Portal>
}

function DropdownMenuItem({ className, ...props }: React.ComponentProps<typeof M.Item>) {
  return <M.Item className={cn(row, className)} {...props} />
}

// A row with a tick on the right when it's on: a sort choice, a filter value, a shown property.
function DropdownMenuCheckItem({ className, checked, children, onSelect, keepOpen, ...props }: Omit<React.ComponentProps<typeof M.Item>, "onSelect"> & { checked: boolean; onSelect: () => void; keepOpen?: boolean }) {
  return (
    <M.Item role="menuitemcheckbox" aria-checked={checked} className={cn(row, className)} onSelect={(e) => { if (keepOpen) e.preventDefault(); onSelect() }} {...props}>
      {children}
      <span className="ml-auto flex w-4 justify-end">{checked && <Check className="!text-foreground" />}</span>
    </M.Item>
  )
}

function DropdownMenuSubTrigger({ className, value, children, ...props }: React.ComponentProps<typeof M.SubTrigger> & { value?: React.ReactNode }) {
  return (
    <M.SubTrigger className={cn(row, "data-[state=open]:bg-accent", className)} {...props}>
      {children}
      <span className="ml-auto flex items-center gap-1.5 pl-4 text-muted-foreground">{value}<ChevronRight className="!text-muted-foreground" /></span>
    </M.SubTrigger>
  )
}

function DropdownMenuLabel({ className, ...props }: React.ComponentProps<typeof M.Label>) {
  return <M.Label className={cn("px-2.5 pt-2 pb-1 text-sm font-medium", className)} {...props} />
}

function DropdownMenuSeparator({ className, ...props }: React.ComponentProps<typeof M.Separator>) {
  return <M.Separator className={cn("-mx-1.5 my-1.5 h-px bg-border", className)} {...props} />
}

export { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem, DropdownMenuCheckItem, DropdownMenuSub, DropdownMenuSubTrigger, DropdownMenuSubContent, DropdownMenuLabel, DropdownMenuSeparator }
