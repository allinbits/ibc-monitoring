import { useEffect, useState } from "react";
import { NavLink, Route, Routes } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Loading } from "./components";
import { network } from "./network";
import { api } from "./api";
import { OverviewPage } from "./pages/Overview";
import { ClientsPage } from "./pages/Clients";
import { RoutesPage } from "./pages/Routes";
import { GovernancePage } from "./pages/Governance";

function initialTheme() {
  try {
    const stored = localStorage.getItem("bridge-theme");
    if (stored === "light" || stored === "dark") return stored;
  } catch {
    /* The system preference still works when storage is disabled. */
  }
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

export function App() {
  const queryClient = useQueryClient();
  const configuration = useQuery({
    queryKey: ["info"],
    queryFn: async () => {
      const info = await api.info();
      queryClient.setDefaultOptions({
        queries: {
          ...queryClient.getDefaultOptions().queries,
          refetchInterval: info.network.poll_interval_ms,
        },
      });
      return info;
    },
    staleTime: Infinity,
    refetchInterval: false,
  });
  const overview = useQuery({ queryKey: ["overview"], queryFn: api.overview });
  const info = overview.data?.info;
  const [theme, setTheme] = useState(initialTheme);
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    try {
      localStorage.setItem("bridge-theme", theme);
    } catch {
      /* Theme remains usable in memory. */
    }
  }, [theme]);
  if (!configuration.data)
    return <Loading error={configuration.error} retry={() => void configuration.refetch()} />;
  return (
    <div className="app wrap">
      <a className="skip-link" href="#main">
        Skip to console
      </a>
      <header className="site-header">
        <NavLink to="/" className="brand" aria-label="gno.land monitoring home">
          <span className="brand-mark" aria-hidden="true">
            g
          </span>
          <span>
            gno.land <span className="brand-light">/ monitor</span>
          </span>
        </NavLink>
        <div className="header-actions">
          <span className="instance-tag">IBC CONSOLE</span>
          <button
            className="icon-button"
            aria-label={`Use ${theme === "light" ? "dark" : "light"} theme`}
            onClick={() => setTheme(theme === "light" ? "dark" : "light")}
          >
            {theme === "light" ? "◐" : "◑"}
          </button>
        </div>
      </header>
      <nav className="console-nav" aria-label="Console pages">
        <NavLink to="/" end>
          Overview
        </NavLink>
        <NavLink to="/routes">Routes</NavLink>
        <NavLink to="/clients">Clients</NavLink>
        <NavLink to="/governance">Governance</NavLink>
      </nav>
      {info?.public && (
        <div className="banner" role="status">
          Public instance. Run your own monitor against your own nodes before acting.
        </div>
      )}
      <main id="main" tabIndex={-1}>
        <Routes>
          <Route path="/" element={<OverviewPage />} />
          <Route path="/clients" element={<ClientsPage />} />
          <Route path="/routes" element={<RoutesPage />} />
          <Route path="/governance" element={<GovernancePage />} />
          <Route
            path="*"
            element={
              <div className="empty-state">
                Page not found. <NavLink to="/">Return to Overview</NavLink>.
              </div>
            }
          />
        </Routes>
      </main>
      <footer className="site-footer">
        <p>
          gno.land / IBC monitoring
          <br />
          Snapshots refresh every {network.poll_interval_ms / 1000} seconds. Governance actions use
          your connected wallet and live RPC state.
        </p>
        {info && (
          <p>
            ibcmon {info.version} · auto-flag {info.autoflag_effective}
            {info.autoflag_effective !== info.autoflag_requested &&
              ` (requested ${info.autoflag_requested}; alarms only)`}
          </p>
        )}
      </footer>
    </div>
  );
}
