import { useState } from "react";
import { LogIn } from "lucide-react";
import { ApiError, login } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/label";
import { APP_NAME, APP_TAGLINE, AppLogo, MapServerCredit } from "@/components/ui/credit";

/** Username + password sign-in (MSAPI_ADMIN_USER / MSAPI_ADMIN_PASSWORD in server/.env). */
export function Login({ onLogin }: { onLogin: () => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      await login(username.trim(), password);
      onLogin();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Cannot reach the API.");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex min-h-full items-center justify-center bg-gradient-to-br from-background to-accent/40 p-4">
      <Card className="w-full max-w-sm shadow-lg">
        <CardHeader className="items-center text-center">
          <AppLogo className="mb-2 size-12 drop-shadow-sm" />
          <CardTitle className="text-xl">{APP_NAME}</CardTitle>
          <CardDescription>{APP_TAGLINE}</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit} className="grid gap-4">
            <Field label="Username" htmlFor="username">
              <Input
                id="username"
                autoFocus
                autoComplete="username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                aria-invalid={!!error}
              />
            </Field>
            <Field label="Password" htmlFor="password" error={error}>
              <Input
                id="password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                aria-invalid={!!error}
              />
            </Field>
            <Button type="submit" disabled={!username.trim() || !password || busy}>
              <LogIn /> {busy ? "Signing in…" : "Sign in"}
            </Button>
          </form>
          <MapServerCredit className="mt-5 text-center" />
        </CardContent>
      </Card>
    </div>
  );
}
