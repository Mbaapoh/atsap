import { useEffect, useRef, useState } from "react";
import { NavLink, Outlet, useLocation } from "react-router";
import { primaryNav } from "./nav";

// The signed-in frame (CONSOLE-DESIGN "App shell"). After each navigation
// it moves focus to the page heading and announces the new page, so
// keyboard and screen-reader users land where sighted users look.
export function AppShell() {
  const location = useLocation();
  const mainRef = useRef<HTMLElement>(null);
  const [announcement, setAnnouncement] = useState("");
  const firstRender = useRef(true);

  useEffect(() => {
    const heading = mainRef.current?.querySelector<HTMLElement>("h1");
    if (firstRender.current) {
      firstRender.current = false;
      return;
    }
    if (heading) {
      heading.focus();
      setAnnouncement(`${heading.textContent ?? ""} page`);
    }
  }, [location.pathname]);

  return (
    <div className="shell">
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <nav className="side-nav" aria-label="Main">
        <div className="product">AtsaPBX</div>
        <ul>
          {primaryNav.map((item) => (
            <li key={item.path}>
              <NavLink to={item.path} end={item.path === "/"}>
                {item.label}
              </NavLink>
            </li>
          ))}
        </ul>
        <ul className="utility">
          <li>
            <NavLink to="/account">Account</NavLink>
          </li>
        </ul>
      </nav>
      <main id="main" ref={mainRef} className="content">
        <Outlet />
      </main>
      <div className="visually-hidden" role="status" aria-live="polite">
        {announcement}
      </div>
    </div>
  );
}
