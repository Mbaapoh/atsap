import { NavLink } from "react-router";
import { primaryNav } from "./nav";

// NavLinks is the navigation list, shared by the desktop rail and the
// small-screen drawer so the two can never disagree.
export function NavLinks({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <>
      <ul className="nav-list">
        {primaryNav.map((item) => (
          <li key={item.path}>
            <NavLink to={item.path} end={item.path === "/"} onClick={onNavigate}>
              {item.label}
            </NavLink>
          </li>
        ))}
      </ul>
      <ul className="nav-list utility">
        <li>
          <NavLink to="/account" onClick={onNavigate}>
            Account
          </NavLink>
        </li>
      </ul>
    </>
  );
}
