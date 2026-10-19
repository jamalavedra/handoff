// The plain logic behind the route, reminder and people blocks: no React, so it's tested on its own
// (artifacts.test.ts).

export type Mode = "walk" | "bike" | "drive";

// duration reads a number of seconds as "52 min" or "1 h 5 min".
export function duration(s: number) {
  const m = Math.max(1, Math.round(s / 60));
  if (m < 60) return `${m} min`;
  const h = Math.floor(m / 60);
  return m % 60 ? `${h} h ${m % 60} min` : `${h} h`;
}

// distance reads metres as "850 m" or "4.3 km".
export const distance = (m: number) => (m < 1000 ? `${Math.round(m)} m` : `${(m / 1000).toFixed(m < 10000 ? 1 : 0)} km`);

// directionsURL opens the same trip in Google Maps, for turn-by-turn on a phone.
export function directionsURL(from: { lat: number; lng: number }, to: { lat: number; lng: number }, mode: Mode) {
  const travel = { walk: "walking", bike: "bicycling", drive: "driving" }[mode];
  return `https://www.google.com/maps/dir/?api=1&origin=${from.lat},${from.lng}&destination=${to.lat},${to.lng}&travelmode=${travel}`;
}

export type Reminder = { title: string; at: string; all_day: boolean; note?: string; task?: string };

// when reads a reminder's date as the person would say it, relative to now: "Tomorrow, 9:30 AM",
// "Fri, Oct 24", "Oct 24, 2027".
export function when(r: Reminder, now = new Date()) {
  const d = localDate(r.at);
  const days = Math.round((startOfDay(d).getTime() - startOfDay(now).getTime()) / 86400000);
  const day =
    days === 0 ? "Today" : days === 1 ? "Tomorrow" : days === -1 ? "Yesterday"
    : days > 1 && days < 7 ? d.toLocaleDateString(undefined, { weekday: "long" })
    : d.toLocaleDateString(undefined, { weekday: "short", month: "short", day: "numeric", ...(d.getFullYear() !== now.getFullYear() && { year: "numeric" }) });
  return r.all_day ? day : `${day}, ${d.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" })}`;
}

// daysLeft is how many days until the reminder (negative when past).
export const daysLeft = (r: Reminder, now = new Date()) => Math.round((startOfDay(localDate(r.at)).getTime() - startOfDay(now).getTime()) / 86400000);

// localDate reads "2026-10-24" or "2026-10-24T09:30" as that day and time where the person is.
export function localDate(at: string) {
  const [d, t = "00:00"] = at.split("T");
  const [y, mo, da] = d.split("-").map(Number);
  const [h, mi] = t.split(":").map(Number);
  return new Date(y, mo - 1, da, h, mi);
}

const startOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate());
const pad = (n: number) => String(n).padStart(2, "0");
const ymd = (d: Date) => `${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}`;
const hms = (d: Date) => `T${pad(d.getHours())}${pad(d.getMinutes())}00`;

// ics is the reminder as a calendar file: an all-day event, or a 30-minute one at its time (floating,
// so it lands at that hour wherever the calendar is), with an alarm.
export function ics(r: Reminder, uid: string, stamp = new Date()) {
  const start = localDate(r.at);
  const end = r.all_day ? new Date(start.getFullYear(), start.getMonth(), start.getDate() + 1) : new Date(start.getTime() + 30 * 60000);
  const esc = (s: string) => s.replace(/[\\;,]/g, (c) => `\\${c}`).replace(/\r?\n/g, "\\n");
  const utc = stamp.toISOString().replace(/[-:]/g, "").replace(/\.\d+/, "");
  return [
    "BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Handoff//Reminder//EN", "BEGIN:VEVENT",
    `UID:${uid}@handoff`, `DTSTAMP:${utc}`,
    r.all_day ? `DTSTART;VALUE=DATE:${ymd(start)}` : `DTSTART:${ymd(start)}${hms(start)}`,
    r.all_day ? `DTEND;VALUE=DATE:${ymd(end)}` : `DTEND:${ymd(end)}${hms(end)}`,
    `SUMMARY:${esc(r.title)}`, ...(r.note ? [`DESCRIPTION:${esc(r.note)}`] : []),
    "BEGIN:VALARM", "ACTION:DISPLAY", `DESCRIPTION:${esc(r.title)}`, r.all_day ? "TRIGGER:PT9H" : "TRIGGER:-PT15M", "END:VALARM",
    "END:VEVENT", "END:VCALENDAR", "",
  ].join("\r\n");
}

// googleCalendarURL adds the reminder to Google Calendar.
export function googleCalendarURL(r: Reminder) {
  const start = localDate(r.at);
  const end = r.all_day ? new Date(start.getFullYear(), start.getMonth(), start.getDate() + 1) : new Date(start.getTime() + 30 * 60000);
  const dates = r.all_day ? `${ymd(start)}/${ymd(end)}` : `${ymd(start)}${hms(start)}/${ymd(end)}${hms(end)}`;
  const q = new URLSearchParams({ action: "TEMPLATE", text: r.title, dates, ...(r.note && { details: r.note }) });
  return `https://calendar.google.com/calendar/render?${q}`;
}

export type Network = "linkedin" | "github" | "x" | "instagram" | "facebook" | "youtube" | "tiktok" | "mastodon" | "web";

// networks name each network, and what following someone there is called.
export const networks: Record<Network, { label: string; action: string }> = {
  linkedin: { label: "LinkedIn", action: "Connect" },
  github: { label: "GitHub", action: "Follow" },
  x: { label: "X", action: "Follow" },
  instagram: { label: "Instagram", action: "Follow" },
  facebook: { label: "Facebook", action: "Add" },
  youtube: { label: "YouTube", action: "Subscribe" },
  tiktok: { label: "TikTok", action: "Follow" },
  mastodon: { label: "Mastodon", action: "Follow" },
  web: { label: "Website", action: "Visit" },
};

export const network = (n: string) => networks[n as Network] ?? networks.web;

// initials for a profile without a picture: "Jane Doe" → "JD".
export const initials = (name: string) =>
  name.split(/\s+/).filter(Boolean).slice(0, 2).map((w) => [...w][0]?.toUpperCase() ?? "").join("") || "?";
