import { Link } from "react-router";
import { PageHeader } from "../components/PageHeader";

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
