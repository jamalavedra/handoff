import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Bot } from "lucide-react";
import { api, type Me } from "../lib";
import { Button, Card, ErrorText, Input } from "../components/ui";
import { useMe } from "../main";

function Centered({ children }: { children: React.ReactNode }) {
  return <div className="flex min-h-screen items-center justify-center p-4"><Card className="w-full max-w-sm p-6">{children}</Card></div>;
}

function DevLink({ link }: { link?: string }) {
  if (!link) return null;
  return (
    <p className="mt-3 rounded-md bg-amber-50 p-2 text-xs text-amber-800">
      Dev mode: <a className="underline" href={link}>open the sign-in link</a>
    </p>
  );
}

export function LoginPage() {
  const me = useMe();
  const params = new URLSearchParams(window.location.search);
  const next = params.get("next") ?? "/";
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState<{ dev_link?: string } | null>(null);
  const [error, setError] = useState<unknown>(params.get("error") === "expired" ? "That link expired or was already used. Ask for a new one." : params.get("error") === "denied" ? "That account isn't a member of this workspace." : null);
  useEffect(() => {
    if (me.data?.member) window.location.replace(next);
  }, [me.data, next]);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      setSent(await api.post("/api/login", { email, next }));
    } catch (err) {
      setError(err);
    }
  };
  return (
    <Centered>
      <h1 className="text-lg font-semibold">Sign in to {me.data?.org?.name}</h1>
      <p className="mb-4 text-sm text-zinc-500">Handoff · {window.location.host}</p>
      {sent ? (
        <>
          <p className="text-sm">If <b>{email}</b> is a member, a sign-in link is on its way.</p>
          <DevLink link={sent.dev_link} />
        </>
      ) : (
        <form onSubmit={submit} className="flex flex-col gap-3">
          <Input type="email" required placeholder="you@company.com" value={email} onChange={(e) => setEmail(e.target.value)} autoFocus aria-label="Email" />
          <Button type="submit">Email me a link</Button>
          {me.data?.google && (
            <Button type="button" variant="outline" onClick={() => (window.location.href = `/auth/google?next=${encodeURIComponent(next)}`)}>
              Continue with Google
            </Button>
          )}
          <ErrorText error={error} />
        </form>
      )}
    </Centered>
  );
}

export function SignupPage({ me }: { me: Me }) {
  const [f, setF] = useState({ name: "", slug: "", email: "" });
  const [done, setDone] = useState<{ url: string; dev_link?: string } | null>(null);
  const [error, setError] = useState<unknown>(null);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      setDone(await api.post("/api/signup", f));
    } catch (err) {
      setError(err);
    }
  };
  return (
    <Centered>
      <h1 className="text-lg font-semibold">Handoff</h1>
      <p className="mb-4 text-sm text-zinc-500">One task list for your company's people and agents.</p>
      {!me.signup ? (
        <p className="text-sm">Signup is closed on this server. Ask your admin for your workspace address.</p>
      ) : done ? (
        <>
          <p className="text-sm">Your workspace is ready at <a className="font-medium underline" href={done.url}>{done.url.replace(/^https?:\/\//, "")}</a>. Check your email for the sign-in link.</p>
          <DevLink link={done.dev_link} />
        </>
      ) : (
        <form onSubmit={submit} className="flex flex-col gap-3">
          <label className="text-sm font-medium">Company name
            <Input required value={f.name} onChange={(e) => setF({ ...f, name: e.target.value, slug: f.slug || "" })} placeholder="Acme" />
          </label>
          <label className="text-sm font-medium">Address
            <div className="flex items-center gap-1">
              <Input required value={f.slug} onChange={(e) => setF({ ...f, slug: e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "") })} placeholder="acme" />
              <span className="text-sm text-zinc-500">.{me.base_host}</span>
            </div>
          </label>
          <label className="text-sm font-medium">Your work email
            <Input required type="email" value={f.email} onChange={(e) => setF({ ...f, email: e.target.value })} placeholder="you@acme.com" />
          </label>
          <Button type="submit">Create workspace</Button>
          <ErrorText error={error} />
        </form>
      )}
    </Centered>
  );
}

export function AuthorizePage() {
  const id = new URLSearchParams(window.location.search).get("request") ?? "";
  const info = useQuery({ queryKey: ["oauth", id], queryFn: () => api.get<{ client_name: string; redirect_host: string; org: string }>(`/api/oauth/request/${id}`), refetchInterval: false });
  const me = useMe();
  const [name, setName] = useState("");
  const [error, setError] = useState<unknown>(null);
  useEffect(() => {
    if (info.data && me.data?.member) setName(`${info.data.client_name} (${me.data.member.name})`);
  }, [info.data, me.data]);
  const decide = async (approve: boolean) => {
    try {
      const r = await api.post<{ redirect: string }>("/api/oauth/consent", { request: id, approve, name });
      window.location.href = r.redirect;
    } catch (err) {
      setError(err);
    }
  };
  if (info.error) return <Centered><ErrorText error={info.error} /></Centered>;
  if (!info.data) return null;
  return (
    <Centered>
      <div className="mb-3 flex size-10 items-center justify-center rounded-full bg-violet-100"><Bot className="size-5 text-violet-700" /></div>
      <h1 className="text-lg font-semibold">Connect {info.data.client_name} to {info.data.org}?</h1>
      <p className="mt-1 text-sm text-zinc-600">
        It will join Handoff as an agent owned by you, and can read and change tasks you can see. Deleting needs your approval. You can change its permissions or disconnect it any time on the Agents page.
      </p>
      <p className="mt-2 text-xs text-zinc-500">Returns to {info.data.redirect_host}</p>
      <label className="mt-4 block text-sm font-medium">Agent name
        <Input value={name} onChange={(e) => setName(e.target.value)} />
      </label>
      <div className="mt-4 flex gap-2">
        <Button className="flex-1" onClick={() => decide(true)}>Allow</Button>
        <Button className="flex-1" variant="outline" onClick={() => decide(false)}>Deny</Button>
      </div>
      <ErrorText error={error} />
    </Centered>
  );
}
