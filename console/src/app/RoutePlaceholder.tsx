import { Link } from "react-router";
import { PageHeader } from "../components/PageHeader";

// A scaffold stand-in for a screen not yet built: the page heading the
// shell focuses after navigation, and nothing else.
export function RoutePlaceholder({ title }: { title: string }) {
  return (
    <section className="page" aria-labelledby="page-title">
      <PageHeader title={title} />
    </section>
  );
}

export function NotFound() {
  return (
    <section className="page" aria-labelledby="page-title">
      <PageHeader title="Page not found" />
      <p>
        This address is not part of the console. <Link to="/">Go to the overview</Link>.
      </p>
    </section>
  );
}
