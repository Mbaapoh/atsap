import { Link } from "react-router";

// A scaffold stand-in for a screen not yet built. It renders the page
// heading the shell focuses after navigation, and nothing else.
export function RoutePlaceholder({ title }: { title: string }) {
  return (
    <section className="page" aria-labelledby="page-title">
      <h1 id="page-title" tabIndex={-1}>
        {title}
      </h1>
    </section>
  );
}

export function NotFound() {
  return (
    <section className="page" aria-labelledby="page-title">
      <h1 id="page-title" tabIndex={-1}>
        Page not found
      </h1>
      <p>
        This address is not part of the console. <Link to="/">Go to the overview</Link>.
      </p>
    </section>
  );
}
