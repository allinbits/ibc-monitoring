import { NavLink, Route, Routes } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "./api";
import { OverviewPage } from "./pages/Overview";
import { ClientsPage } from "./pages/Clients";
import { RoutesPage } from "./pages/Routes";
import { GovernancePage } from "./pages/Governance";

export function App() {
  const overview = useQuery({ queryKey: ["overview"], queryFn: api.overview });
  const info = overview.data?.info;
  return (
    <div className="app">
      {info?.public && (
        <div className="banner" role="status">
          Public instance. Not authoritative: run your own against your own nodes before acting.
        </div>
      )}
      <header>
        <h1>IBC monitoring</h1>
        <nav>
          <NavLink to="/">Overview</NavLink>
          <NavLink to="/clients">Clients</NavLink>
          <NavLink to="/routes">Routes</NavLink>
          <NavLink to="/governance">Governance</NavLink>
        </nav>
        <span className="muted small">
          {info
            ? `ibcmon ${info.version} · auto-flag ${info.autoflag_effective}${
                info.autoflag_effective !== info.autoflag_requested
                  ? ` (requested ${info.autoflag_requested}; the sink is not built, alarms only)`
                  : ""
              }`
            : ""}
        </span>
      </header>
      <main>
        <Routes>
          <Route path="/" element={<OverviewPage />} />
          <Route path="/clients" element={<ClientsPage />} />
          <Route path="/routes" element={<RoutesPage />} />
          <Route path="/governance" element={<GovernancePage />} />
        </Routes>
      </main>
      <footer className="muted small">
        Every number shows where it was read (chain, height, node, verification). The console never
        decides for you: it shows the state and the rule (docs/design.md §10.4).
      </footer>
    </div>
  );
}
