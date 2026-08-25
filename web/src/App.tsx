import { useEffect, useState } from "react";
import { Spin } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import LoginPage from "./LoginPage";
import ProjectsPage from "./ProjectsPage";

type CurrentUser = components["schemas"]["CurrentUser"];

export default function App() {
  const [user, setUser] = useState<CurrentUser | null>(null);
  const [checking, setChecking] = useState(true);

  useEffect(() => {
    client
      .GET("/auth/me")
      .then(({ data }) => setUser(data ?? null))
      .catch(() => setUser(null))
      .finally(() => setChecking(false));
  }, []);

  if (checking) {
    return <Spin fullscreen />;
  }
  if (!user) {
    return <LoginPage onLogin={setUser} />;
  }
  return <ProjectsPage user={user} onLogout={() => setUser(null)} />;
}
