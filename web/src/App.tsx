import { useEffect, useState } from "react";
import { Navigate, Route, Routes } from "react-router-dom";
import { Spin } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import LoginPage from "./LoginPage";
import ProjectsPage from "./ProjectsPage";
import ProjectOkrPage from "./ProjectOkrPage";
import ProjectTasksPage from "./ProjectTasksPage";

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
  const logout = () => setUser(null);
  return (
    <Routes>
      <Route path="/" element={<ProjectsPage user={user} onLogout={logout} />} />
      <Route path="/projects/:projectId" element={<ProjectOkrPage user={user} onLogout={logout} />} />
      <Route path="/projects/:projectId/tasks" element={<ProjectTasksPage user={user} onLogout={logout} />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
