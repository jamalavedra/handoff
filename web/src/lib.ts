import type { Block } from "./blocks";
import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";
import type { ActivityStep } from "./activity";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: { "Content-Type": "application/json", "X-Handoff": "1" },
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: "same-origin",
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, data.error ?? res.statusText);
  return data as T;
}

export const api = {
  get: <T>(path: string) => req<T>("GET", path),
  post: <T>(path: string, body?: unknown) => req<T>("POST", path, body),
  del: <T>(path: string) => req<T>("DELETE", path),
  tool: <T>(name: string, input: unknown = {}) => req<T>("POST", `/api/tools/${name}`, input),
};

export type MemberRef = { id: string; name: string; kind: "human" | "agent" };

export type Task = {
  id: string;
  title: string;
  body?: string;
  status: "open" | "in_progress" | "blocked" | "done" | "cancelled";
  priority: number;
  project: string;
  project_id: string;
  assignee?: MemberRef;
  due?: string;
  labels: string[];
  claimed_by?: MemberRef;
  claim_expires_at?: string;
  // the person the workspace's agent handed this back to for their input
  waiting_on?: MemberRef;
  created_by?: MemberRef;
  on_behalf_of?: MemberRef;
  created_at: string;
  updated_at: string;
  closed_at?: string;
  close_reason?: string;
  deleted_at?: string;
  blocked_by: string[];
  blocks: string[];
  ready: boolean;
  unseen?: boolean;
  // pending while the workspace's triage agent evaluates a new task.
  triage?: "pending" | "done" | "failed";
  triage_error?: string;
  // The start of the newest comment an agent left: its one-line status.
  agent_note?: string;
  repeat?: "daily" | "weekdays" | "weekly" | "biweekly" | "monthly" | "yearly";
};

export type Project = { id: string; name: string; description?: string; emoji?: string; visibility: string; open_tasks: number; members?: string[] };

export type Permissions = { projects: string[]; actions: string[]; approval: string[] };

export type Webhook = { url: string; secret?: string; last_at?: string; last_status?: number; last_error?: string };

export type Member = {
  id: string;
  kind: "human" | "agent";
  name: string;
  email?: string;
  role: string;
  status: string;
  owner?: MemberRef;
  permissions?: Permissions;
  created_at: string;
  webhook?: Webhook;
};

export type Event = {
  id: number;
  task_id?: string;
  task_title?: string;
  actor: MemberRef;
  on_behalf_of?: MemberRef;
  via: string;
  kind: string;
  data: Record<string, unknown>;
  at: string;
};

export type Reaction = { emoji: string; count: number; mine?: boolean; by: string[] };

// Lookup is one call the agent made to a connected service (web search, an internal tool…) for a message.
export type Lookup = { source: string; tool: string; query?: string; failed?: boolean };
export type Comment = { id: string; author: MemberRef; on_behalf_of?: MemberRef; body: string; created_at: string; reactions?: Reaction[]; lookups?: Lookup[]; activity?: ActivityStep[]; options?: string[]; blocks?: Block[] };

export type Me = {
  apex?: boolean;
  base_host?: string;
  signup?: boolean;
  org?: { id?: string; name: string; slug: string; custom_domain?: string; allowed_email_domain?: string };
  member: { id: string; name: string; email: string; role: string } | null;
  mcp_url?: string;
  base_url?: string;
  google?: boolean;
};

export function ago(iso?: string) {
  if (!iso) return "";
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  if (s < 86400 * 7) return `${Math.floor(s / 86400)}d ago`;
  return new Date(iso).toLocaleDateString();
}

export const statusLabel: Record<Task["status"], string> = {
  open: "Open",
  in_progress: "In progress",
  blocked: "Blocked",
  done: "Done",
  cancelled: "Cancelled",
};

export const projectName = (p: { name: string; emoji?: string }) => (p.emoji ? `${p.emoji} ${p.name}` : p.name);

export type Notification = {
  id: number;
  kind: "mention" | "assigned" | "approval" | "agent_done";
  task_id: string;
  task_title: string;
  actor?: MemberRef;
  excerpt?: string;
  read: boolean;
  at: string;
};
