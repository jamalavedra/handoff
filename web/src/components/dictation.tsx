import { useEffect, useRef, useSyncExternalStore } from "react";
import { Mic } from "lucide-react";
import { cn } from "../lib";
import { level } from "../dictation";

export const privacyNote = "Speech is transcribed by your browser's speech service; audio never reaches Handoff.";

const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
const useReducedMotion = () =>
  useSyncExternalStore(
    (l) => {
      reducedMotion.addEventListener("change", l);
      return () => reducedMotion.removeEventListener("change", l);
    },
    () => reducedMotion.matches,
  );

// Waveform scrolls the mic's recent loudness right-to-left, one bar per ~60ms. Bars are scaled directly
// in an animation frame loop rather than through React state. Until the analyser is ready (or if the mic
// stream was refused) it idles with a gentle ripple; with reduced motion it holds still.
export function Waveform({ analyser, bars, className }: { analyser: AnalyserNode | null; bars: number; className?: string }) {
  const ref = useRef<HTMLSpanElement>(null);
  const reduced = useReducedMotion();
  useEffect(() => {
    const el = ref.current;
    if (!el || reduced) return;
    const spans = Array.from(el.children) as HTMLElement[];
    const history: number[] = Array(bars).fill(0);
    const frame = new Uint8Array(analyser?.fftSize ?? 0);
    let peak = 0;
    let last = 0;
    let raf = 0;
    const tick = (t: number) => {
      if (analyser) {
        analyser.getByteTimeDomainData(frame);
        peak = Math.max(peak, level(frame));
      } else {
        peak = 0.18 + 0.12 * Math.sin(t / 260);
      }
      if (t - last > 60) {
        last = t;
        history.shift();
        history.push(peak);
        peak = 0;
        spans.forEach((s, i) => { s.style.transform = `scaleY(${Math.max(0.2, history[i])})`; });
      }
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [analyser, bars, reduced]);
  return (
    <span ref={ref} aria-hidden className={cn("flex h-4 items-center gap-[3px]", className)}>
      {Array.from({ length: bars }, (_, i) => (
        <span key={i} className="h-full w-[3px] rounded-full bg-current" style={{ transform: `scaleY(${reduced ? 0.25 + 0.35 * Math.abs(Math.sin(i * 1.7)) : 0.2})` }} />
      ))}
    </span>
  );
}

// MicButton toggles dictation. Callers hide it where the browser has no SpeechRecognition.
// Given an analyser (wave), it swaps the icon for a small live waveform while listening.
export function MicButton({ listening, onToggle, wave, className }: { listening: boolean; onToggle: () => void; wave?: { analyser: AnalyserNode | null }; className?: string }) {
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-pressed={listening}
      aria-label={listening ? "Stop dictation" : "Dictate"}
      title={listening ? "Stop dictation" : `Dictate. ${privacyNote}`}
      className={cn(
        "flex size-9 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none",
        listening && "bg-red-50 text-red-700 hover:bg-red-100 hover:text-red-700",
        className,
      )}
    >
      {listening && wave ? <Waveform analyser={wave.analyser} bars={5} /> : <Mic className="size-[18px]" />}
    </button>
  );
}
