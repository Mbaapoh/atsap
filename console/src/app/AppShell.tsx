import { useEffect, useRef, useState, type ReactNode } from "react";
import { Outlet, useLocation } from "react-router";
import { CloseIcon } from "../components/icons";
import { Modal } from "../components/Modal";
import { NavLinks } from "./NavLinks";
import { TopBar } from "./TopBar";

interface AppShellProps {
  tenant?: ReactNode;
  licence?: ReactNode;
  // notice is a shell-wide message shown above every page, such as the
  // session-expiry warning.
  notice?: ReactNode;
}

// AppShell is the signed-in frame (CONSOLE-DESIGN "App shell", HLD-19 §7):
// a 240 px navigation rail on wide screens, a menu button and modal drawer
// below 768 px, a top bar, and the page. After each navigation it moves
// focus to the page heading and announces the new page, so keyboard and
// screen-reader users land where sighted users look.
export function AppShell({ tenant, licence, notice }: AppShellProps) {
  const location = useLocation();
  const mainRef = useRef<HTMLElement>(null);
  const menuButton = useRef<HTMLButtonElement>(null);
  const [announcement, setAnnouncement] = useState("");
  // The drawer is open on the page it was opened on: navigating anywhere
  // closes it, by the path changing rather than by an effect.
  const [drawerOpenAt, setDrawerOpenAt] = useState<string | null>(null);
  const drawerOpen = drawerOpenAt === location.pathname;
  const setDrawerOpen = (isOpen: boolean) => setDrawerOpenAt(isOpen ? location.pathname : null);
  // lastPath is the page focus was last settled for. Comparing paths,
  // rather than counting renders, keeps a repeated effect (React runs
  // effects twice in development) from treating the first page as a
  // navigation and pulling focus to its heading.
  const lastPath = useRef(location.pathname);

  useEffect(() => {
    if (lastPath.current === location.pathname) {
      return undefined;
    }
    lastPath.current = location.pathname;
    // After the drawer closes, its focus restoration runs first; the
    // heading then takes focus, which is where the user wants to be.
    const id = window.setTimeout(() => {
      const heading = mainRef.current?.querySelector<HTMLElement>("h1");
      if (heading) {
        heading.focus();
        setAnnouncement(`${heading.textContent ?? ""} page`);
      }
    }, 0);
    return () => window.clearTimeout(id);
  }, [location.pathname]);

  return (
    <div className="shell">
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <nav className="side-nav" aria-label="Main">
        <div className="product">AtsaPBX</div>
        <NavLinks />
      </nav>
      <div className="shell-main">
        <TopBar tenant={tenant} licence={licence} menuOpen={drawerOpen} onMenu={() => setDrawerOpen(true)} menuButtonRef={menuButton} />
        <main id="main" ref={mainRef} className="content" tabIndex={-1}>
          {notice}
          <Outlet />
        </main>
      </div>
      <Modal open={drawerOpen} onClose={() => setDrawerOpen(false)} labelledBy="nav-drawer-title" className="nav-drawer">
        <div id="nav-drawer" className="nav-drawer-inner">
          <div className="nav-drawer-head">
            <span id="nav-drawer-title" className="product">
              AtsaPBX
            </span>
            <button type="button" className="icon-button" aria-label="Close navigation" onClick={() => setDrawerOpen(false)}>
              <CloseIcon />
            </button>
          </div>
          <nav aria-label="Main">
            <NavLinks onNavigate={() => setDrawerOpen(false)} />
          </nav>
        </div>
      </Modal>
      <div className="visually-hidden" role="status" aria-live="polite">
        {announcement}
      </div>
    </div>
  );
}
