import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, ApiError } from "../lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export default function Login() {
  const nav = useNavigate();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  return (
    <div className="flex min-h-screen items-center justify-center">
      <form
        className="w-72 space-y-4"
        onSubmit={async (e) => {
          e.preventDefault();
          setError("");
          try {
            await api("/auth/login", { method: "POST", body: JSON.stringify({ username, password }) });
            nav("/");
          } catch (err) {
            setError(err instanceof ApiError ? err.message : "login failed");
          }
        }}
      >
        <h1 className="text-center text-xl font-bold">Ku-CRUD</h1>
        <div className="space-y-1">
          <Label htmlFor="u">Username</Label>
          <Input id="u" value={username} onChange={(e) => setUsername(e.target.value)} />
        </div>
        <div className="space-y-1">
          <Label htmlFor="p">Password</Label>
          <Input id="p" type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </div>
        {error && <p className="text-sm text-destructive">{error}</p>}
        <Button className="w-full" type="submit">Login</Button>
      </form>
    </div>
  );
}
