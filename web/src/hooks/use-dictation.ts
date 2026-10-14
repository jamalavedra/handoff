import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { appendSpoken, dictationError, speechLang, transcript, type ResultList } from "../dictation";

// The slice of the Web Speech API we use; TypeScript's DOM lib doesn't ship it.
type Recognition = {
  lang: string;
  interimResults: boolean;
  continuous: boolean;
  maxAlternatives: number;
  onresult: ((e: { results: ResultList }) => void) | null;
  onerror: ((e: { error: string }) => void) | null;
  onend: (() => void) | null;
  start(): void;
  stop(): void;
  abort(): void;
};
type RecognitionCtor = new () => Recognition;

const recognitionCtor = (): RecognitionCtor | undefined => {
  const w = window as unknown as { SpeechRecognition?: RecognitionCtor; webkitSpeechRecognition?: RecognitionCtor };
  return w.SpeechRecognition ?? w.webkitSpeechRecognition;
};

type Session = { rec: Recognition; stream?: MediaStream; audio?: AudioContext; ended: boolean; toasted: boolean };

// useDictation streams speech into a text field via the browser's own recognizer (Chrome sends audio to
// its speech service; nothing goes to our API). start(base) keeps what's already typed and calls onText
// with base + transcript on every interim and final result. A pause ends the session, as does stop().
// A separate mic stream feeds an AnalyserNode so the UI can draw a live waveform.
export function useDictation(onText: (text: string) => void) {
  const [supported] = useState(() => !!recognitionCtor());
  const [listening, setListening] = useState(false);
  const [analyser, setAnalyser] = useState<AnalyserNode | null>(null);
  const session = useRef<Session | null>(null);
  const onTextRef = useRef(onText);
  useEffect(() => { onTextRef.current = onText; });

  const end = useCallback((s: Session) => {
    if (s.ended) return;
    s.ended = true;
    s.stream?.getTracks().forEach((t) => t.stop());
    s.audio?.close().catch(() => {});
    if (session.current === s) {
      session.current = null;
      setListening(false);
      setAnalyser(null);
    }
  }, []);

  const start = useCallback((base: string) => {
    const Ctor = recognitionCtor();
    if (!Ctor || session.current) return;
    const rec = new Ctor();
    rec.lang = speechLang(document.documentElement.lang, navigator.language);
    rec.interimResults = true;
    rec.continuous = false;
    rec.maxAlternatives = 1;
    const s: Session = { rec, ended: false, toasted: false };
    rec.onresult = (e) => {
      if (s.ended) return;
      const t = transcript(e.results);
      onTextRef.current(appendSpoken(base, [t.final, t.interim].filter(Boolean).join(" ")));
    };
    rec.onerror = (e) => {
      const msg = dictationError(e.error);
      if (msg && !s.toasted) { s.toasted = true; toast.error(msg); }
    };
    rec.onend = () => end(s);
    try {
      rec.start();
    } catch {
      toast.error(dictationError("")!);
      return;
    }
    session.current = s;
    setListening(true);
    // The waveform is decoration: if the mic stream fails, recognition reports its own permission error.
    navigator.mediaDevices?.getUserMedia({ audio: true }).then((stream) => {
      if (s.ended) { stream.getTracks().forEach((t) => t.stop()); return; }
      const audio = new AudioContext();
      const node = audio.createAnalyser();
      node.fftSize = 256;
      audio.createMediaStreamSource(stream).connect(node);
      Object.assign(s, { stream, audio });
      setAnalyser(node);
    }, () => {});
  }, [end]);

  // stop finishes the utterance (a final result may still arrive); cancel drops it, e.g. when the user types.
  const stop = useCallback(() => session.current?.rec.stop(), []);
  const cancel = useCallback(() => {
    const s = session.current;
    if (!s) return;
    s.rec.abort();
    end(s);
  }, [end]);

  useEffect(() => () => {
    const s = session.current;
    if (!s) return;
    s.rec.abort();
    end(s);
  }, [end]);

  const toggle = useCallback((base: string) => (session.current ? stop() : start(base)), [start, stop]);
  return { supported, listening, analyser, start, stop, cancel, toggle };
}
