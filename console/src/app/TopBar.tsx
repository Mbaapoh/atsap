import type { ReactNode, Ref } from "react";
import { MenuIcon } from "../components/icons";

interface TopBarProps {
  // tenant and licence are filled in by the session and licence features
  // (tasks 2.1 and 2.3); the bar keeps their places and order.
  tenant?: ReactNode;
  licence?: ReactNode;
  // callAction is the global New call action (D-79); it is rendered only
  // when the person may place calls and no call is in progress.
  callAction?: ReactNode;
  // phoneAction is the browser-phone indicator (D-81).
  phoneAction?: ReactNode;
  menuOpen: boolean;
  onMenu: () => void;
  menuButtonRef?: Ref<HTMLButtonElement>;
}

// TopBar shows, in order: the current Tenant, the licence pill, and the
// account link (CONSOLE-DESIGN "App shell"). Below 768 px it also carries
// the menu button that opens the navigation drawer.
export function TopBar({ tenant, licence, callAction, phoneAction, menuOpen, onMenu, menuButtonRef }: TopBarProps) {
  return (
    <header className="top-bar">
      <button
        ref={menuButtonRef}
        type="button"
        className="icon-button menu-button"
        aria-label="Open navigation"
        aria-expanded={menuOpen}
        aria-controls="nav-drawer"
        onClick={onMenu}
      >
        <MenuIcon />
      </button>
      <span className="product product-compact">AtsaPBX</span>
      <div className="top-bar-items">
        {phoneAction ? <div className="top-bar-phone">{phoneAction}</div> : null}
        {callAction ? <div className="top-bar-call">{callAction}</div> : null}
        {tenant ? <div className="top-bar-tenant">{tenant}</div> : null}
        {licence ? <div className="top-bar-licence">{licence}</div> : null}
      </div>
    </header>
  );
}
