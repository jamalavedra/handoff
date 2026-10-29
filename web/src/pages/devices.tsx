import { useEffect, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Smartphone } from "lucide-react";
import { api } from "../lib";
import { Card, ErrorText } from "../components/app";
import { SettingsSection } from "../components/settings";
import { Button } from "@/components/ui/button";
import { SettingsHeader } from "./settings";

// Devices pairs the phone app: the signed-in person shows a code, the app redeems it for a session.
export function DevicesPage() {
  const code = useMutation({ mutationFn: () => api.post<{ code: string; expires_at: number }>("/api/app/code") });
  const [expired, setExpired] = useState(false);
  const at = code.data?.expires_at;
  useEffect(() => {
    if (!at) return;
    setExpired(false);
    const t = setTimeout(() => setExpired(true), Math.max(0, at * 1000 - Date.now()));
    return () => clearTimeout(t);
  }, [at]);
  return (
    <>
      <SettingsHeader active="devices" />
      <SettingsSection title="Devices" description="Sign in to the Handoff app on your phone with a code instead of an email link.">
        <Card className="flex flex-col items-center gap-4 px-4 py-10 text-center">
          {code.data && !expired ? (
            <>
              <div className="font-mono text-4xl font-semibold tracking-[0.15em]">{code.data.code}</div>
              <p className="text-sm text-muted-foreground">Enter this in the Handoff app — works for 10 minutes.</p>
            </>
          ) : (
            <p className="text-sm text-muted-foreground">{expired ? "That code expired — make a new one." : "Get a one-time code to sign in on your phone."}</p>
          )}
          <Button onClick={() => code.mutate()} disabled={code.isPending}>
            <Smartphone />{code.isPending ? "Making…" : code.data ? "Regenerate" : "Pair a device"}
          </Button>
          <ErrorText error={code.error} />
        </Card>
      </SettingsSection>
    </>
  );
}
