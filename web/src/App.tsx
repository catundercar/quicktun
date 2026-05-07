import { Routes, Route, Navigate } from 'react-router-dom';
import { LoginPage } from './auth/LoginPage';
import { ProtectedRoute } from './auth/ProtectedRoute';
import { AppShell } from './layout/AppShell';
import { DashboardPage } from './pages/DashboardPage';

// Empty stubs for the other pages — Task 2 will fill them.
function ProjectsStub() {
  return <div>项目（待实现）</div>;
}
function SitesStub() {
  return <div>站点（待实现）</div>;
}
function ServicesStub() {
  return <div>服务（待实现）</div>;
}
function OperatorsStub() {
  return <div>操作员（待实现）</div>;
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        element={
          <ProtectedRoute>
            <AppShell />
          </ProtectedRoute>
        }
      >
        <Route index element={<Navigate to="/dashboard" replace />} />
        <Route path="/dashboard" element={<DashboardPage />} />
        <Route path="/projects" element={<ProjectsStub />} />
        <Route path="/sites" element={<SitesStub />} />
        <Route path="/services" element={<ServicesStub />} />
        <Route path="/operators" element={<OperatorsStub />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
