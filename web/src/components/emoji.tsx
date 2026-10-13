import { useEffect, useRef, useState } from "react";
import { Smile } from "lucide-react";
import { cn } from "../lib";
import { Button, Input } from "./ui";

const choices = ["📥", "💼", "⚖️", "🚀", "🎯", "📈", "💡", "🛠️", "🧪", "📣", "🤝", "💰", "🧾", "📦", "🗓️", "✈️", "🏠", "❤️", "⭐", "🔥", "🐛", "📚", "🎨", "🤖"];

export function EmojiPicker({ value, onChange, label }: { value: string; onChange: (emoji: string) => void; label: string }) {
  const [open, setOpen] = useState(false);
  const [custom, setCustom] = useState("");
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const away = (e: MouseEvent) => { if (!ref.current?.contains(e.target as Node)) setOpen(false); };
    const esc = (e: KeyboardEvent) => { if (e.key === "Escape") { e.preventDefault(); setOpen(false); } };
    document.addEventListener("mousedown", away);
    document.addEventListener("keydown", esc);
    return () => { document.removeEventListener("mousedown", away); document.removeEventListener("keydown", esc); };
  }, [open]);
  const pick = (e: string) => { onChange(e); setOpen(false); setCustom(""); };
  return (
    <div ref={ref} className="relative">
      <button type="button" aria-label={label} aria-expanded={open} onClick={() => setOpen(!open)} className="flex size-10 shrink-0 items-center justify-center rounded-xl border border-zinc-200 bg-white text-xl hover:border-zinc-300 md:size-9">
        {value || <Smile className="size-4 text-zinc-400" />}
      </button>
      {open && (
        <div role="dialog" aria-label="Pick an emoji" className="absolute top-full right-0 z-30 mt-1 w-64 rounded-2xl border border-zinc-200 bg-white p-2 shadow-lg sm:right-auto sm:left-0">
          <div className="grid grid-cols-6 gap-1">
            {choices.map((e) => (
              <button type="button" key={e} onClick={() => pick(e)} className={cn("flex size-9 items-center justify-center rounded-lg text-xl hover:bg-zinc-100", value === e && "bg-zinc-100 ring-1 ring-accent")}>{e}</button>
            ))}
          </div>
          <div className="mt-2 flex gap-2">
            <Input placeholder="Or paste one…" value={custom} onChange={(e) => setCustom(e.target.value)} aria-label="Custom emoji" className="h-9 sm:h-8"
              onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); if (custom.trim()) pick(custom.trim()); } }} />
            {value && <Button type="button" size="sm" variant="outline" onClick={() => pick("")}>Remove</Button>}
          </div>
        </div>
      )}
    </div>
  );
}
