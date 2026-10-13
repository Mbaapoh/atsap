import { PageHeader } from "../../components/PageHeader";
import { TrunksSection } from "./TrunksSection";

// TrunksRoutesPage is HLD-18 flow A4: the carriers, how outbound calls are
// routed to them, which numbers ring which extension, and the emergency
// numbers that always get through (CONSOLE-DESIGN "Trunks and outbound
// routes").
export function TrunksRoutesPage() {
  return (
    <section className="page" aria-labelledby="page-title">
      <PageHeader title="Trunks & routes" description="Your carriers, how calls reach them, and the numbers that reach you." />
      <TrunksSection />
    </section>
  );
}
