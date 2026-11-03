import { useEffect, useState } from "react";
import { BrowserRouter, Navigate, Route, Routes } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Toaster } from "sonner";
import { session } from "@/api/client";
import { TooltipProvider } from "@/components/ui/misc";
import { Layout } from "./Layout";
import { Login } from "@/features/auth/Login";
import { Dashboard } from "@/features/dashboard/Dashboard";
import { StoresPage } from "@/features/stores/StoresPage";
import { StorePage } from "@/features/stores/StorePage";
import { LayersPage } from "@/features/layers/LayersPage";
import { PublishWizard } from "@/features/layers/PublishWizard";
import { LayerPage } from "@/features/layers/LayerPage";
import { MapViewer } from "@/features/map/MapViewer";
import { SystemPage } from "@/features/system/SystemPage";
import { StylesPage } from "@/features/styles/StylesPage";
import { StyleEditor } from "@/features/styles/StyleEditor";
import { IconsPage } from "@/features/icons/IconsPage";
import { WorkspacesPage } from "@/features/workspaces/WorkspacesPage";
import { useTheme } from "./theme";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 15_000, refetchOnWindowFocus: false, retry: 1 },
  },
});

export function App() {
  const [authed, setAuthed] = useState(() => !!session.get());
  const { theme } = useTheme();

  useEffect(() => {
    // Signed out (expired, idle, revoked): drop cached data from memory too.
    const onUnauthorized = () => {
      queryClient.clear();
      setAuthed(false);
    };
    window.addEventListener("msapi:unauthorized", onUnauthorized);
    return () => window.removeEventListener("msapi:unauthorized", onUnauthorized);
  }, []);

  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <Toaster theme={theme} position="bottom-right" richColors closeButton />
        {!authed ? (
          <Login onLogin={() => setAuthed(true)} />
        ) : (
          <BrowserRouter>
            <Routes>
              {/* Full-screen viewer, no admin chrome. */}
              <Route path="/map" element={<MapViewer />} />
              <Route
                element={
                  <Layout
                    onLogout={() => {
                      queryClient.clear();
                      setAuthed(false);
                    }}
                  />
                }
              >
                <Route index element={<Dashboard />} />
                <Route path="workspaces" element={<WorkspacesPage />} />
                <Route path="stores" element={<StoresPage />} />
                <Route path="stores/:name" element={<StorePage />} />
                <Route path="layers" element={<LayersPage />} />
                <Route path="layers/new" element={<PublishWizard />} />
                <Route path="layers/:name" element={<LayerPage />} />
                <Route path="styles" element={<StylesPage />} />
                <Route path="styles/new" element={<StyleEditor key="new" />} />
                <Route path="styles/:name" element={<StyleEditor />} />
                <Route path="icons" element={<IconsPage />} />
                <Route path="system" element={<SystemPage />} />
                <Route path="*" element={<Navigate to="/" replace />} />
              </Route>
            </Routes>
          </BrowserRouter>
        )}
      </TooltipProvider>
    </QueryClientProvider>
  );
}
