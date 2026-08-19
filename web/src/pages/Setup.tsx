import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { User, Lock, Sparkles, AlertCircle, ArrowRight } from "lucide-react";
import { api, ApiError } from "../lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";

// First-run setup: only reachable while no user exists (server locks POST
// after the first user; this page also self-redirects when needed=false).
export default function Setup() {
  const nav = useNavigate();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    api<{ needed: boolean }>("/setup/status")
      .then((s) => { if (!s.needed) nav("/login"); })
      .catch(() => { /* 401-redirect handles network/auth surprises */ });
  }, [nav]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setLoading(true);
    try {
      await api("/setup", { method: "POST", body: JSON.stringify({ username, password }) });
      nav("/login");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "setup failed");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center">
      <Card className="w-96">
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Sparkles className="h-5 w-5" /> Welcome to Ku-CRUD
          </CardTitle>
          <CardDescription>Create the first admin user to get started.</CardDescription>
        </CardHeader>
        <form onSubmit={handleSubmit}>
          <CardContent className="grid gap-3">
            <div className="grid gap-1">
              <Label htmlFor="su">Username</Label>
              <div className="relative">
                <User className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                <Input id="su" className="pl-8" value={username} onChange={(e) => setUsername(e.target.value)} required />
              </div>
            </div>
            <div className="grid gap-1">
              <Label htmlFor="sp">Password</Label>
              <div className="relative">
                <Lock className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                <Input id="sp" className="pl-8" type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={4} />
              </div>
            </div>
            {error && (
              <p className="flex items-center gap-1 text-sm text-destructive">
                <AlertCircle className="h-4 w-4" /> {error}
              </p>
            )}
          </CardContent>
          <CardFooter>
            <Button className="w-full" type="submit" disabled={loading}>
              Create user <ArrowRight className="ml-2 h-4 w-4" />
            </Button>
          </CardFooter>
        </form>
      </Card>
    </div>
  );
}
