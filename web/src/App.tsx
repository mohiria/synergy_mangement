import { useEffect, useState } from "react";
import { Navigate, Route, Routes } from "react-router-dom";
import { Spin } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import LoginPage from "./LoginPage";
import ProjectsPage from "./ProjectsPage";
import ProjectOkrPage from "./ProjectOkrPage";
import ProjectTasksPage from "./ProjectTasksPage";
import MyWorkPage from "./MyWorkPage";
import ProjectOverviewPage from "./ProjectOverviewPage";
import ProjectSettingsPage from "./ProjectSettingsPage";
import CollaborationPage from "./CollaborationPage";
import ArtifactsPage from "./ArtifactsPage";
import ReportsPage from "./ReportsPage";
import SystemSettingsPage from "./SystemSettingsPage";

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
      <Route path="/projects/:projectId" element={<ProjectOverviewPage user={user} onLogout={logout} />} />
      <Route path="/projects/:projectId/okr" element={<ProjectOkrPage user={user} onLogout={logout} />} />
      <Route path="/projects/:projectId/tasks" element={<ProjectTasksPage user={user} onLogout={logout} />} />
      <Route path="/projects/:projectId/graph" element={<CollaborationPage user={user} onLogout={logout} />} />
      <Route path="/projects/:projectId/artifacts" element={<ArtifactsPage user={user} onLogout={logout} />} />
      <Route path="/projects/:projectId/reports" element={<ReportsPage user={user} onLogout={logout} />} />
      <Route path="/projects/:projectId/my-work" element={<MyWorkPage user={user} onLogout={logout} />} />
      <Route path="/projects/:projectId/settings" element={<ProjectSettingsPage user={user} onLogout={logout} />} />
      {/* #201：系统设置不挂在项目下；/system 无分节时跳到用户管理。 */}
      <Route path="/system" element={<SystemSettingsPage user={user} onLogout={logout} />} />
      <Route path="/system/:section" element={<SystemSettingsPage user={user} onLogout={logout} />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
