// Pure helpers behind voice dictation (hooks/use-dictation.ts); kept DOM-free so node:test can run them.

// The shape of a SpeechRecognitionResultList we read: each result's first alternative and whether it's final.
export type ResultList = ArrayLike<ArrayLike<{ transcript: string }> & { isFinal: boolean }>;

// transcript reads the whole result list each event: finals are settled, interim is the live guess after them.
export function transcript(results: ResultList): { final: string; interim: string } {
  let final = "";
  let interim = "";
  for (let i = 0; i < results.length; i++) {
    const r = results[i];
    if (!r.length) continue;
    if (r.isFinal) final += r[0].transcript;
    else interim += r[0].transcript;
  }
  return { final: final.replace(/\s+/g, " ").trim(), interim: interim.replace(/\s+/g, " ").trim() };
}

// appendSpoken adds dictated words after what was already typed, with exactly one space between.
export function appendSpoken(base: string, spoken: string): string {
  spoken = spoken.trim();
  if (!spoken) return base;
  if (!base.trim()) return spoken;
  return /\s$/.test(base) ? base + spoken : base + " " + spoken;
}

// speechLang picks the recognition language: the page's lang, else the browser's, else English.
export function speechLang(pageLang?: string | null, browserLang?: string | null): string {
  return pageLang?.trim() || browserLang?.trim() || "en-US";
}

// dictationError turns a SpeechRecognition error code into a toast, or null when there's nothing to say
// (the user stopped it, or it was cut off by us).
export function dictationError(code: string): string | null {
  switch (code) {
    case "aborted":
      return null;
    case "not-allowed":
    case "service-not-allowed":
      return "Microphone access is blocked. Allow it in your browser's site settings to dictate.";
    case "no-speech":
      return "Didn't catch anything. Try again a little closer to the mic.";
    case "audio-capture":
      return "No microphone found.";
    case "network":
      return "Dictation needs a connection to your browser's speech service. Check your network and try again.";
    case "language-not-supported":
      return "Dictation isn't available in this language yet.";
    default:
      return "Dictation stopped unexpectedly. Try again.";
  }
}

// level is the loudness of one time-domain frame from an AnalyserNode (bytes centred on 128), as 0–1.
// Speech sits far below full scale, so it's boosted and clamped to keep the waveform lively.
export function level(frame: ArrayLike<number>): number {
  if (!frame.length) return 0;
  let sum = 0;
  for (let i = 0; i < frame.length; i++) {
    const v = (frame[i] - 128) / 128;
    sum += v * v;
  }
  return Math.min(1, Math.sqrt(sum / frame.length) * 4);
}
